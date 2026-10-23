//! Panel certificate trust: the system's checks (OS store), exactly one pinned certificate of a self-signed panel, or
//! (a legacy config not saved since the pin came in) any certificate. The pinned verifier still checks the handshake
//! signature with the certificate's key, so a server that only presents the certificate (without its key) fails.

use std::sync::Arc;
use std::time::Duration;

use chrono::{DateTime, Utc};
use rustls::client::danger::{HandshakeSignatureValid, ServerCertVerified, ServerCertVerifier};
use rustls::crypto::{CryptoProvider, WebPkiSupportedAlgorithms};
use rustls::pki_types::{CertificateDer, ServerName, UnixTime};
use rustls::{ClientConfig, DigitallySignedStruct, SignatureScheme};
use sha2::{Digest, Sha256};

/// The text of the error a pinned connection fails with when the certificate is another one (the API layer looks
/// for it in the error chain to show the translated message).
pub const CERT_CHANGED: &str = "the panel's certificate is not the one trusted in Configure (it changed): check it with the panel's administrator, then trust it again in Configure";

#[derive(Debug, thiserror::Error)]
pub enum TlsError {
    #[error("enter an https://server:port URL")]
    Url,
    #[error("the panel presented no certificate")]
    NoCertificate,
    #[error("{0}")]
    Rustls(#[from] rustls::Error),
    #[error("{0}")]
    Io(#[from] std::io::Error),
    #[error("connection timed out")]
    Timeout,
}

pub fn provider() -> Arc<CryptoProvider> {
    Arc::new(rustls::crypto::ring::default_provider())
}

/// The fingerprint pinned for a certificate: "sha256:" and the hex SHA-256 of its DER.
pub fn cert_pin(der: &[u8]) -> String {
    format!("sha256:{}", hex::encode(Sha256::digest(der)))
}

/// How the client trusts the panel.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Trust {
    System,
    Pinned(String),
    /// The legacy `insecure = true` without a pin.
    Any,
}

/// The rustls client config of a trust mode.
pub fn client_config(trust: &Trust) -> Result<ClientConfig, TlsError> {
    let p = provider();
    let verifier: Arc<dyn ServerCertVerifier> = match trust {
        Trust::System => Arc::new(rustls_platform_verifier::Verifier::new(p.clone())?),
        Trust::Pinned(pin) => Arc::new(Pinned { pin: pin.clone(), algs: p.signature_verification_algorithms }),
        Trust::Any => Arc::new(Capture { algs: p.signature_verification_algorithms, chain: Default::default() }),
    };
    Ok(ClientConfig::builder_with_provider(p)
        .with_safe_default_protocol_versions()?
        .dangerous()
        .with_custom_certificate_verifier(verifier)
        .with_no_client_auth())
}

/// Accepts only the leaf certificate whose fingerprint is `pin`, on every connection.
#[derive(Debug)]
struct Pinned {
    pin: String,
    algs: WebPkiSupportedAlgorithms,
}

impl ServerCertVerifier for Pinned {
    fn verify_server_cert(
        &self,
        end_entity: &CertificateDer<'_>,
        _: &[CertificateDer<'_>],
        _: &ServerName<'_>,
        _: &[u8],
        _: UnixTime,
    ) -> Result<ServerCertVerified, rustls::Error> {
        if cert_pin(end_entity) == self.pin {
            Ok(ServerCertVerified::assertion())
        } else {
            Err(rustls::Error::General(CERT_CHANGED.into()))
        }
    }

    fn verify_tls12_signature(
        &self,
        message: &[u8],
        cert: &CertificateDer<'_>,
        dss: &DigitallySignedStruct,
    ) -> Result<HandshakeSignatureValid, rustls::Error> {
        rustls::crypto::verify_tls12_signature(message, cert, dss, &self.algs)
    }

    fn verify_tls13_signature(
        &self,
        message: &[u8],
        cert: &CertificateDer<'_>,
        dss: &DigitallySignedStruct,
    ) -> Result<HandshakeSignatureValid, rustls::Error> {
        rustls::crypto::verify_tls13_signature(message, cert, dss, &self.algs)
    }

    fn supported_verify_schemes(&self) -> Vec<SignatureScheme> {
        self.algs.supported_schemes()
    }
}

/// Accepts any chain and keeps it (Configure's probe; the legacy "any certificate" mode). The handshake signature is
/// still checked against the presented certificate.
#[derive(Debug)]
struct Capture {
    algs: WebPkiSupportedAlgorithms,
    chain: std::sync::Mutex<Vec<CertificateDer<'static>>>,
}

impl ServerCertVerifier for Capture {
    fn verify_server_cert(
        &self,
        end_entity: &CertificateDer<'_>,
        intermediates: &[CertificateDer<'_>],
        _: &ServerName<'_>,
        _: &[u8],
        _: UnixTime,
    ) -> Result<ServerCertVerified, rustls::Error> {
        if let Ok(mut c) = self.chain.lock() {
            *c = std::iter::once(end_entity).chain(intermediates).map(|d| d.clone().into_owned()).collect();
        }
        Ok(ServerCertVerified::assertion())
    }

    fn verify_tls12_signature(
        &self,
        message: &[u8],
        cert: &CertificateDer<'_>,
        dss: &DigitallySignedStruct,
    ) -> Result<HandshakeSignatureValid, rustls::Error> {
        rustls::crypto::verify_tls12_signature(message, cert, dss, &self.algs)
    }

    fn verify_tls13_signature(
        &self,
        message: &[u8],
        cert: &CertificateDer<'_>,
        dss: &DigitallySignedStruct,
    ) -> Result<HandshakeSignatureValid, rustls::Error> {
        rustls::crypto::verify_tls13_signature(message, cert, dss, &self.algs)
    }

    fn supported_verify_schemes(&self) -> Vec<SignatureScheme> {
        self.algs.supported_schemes()
    }
}

/// The certificate a panel presents, read without trusting it, and whether the system trusts it for that host.
#[derive(Debug, Clone, PartialEq)]
pub struct PanelCert {
    pub pin: String,
    pub subject: String,
    pub issuer: String,
    pub not_before: Option<DateTime<Utc>>,
    pub not_after: Option<DateTime<Utc>>,
    pub trusted: bool,
}

/// Host and port of an https URL (443 when none), or `TlsError::Url`.
pub fn https_host(base: &str) -> Result<(String, u16), TlsError> {
    let u = url::Url::parse(base).map_err(|_| TlsError::Url)?;
    match (u.scheme(), u.host_str()) {
        ("https", Some(h)) if !h.is_empty() => {
            Ok((h.trim_start_matches('[').trim_end_matches(']').to_string(), u.port().unwrap_or(443)))
        }
        _ => Err(TlsError::Url),
    }
}

/// Opens a TLS connection to the panel at `base` only to read its certificate (nothing is sent over it), then asks
/// the system verifier whether it trusts it for the URL's host.
pub async fn probe(base: &str) -> Result<PanelCert, TlsError> {
    let (host, port) = https_host(base)?;
    let p = provider();
    let capture = Arc::new(Capture { algs: p.signature_verification_algorithms, chain: Default::default() });
    let cfg = ClientConfig::builder_with_provider(p.clone())
        .with_safe_default_protocol_versions()?
        .dangerous()
        .with_custom_certificate_verifier(capture.clone())
        .with_no_client_auth();
    let name = ServerName::try_from(host.clone()).map_err(|_| TlsError::Url)?;
    let connect = async {
        let addrs: Vec<_> = tokio::net::lookup_host((host.as_str(), port)).await?.collect();
        let tcp = connect_any(addrs).await?;
        let tls = tokio_rustls::TlsConnector::from(Arc::new(cfg)).connect(name.clone(), tcp).await?;
        drop(tls);
        Ok::<(), std::io::Error>(())
    };
    tokio::time::timeout(Duration::from_secs(10), connect).await.map_err(|_| TlsError::Timeout)??;
    let chain = capture.chain.lock().map(|c| c.clone()).unwrap_or_default();
    let Some(leaf) = chain.first() else { return Err(TlsError::NoCertificate) };
    let system = rustls_platform_verifier::Verifier::new(p)?;
    let trusted = system.verify_server_cert(leaf, &chain[1..], &name, &[], UnixTime::now()).is_ok();
    Ok(describe(leaf, trusted))
}

/// Connects to the first address that answers (a name may resolve to IPv6 and IPv4); each address gets its share of
/// the time, so one that never answers does not use it all.
async fn connect_any(addrs: Vec<std::net::SocketAddr>) -> std::io::Result<tokio::net::TcpStream> {
    let mut last = std::io::Error::other("no address");
    let each = Duration::from_secs(3);
    for a in addrs {
        match tokio::time::timeout(each, tokio::net::TcpStream::connect(a)).await {
            Ok(Ok(s)) => return Ok(s),
            Ok(Err(e)) => last = e,
            Err(_) => last = std::io::Error::new(std::io::ErrorKind::TimedOut, format!("{a}: timed out")),
        }
    }
    Err(last)
}

/// Fingerprint, subject, issuer and validity of a DER certificate.
pub fn describe(der: &[u8], trusted: bool) -> PanelCert {
    let mut c = PanelCert {
        pin: cert_pin(der),
        subject: String::new(),
        issuer: String::new(),
        not_before: None,
        not_after: None,
        trusted,
    };
    if let Ok((_, x)) = x509_parser::parse_x509_certificate(der) {
        c.subject = x.subject().to_string();
        c.issuer = x.issuer().to_string();
        c.not_before = DateTime::from_timestamp(x.validity().not_before.timestamp(), 0);
        c.not_after = DateTime::from_timestamp(x.validity().not_after.timestamp(), 0);
    }
    c
}

#[cfg(test)]
pub(crate) mod tests {
    use super::*;
    use tokio::io::{AsyncReadExt, AsyncWriteExt};

    /// A self-signed certificate for 127.0.0.1/localhost and its key.
    pub(crate) fn self_signed() -> (CertificateDer<'static>, rustls::pki_types::PrivateKeyDer<'static>) {
        let k = rcgen::generate_simple_self_signed(vec!["localhost".into(), "127.0.0.1".into()]).unwrap();
        let der = k.cert.der().clone();
        let key = rustls::pki_types::PrivateKeyDer::Pkcs8(k.signing_key.serialize_der().into());
        (der, key)
    }

    #[derive(Debug)]
    struct One(Arc<rustls::sign::CertifiedKey>);

    impl rustls::server::ResolvesServerCert for One {
        fn resolve(&self, _: rustls::server::ClientHello<'_>) -> Option<Arc<rustls::sign::CertifiedKey>> {
            Some(self.0.clone())
        }
    }

    /// A TLS server on 127.0.0.1 answering every connection with `answer` (a raw HTTP response).
    pub(crate) async fn serve(
        cert: CertificateDer<'static>,
        key: rustls::pki_types::PrivateKeyDer<'static>,
        answer: &'static str,
    ) -> u16 {
        // a resolver, not with_single_cert: the test of a certificate served with another key needs no consistency check
        let signing = provider().key_provider.load_private_key(key).unwrap();
        let ck = Arc::new(rustls::sign::CertifiedKey::new(vec![cert], signing));
        let cfg = rustls::ServerConfig::builder_with_provider(provider())
            .with_safe_default_protocol_versions()
            .unwrap()
            .with_no_client_auth()
            .with_cert_resolver(Arc::new(One(ck)));
        let acceptor = tokio_rustls::TlsAcceptor::from(Arc::new(cfg));
        let l = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let port = l.local_addr().unwrap().port();
        tokio::spawn(async move {
            while let Ok((s, _)) = l.accept().await {
                let a = acceptor.clone();
                tokio::spawn(async move {
                    if let Ok(mut t) = a.accept(s).await {
                        let mut buf = vec![0u8; 8192];
                        let _ = t.read(&mut buf).await;
                        let _ = t.write_all(answer.as_bytes()).await;
                        let _ = t.shutdown().await;
                    }
                });
            }
        });
        port
    }

    async fn get(cfg: ClientConfig, port: u16) -> Result<(), std::io::Error> {
        let tcp = tokio::net::TcpStream::connect(("127.0.0.1", port)).await?;
        let name = ServerName::try_from("localhost").map_err(std::io::Error::other)?;
        let mut t = tokio_rustls::TlsConnector::from(Arc::new(cfg)).connect(name, tcp).await?;
        t.write_all(b"GET / HTTP/1.0\r\n\r\n").await?;
        let mut out = Vec::new();
        t.read_to_end(&mut out).await?;
        Ok(())
    }

    #[tokio::test]
    async fn pinned_accepts_only_its_certificate() {
        let (cert, key) = self_signed();
        let pin = cert_pin(&cert);
        let port = serve(cert, key, "HTTP/1.0 200 OK\r\n\r\n").await;
        assert!(get(client_config(&Trust::Pinned(pin)).unwrap(), port).await.is_ok());
        let other = cert_pin(b"another");
        let err = get(client_config(&Trust::Pinned(other)).unwrap(), port).await.unwrap_err().to_string();
        assert!(err.contains("it changed"), "{err}");
        // the system does not trust a self-signed certificate
        assert!(get(client_config(&Trust::System).unwrap(), port).await.is_err());
    }

    #[tokio::test]
    async fn pinned_certificate_without_its_key_fails() {
        let (cert, _) = self_signed();
        let (_, other_key) = self_signed();
        let pin = cert_pin(&cert);
        let port = serve(cert, other_key, "HTTP/1.0 200 OK\r\n\r\n").await;
        assert!(get(client_config(&Trust::Pinned(pin)).unwrap(), port).await.is_err());
    }

    #[tokio::test]
    async fn probe_reads_an_untrusted_certificate() {
        let (cert, key) = self_signed();
        let pin = cert_pin(&cert);
        let port = serve(cert, key, "HTTP/1.0 200 OK\r\n\r\n").await;
        let c = probe(&format!("https://localhost:{port}")).await.unwrap();
        assert_eq!(c.pin, pin);
        assert!(!c.trusted);
        assert!(c.subject.contains("rcgen") || !c.subject.is_empty(), "{}", c.subject);
        assert!(c.not_after.is_some());
    }

    #[test]
    fn only_https_urls() {
        assert!(matches!(https_host("http://p:8443"), Err(TlsError::Url)));
        assert!(matches!(https_host("panel"), Err(TlsError::Url)));
        assert_eq!(https_host("https://p.domain.local").unwrap(), ("p.domain.local".into(), 443));
        assert_eq!(https_host("https://[::1]:8443").unwrap(), ("::1".into(), 8443));
        assert!(cert_pin(b"x").starts_with("sha256:") && cert_pin(b"x").len() == 7 + 64);
    }
}
