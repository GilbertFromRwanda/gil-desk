//! CLI host (planner task R-25) — the vertical slice's server side.
//! Two ways to be reached, chosen by CLI flags:
//!   --bind <addr>                           direct TLS-over-TCP: listens,
//!                                           accepts any number of
//!                                           connections concurrently.
//!   --relay <addr> --session-token <token>  through the Go relay (Gate
//!                                           G3's "if direct fails ->
//!                                           relay"): dials out (there's
//!                                           no listening concept over a
//!                                           relay — both sides connect
//!                                           to it), handles exactly one
//!                                           session (a token is single-
//!                                           session by design), then
//!                                           exits.
//!
//! The dev TLS certificate this generates has no CA behind it, so the
//! direct-mode client must be told to trust this *exact* cert (see
//! --cert-out below and client.rs --cert) — see crypto::tls module docs
//! for why that's a deliberate pin, not a shortcut, and what production
//! needs instead. Relay mode doesn't use this certificate at all — trust
//! there comes entirely from the session token (see rendezvous::relay).

#[path = "common/mod.rs"]
mod common;

use common::{arg_value, run_handshake_and_heartbeat, AnyConnection};
use nexdesk_core::crypto::tls;
use nexdesk_core::error::Result;
use nexdesk_core::rendezvous::relay;
use nexdesk_core::session::{Session, SessionEvent};
use nexdesk_core::shutdown::Shutdown;
use nexdesk_core::transport::tcp::TcpListener;
use std::io::Write;
use tokio::net::TcpStream;
use tokio_rustls::TlsAcceptor;

#[tokio::main]
async fn main() {
    let args: Vec<String> = std::env::args().collect();
    let device_id = arg_value(&args, "--device-id").unwrap_or_else(|| "nexdesk-host".to_string());

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
        session.handle(SessionEvent::HandshakeStart).ok();
        let result = async {
            let conn = relay::connect(&relay_addr, &session_token).await?;
            run_handshake_and_heartbeat(AnyConnection::Relay(Box::new(conn)), &device_id, &mut session, &shutdown).await
        }
        .await;
        if let Err(e) = result {
            eprintln!("session error: {e}");
        }
        let _ = std::io::stdout().flush();
        println!("SHUTDOWN");
        return;
    }

    let bind_addr = arg_value(&args, "--bind").unwrap_or_else(|| "127.0.0.1:7000".to_string());
    let cert_path =
        arg_value(&args, "--cert-out").unwrap_or_else(|| "nexdesk_host_cert.der".to_string());
    let key_path = format!("{cert_path}.key");

    // Reuse a saved identity across restarts if one exists — a real
    // device's identity should be stable, not regenerated every launch
    // (which would make every client's pinned cert stale on restart).
    let dev_cert = match (std::fs::read(&cert_path), std::fs::read(&key_path)) {
        (Ok(cert_der), Ok(key_der)) => tls::DevCertificate::from_der(cert_der, key_der),
        _ => {
            let dev_cert =
                tls::generate_dev_certificate("localhost").expect("cert generation failed");
            std::fs::write(&cert_path, dev_cert.cert_der.as_ref())
                .expect("failed to write cert file");
            std::fs::write(&key_path, dev_cert.key_der_bytes())
                .expect("failed to write key file");
            dev_cert
        }
    };

    let acceptor = TlsAcceptor::from(tls::server_config(&dev_cert).expect("TLS config failed"));
    let listener = TcpListener::bind(&bind_addr).await.expect("bind failed");
    let local_addr = listener.local_addr().expect("local_addr failed");

    println!("LISTENING {local_addr}");
    println!("CERT {cert_path}");
    let _ = std::io::stdout().flush();

    loop {
        tokio::select! {
            _ = shutdown.triggered() => {
                println!("SHUTDOWN");
                break;
            }
            accepted = listener.accept_raw() => {
                let tcp = match accepted {
                    Ok(tcp) => tcp,
                    Err(e) => { eprintln!("accept error: {e}"); continue; }
                };
                let acceptor = acceptor.clone();
                let device_id = device_id.clone();
                let shutdown = shutdown.clone();
                tokio::spawn(async move {
                    if let Err(e) = handle_direct_connection(acceptor, tcp, device_id, shutdown).await {
                        eprintln!("connection error: {e}");
                    }
                });
            }
        }
    }
}

async fn handle_direct_connection(
    acceptor: TlsAcceptor,
    tcp: TcpStream,
    device_id: String,
    shutdown: Shutdown,
) -> Result<()> {
    let mut session = Session::new();
    session.handle(SessionEvent::Connect).ok();
    session.handle(SessionEvent::HandshakeStart).ok();

    let conn = tls::accept(&acceptor, tcp).await?;
    run_handshake_and_heartbeat(AnyConnection::Tls(Box::new(conn)), &device_id, &mut session, &shutdown).await
}
