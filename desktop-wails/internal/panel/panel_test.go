package panel

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

// fakePanel is an HTTPS panel: /api/login sets a session cookie, /api/me needs it, /api/redirect redirects.
func fakePanel(t *testing.T, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/api/login":
			http.SetCookie(w, &http.Cookie{Name: "adm_session", Value: "s3cr3t", Path: "/", HttpOnly: true, Secure: true})
			_, _ = io.WriteString(w, `{"ok":true}`)
		case "/api/me":
			if c, err := r.Cookie("adm_session"); err != nil || c.Value != "s3cr3t" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `{"user":"administrator","cookies":"`+r.Header.Get("Cookie")+`"}`)
		case "/api/redirect":
			http.Redirect(w, r, "https://elsewhere.example/", http.StatusFound)
		default:
			_, _ = io.WriteString(w, r.URL.Path)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func do(t *testing.T, p *Panel, method, target string, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, target, nil)
	if cookie != "" {
		r.Header.Set("Cookie", cookie)
	}
	w := httptest.NewRecorder()
	p.Middleware(http.NotFoundHandler()).ServeHTTP(w, r)
	return w
}

func pinned(t *testing.T, s *httptest.Server) *Panel {
	t.Helper()
	p, err := New(s.URL, CertPin(s.Certificate().Raw), false)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSessionStaysInGo(t *testing.T) {
	var hits atomic.Int32
	p := pinned(t, fakePanel(t, &hits))
	w := do(t, p, http.MethodPost, "/api/login", "")
	if w.Code != 200 || w.Header().Get("Set-Cookie") != "" {
		t.Fatalf("login: %d, Set-Cookie %q", w.Code, w.Header().Get("Set-Cookie"))
	}
	// the webview's own cookie is dropped; the Go jar's is the only one sent
	w = do(t, p, http.MethodGet, "/api/me", "adm_session=forged; other=1")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"cookies":"adm_session=s3cr3t"`) {
		t.Fatalf("me: %d %s", w.Code, w.Body)
	}
	// sign-out ends the session
	do(t, p, http.MethodPost, "/api/logout", "")
	if w = do(t, p, http.MethodGet, "/api/me", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("after logout: %d", w.Code)
	}
}

func TestRedirectNotFollowed(t *testing.T) {
	var hits atomic.Int32
	p := pinned(t, fakePanel(t, &hits))
	w := do(t, p, http.MethodGet, "/api/redirect", "")
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), ErrRedirect.Error()) {
		t.Fatalf("redirect: %d %s", w.Code, w.Body)
	}
}

func TestPinnedCertificateOnly(t *testing.T) {
	var hits atomic.Int32
	s := fakePanel(t, &hits)
	p, _ := New(s.URL, CertPin([]byte("another certificate")), false) // httptest servers all share one certificate
	before := hits.Load()
	w := do(t, p, http.MethodGet, "/api/me", "")
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "it changed") || hits.Load() != before {
		t.Fatalf("changed certificate: %d %s (hits %d→%d)", w.Code, w.Body, before, hits.Load())
	}
	// without a pin the system roots refuse a self-signed certificate
	p, _ = New(s.URL, "", false)
	if w = do(t, p, http.MethodGet, "/api/me", ""); w.Code != http.StatusBadGateway {
		t.Fatalf("unpinned self-signed: %d", w.Code)
	}
}

func TestPathConfinedToAPI(t *testing.T) {
	var hits atomic.Int32
	p := pinned(t, fakePanel(t, &hits))
	for _, raw := range []string{"/api/%2e%2e/asynqmon", "/api/users%2F..%2Fx", "/api/a/../../asynqmon", `/api/a\b`} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.URL = &url.URL{Path: "/api/x", RawPath: raw}
		if u, err := url.Parse(raw); err == nil {
			r.URL = u
		}
		w := httptest.NewRecorder()
		before := hits.Load()
		p.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound || hits.Load() != before {
			t.Errorf("%s: %d, panel hit %v", raw, w.Code, hits.Load() != before)
		}
	}
	// outside /api the request goes to the SPA, never to the panel
	before := hits.Load()
	if w := do(t, p, http.MethodGet, "/asynqmon", ""); w.Code != http.StatusNotFound || hits.Load() != before {
		t.Fatalf("/asynqmon reached the panel")
	}
	if w := do(t, p, http.MethodGet, "/api/users?q=a%20b", ""); w.Body.String() != "/api/users" {
		t.Fatalf("plain path: %s", w.Body)
	}
}

func TestOnlyHTTPS(t *testing.T) {
	for _, u := range []string{"http://panel:8443", "panel:8443", "https://", "https://u:p@panel", "https://panel/?x=1"} {
		if _, err := New(u, "", false); err != ErrURL {
			t.Errorf("%s: %v", u, err)
		}
	}
}
