//! Transport abstraction (planner task R-07). TCP (`tcp`), TLS-over-TCP
//! (`crypto::tls`), and QUIC (`quic`, planner task R-09) all implement
//! `Connection`.

pub mod backoff;
pub mod framing;
pub mod keepalive;
pub mod quic;
pub mod tcp;

use crate::error::Result;

/// A bidirectional, message-framed connection. Implementations own framing
/// (TCP/TLS: length-prefixed via `framing`; QUIC: the same length-prefixed
/// framing, layered on top of its native stream framing).
pub trait Connection: Send {
    fn send(&mut self, data: &[u8]) -> impl std::future::Future<Output = Result<()>> + Send;
    fn recv(&mut self) -> impl std::future::Future<Output = Result<Vec<u8>>> + Send;
}
