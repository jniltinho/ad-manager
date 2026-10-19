package agent

import (
	"context"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ad-manager/internal/backup"
	"ad-manager/internal/config"
	"ad-manager/internal/shares"
)

func TestTokenAndBearer(t *testing.T) {
	p := filepath.Join(t.TempDir(), "agent.token")
	tok, err := NewToken(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("token file mode %v, want 0600", fi.Mode().Perm())
	}
	if b, _ := os.ReadFile(p); strings.Contains(string(b), tok) {
		t.Fatal("the file must not contain the plain-text token")
	}
	hash, err := loadHash(p)
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{cfg: config.Agent{Queue: "q", ShareRoots: []string{"/srv/shares"}}, hash: hash, role: "member server",
		mgr: &shares.Manager{Roots: []string{"/srv/shares"}}}
	h := a.Routes()
	tests := []struct {
		name, auth string
		code       int
	}{
		{"no token", "", http.StatusUnauthorized},
		{"wrong token", "Bearer x", http.StatusUnauthorized},
		{"right token, invalid task", "Bearer " + tok, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(`{"type":"share:create","payload":{"name":"sysvol","path":"/etc"}}`))
			req.Header.Set("Content-Type", "application/json")
			if tt.auth != "" {
				req.Header.Set("Authorization", tt.auth)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.code {
				t.Fatalf("%d, want %d (%s)", rec.Code, tt.code, rec.Body)
			}
		})
	}
	if rec := httptest.NewRecorder(); true {
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("health: %d", rec.Code)
		}
	}
}

// TestGenerateToken: the hash the panel shows is what the agent loads and
// accepts for that token (and only for it).
func TestGenerateToken(t *testing.T) {
	tok, hash := GenerateToken()
	path := filepath.Join(t.TempDir(), "agent.token")
	if err := os.WriteFile(path, []byte(hash+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := loadHash(path)
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{hash: h}
	if !a.tokenOK(tok) || a.tokenOK(tok+"x") {
		t.Fatal("generated hash does not match its token")
	}
}

// TestReadOnlyOnDC: on a domain controller every change is refused and the DC routes answer; elsewhere they do not.
func TestReadOnlyOnDC(t *testing.T) {
	tok, hash := GenerateToken()
	h, _ := hex.DecodeString(hash)
	call := func(a *Agent, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		a.Routes().ServeHTTP(rec, req)
		return rec
	}
	mgr := &shares.Manager{Roots: []string{"/srv/shares"}}
	dc := &Agent{cfg: config.Agent{Queue: "q"}, hash: h, mgr: mgr, role: roleDC}
	for _, tt := range []struct{ name, path, body string }{
		{"task", "/api/tasks", `{"type":"share:delete","payload":{"name":"Financeiro"}}`},
		{"disconnect", "/api/sessions/close", `{"pid":100,"session_id":"1"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if rec := call(dc, http.MethodPost, tt.path, tt.body); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "read only") {
				t.Fatalf("%d %s", rec.Code, rec.Body)
			}
		})
	}
	fs := &Agent{cfg: config.Agent{Queue: "q"}, hash: h, mgr: mgr, role: "member server"}
	if rec := call(fs, http.MethodGet, "/api/dc/replication", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("file server: %d", rec.Code)
	}
	// a role that could not be read: nothing changes either
	unknown := &Agent{cfg: config.Agent{Queue: "q"}, hash: h, mgr: mgr}
	if rec := call(unknown, http.MethodPost, "/api/tasks", `{"type":"share:delete","payload":{"name":"Financeiro"}}`); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "read only") {
		t.Fatalf("unknown role: %d %s", rec.Code, rec.Body)
	}
	if rec := call(dc, http.MethodGet, "/api/health", ""); !strings.Contains(rec.Body.String(), `"dc":true`) {
		t.Fatalf("health: %s", rec.Body)
	}
}

func TestRoleFromSecurity(t *testing.T) {
	for sec, want := range map[string]string{"user": "standalone server", "USER": "standalone server", "ads": "member server",
		"domain": "member server", "": "auto", "share": "auto"} {
		if got := roleFromSecurity(sec); got != want {
			t.Errorf("roleFromSecurity(%q) = %q, want %q", sec, got, want)
		}
	}
}

func TestOverlaps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		a, b string
		want bool
	}{
		{"/srv/shares", "/var/lib/samba", false},
		{"/var/lib/samba/sysvol/x", "/var/lib/samba", true},
		{"/var/lib", "/var/lib/samba", true},
		{"/srv/shares", "/srv/shares", true},
		{"/srv/shares2", "/srv/shares", false},
		{"/", "/var/lib/samba", true},
	}
	for _, tt := range tests {
		if got := overlaps(tt.a, tt.b); got != tt.want {
			t.Errorf("overlaps(%q, %q) = %v", tt.a, tt.b, got)
		}
	}
}

func TestStaticShares(t *testing.T) {
	p := filepath.Join(t.TempDir(), "smb.conf")
	os.WriteFile(p, []byte("[global]\n\tregistry shares = yes\n[sysvol]\n\tpath = /var/lib/samba/sysvol\n[ netlogon ]\n"), 0o644)
	inc := filepath.Join(filepath.Dir(p), "extra.conf")
	os.WriteFile(inc, []byte("[Dados]\n\tpath = /srv/dados\ninclude = "+p+"\n"), 0o644) // a loop back: read once
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("include = registry\n include = " + inc + "\n")
	f.Close()
	got := staticShares(p)
	if len(got) != 3 || got[0] != "sysvol" || got[1] != "netlogon" || got[2] != "Dados" {
		t.Fatalf("staticShares = %q", got)
	}
}

// A DC without dc_file_server, or with an unknown role, changes no file.
func TestFileModeOff(t *testing.T) {
	t.Parallel()
	for _, a := range []*Agent{{role: roleDC}, {role: "something else"}} {
		if ok, why := a.fileMode(context.Background()); ok || why == "" {
			t.Errorf("role %q: files %v (%s)", a.role, ok, why)
		}
	}
	if ok, _ := (&Agent{role: "member server"}).fileMode(context.Background()); !ok {
		t.Error("a member server has files off")
	}
}

// A server that never synced lists empty jobs and runs, never null (the screens read .length).
func TestSyncListsNeverNull(t *testing.T) {
	a := &Agent{backups: &backup.Store{Dir: t.TempDir()}}
	jobs, err := a.syncJobs()
	if err != nil || jobs == nil || a.syncRuns() == nil {
		t.Fatalf("jobs %v err %v runs %v", jobs, err, a.syncRuns())
	}
	_ = os.MkdirAll(a.syncDir(), 0o700)
	_ = os.WriteFile(filepath.Join(a.syncDir(), "jobs.json"), []byte("null"), 0o600)
	if jobs, err := a.syncJobs(); err != nil || jobs == nil {
		t.Fatalf("saved null: jobs %v err %v", jobs, err)
	}
}

// The restorations' and quarantines' folders are protected under their current and their old names.
func TestProtectedFoldersOldNames(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{cfg: config.Agent{ShareRoots: []string{root}}}
	if !a.restorable(filepath.Join(root, "Finance")) {
		t.Fatal("a plain folder is not restorable")
	}
	for _, d := range []string{backup.RestoreDir, backup.LegacyRestoreDir} {
		if a.restorable(filepath.Join(root, d, "x")) {
			t.Errorf("%s restorable", d)
		}
	}
	for _, d := range []string{SyncDir, LegacySyncDir, backup.RestoreDir, backup.LegacyRestoreDir} {
		if a.validateDest(filepath.Join(root, d, "x")) == nil {
			t.Errorf("%s accepted as a sync destination", d)
		}
	}
	q := Quarantine{Name: "fin-20261018T120000Z-0000000a", Job: "fin", Run: "20261018T120000Z-0000000a"}
	for _, d := range []string{SyncDir, LegacySyncDir} {
		q.Path = filepath.Join(root, d, q.Name)
		if !a.quarantineOK(q) {
			t.Errorf("quarantine in %s refused", d)
		}
	}
}
