//! Keepalive/heartbeat ticking (planner task R-11). A thin wrapper around
//! `tokio::time::Interval` so callers can `tokio::select!` on it alongside
//! `Shutdown::triggered()` without depending on `tokio::time` directly.

use std::time::Duration;
use tokio::time::{interval, Interval};

pub struct Heartbeat {
    interval: Interval,
}

impl Heartbeat {
    pub fn new(period: Duration) -> Self {
        Self {
            interval: interval(period),
        }
    }

    /// Resolves once per `period`. The first call resolves immediately —
    /// callers that want an initial delay should await it once before
    /// starting their loop.
    pub async fn tick(&mut self) {
        self.interval.tick().await;
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tokio::time::Instant;

    #[tokio::test(start_paused = true)]
    async fn ticks_at_the_configured_period() {
        let mut hb = Heartbeat::new(Duration::from_millis(50));
        hb.tick().await; // first tick resolves immediately
        let before = Instant::now();
        hb.tick().await;
        assert_eq!(before.elapsed(), Duration::from_millis(50));
    }
}
