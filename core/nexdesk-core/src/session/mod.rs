//! Session state machine (planner task R-02). Enforces the connection
//! lifecycle from Gate G1: discover -> secure session -> exchange messages
//! -> reconnect after interruption -> shut down cleanly.

use crate::error::{NexError, Result};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Default)]
pub enum SessionState {
    #[default]
    Idle,
    Connecting,
    Handshaking,
    Established,
    Closing,
    Closed,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum SessionEvent {
    Connect,
    HandshakeStart,
    HandshakeComplete,
    Disconnect,
    Closed,
    Fail,
}

/// A session's current state plus the (small, explicit) set of legal
/// transitions out of it. Keeping this as one match arm per state — rather
/// than a lookup table — means an illegal transition is a compile-time-
/// visible gap, not a silently-missing table entry.
#[derive(Debug, Default)]
pub struct Session {
    state: SessionState,
}

impl Session {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn state(&self) -> SessionState {
        self.state
    }

    /// Applies `event`, returning the new state or an error if `event`
    /// isn't legal from the current state. On error, state is unchanged.
    pub fn handle(&mut self, event: SessionEvent) -> Result<SessionState> {
        let next = match (self.state, event) {
            (SessionState::Idle, SessionEvent::Connect) => SessionState::Connecting,

            (SessionState::Connecting, SessionEvent::HandshakeStart) => SessionState::Handshaking,
            (SessionState::Connecting, SessionEvent::Fail) => SessionState::Closed,

            (SessionState::Handshaking, SessionEvent::HandshakeComplete) => {
                SessionState::Established
            }
            (SessionState::Handshaking, SessionEvent::Fail) => SessionState::Closed,

            (SessionState::Established, SessionEvent::Disconnect) => SessionState::Closing,
            (SessionState::Established, SessionEvent::Fail) => SessionState::Closing,

            (SessionState::Closing, SessionEvent::Closed) => SessionState::Closed,

            // Reconnect: a closed session can start a fresh connection
            // attempt without constructing a new Session.
            (SessionState::Closed, SessionEvent::Connect) => SessionState::Connecting,

            (from, event) => {
                return Err(NexError::InvalidSessionTransition { from, event });
            }
        };

        self.state = next;
        Ok(next)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn starts_idle() {
        assert_eq!(Session::new().state(), SessionState::Idle);
    }

    #[test]
    fn full_lifecycle_to_established() {
        let mut session = Session::new();
        assert_eq!(
            session.handle(SessionEvent::Connect).unwrap(),
            SessionState::Connecting
        );
        assert_eq!(
            session.handle(SessionEvent::HandshakeStart).unwrap(),
            SessionState::Handshaking
        );
        assert_eq!(
            session.handle(SessionEvent::HandshakeComplete).unwrap(),
            SessionState::Established
        );
    }

    #[test]
    fn clean_shutdown() {
        let mut session = Session::new();
        session.handle(SessionEvent::Connect).unwrap();
        session.handle(SessionEvent::HandshakeStart).unwrap();
        session.handle(SessionEvent::HandshakeComplete).unwrap();

        assert_eq!(
            session.handle(SessionEvent::Disconnect).unwrap(),
            SessionState::Closing
        );
        assert_eq!(
            session.handle(SessionEvent::Closed).unwrap(),
            SessionState::Closed
        );
    }

    #[test]
    fn reconnect_after_close() {
        let mut session = Session::new();
        session.handle(SessionEvent::Connect).unwrap();
        session.handle(SessionEvent::Fail).unwrap();
        assert_eq!(session.state(), SessionState::Closed);

        assert_eq!(
            session.handle(SessionEvent::Connect).unwrap(),
            SessionState::Connecting
        );
    }

    #[test]
    fn handshake_failure_closes_session() {
        let mut session = Session::new();
        session.handle(SessionEvent::Connect).unwrap();
        session.handle(SessionEvent::HandshakeStart).unwrap();
        assert_eq!(
            session.handle(SessionEvent::Fail).unwrap(),
            SessionState::Closed
        );
    }

    #[test]
    fn illegal_transition_is_rejected_and_state_is_unchanged() {
        let mut session = Session::new();
        let err = session.handle(SessionEvent::HandshakeComplete).unwrap_err();
        assert_eq!(
            err,
            NexError::InvalidSessionTransition {
                from: SessionState::Idle,
                event: SessionEvent::HandshakeComplete,
            }
        );
        // Rejected event must not have mutated state.
        assert_eq!(session.state(), SessionState::Idle);
    }

    #[test]
    fn cannot_connect_while_already_connecting() {
        let mut session = Session::new();
        session.handle(SessionEvent::Connect).unwrap();
        assert!(session.handle(SessionEvent::Connect).is_err());
    }
}
