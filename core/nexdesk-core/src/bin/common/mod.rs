//! Shared between the host/client CLI binaries only (planner tasks
//! R-24/R-25's vertical slice, now extended to optionally connect
//! through the relay instead of directly). Not part of the library's
//! public API — "pick direct-TLS vs relay from a CLI flag" is specific
//! to these two binaries, not something a real caller needs today (a
//! real "try direct, fall back to relay" policy per Gate G3's diagram
//! is separate, larger future work: this is an explicit either/or
//! choice, not an automatic fallback).
//!
//! Included via `#[path = "common/mod.rs"] mod common;` in each binary
//! rather than living in the library, since `src/bin/*.rs` files are
//! each their own crate root and can't otherwise share code directly.

use nexdesk_core::crypto::tls::TlsConnection;
use nexdesk_core::error::Result;
use nexdesk_core::protocol::{self, SessionHello};
use nexdesk_core::rendezvous::relay::RelayConnection;
use nexdesk_core::session::{Session, SessionEvent};
use nexdesk_core::shutdown::Shutdown;
use nexdesk_core::transport::keepalive::Heartbeat;
use nexdesk_core::transport::Connection;
use std::io::Write;
use std::time::Duration;
use tokio::net::TcpStream;

/// Either a direct TLS-over-TCP connection or one relayed through the Go
/// backend — the `Connection` trait's async methods can't be boxed as
/// `dyn Connection` (they use return-position `impl Trait`, which isn't
/// object-safe), so this enum gives static dispatch between the two
/// instead.
pub enum AnyConnection {
    Tls(Box<TlsConnection<TcpStream>>),
    Relay(Box<RelayConnection>),
}

impl Connection for AnyConnection {
    async fn send(&mut self, data: &[u8]) -> Result<()> {
        match self {
            AnyConnection::Tls(c) => c.send(data).await,
            AnyConnection::Relay(c) => c.send(data).await,
        }
    }

    async fn recv(&mut self) -> Result<Vec<u8>> {
        match self {
            AnyConnection::Tls(c) => c.recv().await,
            AnyConnection::Relay(c) => c.recv().await,
        }
    }
}

pub fn arg_value(args: &[String], name: &str) -> Option<String> {
    args.iter()
        .position(|a| a == name)
        .and_then(|i| args.get(i + 1))
        .cloned()
}

/// Sends `my_hello` first, then waits for the peer's — safe for either
/// side of a relay pairing (a full-duplex channel, unlike TCP accept()'s
/// implicit "client connects and sends, host was already waiting"
/// ordering) to call without risking both sides blocking on `recv()`
/// first. Returns once the peer disconnects or `shutdown` fires.
pub async fn run_handshake_and_heartbeat(
    mut conn: AnyConnection,
    my_device_id: &str,
    session: &mut Session,
    shutdown: &Shutdown,
) -> Result<()> {
    let hello = SessionHello {
        protocol_version: 1,
        device_id: my_device_id.to_string(),
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
