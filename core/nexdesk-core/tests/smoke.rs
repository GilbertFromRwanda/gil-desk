//! Workspace smoke test — proves the crate compiles and links as a starting
//! point for Gate G0 (see planner Section 4).

use nexdesk_core::session::SessionState;

#[test]
fn crate_is_linkable() {
    let state = SessionState::Idle;
    assert_eq!(state, SessionState::Idle);
}
