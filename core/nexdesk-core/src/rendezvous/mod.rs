//! Rust-side client for the Go rendezvous/relay control plane (talks gRPC — see
//! Section 21 "Rust ↔ Go" in the planner: this is a network boundary, not FFI).
//!
//! Only the relay leg (`relay`) is implemented so far — a real host/client
//! pair can now connect through `backend/internal/relay` given a valid
//! session token. A Rust client for `RendezvousService` itself
//! (`RegisterDevice`/`RequestSession`/...) is real, separate future work:
//! today only the Electron desktop app (via `@grpc/proto-loader`, not
//! this crate) and the Go backend's own tests call those RPCs, so a
//! NexDesk host/client pair can't yet *obtain* a session token on its
//! own — only use one it's handed.

pub mod relay;
