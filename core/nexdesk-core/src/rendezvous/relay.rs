//! Client for `backend/internal/relay`'s `RelayService.Stream` RPC
//! (planner Gate G3's "if direct fails -> relay" step). Implements the
//! same `Connection` trait TCP/TLS/QUIC do (`transport::Connection`), so
//! a caller that already has a session token can use a relayed
//! connection exactly like any other transport — the session-layer code
//! in `bin/host.rs`/`bin/client.rs` doesn't need to know which one it got.
//!
//! No generated tonic client stub is used here (see this crate's
//! `Cargo.toml` comment on `tonic` for why) — `tonic::client::Grpc`'s
//! low-level `streaming` method is called directly against the
//! `RelayFrame` type `nexdesk-proto` already generates from
//! `relay.proto`, which is exactly what codegen would produce anyway for
//! a single bidi-streaming method.

use crate::error::{NexError, Result};
use crate::transport::Connection;
use http::uri::PathAndQuery;
use nexdesk_proto::nexdesk::v1::{relay_frame::Payload, RelayFrame};
use tokio::sync::mpsc;
use tokio_stream::wrappers::ReceiverStream;
use tonic::codec::ProstCodec;
use tonic::transport::Channel;
use tonic::Request;

const RELAY_STREAM_PATH: &str = "/nexdesk.v1.RelayService/Stream";
const OUTBOUND_BUFFER: usize = 16;

/// A `Connection` carried over the Go backend's relay rather than a
/// direct socket. `data` frames map 1:1 to `Connection::send`/`recv`
/// calls — the relay forwards each `RelayFrame` as its own gRPC stream
/// message, so no additional length-prefix framing (unlike TCP/TLS/QUIC,
/// which need `transport::framing` because a byte stream has no message
/// boundaries of its own) is needed here.
pub struct RelayConnection {
    outbound: mpsc::Sender<RelayFrame>,
    inbound: tonic::Streaming<RelayFrame>,
}

impl Connection for RelayConnection {
    async fn send(&mut self, data: &[u8]) -> Result<()> {
        self.outbound
            .send(RelayFrame {
                payload: Some(Payload::Data(data.to_vec())),
            })
            .await
            .map_err(|_| NexError::Transport("relay outbound channel closed".to_string()))
    }

    async fn recv(&mut self) -> Result<Vec<u8>> {
        let frame = self
            .inbound
            .message()
            .await
            .map_err(|e| NexError::Transport(format!("relay recv failed: {e}")))?
            .ok_or_else(|| NexError::Transport("relay stream closed by peer".to_string()))?;

        match frame.payload {
            Some(Payload::Data(bytes)) => Ok(bytes),
            Some(Payload::SessionToken(_)) => Err(NexError::Transport(
                "received an unexpected session_token frame after the handshake".to_string(),
            )),
            None => Err(NexError::Transport("received an empty relay frame".to_string())),
        }
    }
}

/// Connects to the relay at `relay_addr` (e.g. `http://127.0.0.1:9090`)
/// and presents `session_token` as the required first frame — the same
/// token both sides of a session must present to be paired together
/// (see `relay.proto`'s own docs on why: the relay makes no authorization
/// decision itself, the token already proves the rendezvous server made
/// one).
///
/// Returns as soon as the relay accepts *this* side's token — it does
/// not wait for the peer to also connect (pairing happens independently,
/// server-side, once both have). An earlier version of this function
/// (and of `backend/internal/relay/relay.go`'s handler) did block until
/// both sides were present, because grpc-go defers sending response
/// headers until a handler's first write, and the first-arriving side
/// wrote nothing until paired — a real deadlock (no send call on either
/// side could ever happen, since both were waiting on `connect()` to
/// return first) caught by this crate's own integration test, not a
/// hypothetical one. The fix was server-side: the handler now calls
/// `stream.SendHeader(nil)` immediately after validating the token,
/// before pairing, so `connect()` here behaves like `transport::tcp`'s
/// and `crypto::tls`'s — connected the moment this side's own handshake
/// completes, independent of the peer.
pub async fn connect(relay_addr: &str, session_token: &str) -> Result<RelayConnection> {
    let channel = Channel::from_shared(relay_addr.to_string())
        .map_err(|e| NexError::Transport(format!("invalid relay address {relay_addr}: {e}")))?
        .connect()
        .await
        .map_err(|e| NexError::Transport(format!("connect to relay {relay_addr}: {e}")))?;

    let (tx, rx) = mpsc::channel::<RelayFrame>(OUTBOUND_BUFFER);
    tx.send(RelayFrame {
        payload: Some(Payload::SessionToken(session_token.to_string())),
    })
    .await
    .map_err(|_| NexError::Transport("relay outbound channel closed before handshake".to_string()))?;

    let mut client = tonic::client::Grpc::new(channel);
    client
        .ready()
        .await
        .map_err(|e| NexError::Transport(format!("relay not ready: {e}")))?;

    let path = PathAndQuery::from_static(RELAY_STREAM_PATH);
    let response = client
        .streaming(Request::new(ReceiverStream::new(rx)), path, ProstCodec::default())
        .await
        .map_err(|status| NexError::Transport(format!("relay stream rejected: {status}")))?;

    Ok(RelayConnection {
        outbound: tx,
        inbound: response.into_inner(),
    })
}
