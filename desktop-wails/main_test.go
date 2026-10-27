package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ad-manager/desktop-wails/internal/config"
	"ad-manager/desktop-wails/internal/panel"
)

// testPanel is a self-signed HTTPS panel: health (setup when setup is true), login (password "right"), me.
func testPanel(t *testing.T, setup bool) *httptest.Server {
	t.Helper()
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"setup": setup, "domain": "domain.local"})
		case "/api/login":
			var in struct{ User, Password string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in.Password != "right" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"error":"wrong user or password"}`)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "s", Value: "1", Path: "/"})
		case "/api/me":
			if _, err := r.Cookie("s"); err != nil {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `{"user":"administrator","domain":"domain.local","version":"v0.26.1"}`)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func newTestDesktop(t *testing.T) *desktop {
	t.Helper()
	return newDesktop(config.File{Path: filepath.Join(t.TempDir(), "desktop.toml"), Create: true})
}

func postTest(t *testing.T, d *desktop, req testRequest) TestResult {
	t.Helper()
	b, _ := json.Marshal(req)
	w := httptest.NewRecorder()
	d.routes(http.NotFoundHandler()).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/desktop/test", strings.NewReader(string(b))))
	var res TestResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("test answer %d %s", w.Code, w.Body)
	}
	return res
}

func TestConfigureTrustTestSave(t *testing.T) {
	s := testPanel(t, false)
	d := newTestDesktop(t)
	c := &Configure{d: d}
	if st := c.State(); st.Configured || st.User != "administrator" {
		t.Fatalf("first run: %+v", st)
	}
	res := postTest(t, d, testRequest{ID: 1, URL: s.URL, User: "administrator", Password: "right"})
	if res.OK || res.Cert == nil || res.Cert.Trusted || !strings.HasPrefix(res.Cert.Pin, "sha256:") {
		t.Fatalf("untrusted certificate: %+v", res)
	}
	if _, err := os.Stat(d.file.Path); !os.IsNotExist(err) {
		t.Fatal("a file was written before Save")
	}
	if err := c.Save(1, "", "", "", false); err == nil {
		t.Fatal("Save without a successful test")
	}
	res = postTest(t, d, testRequest{ID: 2, URL: s.URL, User: "administrator", Password: "right", Trust: res.Cert.Pin})
	if !res.OK || res.Domain != "domain.local" || res.Version != "v0.26.1" {
		t.Fatalf("trusted test: %+v", res)
	}
	if err := c.Save(2, "pt-BR", "matrix", "", false); err != nil {
		t.Fatal(err)
	}
	st := c.State()
	if !st.Configured || st.Fingerprint == "" || st.Lang != "pt-BR" {
		t.Fatalf("after save: %+v", st)
	}
	b, _ := os.ReadFile(d.file.Path)
	if strings.Contains(string(b), "right") {
		t.Fatal("the password reached the file")
	}
	// the tested session is the app's: /api/me through the proxy answers without a new sign-in
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	r.Header.Set("X-ADM-Session", "2")
	d.routes(http.NotFoundHandler()).ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "administrator") {
		t.Fatalf("me after save: %d %s", w.Code, w.Body)
	}
}

func TestConfigureErrors(t *testing.T) {
	d := newTestDesktop(t)
	if res := postTest(t, d, testRequest{ID: 1, URL: "http://panel:8443", User: "a", Password: "b"}); res.Error != panel.ErrURL.Error() {
		t.Fatalf("plain http: %+v", res)
	}
	s := testPanel(t, true)
	pin := panel.CertPin(s.Certificate().Raw)
	if res := postTest(t, d, testRequest{ID: 2, URL: s.URL, User: "a", Password: "right", Trust: pin}); res.Error != errSetup.Error() {
		t.Fatalf("panel in setup: %+v", res)
	}
	s = testPanel(t, false)
	if res := postTest(t, d, testRequest{ID: 3, URL: s.URL, User: "a", Password: "wrong", Trust: pin}); res.Error != "wrong user or password" {
		t.Fatalf("wrong password: %+v", res)
	}
}

func TestTestAfterCancelIsDropped(t *testing.T) {
	s := testPanel(t, false)
	d := newTestDesktop(t)
	pin := panel.CertPin(s.Certificate().Raw)
	req := testRequest{ID: 5, URL: s.URL, User: "administrator", Password: "right", Trust: pin}
	d.mu.Lock()
	d.testID = 5
	d.mu.Unlock()
	res, tr := runTest(t.Context(), req)
	if !res.OK || tr == nil {
		t.Fatalf("test: %+v", res)
	}
	(&Configure{d: d}).Cancel(5)
	// a test that finishes after its cancel is reported stale and not adopted
	if res := postTest(t, d, testRequest{ID: 6, URL: s.URL, User: "administrator", Password: "right", Trust: pin}); !res.OK {
		t.Fatalf("new test: %+v", res)
	}
	(&Configure{d: d}).Cancel(6)
	if err := (&Configure{d: d}).Save(6, "", "", "", false); err == nil {
		t.Fatal("saved a cancelled test")
	}
}

func TestRoutes(t *testing.T) {
	d := newTestDesktop(t)
	h := d.routes(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "spa "+r.URL.Path) }))
	get := func(method, target, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, target, strings.NewReader(body)))
		return w
	}
	if w := get(http.MethodGet, "/users", ""); w.Body.String() != "spa /" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-src 'none'") {
		t.Fatalf("history fallback: %s %v", w.Body, w.Header())
	}
	if w := get(http.MethodGet, "/assets/missing.js", ""); w.Body.String() != "spa /assets/missing.js" {
		t.Fatalf("asset: %s", w.Body)
	}
	if w := get(http.MethodGet, "/api/me", ""); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("api before configure: %d", w.Code)
	}
	if w := get(http.MethodPost, "/wails/runtime", `{"object":9,"method":0,"args":{"url":"https://evil.example"}}`); w.Code != http.StatusForbidden {
		t.Fatalf("runtime Browser call: %d", w.Code)
	}
	if w := get(http.MethodPost, "/wails/runtime?object=9", ""); w.Code != http.StatusForbidden {
		t.Fatalf("runtime Browser call by query: %d", w.Code)
	}
	if w := get(http.MethodPost, "/wails/runtime", `{"object":0,"method":0}`); w.Body.String() != "spa /wails/runtime" {
		t.Fatalf("runtime call: %d %s", w.Code, w.Body)
	}
	if policy() != csp || strings.Count(csp, "connect-src") != 1 {
		t.Fatal("one connect-src")
	}
}

func TestDevPolicyHasOneConnectSrc(t *testing.T) {
	t.Setenv("FRONTEND_DEVSERVER_URL", "http://127.0.0.1:9245")
	p := policy()
	if devServer() != "" && (strings.Count(p, "connect-src") != 1 || !strings.HasSuffix(p, "connect-src 'self' ws://127.0.0.1:9245")) {
		t.Fatalf("dev policy: %s", p)
	}
}
