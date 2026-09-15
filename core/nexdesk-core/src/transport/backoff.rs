//! Reconnect retry/backoff (planner task R-10). Exponential, capped, no
//! jitter yet — add jitter if/when real reconnect storms show it's needed
//! (see the planner's "reconnect storms" failure-engineering scenario).

use std::time::Duration;

#[derive(Debug, Clone)]
pub struct Backoff {
    base: Duration,
    max: Duration,
    attempt: u32,
}

impl Backoff {
    pub fn new(base: Duration, max: Duration) -> Self {
        Self {
            base,
            max,
            attempt: 0,
        }
    }

    /// Returns the delay for the next attempt and advances internal state.
    pub fn next_delay(&mut self) -> Duration {
        // Cap the exponent, not just the result: 2^20 is already far past
        // any realistic `max`, and this avoids a u32 shift overflow.
        let exponent = self.attempt.min(20);
        let multiplier = 1u32.checked_shl(exponent).unwrap_or(u32::MAX);
        let delay = self.base.saturating_mul(multiplier).min(self.max);
        self.attempt = self.attempt.saturating_add(1);
        delay
    }

    /// Call after a successful connection so the next failure starts the
    /// backoff sequence over instead of continuing to escalate.
    pub fn reset(&mut self) {
        self.attempt = 0;
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn escalates_exponentially_then_caps_at_max() {
        let mut backoff = Backoff::new(Duration::from_millis(100), Duration::from_secs(2));
        assert_eq!(backoff.next_delay(), Duration::from_millis(100));
        assert_eq!(backoff.next_delay(), Duration::from_millis(200));
        assert_eq!(backoff.next_delay(), Duration::from_millis(400));
        assert_eq!(backoff.next_delay(), Duration::from_millis(800));
        assert_eq!(backoff.next_delay(), Duration::from_millis(1600));
        // 3.2s would be next uncapped — must clamp to max instead.
        assert_eq!(backoff.next_delay(), Duration::from_secs(2));
        assert_eq!(backoff.next_delay(), Duration::from_secs(2));
    }

    #[test]
    fn reset_restarts_the_sequence() {
        let mut backoff = Backoff::new(Duration::from_millis(100), Duration::from_secs(2));
        backoff.next_delay();
        backoff.next_delay();
        backoff.reset();
        assert_eq!(backoff.next_delay(), Duration::from_millis(100));
    }
}
