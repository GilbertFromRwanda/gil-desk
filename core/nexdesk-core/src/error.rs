//! Crate-wide error model (planner task R-04).

#[derive(Debug, thiserror::Error, PartialEq, Eq, Clone)]
pub enum NexError {
    #[error("invalid session transition: {from:?} cannot handle {event:?}")]
    InvalidSessionTransition {
        from: crate::session::SessionState,
        event: crate::session::SessionEvent,
    },

    #[error("session failed: {reason}")]
    SessionFailed { reason: String },

    #[error("configuration error: {0}")]
    Config(String),
}

pub type Result<T> = std::result::Result<T, NexError>;
