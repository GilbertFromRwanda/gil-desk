//! CLI host (planner task R-25) — the vertical slice's server side.
//! Accepts one or more TCP+TLS connections, completes the SessionHello
//! handshake, and holds each session open with a heartbeat until the
//! peer disconnects or the process is asked to shut down (Ctrl+C).
//!
//! The dev TLS certificate this generates has no CA behind it, so the
//! client must be told to trust this *exact* cert (see --cert-out below
//! and client.rs --cert) — see crypto::tls module docs for why that's a
//! deliberate pin, not a shortcut, and what production needs instead.

use nexdesk_core::crypto::tls;
use nexdesk_core::error::Result;
use nexdesk_core::protocol::{self, SessionHello};
use nexdesk_core::session::{Session, SessionEvent};
use nexdesk_core::shutdown::Shutdown;
use nexdesk_core::transport::keepalive::Heartbeat;
use nexdesk_core::transport::tcp::TcpListener;
use nexdesk_core::transport::Connection;
use std::io::Write;
use std::time::Duration;
use tokio::net::TcpStream;
use tokio_rustls::TlsAcceptor;

#[tokio::main]
async fn main() {
    let args: Vec<String> = std::env::args().collect();
    let bind_addr = arg_value(&args, "--bind").unwrap_or_else(|| "127.0.0.1:7000".to_string());
    let device_id = arg_value(&args, "--device-id").unwrap_or_else(|| "nexdesk-host".to_string());
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

    let shutdown = Shutdown::new();
    {
        let shutdown = shutdown.clone();
        tokio::spawn(async move {
            let _ = tokio::signal::ctrl_c().await;
            shutdown.trigger();
        });
    }

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
                    if let Err(e) = handle_connection(acceptor, tcp, device_id, shutdown).await {
                        eprintln!("connection error: {e}");
                    }
                });
            }
        }
    }
}

async fn handle_connection(
    acceptor: TlsAcceptor,
    tcp: TcpStream,
    device_id: String,
    shutdown: Shutdown,
) -> Result<()> {
    let mut session = Session::new();
    session.handle(SessionEvent::Connect).ok();
    session.handle(SessionEvent::HandshakeStart).ok();

    let mut conn = tls::accept(&acceptor, tcp).await?;

    let their_hello_bytes = conn.recv().await?;
    let their_hello: SessionHello = protocol::codec::decode(&their_hello_bytes)?;
    let my_hello = SessionHello {
        protocol_version: 1,
        device_id: device_id.clone(),
    };
    conn.send(&protocol::codec::encode(&my_hello)).await?;

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
                    Err(_) => break, // peer disconnected
                }
            }
        }
    }

    session.handle(SessionEvent::Disconnect).ok();
    session.handle(SessionEvent::Closed).ok();
    println!("CLOSED peer={}", their_hello.device_id);
    let _ = std::io::stdout().flush();
    Ok(())
}

fn arg_value(args: &[String], name: &str) -> Option<String> {
    args.iter()
        .position(|a| a == name)
        .and_then(|i| args.get(i + 1))
        .cloned()
}
