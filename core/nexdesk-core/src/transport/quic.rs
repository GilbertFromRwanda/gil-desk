//! QUIC transport (planner task R-09). Deliberately deferred from Week 5
//! until now — see the planner's own note on why (an 8-point item is
//! large enough to deserve its own focused pass rather than being folded
//! into the TCP work). Built on `quinn`, whose QUIC handshake runs
//! directly on the same `rustls::ClientConfig`/`ServerConfig` the TLS
//! transport already produces (`crypto::tls::server_config` /
//! `client_config_pinned_to`) — no new certificate/verification logic
//! needed here, only wiring.

use crate::crypto::tls::DevCertificate;
use crate::error::{NexError, Result};
use crate::transport::framing::{read_frame, write_frame};
use crate::transport::Connection;
use quinn::crypto::rustls::{QuicClientConfig, QuicServerConfig};
use rustls::pki_types::ServerName;
use std::net::SocketAddr;
use std::sync::Arc;

/// QUIC requires ALPN to be configured (unlike plain TLS-over-TCP, which
/// doesn't need it) — without a protocol both sides agree on, the
/// handshake completes but the connection is unusable. `tls::server_config`/
/// `client_config_pinned_to` don't set one, since plain TLS callers don't
/// need it, so this module adds its own on a cloned config rather than
/// changing behavior for every other caller of those functions.
const ALPN_PROTOCOL: &[u8] = b"nexdesk";

/// A `Connection` over a single bidirectional QUIC stream. QUIC natively
/// supports many concurrent streams per connection; this module only
/// opens one, matching how `TcpConnection`/`TlsConnection` behave — a
/// second stream per connection is a real future optimization (e.g.
/// separating control messages from frame data), not something needed
/// yet.
pub struct QuicConnection {
    // Neither field below is read directly, but both must be kept alive:
    // dropping the last `quinn::Connection` handle closes the connection
    // even while its streams are still in use, and on the client side the
    // `quinn::Endpoint` created just for this connection would otherwise
    // be dropped the moment `connect()` returns (the server side doesn't
    // have this problem — its endpoint is already kept alive by
    // `QuicListener` for as long as the listener itself is).
    _endpoint: Option<quinn::Endpoint>,
    _connection: quinn::Connection,
    send: quinn::SendStream,
    recv: quinn::RecvStream,
}

impl Connection for QuicConnection {
    async fn send(&mut self, data: &[u8]) -> Result<()> {
        write_frame(&mut self.send, data).await
    }

    async fn recv(&mut self) -> Result<Vec<u8>> {
        read_frame(&mut self.recv).await
    }
}

pub struct QuicListener {
    endpoint: quinn::Endpoint,
}

impl QuicListener {
    pub async fn bind(addr: &str, dev_cert: &DevCertificate) -> Result<Self> {
        let mut rustls_config = (*crate::crypto::tls::server_config(dev_cert)?).clone();
        rustls_config.alpn_protocols = vec![ALPN_PROTOCOL.to_vec()];
        let quic_server_config: QuicServerConfig = Arc::new(rustls_config)
            .try_into()
            .map_err(|e| NexError::Transport(format!("build QUIC server config: {e}")))?;
        let server_config = quinn::ServerConfig::with_crypto(Arc::new(quic_server_config));

        let socket_addr: SocketAddr = addr
            .parse()
            .map_err(|e| NexError::Transport(format!("invalid bind address {addr}: {e}")))?;
        let endpoint = quinn::Endpoint::server(server_config, socket_addr)
            .map_err(|e| NexError::Transport(format!("bind QUIC endpoint: {e}")))?;

        Ok(Self { endpoint })
    }

    pub fn local_addr(&self) -> Result<SocketAddr> {
        self.endpoint
            .local_addr()
            .map_err(|e| NexError::Transport(e.to_string()))
    }

    pub async fn accept(&self) -> Result<QuicConnection> {
        let incoming = self
            .endpoint
            .accept()
            .await
            .ok_or_else(|| NexError::Transport("QUIC endpoint closed".to_string()))?;
        let connection = incoming
            .await
            .map_err(|e| NexError::Transport(format!("QUIC handshake failed: {e}")))?;
        let (send, recv) = connection
            .accept_bi()
            .await
            .map_err(|e| NexError::Transport(format!("QUIC accept_bi failed: {e}")))?;
        Ok(QuicConnection {
            _endpoint: None, // server's endpoint is kept alive by QuicListener instead
            _connection: connection,
            send,
            recv,
        })
    }
}

/// Connects to a QUIC endpoint pinned to `expected_cert` (see
/// `crypto::tls` module docs for why pinning, not disabled verification).
pub async fn connect(
    addr: &str,
    server_name: ServerName<'static>,
    client_cfg: Arc<rustls::ClientConfig>,
) -> Result<QuicConnection> {
    let socket_addr: SocketAddr = addr
        .parse()
        .map_err(|e| NexError::Transport(format!("invalid server address {addr}: {e}")))?;

    let mut rustls_config = (*client_cfg).clone();
    rustls_config.alpn_protocols = vec![ALPN_PROTOCOL.to_vec()];
    let quic_client_config: QuicClientConfig = Arc::new(rustls_config)
        .try_into()
        .map_err(|e| NexError::Transport(format!("build QUIC client config: {e}")))?;

    let mut endpoint = quinn::Endpoint::client("0.0.0.0:0".parse().unwrap())
        .map_err(|e| NexError::Transport(format!("create QUIC client endpoint: {e}")))?;
    endpoint.set_default_client_config(quinn::ClientConfig::new(Arc::new(quic_client_config)));

    let server_name_str = match &server_name {
        ServerName::DnsName(name) => name.as_ref().to_string(),
        _ => return Err(NexError::Transport("QUIC requires a DNS server name".to_string())),
    };

    let connection = endpoint
        .connect(socket_addr, &server_name_str)
        .map_err(|e| NexError::Transport(format!("QUIC connect setup failed: {e}")))?
        .await
        .map_err(|e| NexError::Transport(format!("QUIC handshake failed: {e}")))?;

    let (send, recv) = connection
        .open_bi()
        .await
        .map_err(|e| NexError::Transport(format!("QUIC open_bi failed: {e}")))?;

    Ok(QuicConnection {
        _endpoint: Some(endpoint),
        _connection: connection,
        send,
        recv,
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::crypto::tls;

    #[tokio::test]
    async fn establishes_quic_and_round_trips_a_message() {
        let dev_cert = tls::generate_dev_certificate("localhost").unwrap();

        let listener = QuicListener::bind("127.0.0.1:0", &dev_cert).await.unwrap();
        let addr = listener.local_addr().unwrap();

        let server = tokio::spawn(async move {
            let mut conn = listener.accept().await.unwrap();
            let msg = conn.recv().await.unwrap();
            conn.send(&msg).await.unwrap(); // echo

            // QUIC's send buffer lives entirely in userspace (unlike TCP's
            // kernel-buffered send, which is already handed off by the time
            // write_all returns) — dropping the connection immediately
            // after write_all can discard data that hasn't actually been
            // transmitted onto the wire yet. Waiting for the peer to
            // disconnect (this errors once the client drops its side,
            // which only happens *after* it has read the echo) gives the
            // runtime the cycles it needs to actually flush the echo
            // first. This isn't a workaround for a bug in this module —
            // it reflects a real difference between the two transports
            // that any real caller needs to respect: don't tear down a
            // QUIC connection immediately after your last write if you
            // need that write delivered.
            let _ = conn.recv().await;
        });

        let client_cfg = tls::client_config_pinned_to(dev_cert.cert_der.clone());
        let server_name = ServerName::try_from("localhost").unwrap();
        let mut client = connect(&addr.to_string(), server_name, client_cfg)
            .await
            .unwrap();

        client.send(b"hello over quic").await.unwrap();
        let echoed = client.recv().await.unwrap();
        drop(client); // lets the server's pending recv() above observe the disconnect

        server.await.unwrap();
        assert_eq!(echoed, b"hello over quic");
    }

    #[tokio::test]
    async fn rejects_a_cert_that_does_not_match_the_pin() {
        let real_cert = tls::generate_dev_certificate("localhost").unwrap();
        let listener = QuicListener::bind("127.0.0.1:0", &real_cert).await.unwrap();
        let addr = listener.local_addr().unwrap();

        let server = tokio::spawn(async move {
            let _ = listener.accept().await;
        });

        let wrong_cert = tls::generate_dev_certificate("localhost").unwrap();
        let client_cfg = tls::client_config_pinned_to(wrong_cert.cert_der);
        let server_name = ServerName::try_from("localhost").unwrap();
        let result = connect(&addr.to_string(), server_name, client_cfg).await;

        assert!(result.is_err());
        let _ = server.await;
    }
}
