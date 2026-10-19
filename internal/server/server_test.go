package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ad-manager/internal/ad"
	"ad-manager/internal/audit"
	"ad-manager/internal/config"
)

// fakeDir implements only what the tests use; anything else panics.
type fakeDir struct {
	directory
	admin bool
	reset func(sam, pwd string) error
}

func (f *fakeDir) Close()                 {}
func (f *fakeDir) NetBIOS() string        { return "EMPRESA" }
func (f *fakeDir) IsAdmin() (bool, error) { return f.admin, nil }
func (f *fakeDir) Users(string) ([]ad.User, error) {
	return []ad.User{{Sam: "joao", Name: "João"}}, nil
}
func (f *fakeDir) Export() (*ad.CSVSet, error) {
	return &ad.CSVSet{Groups: [][]string{{"Financeiro", "security", "global", "", "CN=Users", ""}}}, nil
}
func (f *fakeDir) Delete(string) error               { return ad.ErrProtected }
func (f *fakeDir) User(sam string) (*ad.User, error) { return nil, ad.ErrNotFound }
func (f *fakeDir) ResetPassword(sam, pwd string, _ bool) error {
	if f.reset != nil {
		return f.reset(sam, pwd)
	}
	return nil
}

const goodPassword = "Right-Password-1"

// newTest builds a Server whose directory accepts only goodPassword.
func newTest(t *testing.T, admin bool) (*Server, http.Handler) {
	t.Helper()
	cfg := &config.Config{AD: ad.Config{Domain: "empresa.test", URL: "ldaps://127.0.0.1:1"}, Server: config.Server{SessionMinutes: 30}}
	s := &Server{cfg: cfg, sessions: map[string]*session{}, fails: map[string][]time.Time{}}
	s.dial = func(_ *ad.Config, user, pass string) (directory, error) {
		if pass != goodPassword {
			return nil, ad.ErrBadLogin
		}
		return &fakeDir{admin: admin}, nil
	}
	return s, s.routes()
}

// do sends a request to h and records the response.
func do(h http.Handler, method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "10.0.0.9:5000"
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// login logs in and returns the session cookie.
func login(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	rec := do(h, http.MethodPost, "/api/login", `{"user":"admin","password":"`+goodPassword+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == cookieName {
			return c
		}
	}
	t.Fatal("login without a session cookie")
	return nil
}

func TestLogin(t *testing.T) {
	tests := []struct {
		name   string
		admin  bool
		pass   string
		status int
	}{
		{"administrator gets in", true, goodPassword, http.StatusOK},
		{"wrong password", true, "x", http.StatusUnauthorized},
		{"regular user is denied", false, goodPassword, http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, h := newTest(t, tt.admin)
			rec := do(h, http.MethodPost, "/api/login", `{"user":"u","password":"`+tt.pass+`"}`)
			if rec.Code != tt.status {
				t.Fatalf("status %d, want %d (%s)", rec.Code, tt.status, rec.Body)
			}
			cookie := rec.Header().Get("Set-Cookie")
			if tt.status != http.StatusOK && cookie != "" {
				t.Fatalf("a refused login must not create a session: %s", cookie)
			}
			if tt.status == http.StatusOK {
				for _, attr := range []string{"HttpOnly", "Secure", "SameSite=Strict"} {
					if !strings.Contains(cookie, attr) {
						t.Errorf("cookie without %s: %s", attr, cookie)
					}
				}
			}
		})
	}
}

func TestRateLimit(t *testing.T) {
	_, h := newTest(t, true)
	for range maxFails {
		do(h, http.MethodPost, "/api/login", `{"user":"u","password":"x"}`)
	}
	if rec := do(h, http.MethodPost, "/api/login", `{"user":"u","password":"`+goodPassword+`"}`); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("6th attempt: %d, want 429", rec.Code)
	}
}

func TestSession(t *testing.T) {
	s, h := newTest(t, true)
	if rec := do(h, http.MethodGet, "/api/users", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without cookie: %d", rec.Code)
	}
	c := login(t, h)
	if rec := do(h, http.MethodGet, "/api/users", "", c); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "joao") {
		t.Fatalf("with cookie: %d %s", rec.Code, rec.Body)
	}
	t.Run("expires when idle", func(t *testing.T) {
		s.mu.Lock()
		s.sessions[c.Value].seen = time.Now().Add(-31 * time.Minute)
		s.mu.Unlock()
		if rec := do(h, http.MethodGet, "/api/users", "", c); rec.Code != http.StatusUnauthorized {
			t.Fatalf("expired session: %d", rec.Code)
		}
	})
	t.Run("logout invalidates", func(t *testing.T) {
		c := login(t, h)
		do(h, http.MethodPost, "/api/logout", "", c)
		if rec := do(h, http.MethodGet, "/api/users", "", c); rec.Code != http.StatusUnauthorized {
			t.Fatalf("after logout: %d", rec.Code)
		}
	})
}

func TestDirectoryErrors(t *testing.T) {
	_, h := newTest(t, true)
	c := login(t, h)
	tests := []struct {
		name, method, path string
		status             int
	}{
		{"protected becomes 403", http.MethodDelete, "/api/users/Administrator", http.StatusForbidden},
		{"missing becomes 404", http.MethodGet, "/api/users/nobody", http.StatusNotFound},
		{"unknown API is 404, not the SPA", http.MethodGet, "/api/nothing", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := do(h, tt.method, tt.path, "", c); rec.Code != tt.status {
				t.Fatalf("%d, want %d (%s)", rec.Code, tt.status, rec.Body)
			}
		})
	}
}

func TestExportFileNames(t *testing.T) {
	_, h := newTest(t, true)
	c := login(t, h)
	rec := do(h, http.MethodGet, "/api/export/groups", "", c)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Disposition"), `filename="groups.csv"`) ||
		!strings.Contains(rec.Body.String(), "Financeiro") {
		t.Fatalf("export groups: %d %v %s", rec.Code, rec.Header(), rec.Body)
	}
	if rec := do(h, http.MethodGet, "/api/export/grupos", "", c); rec.Code != http.StatusNotFound {
		t.Fatalf("old name grupos: %d, want 404", rec.Code)
	}
}

func TestAuditHasNoPassword(t *testing.T) {
	var buf bytes.Buffer
	audit.SetWriter(&buf)
	_, h := newTest(t, true)
	c := login(t, h)
	rec := do(h, http.MethodPost, "/api/users/joao/password", `{"password":"New-Secret-Password-9","must_change":true}`, c)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("reset: %d %s", rec.Code, rec.Body)
	}
	log := buf.String()
	if !strings.Contains(log, `"action":"password.reset"`) || !strings.Contains(log, `"target":"joao"`) {
		t.Fatalf("incomplete audit: %s", log)
	}
	if strings.Contains(log, "New-Secret-Password-9") || strings.Contains(log, goodPassword) {
		t.Fatal("password leaked into the audit log")
	}
}

func TestRunRequiresHTTPS(t *testing.T) {
	tests := []struct {
		name   string
		listen string
		trust  bool
		dev    bool
		ok     bool
	}{
		{"HTTP on all interfaces", "0.0.0.0:8443", false, true, false},
		{"loopback without option", "127.0.0.1:8443", false, false, false},
		{"loopback behind a proxy", "127.0.0.1:0", true, false, true},
		{"loopback in development", "127.0.0.1:0", false, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{AD: ad.Config{Domain: "x.test", URL: "ldaps://127.0.0.1:1"},
				Server: config.Server{Listen: tt.listen, TrustProxy: tt.trust, SessionMinutes: 30}}
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			err := Run(ctx, cfg, tt.dev)
			if tt.ok && err != nil {
				t.Fatalf("should start: %v", err)
			}
			if !tt.ok && (err == nil || !strings.Contains(err.Error(), "HTTPS")) {
				t.Fatalf("should refuse for lack of HTTPS, got %v", err)
			}
		})
	}
}
