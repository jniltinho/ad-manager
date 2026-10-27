// Package panel is the desktop's only way to the AD Manager panel: a reverse proxy for the SPA's same-origin /api
// requests and the client behind it (https only, the system's roots or exactly the pinned certificate, no redirect
// ever followed, the session cookie kept in Go and never given to the webview).
package panel

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrRedirect is a panel answer that redirects: the desktop does not follow it.
var ErrRedirect = errors.New("the panel answered with a redirect, which is not followed: check the panel URL")

// ErrCertChanged is a panel certificate other than the one the operator trusted in Configure.
var ErrCertChanged = errors.New("the panel's certificate is not the one trusted in Configure (it changed): check it with the panel's administrator, then trust it again in Configure")

// ErrStale is a request built under a session that has ended (a sign-in or a sign-out came in between).
var ErrStale = errors.New("stale session")

// BridgeCap is the most one request or answer carries through the webview's bridge, which may buffer a whole body
// (WebView2 does): bigger files go through Download and Upload, in Go.
const BridgeCap = 32 << 20

// ErrTooLarge is a body over BridgeCap.
var ErrTooLarge = fmt.Errorf("larger than %d MiB, the most the desktop app passes through its window at once", BridgeCap>>20)

// ErrURL is a panel URL that is not https://server[:port].
var ErrURL = errors.New("Panel URL: enter an https://server:port URL") //nolint:staticcheck // the field's name starts the message

// Panel is the configured panel and its current session (cookie jar). A sign-in or a sign-out starts a new session:
// the requests still running for the previous one are cancelled.
type Panel struct {
	base *url.URL
	tls  *tls.Config

	mu     sync.Mutex
	gen    uint64 // the session's generation: the SPA sends it on every request (X-ADM-Session)
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

// sess is one session: its generation, client (cookie jar) and context.
type sess struct {
	gen uint64
	hc  *http.Client
	ctx context.Context
}

// reset starts a new session and returns it: a new cookie jar and context; the previous session's requests are
// cancelled and its idle connections closed.
func (p *Panel) reset() sess {
	p.mu.Lock()
	s, old, oldHC := p.resetLocked()
	p.mu.Unlock()
	end(old, oldHC)
	return s
}

// resetIf ends session gen only while it is still the current one: a sign-out answering after a newer sign-in leaves
// that one alone.
func (p *Panel) resetIf(gen uint64) {
	p.mu.Lock()
	if p.gen != gen {
		p.mu.Unlock()
		return
	}
	_, old, oldHC := p.resetLocked()
	p.mu.Unlock()
	end(old, oldHC)
}

func (p *Panel) resetLocked() (sess, context.CancelFunc, *http.Client) {
	jar, _ := cookiejar.New(nil)
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	// no whole-request timeout: a backup or an import may stream for minutes, as in a browser; the panel still has to
	// start answering within ResponseHeaderTimeout
	hc := &http.Client{Jar: jar,
		Transport: &http.Transport{TLSClientConfig: p.tls, DialContext: d.DialContext, TLSHandshakeTimeout: 10 * time.Second,
			ResponseHeaderTimeout: 10 * time.Minute, IdleConnTimeout: 90 * time.Second, ForceAttemptHTTP2: true,
			MaxIdleConnsPerHost: 8},
		// never followed: a redirect could send the session elsewhere, or over plain HTTP
		CheckRedirect: func(*http.Request, []*http.Request) error { return ErrRedirect }}
	ctx, cancel := context.WithCancel(context.Background())
	old, oldHC := p.cancel, p.hc
	p.gen++
	p.hc, p.ctx, p.cancel = hc, ctx, cancel
	return sess{p.gen, hc, ctx}, old, oldHC
}

func end(cancel context.CancelFunc, hc *http.Client) {
	if cancel != nil {
		cancel()
		hc.CloseIdleConnections()
	}
}

// Generation is the current session's generation.
func (p *Panel) Generation() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.gen
}

// session is session gen, checked under the same lock that picks it: a request built under an older session never
// runs with the new one.
func (p *Panel) session(gen uint64) (sess, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if gen != p.gen {
		return sess{}, ErrStale
	}
	return sess{p.gen, p.hc, p.ctx}, nil
}

// public are the requests the SPA makes without a session, before sign-in or as an <img>: no generation needed.
func public(method, path string) bool {
	return method == http.MethodPost && path == "/api/login" ||
		method == http.MethodGet && (path == "/api/health" || path == "/api/i18n" || path == "/api/brand/logo")
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
	logout := r.Method == http.MethodPost && path == "/api/logout"
	gen, perr := strconv.ParseUint(r.Header.Get("X-ADM-Session"), 10, 64)
	var se sess
	var err error
	switch {
	case r.Method == http.MethodPost && path == "/api/login":
		se = p.reset()
	case public(r.Method, path):
		se, err = p.session(p.Generation())
	case perr != nil:
		err = ErrStale // no generation
	default:
		se, err = p.session(gen)
	}
	if err != nil {
		w.Header().Set("X-ADM-Stale", "1")
		w.Header().Set("X-ADM-Session", strconv.FormatUint(p.Generation(), 10))
		writeError(w, http.StatusConflict, err)
		return
	}
	if logout {
		// whatever the panel answers, this session is over (unless a newer sign-in already replaced it)
		defer p.resetIf(se.gen)
	}
	ctx, cancel := context.WithCancel(se.ctx)
	defer cancel()
	stop := context.AfterFunc(r.Context(), cancel)
	defer stop()

	if r.ContentLength > BridgeCap {
		writeError(w, http.StatusRequestEntityTooLarge, ErrTooLarge)
		return
	}
	target := p.base.String() + path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, target, http.MaxBytesReader(w, r.Body, BridgeCap))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	req.ContentLength = r.ContentLength
	req.Header = r.Header.Clone()
	req.Header.Del("X-ADM-Session")
	for _, h := range dropRequest {
		req.Header.Del(h)
	}
	res, err := se.hc.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, cause(err))
		return
	}
	defer res.Body.Close()
	if res.ContentLength > BridgeCap {
		writeError(w, http.StatusBadGateway, ErrTooLarge)
		return
	}
	var body io.Reader = res.Body
	if res.ContentLength < 0 || logout {
		// an answer of unknown length is read whole (up to the cap) before anything is committed, so a failure or an
		// answer over the cap becomes an error instead of a cut body with the panel's status; a logout's is read
		// before its session ends
		b, err := io.ReadAll(io.LimitReader(res.Body, BridgeCap+1))
		switch {
		case err != nil:
			writeError(w, http.StatusBadGateway, cause(err))
			return
		case len(b) > BridgeCap:
			writeError(w, http.StatusBadGateway, ErrTooLarge)
			return
		}
		body = bytes.NewReader(b)
	}
	for k, v := range res.Header {
		w.Header()[k] = v
	}
	for _, h := range dropResponse {
		w.Header().Del(h)
	}
	if logout {
		p.resetIf(se.gen)
	}
	w.Header().Set("X-ADM-Session", strconv.FormatUint(p.Generation(), 10))
	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, body)
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
