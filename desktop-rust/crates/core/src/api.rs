//! The panel client (session cookie). `via(id)` is the same session for one file server's agent API through the
//! panel's proxy: the app never talks to an agent itself nor holds its token. Redirects are never followed.

use std::path::Path;
use std::sync::Arc;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::Duration;

use futures_util::StreamExt;
use reqwest::{Method, StatusCode};
use secrecy::{ExposeSecret, SecretString};
use serde::Serialize;
use serde::de::DeserializeOwned;
use tokio::io::AsyncWriteExt;
use zeroize::Zeroizing;

use crate::i18n::{server_text, t};
use crate::model::Health;
use crate::tls::{self, Trust};

/// What a request ends in when it fails. Texts are kept in English and translated when displayed, so a catalog that
/// arrives later still translates an error already on screen.
#[derive(Debug, Clone, PartialEq, thiserror::Error)]
pub enum ApiError {
    /// The panel answered 401 to anything but the sign-in: back to the sign-in screen.
    #[error("{}", t("session expired: sign in again"))]
    SessionExpired,
    /// One of this app's own messages (an English source string of the catalog).
    #[error("{}", t(.0))]
    App(&'static str),
    /// The panel's or agent's `{"error": …}` text, translated with the panel's catalog.
    #[error("{}", server_text(.0))]
    Server(String),
    /// Anything else (transport, HTTP status, decoding), shown as is.
    #[error("{0}")]
    Other(String),
}

impl ApiError {
    fn msg(s: impl Into<String>) -> Self {
        Self::Other(s.into())
    }
}

pub type ApiResult<T> = Result<T, ApiError>;

const REDIRECT: &str = "the panel answered with a redirect, which is not followed: check the panel URL";
const MAX_BODY: usize = 8 << 20;

static NEXT_ID: AtomicU64 = AtomicU64::new(1);

/// A panel session. Clones share the cookie jar; `id` tells sessions apart (a result for a session that was replaced
/// meanwhile is dropped).
#[derive(Clone)]
pub struct Api {
    pub base: String,
    prefix: String,
    client: reqwest::Client,
    timeout: Option<Duration>,
    pub id: u64,
    /// A bearer token (tests); the app always uses the session cookie.
    token: Arc<str>,
    /// Set by any answer that says the session ended (401), whoever made the call: the app goes back to sign-in.
    expired: Arc<std::sync::atomic::AtomicBool>,
}

impl std::fmt::Debug for Api {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Api").field("base", &self.base).field("prefix", &self.prefix).field("id", &self.id).finish()
    }
}

impl Api {
    /// Whether an answer said this session ended (also to a call whose error the caller showed or ignored).
    pub fn expired(&self) -> bool {
        self.expired.load(Ordering::Relaxed)
    }

    fn check(&self, status: StatusCode, path: &str, data: &[u8]) -> ApiResult<()> {
        let r = check(status, path, data, self.token.is_empty());
        if r == Err(ApiError::SessionExpired) {
            self.expired.store(true, Ordering::Relaxed);
        }
        r
    }

    pub fn new(base: &str, trust: &Trust) -> ApiResult<Self> {
        let cfg = tls::client_config(trust).map_err(|e| ApiError::msg(e.to_string()))?;
        let client = reqwest::Client::builder()
            .tls_backend_preconfigured(cfg)
            // never plain HTTP, whatever the configuration file says: the password goes over this client
            .https_only(true)
            .redirect(reqwest::redirect::Policy::none())
            .cookie_store(true)
            .connect_timeout(Duration::from_secs(15))
            .build()
            .map_err(|e| ApiError::msg(chain(&e)))?;
        Ok(Self {
            base: base.trim_end_matches('/').to_string(),
            prefix: String::new(),
            client,
            timeout: Some(Duration::from_secs(20)),
            id: NEXT_ID.fetch_add(1, Ordering::Relaxed),
            token: Arc::from(""),
            expired: Default::default(),
        })
    }

    /// The client of the configured panel: exactly the pinned certificate of a self-signed panel, any certificate for
    /// a legacy config not saved since the pin came in, else the system's checks.
    pub fn for_panel(url: &str, fingerprint: &str, insecure: bool) -> ApiResult<Self> {
        let trust = if !fingerprint.is_empty() {
            Trust::Pinned(fingerprint.into())
        } else if insecure {
            Trust::Any
        } else {
            Trust::System
        };
        Self::new(url, &trust)
    }

    #[cfg(test)]
    pub fn with_token(mut self, token: &str) -> Self {
        self.token = Arc::from(token);
        self
    }

    /// The same session for the agent of the panel's file server `id`: "/api/shares" goes to
    /// "/api/agents/{id}/shares".
    pub fn via(&self, id: i64) -> Self {
        let mut c = self.clone();
        c.prefix = format!("/api/agents/{id}");
        c
    }

    /// A copy whose requests may take up to `d` (a folder count, a connection test).
    pub fn with_timeout(&self, d: Duration) -> Self {
        let mut c = self.clone();
        c.timeout = Some(d);
        c
    }

    fn url(&self, path: &str) -> (String, String) {
        let path = match path.strip_prefix("/api") {
            Some(rest) if !self.prefix.is_empty() && rest.starts_with('/') => format!("{}{rest}", self.prefix),
            _ => path.to_string(),
        };
        (format!("{}{path}", self.base), path)
    }

    fn request(&self, method: Method, url: &str, timeout: Option<Duration>) -> reqwest::RequestBuilder {
        let mut r = self.client.request(method, url);
        if let Some(d) = timeout {
            r = r.timeout(d);
        }
        if !self.token.is_empty() {
            r = r.bearer_auth(&*self.token);
        }
        r
    }

    /// Sends `body` as JSON to `path` and returns the raw answer; error answers become errors with the server's
    /// message (translated).
    async fn call(&self, method: Method, path: &str, body: Option<&(dyn erased::Json + Sync)>) -> ApiResult<Vec<u8>> {
        let (url, path) = self.url(path);
        let mut r = self.request(method, &url, self.timeout).header("Content-Type", "application/json");
        if let Some(b) = body {
            let body = b.to_json();
            r = r.body(body.to_vec()); // ours is wiped when dropped (it may carry a password)
        }
        let resp = r.send().await.map_err(|e| transport(&e))?;
        let status = resp.status();
        let data = read_capped(resp).await?;
        self.check(status, &path, &data)?;
        Ok(data)
    }

    /// GET path decoded as JSON (a `null` body is the empty value).
    pub async fn get<T: DeserializeOwned + Default>(&self, path: &str) -> ApiResult<T> {
        let data = self.call(Method::GET, path, None).await?;
        decode(&data)
    }

    /// method path with a JSON body, the answer decoded as JSON (empty: the default value).
    pub async fn send<T: DeserializeOwned + Default, B: Serialize + Sync>(
        &self,
        method: Method,
        path: &str,
        body: Option<&B>,
    ) -> ApiResult<T> {
        let data = match body {
            Some(b) => self.call(method, path, Some(&erased::Wrap(b))).await?,
            None => self.call(method, path, None).await?,
        };
        decode(&data)
    }

    /// method path with no body and no answer of interest.
    pub async fn act(&self, method: Method, path: &str) -> ApiResult<()> {
        self.call(method, path, None).await.map(|_| ())
    }

    /// method path with a JSON body and no answer of interest.
    pub async fn act_with<B: Serialize + Sync>(&self, method: Method, path: &str, body: &B) -> ApiResult<()> {
        self.call(method, path, Some(&erased::Wrap(body))).await.map(|_| ())
    }

    /// Signs in (`POST /api/login`) and reads the session (`GET /api/me`). The body that carries the password is
    /// wiped once sent.
    pub async fn login(&self, user: &str, password: &SecretString) -> ApiResult<crate::model::Me> {
        #[derive(Serialize)]
        struct Login<'a> {
            user: &'a str,
            password: &'a str,
        }
        // borrowed, so the only copy of the password outside the secret is this buffer, wiped on drop (reqwest still
        // copies it into its request body, which it frees after sending)
        let body = Zeroizing::new(
            serde_json::to_vec(&Login { user, password: password.expose_secret() })
                .map_err(|e| ApiError::msg(e.to_string()))?,
        );
        let (url, path) = self.url("/api/login");
        let resp = self
            .request(Method::POST, &url, self.timeout)
            .header("Content-Type", "application/json")
            .body(body.to_vec())
            .send()
            .await
            .map_err(|e| transport(&e))?;
        let status = resp.status();
        let data = read_capped(resp).await?;
        self.check(status, &path, &data)?;
        self.get("/api/me").await
    }

    /// Ends the session (errors ignored: the session is being dropped anyway).
    pub async fn logout(&self) {
        let _ = self.act(Method::POST, "/api/logout").await;
    }

    /// The panel's public /api/health: also when it answers 503 (the directory is down, the brand is still there).
    pub async fn health(&self) -> ApiResult<Health> {
        let url = format!("{}/api/health", self.base);
        let resp = self.request(Method::GET, &url, self.timeout).send().await.map_err(|e| transport(&e))?;
        let status = resp.status();
        if status.is_redirection() {
            return Err(ApiError::App(REDIRECT));
        }
        if status != StatusCode::OK && status != StatusCode::SERVICE_UNAVAILABLE {
            return Err(ApiError::msg(format!("/api/health: HTTP {}", status.as_u16())));
        }
        decode(&read_capped(resp).await?)
    }

    /// The panel's public catalog of server messages (`/api/i18n`).
    pub async fn server_catalog(&self) -> ApiResult<crate::i18n::ServerCatalog> {
        self.get("/api/i18n").await
    }

    /// Downloads a PNG, JPEG or SVG of at most 2 MiB (the company logo) with its content type.
    pub async fn image(&self, path: &str) -> ApiResult<(Vec<u8>, String)> {
        let url = format!("{}{path}", self.base);
        let resp = self.request(Method::GET, &url, self.timeout).send().await.map_err(|e| transport(&e))?;
        if resp.status() != StatusCode::OK {
            return Err(ApiError::msg(format!("{path}: HTTP {}", resp.status().as_u16())));
        }
        let ct = resp.headers().get("content-type").and_then(|v| v.to_str().ok()).unwrap_or_default().to_string();
        let mut out = Vec::new();
        let mut s = resp.bytes_stream();
        while let Some(chunk) = s.next().await {
            out.extend_from_slice(&chunk.map_err(|e| transport(&e))?);
            if out.len() > 2 << 20 {
                return Err(ApiError::msg(format!("{path}: larger than 2 MiB")));
            }
        }
        Ok((out, ct))
    }

    /// Copies the body of GET path (not JSON: a CSV, a backup) to the file `to`, with no total timeout.
    pub async fn download(&self, path: &str, to: &Path) -> ApiResult<()> {
        let (url, path) = self.url(path);
        let resp = self.request(Method::GET, &url, None).send().await.map_err(|e| transport(&e))?;
        let status = resp.status();
        if !status.is_success() {
            let data = read_capped(resp).await.unwrap_or_default();
            self.check(status, &path, &data)?;
            return Err(ApiError::msg(format!("{path}: HTTP {}", status.as_u16())));
        }
        let mut f = tokio::fs::File::create(to).await.map_err(|e| ApiError::msg(e.to_string()))?;
        let mut s = resp.bytes_stream();
        while let Some(chunk) = s.next().await {
            f.write_all(&chunk.map_err(|e| transport(&e))?).await.map_err(|e| ApiError::msg(e.to_string()))?;
        }
        f.flush().await.map_err(|e| ApiError::msg(e.to_string()))
    }

    /// The body of GET path as text (a run's log).
    pub async fn get_text(&self, path: &str) -> ApiResult<String> {
        let (url, path) = self.url(path);
        let resp = self.request(Method::GET, &url, None).send().await.map_err(|e| transport(&e))?;
        let status = resp.status();
        let data = read_capped(resp).await?;
        self.check(status, &path, &data)?;
        Ok(String::from_utf8_lossy(&data).into_owned())
    }

    /// Sends the file `from` as the body of a POST (a backup being sent: no total timeout) and decodes the answer.
    /// PUT `path` with one file as the multipart field `field` (the panel's logo); `name` is the file's name, its
    /// content type taken from the extension.
    pub async fn put_file(&self, path: &str, field: &str, name: &str, bytes: Vec<u8>) -> ApiResult<()> {
        let (url, path) = self.url(path);
        let mut r = [0u8; 16];
        getrandom::fill(&mut r).map_err(|e| ApiError::msg(e.to_string()))?;
        let boundary = format!("adm-{}", r.iter().map(|b| format!("{b:02x}")).collect::<String>());
        let mut body = multipart_head(&boundary, field, name);
        body.extend_from_slice(&bytes);
        body.extend_from_slice(format!("\r\n--{boundary}--\r\n").as_bytes());
        let resp = self
            .request(Method::PUT, &url, None)
            .header("Content-Type", format!("multipart/form-data; boundary={boundary}"))
            .body(body)
            .send()
            .await
            .map_err(|e| transport(&e))?;
        let status = resp.status();
        let data = read_capped(resp).await?;
        self.check(status, &path, &data)
    }

    pub async fn upload<T: DeserializeOwned + Default>(&self, path: &str, from: &Path) -> ApiResult<T> {
        let (url, path) = self.url(path);
        let f = tokio::fs::File::open(from).await.map_err(|e| ApiError::msg(e.to_string()))?;
        let size = f.metadata().await.map(|m| m.len()).ok();
        let stream = tokio_util_stream(f);
        let mut r = self
            .request(Method::POST, &url, None)
            .header("Content-Type", "application/octet-stream")
            .body(reqwest::Body::wrap_stream(stream));
        if let Some(n) = size {
            r = r.header("Content-Length", n);
        }
        let resp = r.send().await.map_err(|e| transport(&e))?;
        let status = resp.status();
        let data = read_capped(resp).await?;
        self.check(status, &path, &data)?;
        decode(&data)
    }
}

/// The opening of a one-file multipart body: the boundary, the part's headers and the blank line.
fn multipart_head(boundary: &str, field: &str, name: &str) -> Vec<u8> {
    let ext = name.rsplit('.').next().unwrap_or_default().to_ascii_lowercase();
    let kind = match ext.as_str() {
        "png" => "image/png",
        "jpg" | "jpeg" => "image/jpeg",
        "svg" => "image/svg+xml",
        _ => "application/octet-stream",
    };
    // a quote or line break in the name would break the header: keep only safe characters
    let safe: String = name.chars().filter(|c| !matches!(c, '"' | '\r' | '\n' | '\\')).collect();
    format!(
        "--{boundary}\r\nContent-Disposition: form-data; name=\"{field}\"; filename=\"{safe}\"\r\nContent-Type: {kind}\r\n\r\n"
    )
    .into_bytes()
}

/// A file as a stream of chunks for an upload body.
fn tokio_util_stream(
    f: tokio::fs::File,
) -> impl futures_util::Stream<Item = Result<Vec<u8>, std::io::Error>> + Send + 'static {
    futures_util::stream::unfold(f, |mut f| async move {
        use tokio::io::AsyncReadExt;
        let mut buf = vec![0u8; 64 << 10];
        match f.read(&mut buf).await {
            Ok(0) => None,
            Ok(n) => {
                buf.truncate(n);
                Some((Ok(buf), f))
            }
            Err(e) => Some((Err(e), f)),
        }
    })
}

/// What Configure's test found: the panel's certificate (when one was read) and, when it may be used, the signed-in
/// client with the panel's health and the session.
pub struct TestOutcome {
    pub cert: Option<tls::PanelCert>,
    pub result: ApiResult<(Api, Health, crate::model::Me)>,
}

/// Configure's test: the panel's certificate (when the system does not trust it and `pin` is not its fingerprint,
/// nothing else is tried), its health and a real sign-in with the account.
pub async fn test_panel(base: &str, account: &str, password: &SecretString, pin: &str) -> TestOutcome {
    let cert = match tls::probe(base).await {
        Ok(c) => c,
        Err(tls::TlsError::Url) => {
            return TestOutcome { cert: None, result: Err(ApiError::App("enter an https://server:port URL")) };
        }
        Err(e) => return TestOutcome { cert: None, result: Err(ApiError::msg(e.to_string())) },
    };
    let trust = if cert.trusted {
        Trust::System
    } else if cert.pin == pin {
        Trust::Pinned(pin.into())
    } else {
        return TestOutcome { cert: Some(cert), result: Err(ApiError::App("untrusted certificate")) };
    };
    let result = async {
        let client = Api::new(base, &trust)?;
        let h = client.health().await?;
        if h.setup {
            return Err(ApiError::App("the panel is being set up: finish its setup wizard in a browser first"));
        }
        let me = client.login(account, password).await?;
        Ok((client, h, me))
    }
    .await;
    TestOutcome { cert: Some(cert), result }
}

/// Turns a non-2xx answer into its error: 3xx never followed, 401 expired (unless signing in or with a token), the
/// server's `{"error": …}` translated, else "path: HTTP n".
fn check(status: StatusCode, path: &str, data: &[u8], session: bool) -> ApiResult<()> {
    if status.is_redirection() {
        return Err(ApiError::App(REDIRECT));
    }
    if status == StatusCode::UNAUTHORIZED && session && path != "/api/login" {
        return Err(ApiError::SessionExpired);
    }
    if status.as_u16() >= 300 {
        #[derive(serde::Deserialize, Default)]
        struct E {
            #[serde(default)]
            error: String,
        }
        if let Ok(e) = serde_json::from_slice::<E>(data)
            && !e.error.is_empty()
        {
            return Err(ApiError::Server(e.error));
        }
        return Err(ApiError::msg(format!("{path}: HTTP {}", status.as_u16())));
    }
    Ok(())
}

fn decode<T: DeserializeOwned + Default>(data: &[u8]) -> ApiResult<T> {
    if data.iter().all(u8::is_ascii_whitespace) {
        return Ok(T::default());
    }
    serde_json::from_slice::<Option<T>>(data).map(Option::unwrap_or_default).map_err(|e| ApiError::msg(e.to_string()))
}

async fn read_capped(resp: reqwest::Response) -> ApiResult<Vec<u8>> {
    let mut out = Vec::new();
    let mut s = resp.bytes_stream();
    while let Some(chunk) = s.next().await {
        let chunk = chunk.map_err(|e| transport(&e))?;
        let room = MAX_BODY.saturating_sub(out.len());
        out.extend_from_slice(&chunk[..chunk.len().min(room)]);
        if room <= chunk.len() {
            break;
        }
    }
    Ok(out)
}

/// A transport error: the known TLS refusal in the interface language, else the error and its causes.
fn transport(e: &reqwest::Error) -> ApiError {
    let full = chain(e);
    if full.contains(tls::CERT_CHANGED) {
        return ApiError::App(tls::CERT_CHANGED);
    }
    if e.is_timeout() {
        return ApiError::msg(format!("{}: {}", e.url().map(|u| u.path()).unwrap_or_default(), t("timed out")));
    }
    ApiError::msg(full)
}

/// An error and its causes, joined with ": " (repeats dropped).
fn chain(e: &dyn std::error::Error) -> String {
    let mut parts: Vec<String> = vec![e.to_string()];
    let mut cur = e.source();
    while let Some(s) = cur {
        let txt = s.to_string();
        if !parts.iter().any(|p| p.contains(&txt)) {
            parts.push(txt);
        }
        cur = s.source();
    }
    parts.join(": ")
}

/// JSON bodies behind one object-safe trait (the request builder wants bytes).
mod erased {
    use zeroize::Zeroizing;

    pub trait Json {
        fn to_json(&self) -> Zeroizing<Vec<u8>>;
    }
    pub struct Wrap<'a, T: ?Sized>(pub &'a T);
    impl<T: serde::Serialize + ?Sized> Json for Wrap<'_, T> {
        fn to_json(&self) -> Zeroizing<Vec<u8>> {
            Zeroizing::new(serde_json::to_vec(self.0).unwrap_or_default())
        }
    }
}

/// Escapes a URL path segment.
pub fn esc(s: &str) -> String {
    url::form_urlencoded::byte_serialize(s.as_bytes()).collect::<String>().replace('+', "%20")
}

/// Escapes a query value.
pub fn q(s: &str) -> String {
    url::form_urlencoded::byte_serialize(s.as_bytes()).collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tls::tests::{self_signed, serve};

    /// A raw HTTP/1.1 answer with the right length.
    fn resp(status: &str, headers: &str, body: &str) -> &'static str {
        Box::leak(
            format!("HTTP/1.1 {status}\r\n{headers}Content-Length: {}\r\nConnection: close\r\n\r\n{body}", body.len())
                .into_boxed_str(),
        )
    }

    async fn pinned(answer: &'static str) -> Api {
        let (cert, key) = self_signed();
        let pin = tls::cert_pin(&cert);
        let port = serve(cert, key, answer).await;
        Api::for_panel(&format!("https://localhost:{port}"), &pin, false).unwrap()
    }

    #[tokio::test]
    #[expect(clippy::await_holding_lock, reason = "the language is global: the lock keeps English for the whole test")]
    async fn redirect_is_not_followed() {
        let _g = crate::i18n::tests::english();
        let a = pinned(resp("302 Found", "Location: http://evil/\r\n", "")).await;
        let e = a.act(Method::GET, "/api/me").await.unwrap_err().to_string();
        assert!(e.contains("redirect, which is not followed"), "{e}");
        let e = a.health().await.unwrap_err().to_string();
        assert!(e.contains("redirect"), "{e}");
    }

    #[tokio::test]
    #[expect(clippy::await_holding_lock, reason = "the language is global: the lock keeps English for the whole test")]
    async fn unauthorized_is_session_expired_except_login() {
        let _g = crate::i18n::tests::english();
        let a = pinned(resp("401 Unauthorized", "", r#"{"error":"invalid password"}"#)).await;
        assert!(matches!(a.act(Method::GET, "/api/users").await, Err(ApiError::SessionExpired)));
        let e = a.act(Method::POST, "/api/login").await.unwrap_err().to_string();
        assert_eq!(e, "invalid password");
    }

    #[tokio::test]
    async fn json_and_null() {
        let a = pinned(resp("200 OK", "", "null")).await;
        let v: Vec<crate::model::User> = a.get("/api/users").await.unwrap();
        assert!(v.is_empty());
    }

    #[tokio::test]
    async fn health_reads_503() {
        let body = r#"{"domain":"domain.local","brand":{"theme":"classic-blue"}}"#;
        let h = pinned(resp("503 Service Unavailable", "", body)).await.health().await.unwrap();
        assert_eq!((h.domain.as_str(), h.brand.theme.as_str()), ("domain.local", "classic-blue"));
    }

    #[tokio::test]
    async fn plain_http_is_refused() {
        let a = Api::for_panel("http://127.0.0.1:9", "", false).unwrap();
        assert!(a.act(Method::GET, "/api/health").await.is_err());
        assert!(a.health().await.is_err());
    }

    #[test]
    fn via_rewrites_api_paths() {
        let a = Api::new("https://p:8443/", &Trust::Any).unwrap().via(3);
        assert_eq!(a.url("/api/shares").0, "https://p:8443/api/agents/3/shares");
        assert_eq!(esc("a b/c"), "a%20b%2Fc");
    }
}
