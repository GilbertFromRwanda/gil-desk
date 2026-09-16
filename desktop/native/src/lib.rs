//! Rust <-> Electron boundary (planner tasks F-18/F-19/E-01). The Phase 0
//! smoke test (`native_version`/`initial_session_state`) proved the
//! napi-rs toolchain works end to end; the input-event functions below
//! are the first *real* session/transport-adjacent calls Phase 4 needs —
//! encoding real captured keyboard/mouse/scroll events (planner tasks
//! E-14/E-15/E-16) into the exact wire format `core::input::InputEvent`
//! already defines and tests, rather than re-implementing that format in
//! TypeScript.

#[macro_use]
extern crate napi_derive;

use napi::bindgen_prelude::Buffer;
use nexdesk_core::input::InputEvent;
use nexdesk_core::rendezvous::relay::{self, RelayConnection};
use nexdesk_core::session::SessionState;
use nexdesk_core::transport::Connection;
use std::sync::Arc;
use tokio::sync::Mutex;

#[napi]
pub fn native_version() -> String {
    env!("CARGO_PKG_VERSION").to_string()
}

/// Calls into the actual Rust core (not just a hardcoded string), proving
/// Node -> Rust -> nexdesk-core works across the real boundary.
#[napi]
pub fn initial_session_state() -> String {
    match SessionState::Idle {
        SessionState::Idle => "idle".to_string(),
        _ => unreachable!(),
    }
}

#[napi]
pub fn encode_key_down(code: u32) -> Buffer {
    InputEvent::KeyDown(code).encode().into()
}

#[napi]
pub fn encode_key_up(code: u32) -> Buffer {
    InputEvent::KeyUp(code).encode().into()
}

#[napi]
pub fn encode_mouse_move(x: i32, y: i32) -> Buffer {
    InputEvent::MouseMove { x, y }.encode().into()
}

#[napi]
pub fn encode_mouse_button(button: u8, down: bool) -> Buffer {
    InputEvent::MouseButton { button, down }.encode().into()
}

#[napi]
pub fn encode_scroll(dx: i32, dy: i32) -> Buffer {
    InputEvent::Scroll { dx, dy }.encode().into()
}

/// Mirrors `InputEvent`'s variants as a plain object rather than exposing
/// an enum across the FFI boundary — `kind` discriminates which of the
/// other (variant-specific) fields are populated. Used by the renderer to
/// prove a captured event survives a real encode -> decode round trip
/// through the compiled native addon, not just a JS-side assumption.
#[napi(object)]
pub struct DecodedInputEvent {
    pub kind: String,
    pub code: Option<u32>,
    pub x: Option<i32>,
    pub y: Option<i32>,
    pub button: Option<u32>,
    pub down: Option<bool>,
    pub dx: Option<i32>,
    pub dy: Option<i32>,
}

#[napi]
pub fn decode_input_event(bytes: Buffer) -> napi::Result<DecodedInputEvent> {
    let event = InputEvent::decode(&bytes)
        .map_err(|e| napi::Error::from_reason(e.to_string()))?;
    Ok(match event {
        InputEvent::KeyDown(code) => DecodedInputEvent {
            kind: "keyDown".to_string(),
            code: Some(code),
            x: None,
            y: None,
            button: None,
            down: None,
            dx: None,
            dy: None,
        },
        InputEvent::KeyUp(code) => DecodedInputEvent {
            kind: "keyUp".to_string(),
            code: Some(code),
            x: None,
            y: None,
            button: None,
            down: None,
            dx: None,
            dy: None,
        },
        InputEvent::MouseMove { x, y } => DecodedInputEvent {
            kind: "mouseMove".to_string(),
            code: None,
            x: Some(x),
            y: Some(y),
            button: None,
            down: None,
            dx: None,
            dy: None,
        },
        InputEvent::MouseButton { button, down } => DecodedInputEvent {
            kind: "mouseButton".to_string(),
            code: None,
            x: None,
            y: None,
            button: Some(button as u32),
            down: Some(down),
            dx: None,
            dy: None,
        },
        InputEvent::Scroll { dx, dy } => DecodedInputEvent {
            kind: "scroll".to_string(),
            code: None,
            x: None,
            y: None,
            button: None,
            down: None,
            dx: Some(dx),
            dy: Some(dy),
        },
    })
}

fn to_napi_err(e: nexdesk_core::error::NexError) -> napi::Error {
    napi::Error::from_reason(e.to_string())
}

/// A live session with a real peer, connected through the relay
/// (`rendezvous::relay`, Gate G3). This is the first time the Electron
/// app can reach an actual `nexdesk-core` session rather than only the
/// Go control plane (auth, device registry, `RequestSession`) — the
/// desktop UI's own gRPC calls (`main/rendezvousClient.ts`) get a
/// session token; this class is what turns that token into bytes
/// actually flowing to and from a peer, via the exact same
/// `core::input::InputEvent`-producing code path `encodeKeyDown` etc.
/// above feed into (that wiring — pushing captured input through an open
/// `RelaySession` instead of only round-tripping it locally — is real,
/// separate future work; this class only proves the transport side).
///
/// `RelayConnection` is wrapped in `Arc<Mutex<_>>` rather than accessed
/// via `&mut self` directly: napi-rs class instances can have more than
/// one JS-side call in flight (e.g. a `send` and a `recv` awaited
/// concurrently from TypeScript), and `Connection::send`/`recv` need
/// exclusive access to the underlying stream for the duration of each
/// call.
#[napi]
pub struct RelaySession {
    conn: Arc<Mutex<RelayConnection>>,
    peer_device_id: String,
}

#[napi]
impl RelaySession {
    #[napi(getter)]
    pub fn peer_device_id(&self) -> String {
        self.peer_device_id.clone()
    }

    #[napi]
    pub async fn send(&self, data: Buffer) -> napi::Result<()> {
        self.conn.lock().await.send(&data).await.map_err(to_napi_err)
    }

    #[napi]
    pub async fn recv(&self) -> napi::Result<Buffer> {
        let bytes = self.conn.lock().await.recv().await.map_err(to_napi_err)?;
        Ok(bytes.into())
    }
}

/// Connects through the relay and completes the `SessionHello` handshake
/// (`rendezvous::relay::connect_and_handshake` — the same function
/// `bin/host.rs`/`bin/client.rs`'s relay mode would otherwise have had to
/// be duplicated to get here). Resolves once the peer's hello is
/// received; the JS `Promise` this returns is exactly as long-blocking as
/// the underlying handshake and no longer.
#[napi]
pub async fn connect_relay_session(
    relay_addr: String,
    session_token: String,
    device_id: String,
) -> napi::Result<RelaySession> {
    let (conn, peer_device_id) = relay::connect_and_handshake(&relay_addr, &session_token, &device_id)
        .await
        .map_err(to_napi_err)?;
    Ok(RelaySession {
        conn: Arc::new(Mutex::new(conn)),
        peer_device_id,
    })
}
