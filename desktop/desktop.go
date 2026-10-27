package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"ad-manager/desktop/internal/config"
	"ad-manager/desktop/internal/panel"
)

// desktop is the app's state: desktop.toml and the panel it points to (none until Configure saves one), and the
// connection test Configure is running.
type desktop struct {
	app *application.App

	mu           sync.Mutex
	file         config.File
	cfg          *config.Config
	fileProblem  string // why desktop.toml cannot be read (loose permissions, a parse error): nothing overwrites it
	problem      string // what Configure says: fileProblem, or a panel URL in the file that is not usable
	unconfigured bool
	panel        *panel.Panel
	testID       int64  // the newest test Configure started; an older one finishing is signed out and dropped
	cancelled    int64  // every test id up to this one was cancelled (an edit, leaving Configure), even if it comes later
	trial        *trial // the last successful test, which Save adopts
	notice       string // said once after sign-in
}

// trial is a successful Configure test: its panel is signed in.
type trial struct {
	id             int64
	p              *panel.Panel
	gen            uint64
	url, user, pin string
}

func newDesktop(f config.File) *desktop {
	d := &desktop{file: f}
	cfg, unconfigured, err := config.Load(&d.file)
	if err != nil {
		d.fileProblem, d.problem, d.unconfigured, cfg = err.Error(), err.Error(), true, &config.Config{User: "administrator"}
	}
	d.cfg, d.notice = cfg, d.file.Notice
	if !d.unconfigured && !unconfigured {
		p, err := panel.New(cfg.URL, cfg.Fingerprint, cfg.Insecure)
		if err != nil {
			d.problem, unconfigured = err.Error(), true
		}
		d.panel = p
	}
	d.unconfigured = d.unconfigured || unconfigured
	return d
}

// current is the configured panel, or nil.
func (d *desktop) current() *panel.Panel {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.panel
}

func (d *desktop) safeGPU() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cfg.SafeGPU
}

// ── Shell: what the SPA asks of the app

// Shell is the service the SPA calls for what a webview cannot do on its own.
type Shell struct{ d *desktop }

// PanelURL is the panel's base URL, for links built for use outside the app (the single-use password link).
func (s *Shell) PanelURL() string {
	if p := s.d.current(); p != nil {
		return p.URL()
	}
	return ""
}

// defaultSupport is the support link of a panel without its own (the web's DEFAULT_SUPPORT).
const defaultSupport = "https://github.com/jniltinho/ad-manager/wiki"

// OpenExternal opens target in the system browser: the panel's /asynqmon or /swagger, or the support link the panel
// publishes (https only); nothing else.
func (s *Shell) OpenExternal(target string) error {
	p := s.d.current()
	if p != nil && (target == "/asynqmon" || target == "/swagger") {
		return s.d.app.Browser.OpenURL(p.URL() + target)
	}
	if strings.HasPrefix(target, "https://") && (target == defaultSupport || p != nil && target == supportURL(p)) {
		return s.d.app.Browser.OpenURL(target)
	}
	return errors.New("only the panel's /asynqmon and /swagger pages and its support link open outside the app")
}

// supportURL is the support link the panel publishes (its public /api/health brand).
func supportURL(p *panel.Panel) string {
	var h struct {
		Brand struct {
			SupportURL string `json:"support_url"`
		} `json:"brand"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if p.Call(ctx, p.Generation(), http.MethodGet, "/api/health", nil, &h) != nil {
		return ""
	}
	return h.Brand.SupportURL
}

// Session is the panel session's generation, which the SPA sends on every request (X-ADM-Session).
func (s *Shell) Session() uint64 {
	if p := s.d.current(); p != nil {
		return p.Generation()
	}
	return 0
}

// UploadLimit is the most the SPA sends through the webview's bridge in one form (bigger files go through Files).
func (s *Shell) UploadLimit() int64 { return panel.BridgeCap - 1<<20 }

// Version is the desktop app's version.
func (s *Shell) Version() string { return version }

// Notice is said once after sign-in (the agent tokens of an older desktop.toml were removed), then empty.
func (s *Shell) Notice() string {
	s.d.mu.Lock()
	defer s.d.mu.Unlock()
	n := s.d.notice
	s.d.notice = ""
	return n
}

// ── Configure: the panel, the account and this computer's preferences

// Configure is the service of the Configure screen. Its test runs through POST /desktop/test (routes.go), not a
// binding: Wails would log a binding's arguments, the password among them, at debug level.
type Configure struct{ d *desktop }

// ConfigState is desktop.toml as Configure shows it.
type ConfigState struct {
	Configured  bool   `json:"configured"`
	Problem     string `json:"problem"`
	Path        string `json:"path"`
	URL         string `json:"url"`
	User        string `json:"user"`
	Fingerprint string `json:"fingerprint"`
	Lang        string `json:"lang"`
	Theme       string `json:"theme"`
	Flat        string `json:"flat"`
	SafeGPU     bool   `json:"safeGPU"`
	OS          string `json:"os"`
}

// State is the configuration on screen.
func (c *Configure) State() ConfigState {
	d := c.d
	d.mu.Lock()
	defer d.mu.Unlock()
	return ConfigState{Configured: !d.unconfigured && d.panel != nil, Problem: d.problem, Path: d.file.Path,
		URL: d.cfg.URL, User: d.cfg.User, Fingerprint: d.cfg.Fingerprint, Lang: d.cfg.Lang, Theme: d.cfg.Theme,
		Flat: d.cfg.Flat, SafeGPU: d.cfg.SafeGPU, OS: runtime.GOOS}
}

// Cancel drops every test up to id: an edit of the URL, user or password, or leaving Configure. A test still
// running, or whose request arrives later, is dropped when it finishes; a successful one is signed out.
func (c *Configure) Cancel(id int64) {
	d := c.d
	d.mu.Lock()
	if id > d.cancelled {
		d.cancelled = id
	}
	t := d.trial
	if t != nil && t.id <= d.cancelled {
		d.trial = nil
	} else {
		t = nil
	}
	d.mu.Unlock()
	if t != nil {
		go t.p.Logout(t.gen)
	}
}

// Save writes the tested panel, account and the preferences, and makes the tested session the app's: the SPA then
// reloads signed in, without asking the password again.
func (c *Configure) Save(id int64, lang, theme, flat string, safeGPU bool) error {
	d := c.d
	d.mu.Lock()
	t := d.trial
	if t == nil || t.id != id || id != d.testID || id <= d.cancelled {
		d.mu.Unlock()
		return errors.New("test the connection before saving")
	}
	if d.fileProblem != "" { // the file on disk is not read: saving would replace it (and its other sections)
		d.mu.Unlock()
		return errors.New(d.fileProblem)
	}
	cfg := *d.cfg
	cfg.URL, cfg.User, cfg.Fingerprint, cfg.Insecure = t.url, t.user, t.pin, false
	cfg.Lang, cfg.Theme, cfg.Flat, cfg.SafeGPU = lang, theme, flat, safeGPU
	if err := config.Save(&d.file, &cfg); err != nil {
		d.mu.Unlock()
		return err
	}
	// the old panel's session ends here before the new one is published (nothing of it runs after Save); the old
	// panel is then told to end it
	var signOut func()
	if d.panel != nil && d.panel != t.p {
		signOut = d.panel.Detach()
	}
	d.cfg, d.panel, d.trial, d.unconfigured, d.problem = &cfg, t.p, nil, false, ""
	d.mu.Unlock()
	if signOut != nil {
		go signOut()
	}
	return nil
}

// SavePreferences writes the language, the appearance and the graphics at once, without a test and offline (also
// before the panel is configured: the file then holds preferences only).
func (c *Configure) SavePreferences(lang, theme, flat string, safeGPU bool) error {
	d := c.d
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.fileProblem != "" {
		return errors.New(d.fileProblem)
	}
	cfg := *d.cfg
	cfg.Lang, cfg.Theme, cfg.Flat, cfg.SafeGPU = lang, theme, flat, safeGPU
	if err := config.Save(&d.file, &cfg); err != nil {
		return err
	}
	d.cfg = &cfg
	return nil
}

// testRequest is POST /desktop/test: the values on the Configure screen.
type testRequest struct {
	ID       int64  `json:"id"`
	URL      string `json:"url"`
	User     string `json:"user"`
	Password string `json:"password"`
	Trust    string `json:"trust"` // the fingerprint the operator trusted (or the one already pinned)
}

// TestResult is what Configure shows after a test.
type TestResult struct {
	OK      bool        `json:"ok"`
	Stale   bool        `json:"stale"` // an edit or a newer test came in between: shown nowhere
	Cert    *panel.Cert `json:"cert,omitempty"`
	Error   string      `json:"error,omitempty"`
	User    string      `json:"user,omitempty"`
	Domain  string      `json:"domain,omitempty"`
	Version string      `json:"version,omitempty"`
}

// serveTest runs a Configure test: the certificate (pinned only when the operator trusted it), the panel's health,
// a real sign-in with the account and /api/me.
func (d *desktop) serveTest(w http.ResponseWriter, r *http.Request) {
	var req testRequest
	if r.Method != http.MethodPost || json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req) != nil || req.ID <= 0 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	d.mu.Lock()
	refused := req.ID <= d.cancelled || req.ID < d.testID // cancelled before it arrived, or a newer test runs
	if !refused {
		d.testID = req.ID
	}
	d.mu.Unlock()
	if refused {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(TestResult{Stale: true})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	res, t := runTest(ctx, req)
	d.mu.Lock()
	current := d.testID == req.ID && req.ID > d.cancelled
	if current && t != nil {
		old := d.trial
		d.trial = t
		if old != nil {
			go old.p.Logout(old.gen)
		}
	}
	d.mu.Unlock()
	if !current {
		if t != nil {
			go t.p.Logout(t.gen)
		}
		res = TestResult{Stale: true}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// errLocalNetwork is macOS's Local Network privacy refusing the panel's address.
var errLocalNetwork = errors.New("macOS blocks this app from the local network (System Settings, Privacy & Security, Local Network)")

// errSetup is a panel whose setup wizard has not finished.
var errSetup = errors.New("the panel is being set up: finish its setup wizard in a browser first")

func runTest(ctx context.Context, req testRequest) (TestResult, *trial) {
	fail := func(err error) (TestResult, *trial) { return TestResult{Error: err.Error()}, nil }
	if _, err := panel.New(req.URL, "", false); err != nil {
		return fail(err)
	}
	cert, err := panel.ProbeCert(ctx, req.URL)
	if runtime.GOOS == "darwin" && errors.Is(err, syscall.EHOSTUNREACH) {
		err = fmt.Errorf("%w: %w", errLocalNetwork, err) // macOS 15 refuses a LAN address until the app is allowed
	}
	if err != nil {
		return fail(err)
	}
	pin := ""
	if !cert.Trusted {
		if req.Trust != cert.Pin {
			return TestResult{Cert: cert}, nil // shown with "Trust this certificate"
		}
		pin = cert.Pin
	}
	p, err := panel.New(req.URL, pin, false)
	if err != nil {
		return fail(err)
	}
	var health struct {
		Setup bool `json:"setup"`
	}
	if err := p.Call(ctx, p.Generation(), http.MethodGet, "/api/health", nil, &health); err != nil {
		return fail(err)
	}
	if health.Setup {
		return fail(errSetup)
	}
	gen, err := p.Login(ctx, req.User, req.Password)
	if err != nil {
		return fail(err)
	}
	var me struct {
		User    string `json:"user"`
		Domain  string `json:"domain"`
		Version string `json:"version"`
	}
	if err := p.Call(ctx, gen, http.MethodGet, "/api/me", nil, &me); err != nil {
		go p.Logout(gen)
		return fail(err)
	}
	return TestResult{OK: true, User: me.User, Domain: me.Domain, Version: me.Version},
		&trial{id: req.ID, p: p, gen: gen, url: p.URL(), user: req.User, pin: pin}
}
