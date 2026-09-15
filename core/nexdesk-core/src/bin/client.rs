//! CLI client (planner task R-24) — the vertical slice's client side.
//! Connects to a host over TCP+TLS, completes the SessionHello handshake,
//! exchanges heartbeats, and — if the connection drops — reconnects with
//! exponential backoff until told to stop (Ctrl+C), rather than exiting.

use nexdesk_core::crypto::tls;
use nexdesk_core::error::{NexError, Result};
use nexdesk_core::protocol::{self, SessionHello};
use nexdesk_core::session::{Session, SessionEvent};
use nexdesk_core::shutdown::Shutdown;
use nexdesk_core::transport::backoff::Backoff;
use nexdesk_core::transport::keepalive::Heartbeat;
use nexdesk_core::transport::Connection;
use rustls::pki_types::{CertificateDer, ServerName};
use std::io::Write;
use std::time::Duration;
use tokio::net::TcpStream;
use tokio_rustls::TlsConnector;

#[tokio::main]
async fn main() {
    let args: Vec<String> = std::env::args().collect();
    let server_addr = arg_value(&args, "--server").expect("--server <addr> is required");
    let cert_path = arg_value(&args, "--cert").expect("--cert <path> is required");
    let device_id =
        arg_value(&args, "--device-id").unwrap_or_else(|| "nexdesk-client".to_string());

    let cert_bytes = std::fs::read(&cert_path).expect("failed to read cert file");
    let expected_cert = CertificateDer::from(cert_bytes);

    let shutdown = Shutdown::new();
    {
        let shutdown = shutdown.clone();
        tokio::spawn(async move {
            let _ = tokio::signal::ctrl_c().await;
            shutdown.trigger();
        });
    }

    let mut backoff = Backoff::new(Duration::from_millis(200), Duration::from_secs(5));
    let mut session = Session::new();

    while !shutdown.is_triggered() {
        session.handle(SessionEvent::Connect).ok();
        match run_session(
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

async fn run_session(
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
    let mut conn = tls::connect(&connector, server_name, tcp).await?;

    let hello = SessionHello {
        protocol_version: 1,
        device_id: device_id.to_string(),
    };
    conn.send(&protocol::codec::encode(&hello)).await?;
    let their_hello_bytes = conn.recv().await?;
    let their_hello: SessionHello = protocol::codec::decode(&their_hello_bytes)?;

    session.handle(SessionEvent::HandshakeComplete).ok();
    println!("ESTABLISHED peer={}", their_hello.device_id);
    let _ = std::io::stdout().flush();

    let mut heartbeat = Heartbeat::new(Duration::from_secs(5));
    loop {
        tokio::select! {
            _ = shutdown.triggered() => break,
            _ = heartbeat.tick() => {
                if conn.send(b"PING").await.is_err() {
                    break;
                }
            }
            result = conn.recv() => {
                match result {
                    Ok(bytes) => println!("RECV {}", String::from_utf8_lossy(&bytes)),
                    Err(_) => break, // host disconnected
                }
            }
        }
    }

    session.handle(SessionEvent::Disconnect).ok();
    session.handle(SessionEvent::Closed).ok();
    println!("CLOSED");
    Ok(())
}

fn arg_value(args: &[String], name: &str) -> Option<String> {
    args.iter()
        .position(|a| a == name)
        .and_then(|i| args.get(i + 1))
        .cloned()
}
