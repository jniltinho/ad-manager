package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"ad-manager/internal/ad"
	"ad-manager/internal/store"
)

// The import credential: checked as a Domain Admin, kept sealed, never returned, sent with a GPO task, removable.
func TestImportCredential(t *testing.T) {
	s, h := newTestStore(t)
	s.cfg.DB.Path = filepath.Join(t.TempDir(), "t.db") // the secret box's key beside it, not in the package folder
	if rec := do(h, http.MethodGet, "/api/settings/import-credential", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without session: %d", rec.Code)
	}
	ck := login(t, h)
	get := func() (string, bool) {
		var v struct {
			User string
			Set  bool
		}
		rec := do(h, http.MethodGet, "/api/settings/import-credential", "", ck)
		if strings.Contains(rec.Body.String(), goodPassword) {
			t.Fatalf("the password left through the API: %s", rec.Body)
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &v)
		return v.User, v.Set
	}
	if _, set := get(); set {
		t.Fatal("set before saving")
	}
	if rec := do(h, http.MethodPut, "/api/settings/import-credential", `{"user":"Administrator","password":"wrong"}`, ck); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, http.MethodPut, "/api/settings/import-credential", `{"user":"bad user","password":"x"}`, ck); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid user: %d", rec.Code)
	}
	if _, set := get(); set {
		t.Fatal("a refused credential was kept")
	}
	if rec := do(h, http.MethodPut, "/api/settings/import-credential", `{"user":"Administrator","password":"`+goodPassword+`"}`, ck); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	if u, set := get(); u != "Administrator" || !set {
		t.Fatalf("after saving: %q %v", u, set)
	}
	raw, _ := s.store.Setting(importCredKey)
	if strings.Contains(string(raw), goodPassword) || strings.Contains(string(raw), base64.StdEncoding.EncodeToString([]byte(goodPassword))) {
		t.Fatalf("stored in clear: %s", raw)
	}

	// a GPO task carries it to the agent
	var got struct {
		Admin map[string]string `json:"admin"`
	}
	restoredBy, memberAsked := "", false
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			memberAsked = memberAsked || strings.HasPrefix(r.Host, "localhost.")
			_ = json.NewEncoder(w).Encode(map[string]any{"dc": true, "realm": "EMPRESA.TEST", "gpo_restore": true})
		case "/api/dc/gpos/restore":
			restoredBy = r.Host
			_ = json.NewDecoder(r.Body).Decode(&got)
			_, _ = w.Write([]byte(`{"guid":"{91048D73-A843-4FF2-B88F-9BCC2FB86614}","dc":"dc1.empresa.test"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer agent.Close()
	// first a host that only says it is a DC (a name that is no DC in the directory): skipped, never sent a secret
	member := strings.Replace(agent.URL, "127.0.0.1", "localhost.", 1)
	for _, a := range []*store.Agent{{Name: "0-member", URL: member, Token: "t"}, {Name: "dc1", URL: agent.URL, Token: "t"}} {
		if err := s.store.SaveAgent(a); err != nil {
			t.Fatal(err)
		}
	}
	s.dial = func(_ *ad.Config, _, pass string) (directory, error) {
		if pass != goodPassword {
			return nil, ad.ErrBadLogin
		}
		return &gpoDir{computersDir{fakeDir{admin: true}}}, nil
	}
	b, _ := json.Marshal(map[string]any{"name": "Copy", "archive": base64.StdEncoding.EncodeToString([]byte{0x1f, 0x8b})})
	if rec := do(h, http.MethodPost, "/api/gpos/restore", string(b), ck); rec.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", rec.Code, rec.Body)
	}
	if got.Admin["user"] != "Administrator" || got.Admin["password"] != goodPassword {
		t.Fatalf("the agent got admin = %v", got.Admin)
	}
	if !memberAsked || !strings.HasPrefix(restoredBy, "127.0.0.1:") {
		t.Fatalf("restored through %q (member asked: %v), not the loopback agent", restoredBy, memberAsked)
	}

	if rec := do(h, http.MethodDelete, "/api/settings/import-credential", "", ck); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d", rec.Code)
	}
	if _, set := get(); set {
		t.Fatal("still set after removing")
	}
}

// A profile's password: sealed, bound to the profile, reported only as has_password, gone with the profile.
func TestSourcePasswordKept(t *testing.T) {
	s, _ := newTestStore(t)
	s.cfg.DB.Path = filepath.Join(t.TempDir(), "t.db")
	x := &store.Source{Name: "old-dc", URL: "ldap://192.0.2.10", Domain: "domain.local", User: "administrator"}
	if err := s.store.SaveSource(x); err != nil || x.ID == 0 {
		t.Fatalf("save: %v %d", err, x.ID)
	}
	if err := s.keepSourcePassword(x, "S3cret pw"); err != nil {
		t.Fatal(err)
	}
	if got := s.keptSourcePassword(x.ID, x.URL, x.Domain, x.User); got != "S3cret pw" {
		t.Fatalf("kept = %q", got)
	}
	if got := s.keptSourcePassword(x.ID, "ldap://198.51.100.9", x.Domain, x.User); got != "" {
		t.Fatalf("sent to another server: %q", got)
	}
	if got := s.keptSourcePassword(x.ID, x.URL, x.Domain, "someone"); got != "" {
		t.Fatalf("used for another account: %q", got)
	}
	list, _ := s.store.Sources()
	if len(list) != 1 || !list[0].HasPassword {
		t.Fatalf("Sources = %+v", list)
	}
	if b, _ := json.Marshal(list); strings.Contains(string(b), "S3cret") {
		t.Fatalf("the password in the list: %s", b)
	}
	// moved to another profile, the sealed value opens nothing
	sealed, _ := s.store.SourceSecret(x.ID)
	y := &store.Source{Name: "other", URL: "ldap://192.0.2.11", Domain: "domain.local"}
	_ = s.store.SaveSource(y)
	_ = s.store.SetSourceSecret(y.ID, sealed)
	if got := s.keptSourcePassword(y.ID, y.URL, y.Domain, y.User); got != "" {
		t.Fatalf("opened on another profile: %q", got)
	}
	// the profile saved over with another account (or a concurrent save): the old sealed value opens nothing
	other := &store.Source{Name: x.Name, URL: x.URL, Domain: x.Domain, User: "someone"}
	_ = s.store.SaveSource(other)
	if got := s.keptSourcePassword(x.ID, other.URL, other.Domain, other.User); got != "" {
		t.Fatalf("opened for another account: %q", got)
	}
	if err := s.store.DeleteSource(x.ID); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.store.SourceSecret(x.ID); v != nil {
		t.Fatal("the secret outlived its profile")
	}
}

// The credential goes to an agent registered by a domain controller's DNS name (or one of its addresses), never to a
// member host that only says it is a DC.
func TestImportCredAgentHost(t *testing.T) {
	cs := []ad.Computer{{DNSName: "dc1.empresa.test", IsDC: true}, {DNSName: "fs1.empresa.test"}, {DNSName: "127.0.0.2", IsDC: true}}
	for host, want := range map[string]bool{"dc1.empresa.test": true, "DC1.empresa.test.": true, "fs1.empresa.test": false,
		"other.empresa.test": false, "127.0.0.2": true, "192.0.2.7": false} {
		if got := dcHost(context.Background(), host, cs); got != want {
			t.Errorf("dcHost(%q) = %v", host, got)
		}
	}
}
