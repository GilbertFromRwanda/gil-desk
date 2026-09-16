//! Proves `rendezvous::relay::connect` against a *real* running Go relay
//! (`backend/internal/relay`), not a mock — this is the first time
//! anything in `nexdesk-core` has spoken to the Go control plane at all.
//!
//! Needs external infrastructure this crate's other tests don't (a real
//! backend process), so it's `#[ignore]`d by default rather than run on
//! every `cargo test`. To run it:
//!
//! ```bash
//! # in backend/, with a *fixed* signing key so this test can construct
//! # a token the server will actually accept:
//! NEXDESK_SESSION_SIGNING_KEY=relay-transport-test-key \
//! NEXDESK_POSTGRES_URL=postgres://nexdesk:nexdesk@localhost:55432/nexdesk \
//! NEXDESK_REDIS_ADDR=localhost:56379 \
//! NEXDESK_GRPC_ADDR=:9090 \
//! go run ./cmd/server
//!
//! # in core/, in another terminal:
//! cargo test -p nexdesk-core --test relay_transport -- --ignored
//! ```
//!
//! There's no Rust-side `RequestSession` client yet (see
//! `rendezvous/mod.rs`'s module docs — real, separate future work), so
//! the token below is constructed directly in the same HMAC format
//! `backend/internal/rendezvous/token.go` signs, using the signing key
//! given to the server above. This tests the real wire behavior (a real
//! signature the real server verifies) rather than assuming the format.

use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use base64::Engine;
use hmac::{Hmac, Mac};
use nexdesk_core::rendezvous::relay;
use nexdesk_core::transport::Connection;
use sha2::Sha256;
use std::time::{SystemTime, UNIX_EPOCH};

const TEST_SIGNING_KEY: &[u8] = b"relay-transport-test-key";

fn issue_session_token(requester_device_id: &str, target_device_id: &str) -> String {
    let expires_at = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_secs()
        + 60;
    let payload = format!(
        "{}|{}|{}",
        URL_SAFE_NO_PAD.encode(requester_device_id),
        URL_SAFE_NO_PAD.encode(target_device_id),
        expires_at
    );
    let mut mac = Hmac::<Sha256>::new_from_slice(TEST_SIGNING_KEY).unwrap();
    mac.update(payload.as_bytes());
    let sig = mac.finalize().into_bytes();
    format!("{payload}.{}", URL_SAFE_NO_PAD.encode(sig))
}

fn relay_addr() -> String {
    std::env::var("NEXDESK_RELAY_ADDR").unwrap_or_else(|_| "http://127.0.0.1:9090".to_string())
}

#[tokio::test]
#[ignore = "needs a real backend running with a matching NEXDESK_SESSION_SIGNING_KEY — see this file's module docs"]
async fn relays_bytes_between_two_real_connections() {
    let token = issue_session_token("relay-transport-requester", "relay-transport-target");

    // Sequential, deliberately: this is the actual regression case for
    // the deadlock relay.rs's connect() docs describe — requester's
    // connect() must resolve on its own, without waiting for target to
    // show up at all, or this hangs forever (it did, before
    // relay.go started sending response headers before pairing).
    let addr = relay_addr();
    let mut requester = relay::connect(&addr, &token)
        .await
        .expect("requester connects to the real relay, without target existing yet");
    let mut target = relay::connect(&addr, &token)
        .await
        .expect("target connects to the real relay");

    requester.send(b"hello from requester").await.unwrap();
    let got = target.recv().await.unwrap();
    assert_eq!(got, b"hello from requester");

    target.send(b"hello from target").await.unwrap();
    let got = requester.recv().await.unwrap();
    assert_eq!(got, b"hello from target");
}

#[tokio::test]
#[ignore = "needs a real backend running — see this file's module docs"]
async fn rejects_a_forged_token() {
    let mut mac = Hmac::<Sha256>::new_from_slice(b"the-wrong-key").unwrap();
    mac.update(b"forged-payload");
    let forged = format!("forged-payload.{}", URL_SAFE_NO_PAD.encode(mac.finalize().into_bytes()));

    // Whether tonic surfaces the server's rejection from connect() itself
    // (a trailers-only error response) or only once a message is actually
    // read depends on gRPC/HTTP2 timing details outside this crate's
    // control, so this checks whichever point it shows up at rather than
    // assuming one.
    match relay::connect(&relay_addr(), &forged).await {
        Err(_) => {}
        Ok(mut conn) => {
            let result = conn.recv().await;
            assert!(result.is_err(), "a forged token must be rejected, if not at connect() then on first recv()");
        }
    }
}
