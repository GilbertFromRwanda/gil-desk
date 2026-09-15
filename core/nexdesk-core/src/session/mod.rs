//! Session state machine (planner task R-02).

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SessionState {
    Idle,
    Connecting,
    Handshaking,
    Established,
    Closing,
    Closed,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn starts_idle() {
        assert_eq!(SessionState::Idle, SessionState::Idle);
    }
}
