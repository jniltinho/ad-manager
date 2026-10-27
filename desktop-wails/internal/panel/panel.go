// Package panel is the desktop's only way to the AD Manager panel: a reverse proxy for the SPA's same-origin /api
// requests and the client behind it (https only, the system's roots or exactly the pinned certificate, no redirect
// ever followed, the session cookie kept in Go and never given to the webview).
package panel

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ErrRedirect is a panel answer that redirects: the desktop does not follow it.
var ErrRedirect = errors.New("the panel answered with a redirect, which is not followed: check the panel URL")

// ErrCertChanged is a panel certificate other than the one the operator trusted in Configure.
var ErrCertChanged = errors.New("the panel's certificate is not the one trusted in Configure (it changed): check it with the panel's administrator, then trust it again in Configure")

// ErrURL is a panel URL that is not https://server[:port].
var ErrURL = errors.New("Panel URL: enter an https://server:port URL") //nolint:staticcheck // the field's name starts the message

// Panel is the configured panel and its current session (cookie jar). A sign-in or a sign-out starts a new session:
// the requests still running for the previous one are cancelled.
type Panel struct {
	base *url.URL
	tls  *tls.Config

	mu     sync.Mutex
	hc     *http.Client
	ctx    context.Context
	cancel context.CancelFunc
}

// New is the panel at rawURL. A non-empty fingerprint ("sha256:<hex>") accepts exactly that certificate; insecure
// (a legacy desktop.toml without a pin) accepts any; otherwise the system's roots verify it.
func New(rawURL, fingerprint string, insecure bool) (*Panel, error) {
	u, err := url.Parse(strings.TrimRight(rawURL, "/"))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, ErrURL
	}
	p := &Panel{base: u, tls: &tls.Config{MinVersion: tls.VersionTLS12}}
	switch {
	case fingerprint != "":
		p.tls = PinnedTLS(fingerprint)
	case insecure:
		p.tls.InsecureSkipVerify = true //nolint:gosec // the legacy "accept any certificate" of an unpinned file
	}
	p.reset()
	return p, nil
}

// URL is the panel's base URL (no trailing slash).
func (p *Panel) URL() string { return p.base.String() }

// PinnedTLS accepts only the leaf certificate whose fingerprint is pin, on every connection (resumed ones too). Go
// still verifies the handshake's signature with that certificate's key.
func PinnedTLS(pin string) *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true, //nolint:gosec // checked against the pin below
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 || CertPin(cs.PeerCertificates[0].Raw) != pin {
				return ErrCertChanged
			}
			return nil
		}}
}

// CertPin is the fingerprint pinned for a certificate: "sha256:" and the hex SHA-256 of its DER.
func CertPin(der []byte) string {
	sum := sha256.Sum256(der)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// reset starts a new session: a new cookie jar and context; the previous session's requests are cancelled.
func (p *Panel) reset() {
	jar, _ := cookiejar.New(nil)
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	// no whole-request timeout: a backup or an import may stream for minutes, as in a browser
	hc := &http.Client{Jar: jar,
		Transport: &http.Transport{TLSClientConfig: p.tls, DialContext: d.DialContext, TLSHandshakeTimeout: 10 * time.Second,
			ForceAttemptHTTP2: true, MaxIdleConnsPerHost: 8},
		// never followed: a redirect could send the session elsewhere, or over plain HTTP
		CheckRedirect: func(*http.Request, []*http.Request) error { return ErrRedirect }}
	ctx, cancel := context.WithCancel(context.Background())
	p.mu.Lock()
	old := p.cancel
	p.hc, p.ctx, p.cancel = hc, ctx, cancel
	p.mu.Unlock()
	if old != nil {
		old()
	}
}

// session is the current client and its context.
func (p *Panel) session() (*http.Client, context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hc, p.ctx
}

// Middleware routes the asset server's requests: /api/… to the panel, everything else to next (the embedded SPA and
// Wails' own /wails/ runtime).
func (p *Panel) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api" {
			next.ServeHTTP(w, r)
			return
		}
		p.ServeHTTP(w, r)
	})
}

// apiPath is the panel path for a request, or false when the path could reach outside /api/ (dot segments, encoded
// slashes or backslashes): the browser normalizes "..", a raw request to the handler may not.
func apiPath(u *url.URL) (string, bool) {
	raw := u.EscapedPath()
	low := strings.ToLower(raw)
	if !strings.HasPrefix(raw, "/api/") || strings.Contains(low, "%2f") || strings.Contains(low, "%5c") ||
		strings.Contains(u.Path, `\`) {
		return "", false
	}
	for _, seg := range strings.Split(u.Path, "/") {
		if seg == "." || seg == ".." {
			return "", false
		}
	}
	return raw, true
}

// hop-by-hop and session headers never passed between the webview and the panel.
var dropRequest = []string{"Cookie", "Connection", "Keep-Alive", "Proxy-Connection", "Te", "Trailer",
	"Transfer-Encoding", "Upgrade", "Origin", "Referer"}
var dropResponse = []string{"Set-Cookie", "Connection", "Keep-Alive", "Proxy-Connection", "Trailer",
	"Transfer-Encoding", "Upgrade", "Strict-Transport-Security", "Alt-Svc"}

// ServeHTTP forwards one /api request to the panel with the current session.
func (p *Panel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path, ok := apiPath(r.URL)
	if !ok {
		http.NotFound(w, r)
		return
	}
	login := r.Method == http.MethodPost && path == "/api/login"
	if login {
		p.reset()
	}
	hc, sctx := p.session()
	ctx, cancel := context.WithCancel(sctx)
	defer cancel()
	stop := context.AfterFunc(r.Context(), cancel)
	defer stop()

	target := p.base.String() + path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, target, r.Body)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	req.ContentLength = r.ContentLength
	req.Header = r.Header.Clone()
	for _, h := range dropRequest {
		req.Header.Del(h)
	}
	res, err := hc.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, cause(err))
		return
	}
	defer res.Body.Close()
	if r.Method == http.MethodPost && path == "/api/logout" {
		defer p.reset()
	}
	for k, v := range res.Header {
		w.Header()[k] = v
	}
	for _, h := range dropResponse {
		w.Header().Del(h)
	}
	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, res.Body)
}

// cause is the error to show for a failed request: the desktop's own messages unwrapped from url.Error.
func cause(err error) error {
	for _, e := range []error{ErrRedirect, ErrCertChanged} {
		if errors.Is(err, e) {
			return e
		}
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// writeError answers as the panel does: {"error": "…"}, which the SPA's api() shows.
func writeError(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
