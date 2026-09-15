//! Transport abstraction (planner task R-07). TCP (`tcp`) is implemented
//! and tested against a real socket; QUIC (planner task R-09) is
//! deliberately deferred — it needs its own TLS/certificate setup and is
//! large enough (8 points, "split if possible" per the planner's own
//! scale) to land as a separate, focused piece of work rather than being
//! folded into this one.

pub mod backoff;
pub mod framing;
pub mod keepalive;
pub mod tcp;

use crate::error::Result;

/// A bidirectional, message-framed connection. Implementations own framing
/// (TCP: length-prefixed; QUIC, once added: its native stream framing).
pub trait Connection: Send {
    fn send(&mut self, data: &[u8]) -> impl std::future::Future<Output = Result<()>> + Send;
    fn recv(&mut self) -> impl std::future::Future<Output = Result<Vec<u8>>> + Send;
}
