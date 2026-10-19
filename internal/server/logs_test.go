package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ad-manager/internal/audit"
)

// TestRequestLog: API calls go to the access log with the operator, password
// link tokens are redacted and SPA files are skipped.
func TestRequestLog(t *testing.T) {
	p := filepath.Join(t.TempDir(), "access.log")
	f, err := audit.OpenAccess(p)
	if err != nil {
		t.Fatal(err)
	}
	defer audit.OpenAccess("") //nolint:errcheck // back to discarding
	s, h := newTest(t, true)
	s.cfg.Server.AccessLog = p
	ck := login(t, h)
	do(h, http.MethodGet, "/api/users", "", ck)
	do(h, http.MethodGet, "/api/password-link/0123456789abcdef", "")
	do(h, http.MethodGet, "/assets/index.js", "")
	f.Close()

	b, _ := os.ReadFile(p)
	log := string(b)
	if strings.Contains(log, "0123456789abcdef") || !strings.Contains(log, `"/api/password-link/***"`) {
		t.Fatalf("token not redacted:\n%s", log)
	}
	if strings.Contains(log, "/assets/") {
		t.Fatalf("static file logged:\n%s", log)
	}

	rec := do(h, http.MethodGet, "/api/http-log?q=users", "", ck)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"user":"admin"`) {
		t.Fatalf("http-log: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, http.MethodGet, "/api/http-log", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("http-log without session: %d", rec.Code)
	}
}
