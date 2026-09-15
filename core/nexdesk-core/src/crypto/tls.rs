//! TLS 1.3 transport security (planner tasks R-13, R-15). Key exchange and
//! record encryption are entirely `rustls`'s (TLS 1.3 ECDHE) — this module
//! only orchestrates: generating dev certs, configuring rustls, and
//! adapting a TLS stream to the `Connection` trait so callers can't tell
//! it apart from plain TCP.
//!
//! Certificate verification here **pins** the peer's exact certificate
//! rather than disabling verification — appropriate for a peer-to-peer
//! tool with no CA, but it means the pinned cert must come from a trusted
//! channel (in production: the rendezvous server, once device identity/
//! G-08/G-17 exists). Tests pin the cert they just generated.

use crate::error::{NexError, Result};
use crate::transport::framing::{read_frame, write_frame};
use crate::transport::Connection;
use rustls::pki_types::{CertificateDer, PrivateKeyDer, ServerName, UnixTime};
use std::sync::Arc;
use tokio::io::{AsyncRead, AsyncWrite};
use tokio::net::TcpStream;
use tokio_rustls::{TlsAcceptor, TlsConnector};

/// Installs `ring` as the process-wide rustls crypto provider. Safe to call
/// more than once (e.g. from multiple tests) — a second install just fails
/// and is ignored, since the first one already took effect.
fn ensure_crypto_provider() {
    let _ = rustls::crypto::ring::default_provider().install_default();
}

/// A self-signed certificate + key pair, for development/testing only.
/// Production needs real device identity (see module docs) — never ship
/// this as the trust mechanism for a real deployment.
pub struct DevCertificate {
    pub cert_der: CertificateDer<'static>,
    key_der: PrivateKeyDer<'static>,
}

pub fn generate_dev_certificate(subject_alt_name: &str) -> Result<DevCertificate> {
    let certified_key = rcgen::generate_simple_self_signed(vec![subject_alt_name.to_string()])
        .map_err(|e| NexError::Transport(format!("cert generation failed: {e}")))?;
    let cert_der = certified_key.cert.der().clone();
    let key_der = PrivateKeyDer::Pkcs8(certified_key.key_pair.serialize_der().into());
    Ok(DevCertificate { cert_der, key_der })
}

impl DevCertificate {
    /// Reconstructs a previously-generated dev certificate from its saved
    /// DER bytes, so a host process can keep the same identity across
    /// restarts instead of generating (and presenting) a new one every
    /// time it starts — a real device's identity should be stable.
    pub fn from_der(cert_der: Vec<u8>, key_der: Vec<u8>) -> DevCertificate {
        DevCertificate {
            cert_der: CertificateDer::from(cert_der),
            key_der: PrivateKeyDer::Pkcs8(key_der.into()),
        }
    }

    pub fn key_der_bytes(&self) -> &[u8] {
        self.key_der.secret_der()
    }
}

pub fn server_config(cert: &DevCertificate) -> Result<Arc<rustls::ServerConfig>> {
    ensure_crypto_provider();
    let config = rustls::ServerConfig::builder()
        .with_no_client_auth()
        .with_single_cert(vec![cert.cert_der.clone()], cert.key_der.clone_key())
        .map_err(|e| NexError::Transport(format!("TLS server config: {e}")))?;
    Ok(Arc::new(config))
}

/// A client config that trusts *only* `expected_cert` — see module docs on
/// why this is pinning, not "skip verification."
pub fn client_config_pinned_to(expected_cert: CertificateDer<'static>) -> Arc<rustls::ClientConfig> {
    ensure_crypto_provider();
    let verifier = Arc::new(PinnedCertVerifier {
        expected: expected_cert,
    });
    let config = rustls::ClientConfig::builder()
        .dangerous()
        .with_custom_certificate_verifier(verifier)
        .with_no_client_auth();
    Arc::new(config)
}

#[derive(Debug)]
struct PinnedCertVerifier {
    expected: CertificateDer<'static>,
}

impl rustls::client::danger::ServerCertVerifier for PinnedCertVerifier {
    fn verify_server_cert(
        &self,
        end_entity: &CertificateDer<'_>,
        _intermediates: &[CertificateDer<'_>],
        _server_name: &ServerName<'_>,
        _ocsp_response: &[u8],
        _now: UnixTime,
    ) -> std::result::Result<rustls::client::danger::ServerCertVerified, rustls::Error> {
        if end_entity.as_ref() == self.expected.as_ref() {
            Ok(rustls::client::danger::ServerCertVerified::assertion())
        } else {
            Err(rustls::Error::General(
                "server certificate does not match pinned certificate".into(),
            ))
        }
    }

    fn verify_tls12_signature(
        &self,
        message: &[u8],
        cert: &CertificateDer<'_>,
        dss: &rustls::DigitallySignedStruct,
    ) -> std::result::Result<rustls::client::danger::HandshakeSignatureValid, rustls::Error> {
        rustls::crypto::verify_tls12_signature(
            message,
            cert,
            dss,
            &rustls::crypto::ring::default_provider().signature_verification_algorithms,
        )
    }

    fn verify_tls13_signature(
        &self,
        message: &[u8],
        cert: &CertificateDer<'_>,
        dss: &rustls::DigitallySignedStruct,
    ) -> std::result::Result<rustls::client::danger::HandshakeSignatureValid, rustls::Error> {
        rustls::crypto::verify_tls13_signature(
            message,
            cert,
            dss,
            &rustls::crypto::ring::default_provider().signature_verification_algorithms,
        )
    }

    fn supported_verify_schemes(&self) -> Vec<rustls::SignatureScheme> {
        rustls::crypto::ring::default_provider()
            .signature_verification_algorithms
            .supported_schemes()
    }
}

/// A `Connection` over an established TLS stream. Generic over the inner
/// I/O type so the same adapter serves both `accept()` and `connect()`
/// (rustls gives client and server distinct stream types).
pub struct TlsConnection<S> {
    stream: tokio_rustls::TlsStream<S>,
}

impl<S: AsyncRead + AsyncWrite + Unpin + Send> Connection for TlsConnection<S> {
    async fn send(&mut self, data: &[u8]) -> Result<()> {
        write_frame(&mut self.stream, data).await
    }

    async fn recv(&mut self) -> Result<Vec<u8>> {
        read_frame(&mut self.stream).await
    }
}

pub async fn accept(
    acceptor: &TlsAcceptor,
    tcp: TcpStream,
) -> Result<TlsConnection<TcpStream>> {
    let stream = acceptor
        .accept(tcp)
        .await
        .map_err(|e| NexError::Transport(format!("TLS accept failed: {e}")))?;
    Ok(TlsConnection {
        stream: tokio_rustls::TlsStream::Server(stream),
    })
}

pub async fn connect(
    connector: &TlsConnector,
    server_name: ServerName<'static>,
    tcp: TcpStream,
) -> Result<TlsConnection<TcpStream>> {
    let stream = connector
        .connect(server_name, tcp)
        .await
        .map_err(|e| NexError::Transport(format!("TLS connect failed: {e}")))?;
    Ok(TlsConnection {
        stream: tokio_rustls::TlsStream::Client(stream),
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::transport::tcp::TcpListener;

    #[tokio::test]
    async fn establishes_tls_1_3_and_round_trips_a_message() {
        let dev_cert = generate_dev_certificate("localhost").unwrap();
        let server_cfg = server_config(&dev_cert).unwrap();
        let client_cfg = client_config_pinned_to(dev_cert.cert_der.clone());

        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        let acceptor = TlsAcceptor::from(server_cfg);
        let connector = TlsConnector::from(client_cfg);

        let server = tokio::spawn(async move {
            let tcp = listener.accept_raw().await.unwrap();
            let mut conn = accept(&acceptor, tcp).await.unwrap();
            let msg = conn.recv().await.unwrap();
            conn.send(&msg).await.unwrap();
        });

        let tcp = TcpStream::connect(addr.to_string()).await.unwrap();
        let server_name = ServerName::try_from("localhost").unwrap();
        let mut client = connect(&connector, server_name, tcp).await.unwrap();
        client.send(b"secure hello").await.unwrap();
        let echoed = client.recv().await.unwrap();

        server.await.unwrap();
        assert_eq!(echoed, b"secure hello");

        let (_, session) = client.stream.get_ref();
        assert_eq!(
            session.protocol_version(),
            Some(rustls::ProtocolVersion::TLSv1_3)
        );
    }

    #[tokio::test]
    async fn rejects_a_cert_that_does_not_match_the_pin() {
        let real_cert = generate_dev_certificate("localhost").unwrap();
        let server_cfg = server_config(&real_cert).unwrap();

        // Pin to a *different* cert than the server actually presents.
        let wrong_cert = generate_dev_certificate("localhost").unwrap();
        let client_cfg = client_config_pinned_to(wrong_cert.cert_der);

        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        let acceptor = TlsAcceptor::from(server_cfg);
        let connector = TlsConnector::from(client_cfg);

        let server = tokio::spawn(async move {
            let tcp = listener.accept_raw().await.unwrap();
            // The handshake will fail on the client's side; accept() here
            // may itself error too, which is fine either way.
            let _ = accept(&acceptor, tcp).await;
        });

        let tcp = TcpStream::connect(addr.to_string()).await.unwrap();
        let server_name = ServerName::try_from("localhost").unwrap();
        let result = connect(&connector, server_name, tcp).await;

        assert!(result.is_err());
        let _ = server.await;
    }
}
