//! TLS 1.3 session security (planner tasks R-13..R-18).
//!
//! Key exchange (R-15) is TLS 1.3's own ECDHE, performed entirely inside
//! `rustls` — nothing here implements cryptographic primitives. The
//! planner's R-14 ("Noise/session handshake design") is realized as TLS
//! 1.3 (see the architecture decision in Section 2), with the *application*
//! handshake (`SessionHello` exchange) riding on top of it — see
//! `session::tests` and the Gate G1 integration test for that flow.

pub mod credential_store;
pub mod replay;
pub mod tls;
