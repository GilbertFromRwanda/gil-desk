//! Replay protection (planner task R-17). A sliding-window nonce check —
//! the same technique IPsec/DTLS anti-replay windows use: track the
//! highest nonce seen plus a small set of recently-accepted nonces, so
//! reordering within the window is tolerated but duplicates and anything
//! older than the window are rejected. This guards the application-level
//! handshake/session messages; it's defense in depth on top of TLS 1.3
//! (whose own record layer already prevents record replay — this catches
//! replay at a level TLS doesn't see, e.g. an attacker re-submitting a
//! captured plaintext `SessionHello` through a *new* connection).

use crate::error::{NexError, Result};
use std::collections::HashSet;

pub struct ReplayGuard {
    window: u64,
    highest: Option<u64>,
    seen: HashSet<u64>,
}

impl ReplayGuard {
    pub fn new(window: u64) -> Self {
        assert!(window > 0, "replay window must be non-zero");
        Self {
            window,
            highest: None,
            seen: HashSet::new(),
        }
    }

    /// Accepts `nonce` if it hasn't been seen and isn't older than the
    /// window, recording it. Rejects (without side effects) otherwise.
    pub fn check_and_record(&mut self, nonce: u64) -> Result<()> {
        let Some(highest) = self.highest else {
            self.highest = Some(nonce);
            self.seen.insert(nonce);
            return Ok(());
        };

        if nonce > highest {
            let new_floor = nonce.saturating_sub(self.window - 1);
            self.seen.retain(|&n| n >= new_floor);
            self.seen.insert(nonce);
            self.highest = Some(nonce);
            return Ok(());
        }

        let floor = highest.saturating_sub(self.window - 1);
        if nonce < floor {
            return Err(NexError::Transport(format!(
                "nonce {nonce} is outside the replay window (floor {floor})"
            )));
        }
        if !self.seen.insert(nonce) {
            return Err(NexError::Transport(format!(
                "nonce {nonce} already seen (replay)"
            )));
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn accepts_strictly_increasing_nonces() {
        let mut guard = ReplayGuard::new(64);
        for n in 0..10 {
            guard.check_and_record(n).unwrap();
        }
    }

    #[test]
    fn rejects_an_exact_duplicate() {
        let mut guard = ReplayGuard::new(64);
        guard.check_and_record(5).unwrap();
        assert!(guard.check_and_record(5).is_err());
    }

    #[test]
    fn tolerates_reordering_within_the_window() {
        let mut guard = ReplayGuard::new(64);
        guard.check_and_record(5).unwrap();
        guard.check_and_record(3).unwrap();
        guard.check_and_record(4).unwrap();
        // 3 and 4 already consumed — replaying either must fail now.
        assert!(guard.check_and_record(3).is_err());
        assert!(guard.check_and_record(4).is_err());
    }

    #[test]
    fn rejects_a_nonce_older_than_the_window() {
        let mut guard = ReplayGuard::new(4);
        guard.check_and_record(100).unwrap();
        // Window is [97, 100]; 50 is long gone.
        assert!(guard.check_and_record(50).is_err());
    }

    #[test]
    fn first_nonce_is_always_accepted_even_if_nonzero() {
        let mut guard = ReplayGuard::new(64);
        guard.check_and_record(1_000_000).unwrap();
    }
}
