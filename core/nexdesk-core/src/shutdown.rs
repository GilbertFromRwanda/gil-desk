//! Cancellation/shutdown model (planner task R-05). A `Shutdown` is cloned
//! into every task that needs to observe a coordinated shutdown signal;
//! calling `trigger()` on any clone (or the original) wakes all of them.

use tokio_util::sync::CancellationToken;

#[derive(Debug, Clone, Default)]
pub struct Shutdown {
    token: CancellationToken,
}

impl Shutdown {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn trigger(&self) {
        self.token.cancel();
    }

    pub fn is_triggered(&self) -> bool {
        self.token.is_cancelled()
    }

    /// Resolves once `trigger()` has been called on this handle or any
    /// clone of it. Intended for `tokio::select!` alongside real work.
    pub async fn triggered(&self) {
        self.token.cancelled().await;
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::Duration;

    #[tokio::test]
    async fn trigger_wakes_all_clones() {
        let shutdown = Shutdown::new();
        let worker = shutdown.clone();

        let handle = tokio::spawn(async move {
            worker.triggered().await;
            "shut down cleanly"
        });

        // Give the task a moment to start waiting, then trigger shutdown.
        tokio::time::sleep(Duration::from_millis(10)).await;
        assert!(!handle.is_finished());
        shutdown.trigger();

        let result = tokio::time::timeout(Duration::from_secs(1), handle)
            .await
            .expect("task did not observe shutdown in time")
            .unwrap();
        assert_eq!(result, "shut down cleanly");
    }

    #[tokio::test]
    async fn is_triggered_reflects_state() {
        let shutdown = Shutdown::new();
        assert!(!shutdown.is_triggered());
        shutdown.trigger();
        assert!(shutdown.is_triggered());
    }
}
