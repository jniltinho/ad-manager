package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// fakePanel is a TLS panel (self-signed, as a fresh installation) that signs in admin/good, refuses nonadmin as not
// a Domain Admin, registers one file server (id 7) and records the paths it was asked for.
type fakePanel struct {
	*httptest.Server
	mu    sync.Mutex
	paths []string
}

func newFakePanel(t *testing.T) *fakePanel {
	t.Helper()
	p := &fakePanel{}
	p.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.paths = append(p.paths, r.URL.RequestURI())
		p.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		signed := func() bool { c, err := r.Cookie("s"); return err == nil && c.Value == "ok" }
		switch r.URL.Path {
		case "/api/health":
			_, _ = w.Write([]byte(`{"domain":"domain.local","version":"v0.20.0"}`))
		case "/api/login":
			var in struct{ User, Password string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			switch {
			case in.Password != "good":
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"invalid user or password"}`))
			case in.User != "admin":
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"access denied: domain administrators only"}`))
			default:
				http.SetCookie(w, &http.Cookie{Name: "s", Value: "ok", Path: "/"})
				_, _ = w.Write([]byte(`{}`))
			}
		case "/api/redirect":
			http.Redirect(w, r, "http://example.invalid/api/me", http.StatusTemporaryRedirect)
		default:
			if !signed() {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"not signed in"}`))
				return
			}
			switch r.URL.Path {
			case "/api/me":
				_, _ = w.Write([]byte(`{"user":"admin","domain":"domain.local"}`))
			case "/api/agents":
				_, _ = w.Write([]byte(`[{"id":7,"name":"fs01","url":"https://fs01.domain.local:9443"}]`))
			case "/api/agents/7/shares":
				_, _ = w.Write([]byte(`[{"name":"Finance","path":"/srv/shares/Finance"}]`))
			case "/api/agents/7/sizes":
				_, _ = w.Write([]byte(`{"sizes":{"Finance":42}}`))
			case "/api/agents/7/samba/logs":
				_, _ = w.Write([]byte(`{"dir":"/var/log/samba","files":[]}`))
			default:
				_, _ = w.Write([]byte(`{}`))
			}
		}
	}))
	t.Cleanup(p.Close)
	return p
}

// asked waits up to 3 s for the panel to have been asked for path (a prefix of the request URI).
func (p *fakePanel) asked(path string) bool {
	for range 60 {
		p.mu.Lock()
		ok := slices.ContainsFunc(p.paths, func(s string) bool { return strings.HasPrefix(s, path) })
		p.mu.Unlock()
		if ok {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func (p *fakePanel) pin() string { return certPin(p.Certificate().Raw) }

// signedIn is a pinned client of p, signed in as admin.
func (p *fakePanel) signedIn(t *testing.T) *api {
	t.Helper()
	c := newClient(p.URL, "", pinnedTLS(p.pin()))
	if err := c.do(http.MethodPost, "/api/login", map[string]string{"user": "admin", "password": "good"}, nil); err != nil {
		t.Fatal(err)
	}
	return c
}

// TestPanelCertificatePin: a self-signed panel is not trusted by the system; pinned, exactly its certificate is
// accepted, any other pin fails before a request; a redirect is never followed.
func TestPanelCertificatePin(t *testing.T) {
	inLang(t, "en")
	p := newFakePanel(t)
	cert, err := probeCert(p.URL)
	if err != nil || cert.Trusted || cert.Pin != p.pin() || !strings.HasPrefix(cert.Pin, "sha256:") {
		t.Fatalf("probeCert = %+v, %v", cert, err)
	}
	var h panelHealth
	if err := newClient(p.URL, "", pinnedTLS(p.pin())).health(&h); err != nil || h.Domain != "domain.local" {
		t.Fatalf("pinned: %+v %v", h, err)
	}
	before := len(p.paths)
	err = newClient(p.URL, "", pinnedTLS("sha256:00")).do(http.MethodGet, "/api/health", nil, &h)
	if err == nil || !strings.Contains(err.Error(), "certificate") || len(p.paths) != before {
		t.Fatalf("another pin: %v (requests %d → %d)", err, before, len(p.paths))
	}
	if err := p.signedIn(t).do(http.MethodGet, "/api/redirect", nil, nil); err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("redirect: %v", err)
	}
}

// TestTestPanel: Configure's test signs in for real and gives the panel's reason when it cannot.
func TestTestPanel(t *testing.T) {
	inLang(t, "en")
	p := newFakePanel(t)
	if _, cert, _, _, err := testPanel(p.URL, "admin", "good", ""); err == nil || cert == nil || cert.Trusted {
		t.Fatalf("untrusted certificate not pinned yet: %+v %v", cert, err)
	}
	if _, _, _, _, err := testPanel(p.URL, "admin", "bad", p.pin()); err == nil || !strings.Contains(err.Error(), "invalid user or password") {
		t.Fatalf("wrong password: %v", err)
	}
	if _, _, _, _, err := testPanel(p.URL, "nonadmin", "good", p.pin()); err == nil || !strings.Contains(err.Error(), "domain administrators") {
		t.Fatalf("not a Domain Admin: %v", err)
	}
	c, _, h, m, err := testPanel(p.URL, "admin", "good", p.pin())
	if err != nil || m.User != "admin" || h.Version != "v0.20.0" {
		t.Fatalf("sign-in: %+v %+v %v", m, h, err)
	}
	var again me
	if err := c.do(http.MethodGet, "/api/me", nil, &again); err != nil || again.User != "admin" {
		t.Fatalf("the tested client is signed in: %+v %v", again, err)
	}
}

// TestFileServersThroughPanel: Shares and the Samba logs list the panel's file servers and reach them only through
// its proxy, with the session.
func TestFileServersThroughPanel(t *testing.T) {
	p := newFakePanel(t)
	a := test.NewApp()
	w := a.NewWindow("t")
	defer onScreen(func() { w.Close(); a.Quit() }) // late background results are ordered with the teardown too
	u := &ui{a: a, w: w, cfg: &config{}, panel: p.signedIn(t), status: newText("", col("footer-strong"), 11, false)}
	onScreen(func() { w.SetContent(u.sharesPage()) })
	for _, path := range []string{"/api/agents", "/api/agents/7/shares", "/api/agents/7/sizes"} {
		if !p.asked(path) {
			t.Fatalf("Shares never asked the panel for %s: %q", path, p.paths)
		}
	}
	via := p.signedIn(t).via(7)
	var r map[string]any
	if err := via.do(http.MethodGet, "/api/sizes?refresh=1", nil, &r); err != nil || !p.asked("/api/agents/7/sizes?refresh=1") {
		t.Fatalf("via keeps the query: %v %q", err, p.paths)
	}
	var logs bool
	onScreen(func() { w.SetContent(u.logsPage()) })
	for range 60 { // the file server's Samba tab comes once the panel listed it
		onScreen(func() {
			logs = slices.ContainsFunc(texts(w.Content()), func(s string) bool { return strings.Contains(s, "fs01") })
		})
		if logs {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !logs {
		t.Fatalf("no Samba tab for the panel's file server: %q", texts(w.Content()))
	}
}

// TestConfigureSave: Save waits for a successful test of the current values; an untrusted certificate is trusted
// only on the operator's word; the saved file has the pin and never the password, and the app opens signed in.
func TestConfigureSave(t *testing.T) {
	inLang(t, "en")
	defer func(p string) { onScreen(func() { configPath = p }) }(configPath)
	onScreen(func() { configPath = filepath.Join(t.TempDir(), "desktop.toml") })
	p := newFakePanel(t)
	a := test.NewApp()
	w := a.NewWindow("t")
	defer onScreen(func() { w.Close(); a.Quit() }) // late background results are ordered with the teardown too
	cfg := &config{}
	cfg.Panel.User = "admin"
	u := &ui{a: a, w: w, cfg: cfg, panel: newAPI("", "", false), status: newText("", col("footer-strong"), 11, false)}
	onScreen(func() { u.setup(nil) })
	var url, usr, pwd *ringEntry
	onScreen(func() {
		es := entriesIn(w.Content())
		url, usr, pwd = es[0], es[1], es[2]
		url.SetText(p.URL)
		pwd.SetText("good")
	})
	save := func() *volButton { return buttonIn(w.Content(), "Save") } // on the screen's goroutine (onScreen)
	tap := func(label string) bool {
		for range 60 {
			pressed := false
			onScreen(func() {
				if b := buttonIn(w.Content(), label); b != nil && !b.Disabled() {
					b.press()
					pressed = true
				}
			})
			if pressed {
				return true
			}
			time.Sleep(50 * time.Millisecond)
		}
		return false
	}
	saveEnabled := func() (on bool) {
		onScreen(func() { on = !save().Disabled() })
		return on
	}
	waitSave := func(enabled bool) bool {
		for range 60 {
			if saveEnabled() == enabled {
				return true
			}
			time.Sleep(50 * time.Millisecond)
		}
		return false
	}
	if saveEnabled() {
		t.Fatal("Save before any test")
	}
	if !tap("Test connection") || !tap("Trust this certificate") || !waitSave(true) {
		t.Fatalf("trusting the certificate and testing did not enable Save: %q", texts(w.Content()))
	}
	onScreen(func() { usr.SetText("nonadmin") })
	if saveEnabled() {
		t.Fatal("Save stays enabled after the user changed")
	}
	if !p.asked("/api/logout") {
		t.Fatal("the abandoned test's session was not ended")
	}
	onScreen(func() { usr.SetText("admin") })
	if !tap("Test connection") || !waitSave(true) || !tap("Save") {
		t.Fatalf("second test: %q", texts(w.Content()))
	}
	b, err := os.ReadFile(configPath)
	if err != nil || !strings.Contains(string(b), p.pin()) || strings.Contains(string(b), "good") || strings.Contains(string(b), "insecure") {
		t.Fatalf("saved config:\n%s (%v)", b, err)
	}
	var screen string
	onScreen(func() { screen = u.screen })
	if screen != "shell" || u.me.User != "admin" || u.cfg.Panel.Fingerprint != p.pin() {
		t.Fatalf("after Save: screen %q, me %+v, pin %q", screen, u.me, u.cfg.Panel.Fingerprint)
	}
}

// entriesIn lists the text entries under o, in order.
func entriesIn(o fyne.CanvasObject) []*ringEntry {
	if e, ok := o.(*ringEntry); ok {
		return []*ringEntry{e}
	}
	var out []*ringEntry
	for _, k := range kids(o) {
		out = append(out, entriesIn(k)...)
	}
	return out
}

// buttonIn finds the button labeled text under o.
func buttonIn(o fyne.CanvasObject, text string) *volButton {
	if b, ok := o.(*volButton); ok && b.Text == text {
		return b
	}
	for _, k := range kids(o) {
		if b := buttonIn(k, text); b != nil {
			return b
		}
	}
	return nil
}

func kids(o fyne.CanvasObject) []fyne.CanvasObject {
	switch x := o.(type) {
	case *fyne.Container:
		return x.Objects
	case *volButton:
		return nil
	case fyne.Widget:
		return test.WidgetRenderer(x).Objects()
	}
	return nil
}

// onScreen runs f on the UI goroutine holding uiMu, so the background work's screen updates (onUI) never run while a
// test reads or drives the screen (Fyne's test driver runs fyne.Do on the calling goroutine).
func onScreen(f func()) {
	uiMu.Lock()
	defer uiMu.Unlock()
	fyne.DoAndWait(f)
}

// inLang switches the interface language for one test, ordered with the background work of other tests' screens
// still finishing (it reads the language as it draws).
func inLang(t *testing.T, l string) {
	var old string
	onScreen(func() { old = lang; setLang(l) })
	t.Cleanup(func() { onScreen(func() { setLang(old) }) })
}
