//! CLI client (planner task R-24) — the vertical slice's client side.
//! Two ways to reach a host, chosen by CLI flags:
//!   --server <addr> --cert <path>          direct TLS-over-TCP; reconnects
//!                                           with exponential backoff if the
//!                                           connection drops, since a host
//!                                           at a fixed address is expected
//!                                           to come back.
//!   --relay <addr> --session-token <token>  through the Go relay (Gate G3's
//!                                           "if direct fails -> relay"),
//!                                           runs exactly once — a session
//!                                           token is single-session by
//!                                           design (backend/internal/
//!                                           rendezvous/token.go), so
//!                                           there's nothing valid to
//!                                           reconnect with after it ends.
//! Both complete the same SessionHello handshake and exchange heartbeats.

#[path = "common/mod.rs"]
mod common;

use common::{arg_value, run_handshake_and_heartbeat, AnyConnection};
use nexdesk_core::crypto::tls;
use nexdesk_core::error::{NexError, Result};
use nexdesk_core::rendezvous::relay;
use nexdesk_core::session::{Session, SessionEvent};
use nexdesk_core::shutdown::Shutdown;
use nexdesk_core::transport::backoff::Backoff;
use rustls::pki_types::{CertificateDer, ServerName};
use std::io::Write;
use std::time::Duration;
use tokio::net::TcpStream;
use tokio_rustls::TlsConnector;

#[tokio::main]
async fn main() {
    let args: Vec<String> = std::env::args().collect();
    let device_id =
        arg_value(&args, "--device-id").unwrap_or_else(|| "nexdesk-client".to_string());

    let shutdown = Shutdown::new();
    {
        let shutdown = shutdown.clone();
        tokio::spawn(async move {
            let _ = tokio::signal::ctrl_c().await;
            shutdown.trigger();
        });
    }

    if let Some(relay_addr) = arg_value(&args, "--relay") {
        let session_token =
            arg_value(&args, "--session-token").expect("--session-token <token> is required with --relay");

        let mut session = Session::new();
        session.handle(SessionEvent::Connect).ok();
        match run_relay_session(&relay_addr, &session_token, &device_id, &mut session, &shutdown).await {
            Ok(()) => println!("SESSION_ENDED"),
            Err(e) => eprintln!("session error: {e}"),
        }
        let _ = std::io::stdout().flush();
        println!("SHUTDOWN");
        return;
    }

    let server_addr = arg_value(&args, "--server").expect("--server <addr> is required (or use --relay)");
    let cert_path = arg_value(&args, "--cert").expect("--cert <path> is required");
    let cert_bytes = std::fs::read(&cert_path).expect("failed to read cert file");
    let expected_cert = CertificateDer::from(cert_bytes);

    let mut backoff = Backoff::new(Duration::from_millis(200), Duration::from_secs(5));
    let mut session = Session::new();

    while !shutdown.is_triggered() {
        session.handle(SessionEvent::Connect).ok();
        match run_direct_session(
            &server_addr,
            expected_cert.clone(),
            &device_id,
            &mut session,
            &shutdown,
        )
        .await
        {
            Ok(()) => println!("SESSION_ENDED"),
            Err(e) => eprintln!("session error: {e}"),
        }
        let _ = std::io::stdout().flush();

        if shutdown.is_triggered() {
            break;
        }
        let delay = backoff.next_delay();
        println!("RECONNECTING in {delay:?}");
        let _ = std::io::stdout().flush();
        tokio::time::sleep(delay).await;
    }

    println!("SHUTDOWN");
}

async fn run_direct_session(
    server_addr: &str,
    expected_cert: CertificateDer<'static>,
    device_id: &str,
    session: &mut Session,
    shutdown: &Shutdown,
) -> Result<()> {
    session.handle(SessionEvent::HandshakeStart).ok();

    let connector = TlsConnector::from(tls::client_config_pinned_to(expected_cert));
    let tcp = TcpStream::connect(server_addr)
        .await
        .map_err(|e| NexError::Transport(e.to_string()))?;
    let server_name = ServerName::try_from("localhost").expect("static, always valid");
    let conn = tls::connect(&connector, server_name, tcp).await?;

    run_handshake_and_heartbeat(AnyConnection::Tls(Box::new(conn)), device_id, session, shutdown).await
}

async fn run_relay_session(
    relay_addr: &str,
    session_token: &str,
    device_id: &str,
    session: &mut Session,
    shutdown: &Shutdown,
) -> Result<()> {
    session.handle(SessionEvent::HandshakeStart).ok();
    let conn = relay::connect(relay_addr, session_token).await?;
    run_handshake_and_heartbeat(AnyConnection::Relay(Box::new(conn)), device_id, session, shutdown).await
}
