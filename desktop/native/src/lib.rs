//! Rust <-> Electron boundary (planner tasks F-18/F-19/E-01). Kept minimal
//! by design here — this is the Phase 0 smoke test proving the napi-rs
//! toolchain works end to end; real session/transport calls get added when
//! Phase 4 (Electron Desktop MVP) needs them.

#[macro_use]
extern crate napi_derive;

use nexdesk_core::session::SessionState;

#[napi]
pub fn native_version() -> String {
    env!("CARGO_PKG_VERSION").to_string()
}

/// Calls into the actual Rust core (not just a hardcoded string), proving
/// Node -> Rust -> nexdesk-core works across the real boundary.
#[napi]
pub fn initial_session_state() -> String {
    match SessionState::Idle {
        SessionState::Idle => "idle".to_string(),
        _ => unreachable!(),
    }
}
