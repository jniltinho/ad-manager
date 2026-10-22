// Package server is the web panel: a JSON API (Echo v5) plus the embedded Vue
// SPA. Each authenticated request opens an LDAPS connection with the
// logged-in operator's credentials, which are kept only in process memory.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"ad-manager/internal/ad"
	"ad-manager/internal/audit"
	"ad-manager/internal/backup"
	"ad-manager/internal/buildinfo"
	"ad-manager/internal/config"
	"ad-manager/internal/i18n"
	"ad-manager/internal/sambalog"
	"ad-manager/internal/store"
	"ad-manager/internal/tlsutil"
	"ad-manager/internal/winrpc"
	"ad-manager/web"

	"github.com/labstack/echo/v5"
	emw "github.com/labstack/echo/v5/middleware"
)

// cookieName is the session cookie.
const cookieName = "adc_session"

// session is a logged-in operator; the password stays in memory only.
type session struct {
	user, pass string
	seen       time.Time
}

// Server holds the configuration, sessions and failed-login counters.
type Server struct {
	cfg   *config.Config
	store *store.Store // local SQLite; nil disables agents and source profiles
	// the panel's own backups (one store: its lock keeps one backup at a time)
	panelBackups     *backup.Store
	panelBackupsOnce sync.Once
	panelRestoreMu   sync.Mutex // one panel restoration at a time
	mu               sync.Mutex
	sessions         map[string]*session
	fails            map[string][]time.Time
	netbios          string // read from AD at the first login
	dial             func(cfg *ad.Config, user, pass string) (directory, error)
	kpasswd          func(cfg *ad.Config, sam, oldPwd, newPwd string) error // Kerberos password change
	rpc              rpcClient                                              // DNS and remote sessions (winrpc, in a child process)
	linkMu           sync.Mutex                                             // serializes password link generation and use
}

// directory is the subset of *ad.Conn the handlers use (allows a fake in tests).
type directory interface {
	Close()
	IsAdmin() (bool, error)
	NetBIOS() string
	Users(q string) ([]ad.User, error)
	User(sam string) (*ad.User, error)
	CreateUser(in ad.UserInput, password, ouDN string, mustChange bool) (*ad.User, error)
	UpdateUser(sam string, in ad.UserInput) (*ad.User, error)
	SetEnabled(sam string, enabled bool) error
	Unlock(sam string) error
	Move(sam, ouDN string) error
	Delete(sam string) error
	ResetPassword(sam, password string, mustChange bool) error
	ChangeOwnPassword(oldPwd, newPwd string) error
	PrepareLinkReset(sam, temp string) (string, error)
	Groups(q string) ([]ad.Group, error)
	Group(name string) (*ad.Group, error)
	CreateGroup(name, description, typ, scope, ouDN string) (*ad.Group, error)
	SetDescription(sam, description string) error
	AddMember(group, member string) error
	RemoveMember(group, member string) error
	OUs() ([]ad.OU, error)
	CreateOU(name, description, parentDN string) (*ad.OU, error)
	DeleteOU(dn string) error
	Computers(q string) ([]ad.Computer, error)
	GPOs() ([]ad.GPO, []ad.GPOContainer, error)
	DNSRecords() ([]ad.DNSRecord, int, error)
	DCHostName() (string, error)
	Policy() (*ad.Policy, error)
	SetPolicy(p ad.Policy) error
	Export() (*ad.CSVSet, error)
	Plan(d ad.ImportData) ([]ad.Action, error)
	Apply(plan []ad.Action, genPassword func() string) map[string]string
	SetImportMetadata(sam string, lastLogon time.Time, os, osVersion string) error
	DomainSID() (string, error)
}

// New builds the Echo instance with the API and the SPA. st may be nil.
func New(cfg *config.Config, st *store.Store) *echo.Echo {
	s := &Server{cfg: cfg, store: st, sessions: map[string]*session{}, fails: map[string][]time.Time{},
		kpasswd: ad.ChangePasswordKerberos, rpc: winrpc.Isolated{Exe: self()},
		dial: func(c *ad.Config, u, p string) (directory, error) {
			conn, err := ad.Dial(c, u, p)
			if err != nil {
				return nil, err // avoid a nil *ad.Conn inside a non-nil interface
			}
			return conn, nil
		}}
	return s.routes()
}

// routes registers every route.
func (s *Server) routes() *echo.Echo {
	e := echo.New()
	if s.cfg.Server.TrustProxy {
		// only the local (loopback) proxy is trusted: a private network would allow spoofing the IP
		e.IPExtractor = echo.ExtractIPFromXFFHeader(echo.TrustLoopback(true), echo.TrustLinkLocal(false), echo.TrustPrivateNet(false))
	} else {
		e.IPExtractor = echo.ExtractIPDirect()
	}
	e.Use(emw.Recover(), securityHeaders, s.requestLog())

	api := e.Group("/api")
	if s.cfg.Setup { // a fresh installation: only the setup wizard
		api.GET("/brand/logo", s.brandLogo)
		api.GET("/i18n", func(c *echo.Context) error { return c.JSON(http.StatusOK, i18n.PTCatalog()) })
		s.setupRoutes(api)
		if dist, err := web.Dist(); err == nil {
			spa(e, dist, func() bool { return false }, func() (Theme, bool) { t, _ := themeByID(s.settings().Theme); return t, false })
		}
		return e
	}
	api.GET("/health", s.health)
	api.GET("/brand/logo", s.brandLogo)
	// pt-BR catalog of the server messages (public: the login page needs it too)
	api.GET("/i18n", func(c *echo.Context) error { return c.JSON(http.StatusOK, i18n.PTCatalog()) })
	api.POST("/login", s.login)
	api.GET("/password-link/:token", s.passwordLinkInfo)
	api.POST("/password-link/:token", s.usePasswordLink)
	p := api.Group("", s.auth)
	p.POST("/logout", s.logout)
	p.GET("/me", func(c *echo.Context) error {
		return c.JSON(http.StatusOK, map[string]any{"user": sess(c).user, "domain": s.cfg.AD.Domain, "netbios": s.netbios,
			"version": buildinfo.Version, "asynqmon": s.cfg.Redis.Addr != "", "settings": s.settings(), "brand": s.brand()})
	})
	p.POST("/me/password", s.ownPassword)
	p.GET("/summary", s.summary)
	p.GET("/audit", s.logs)
	p.GET("/http-log", s.httpLog)
	p.GET("/samba/logs", sambalog.ListHandler(s.cfg.Samba.LogDir))
	p.GET("/samba/logs/:name", sambalog.ReadHandler(s.cfg.Samba.LogDir, "name"))
	p.GET("/settings", s.getSettings)
	p.PUT("/settings", s.putSettings)
	p.PUT("/settings/logo", s.putLogo)
	p.DELETE("/settings/logo", s.deleteLogo)

	p.GET("/users", s.listUsers)
	p.POST("/users", s.createUser)
	p.GET("/users/:sam", s.getUser)
	p.PUT("/users/:sam", s.updateUser)
	p.DELETE("/users/:sam", s.objAction("user.delete", func(d directory, sam string, _ *echo.Context) error { return d.Delete(sam) }))
	p.POST("/users/:sam/enable", s.objAction("user.enable", func(d directory, sam string, _ *echo.Context) error { return d.SetEnabled(sam, true) }))
	p.POST("/users/:sam/disable", s.objAction("user.disable", func(d directory, sam string, _ *echo.Context) error { return d.SetEnabled(sam, false) }))
	p.POST("/users/:sam/unlock", s.objAction("user.unlock", func(d directory, sam string, _ *echo.Context) error { return d.Unlock(sam) }))
	p.POST("/users/:sam/move", s.objAction("object.move", moveTo))
	p.POST("/users/:sam/password", s.objAction("password.reset", func(d directory, sam string, c *echo.Context) error {
		var in struct {
			Password   string `json:"password"`
			MustChange bool   `json:"must_change"`
		}
		if err := c.Bind(&in); err != nil {
			return ad.ErrWrongInput
		}
		return d.ResetPassword(sam, in.Password, in.MustChange)
	}))

	p.POST("/users/:sam/password-link", s.createPasswordLink)

	p.GET("/groups", s.listGroups)
	p.POST("/groups", s.createGroup)
	p.GET("/groups/:sam", s.getGroup)
	p.PUT("/groups/:sam", s.objAction("group.update", func(d directory, sam string, c *echo.Context) error {
		var in struct {
			Description string `json:"description"`
		}
		if err := c.Bind(&in); err != nil {
			return ad.ErrWrongInput
		}
		return d.SetDescription(sam, in.Description)
	}))
	p.DELETE("/groups/:sam", s.objAction("group.delete", func(d directory, sam string, _ *echo.Context) error { return d.Delete(sam) }))
	p.POST("/groups/:sam/members", s.objAction("group.add", func(d directory, sam string, c *echo.Context) error {
		var in struct {
			Member string `json:"member"`
		}
		if err := c.Bind(&in); err != nil || in.Member == "" {
			return ad.ErrWrongInput
		}
		return d.AddMember(sam, in.Member)
	}))
	p.DELETE("/groups/:sam/members/:member", s.objAction("group.remove", func(d directory, sam string, c *echo.Context) error {
		return d.RemoveMember(sam, param(c, "member"))
	}))

	p.GET("/ous", s.listOUs)
	p.POST("/ous", s.createOU)
	p.DELETE("/ous", s.deleteOU)

	p.GET("/computers", s.listComputers)
	p.GET("/computers/sessions", s.remoteSessions)
	p.GET("/gpos", s.listGPOs)
	p.GET("/dns/zones", s.dnsZones)
	p.GET("/dns/records", s.dnsRecords)
	p.POST("/dns/records", s.dnsChange(false))
	p.DELETE("/dns/records", s.dnsChange(true))
	// :sam is the real sAMAccountName (NAME$), as returned by /computers.
	p.POST("/computers/:sam/enable", s.objAction("computer.enable", func(d directory, sam string, _ *echo.Context) error { return d.SetEnabled(sam, true) }))
	p.POST("/computers/:sam/disable", s.objAction("computer.disable", func(d directory, sam string, _ *echo.Context) error { return d.SetEnabled(sam, false) }))
	p.POST("/computers/:sam/move", s.objAction("object.move", moveTo))
	p.DELETE("/computers/:sam", s.objAction("computer.delete", func(d directory, sam string, _ *echo.Context) error { return d.Delete(sam) }))

	p.GET("/export/:file", s.export)
	p.POST("/import/plan", s.importCSV(false))
	p.POST("/import/apply", s.importCSV(true))

	p.GET("/policy", s.getPolicy)
	p.PUT("/policy", s.setPolicy)

	s.extras(e, p)
	s.swagger(e)

	if dist, err := web.Dist(); err != nil {
		slog.Error("embedded SPA unavailable", "err", err)
	} else {
		spa(e, dist, func() bool { return s.settings().Swagger }, func() (Theme, bool) {
			st := s.settings()
			t, _ := themeByID(st.Theme)
			return t, st.Flat
		})
	}
	return e
}

// Run serves until ctx is cancelled. It requires HTTPS, except on loopback
// behind a trusted proxy (trust_proxy) or with insecureDev.
func Run(ctx context.Context, cfg *config.Config, insecureDev bool) error {
	sc := cfg.Server
	host, _, err := net.SplitHostPort(sc.Listen)
	if err != nil {
		return fmt.Errorf("server.listen: %w", err)
	}
	ip := net.ParseIP(host)
	loopback := host == "localhost" || (ip != nil && ip.IsLoopback())
	if cfg.Setup { // the wizard is opened from another machine: all addresses, HTTPS with the package's certificate
		// (or a self-signed one), whatever the example config says
		_, port, _ := net.SplitHostPort(sc.Listen)
		sc.Listen = net.JoinHostPort("0.0.0.0", port)
		host, ip, loopback = "0.0.0.0", nil, false
		if sc.CertFile == "" {
			if _, err := os.Stat("/opt/ad-manager/tls/tls.crt"); err == nil {
				sc.CertFile, sc.KeyFile = "/opt/ad-manager/tls/tls.crt", "/opt/ad-manager/tls/tls.key"
			} else {
				sc.SelfSigned = true
			}
		}
	}
	tlsCfg, fp, err := tlsutil.Config(sc.CertFile, sc.KeyFile, sc.SelfSigned, cfg.StateDir())
	if err != nil {
		return err
	}
	tls := tlsCfg != nil
	if !tls && !(loopback && (sc.TrustProxy || insecureDev)) {
		return errors.New("the panel requires HTTPS: set server.cert_file/key_file or self_signed = true, or listen on 127.0.0.1 behind a TLS proxy (trust_proxy = true); for local development use --insecure-dev")
	}
	f, err := audit.Open(sc.AuditLog)
	if err != nil {
		return fmt.Errorf("audit_log: %w", err)
	}
	defer f.Close()
	af, err := audit.OpenAccess(sc.AccessLog)
	if err != nil {
		return fmt.Errorf("access_log: %w", err)
	}
	defer af.Close()
	var st *store.Store
	if cfg.DB.Path != "" {
		if st, err = store.Open(cfg.DB.Path); err != nil {
			return fmt.Errorf("db.path: %w", err)
		}
		defer st.Close()
	}

	srv := &http.Server{Addr: sc.Listen, Handler: New(cfg, st), TLSConfig: tlsCfg, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("panel listening", "listen", sc.Listen, "tls", tls, "sha256", fp, "version", buildinfo.Version)
		if tls {
			errCh <- srv.ListenAndServeTLS("", "")
		} else {
			errCh <- srv.ListenAndServe()
		}
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shut, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shut)
}

// securityHeaders sets the security headers on every response.
func securityHeaders(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		h := c.Response().Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Strict-Transport-Security", "max-age=31536000")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'")
		return next(c)
	}
}

// ── session and login ────────────────────────────────────────────────────────

// newID returns a random session ID.
func newID() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // crypto/rand.Read does not fail (Go ≥ 1.24)
	return hex.EncodeToString(b)
}

// ttl is the idle time before a session expires (runtime setting).
func (s *Server) ttl() time.Duration { return time.Duration(s.settings().SessionMinutes) * time.Minute }

// auth is the middleware that requires a valid session.
func (s *Server) auth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if err := s.session(c); err != "" {
			return c.JSON(http.StatusUnauthorized, msg(err))
		}
		return next(c)
	}
}

// authPage protects pages opened directly in the browser (/asynqmon, /swagger): without
// a session it redirects to the login, which comes back here afterwards.
func (s *Server) authPage(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if s.session(c) != "" {
			next := "/asynqmon"
			if strings.HasPrefix(c.Request().URL.Path, "/swagger") {
				next = "/swagger"
			}
			return c.Redirect(http.StatusFound, "/?next="+next)
		}
		return next(c)
	}
}

// session validates the cookie and stores the session in the context; it
// returns the rejection reason, or "" on success.
func (s *Server) session(c *echo.Context) string {
	ck, err := c.Cookie(cookieName)
	if err != nil {
		return "please sign in"
	}
	s.mu.Lock()
	se, ok := s.sessions[ck.Value]
	if ok && time.Since(se.seen) > s.ttl() {
		delete(s.sessions, ck.Value)
		ok = false
	}
	if ok {
		se.seen = time.Now()
	}
	s.mu.Unlock()
	if !ok {
		return "session expired"
	}
	c.Set("sess", se)
	c.Set("sid", ck.Value)
	return ""
}

// sess returns the session stored by auth.
func sess(c *echo.Context) *session { return c.Get("sess").(*session) }

// Login rate limit per IP.
const (
	maxFails   = 5
	failWindow = 5 * time.Minute
)

// tooMany drops old failures and reports whether ip hit the limit.
func (s *Server) tooMany(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	recent := s.fails[ip][:0]
	for _, t := range s.fails[ip] {
		if time.Since(t) < failWindow {
			recent = append(recent, t)
		}
	}
	s.fails[ip] = recent
	return len(recent) >= maxFails
}

// fail records a failed login for ip.
func (s *Server) fail(ip string) {
	s.mu.Lock()
	s.fails[ip] = append(s.fails[ip], time.Now())
	s.mu.Unlock()
}

// login handles POST /api/login: binds as the operator and requires admin
// rights.
func (s *Server) login(c *echo.Context) error {
	ip := c.RealIP()
	if s.tooMany(ip) {
		return c.JSON(http.StatusTooManyRequests, msg("too many attempts; wait a few minutes"))
	}
	var in struct {
		User     string `json:"user"`
		Password string `json:"password"`
	}
	if err := c.Bind(&in); err != nil || in.User == "" || in.Password == "" {
		return c.JSON(http.StatusBadRequest, msg("enter user and password"))
	}
	d, err := s.dial(&s.cfg.AD, in.User, in.Password)
	if err != nil {
		s.fail(ip)
		audit.Log(in.User, ip, "login", in.User, err)
		if errors.Is(err, ad.ErrBadLogin) {
			return c.JSON(http.StatusUnauthorized, msg(err.Error()))
		}
		return c.JSON(http.StatusBadGateway, msg("directory unavailable: "+err.Error()))
	}
	defer d.Close()
	ok, err := d.IsAdmin()
	if err != nil || !ok {
		s.fail(ip)
		audit.Log(in.User, ip, "login", in.User, errors.New("access denied"))
		return c.JSON(http.StatusForbidden, msg("access denied: domain administrators only"))
	}
	_, sam := ad.BindName(in.User, s.cfg.AD.Domain)
	nb := d.NetBIOS()
	id := newID()
	s.mu.Lock()
	s.netbios = nb
	for k, v := range s.sessions { // lazy cleanup
		if time.Since(v.seen) > s.ttl() {
			delete(s.sessions, k)
		}
	}
	s.sessions[id] = &session{user: sam, pass: in.Password, seen: time.Now()}
	s.mu.Unlock()
	c.SetCookie(&http.Cookie{Name: cookieName, Value: id, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	audit.Log(sam, ip, "login", sam, nil)
	return c.JSON(http.StatusOK, map[string]string{"user": sam, "domain": s.cfg.AD.Domain})
}

// logout handles POST /api/logout.
func (s *Server) logout(c *echo.Context) error {
	s.mu.Lock()
	delete(s.sessions, c.Get("sid").(string))
	s.mu.Unlock()
	c.SetCookie(&http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	return c.NoContent(http.StatusNoContent)
}

// with opens the operator's connection and calls fn.
func (s *Server) with(c *echo.Context, fn func(d directory) error) error {
	se := sess(c)
	d, err := s.dial(&s.cfg.AD, se.user, se.pass)
	if err != nil {
		return fail(c, err)
	}
	defer d.Close()
	if err := fn(d); err != nil {
		return fail(c, err)
	}
	return nil
}

// change is with plus an audit record.
func (s *Server) change(c *echo.Context, action, target string, fn func(d directory) error) error {
	var opErr error
	err := s.with(c, func(d directory) error { opErr = fn(d); return opErr })
	if opErr != nil || err == nil {
		audit.Log(sess(c).user, c.RealIP(), action, target, opErr)
	}
	return err
}

// objAction builds a POST/PUT/DELETE handler on /resource/:sam that only
// answers 204.
func (s *Server) objAction(action string, fn func(d directory, sam string, c *echo.Context) error) echo.HandlerFunc {
	return func(c *echo.Context) error {
		sam := param(c, "sam")
		err := s.change(c, action, sam, func(d directory) error { return fn(d, sam, c) })
		if err != nil || committed(c) {
			return err
		}
		return c.NoContent(http.StatusNoContent)
	}
}

// moveTo moves the object to the OU given in the body.
func moveTo(d directory, sam string, c *echo.Context) error {
	var in struct {
		OU string `json:"ou"`
	}
	if err := c.Bind(&in); err != nil || in.OU == "" {
		return ad.ErrWrongInput
	}
	return d.Move(sam, in.OU)
}

// param decodes a path parameter (Echo v5 hands over the raw segment).
func param(c *echo.Context, name string) string {
	v, err := url.PathUnescape(c.Param(name))
	if err != nil {
		return c.Param(name)
	}
	return v
}

// msg wraps an error message in the API's JSON shape.
func msg(s string) map[string]string { return map[string]string{"error": s} }

// fail translates ad package errors into HTTP status codes.
func fail(c *echo.Context, err error) error {
	code := http.StatusInternalServerError
	switch {
	case errors.Is(err, ad.ErrNotFound):
		code = http.StatusNotFound
	case errors.Is(err, ad.ErrConflict), errors.Is(err, ad.ErrNotEmpty):
		code = http.StatusConflict
	case errors.Is(err, ad.ErrProtected):
		code = http.StatusForbidden
	case errors.Is(err, ad.ErrPwdPolicy):
		code = http.StatusUnprocessableEntity
	case errors.Is(err, ad.ErrWrongInput), errors.Is(err, ad.ErrNeedsTLS):
		code = http.StatusBadRequest
	case errors.Is(err, ad.ErrBadLogin):
		code = http.StatusUnauthorized
	}
	return c.JSON(code, msg(err.Error()))
}

// ── handlers ─────────────────────────────────────────────────────────────────

// health handles GET /api/health (public): LDAP reachability and branding.
func (s *Server) health(c *echo.Context) error {
	u, err := url.Parse(s.cfg.AD.URL)
	status, code := "up", http.StatusOK
	if err == nil {
		port := u.Port()
		if port == "" {
			port = map[string]string{"ldaps": "636", "ldap": "389"}[u.Scheme]
		}
		var conn net.Conn
		if conn, err = net.DialTimeout("tcp", net.JoinHostPort(u.Hostname(), port), 3*time.Second); err == nil {
			conn.Close()
		}
	}
	if err != nil {
		status, code = "down", http.StatusServiceUnavailable
	}
	s.mu.Lock()
	nb := s.netbios
	s.mu.Unlock()
	if nb == "" {
		nb = strings.ToUpper(strings.SplitN(s.cfg.AD.Domain, ".", 2)[0])
	}
	return c.JSON(code, map[string]any{"ldap": status, "version": buildinfo.Version, "domain": s.cfg.AD.Domain, "netbios": nb, "brand": s.brand()})
}

// ownPassword handles POST /api/me/password.
func (s *Server) ownPassword(c *echo.Context) error {
	var in struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if err := c.Bind(&in); err != nil || in.Old == "" || in.New == "" {
		return c.JSON(http.StatusBadRequest, msg("enter the current and the new password"))
	}
	se := sess(c)
	err := s.change(c, "password.change", se.user, func(d directory) error { return d.ChangeOwnPassword(in.Old, in.New) })
	if err != nil || committed(c) {
		return err
	}
	s.mu.Lock()
	se.pass = in.New
	s.mu.Unlock()
	return c.NoContent(http.StatusNoContent)
}

// summary handles GET /api/summary (dashboard numbers).
func (s *Server) summary(c *echo.Context) error {
	return s.with(c, func(d directory) error {
		us, err := d.Users("")
		if err != nil {
			return err
		}
		gs, err := d.Groups("")
		if err != nil {
			return err
		}
		cs, err := d.Computers("")
		if err != nil {
			return err
		}
		pol, err := d.Policy()
		if err != nil {
			return err
		}
		out := map[string]any{"domain": s.cfg.AD.Domain, "server": s.cfg.AD.URL, "groups": len(gs), "computers": len(cs), "policy": pol}
		var enabled, locked, never int
		for _, u := range us {
			if u.Enabled {
				enabled++
			}
			if u.Locked {
				locked++
			}
			if u.Enabled && u.LastLogon.IsZero() {
				never++
			}
		}
		var dcs []ad.Computer
		os := map[string]int{}
		for _, m := range cs {
			if m.IsDC {
				dcs = append(dcs, m)
			}
			os[m.OS]++
		}
		out["users"] = map[string]int{"total": len(us), "enabled": enabled, "disabled": len(us) - enabled, "locked": locked, "never_logged": never}
		out["dcs"] = dcs
		out["os"] = os
		return c.JSON(http.StatusOK, out)
	})
}

// listUsers handles GET /api/users.
func (s *Server) listUsers(c *echo.Context) error {
	return s.with(c, func(d directory) error {
		us, err := d.Users(c.QueryParam("q"))
		if err == nil {
			err = c.JSON(http.StatusOK, us)
		}
		return err
	})
}

// getUser handles GET /api/users/:sam.
func (s *Server) getUser(c *echo.Context) error {
	return s.with(c, func(d directory) error {
		u, err := d.User(param(c, "sam"))
		if err == nil {
			err = c.JSON(http.StatusOK, u)
		}
		return err
	})
}

// createUser handles POST /api/users.
func (s *Server) createUser(c *echo.Context) error {
	var in struct {
		ad.UserInput
		Password   string `json:"password"`
		OU         string `json:"ou"`
		MustChange bool   `json:"must_change"`
	}
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, msg("invalid data"))
	}
	return s.change(c, "user.create", in.Sam, func(d directory) error {
		u, err := d.CreateUser(in.UserInput, in.Password, in.OU, in.MustChange)
		if err == nil {
			err = c.JSON(http.StatusCreated, u)
		}
		return err
	})
}

// updateUser handles PUT /api/users/:sam.
func (s *Server) updateUser(c *echo.Context) error {
	var in ad.UserInput
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, msg("invalid data"))
	}
	sam := param(c, "sam")
	return s.change(c, "user.update", sam, func(d directory) error {
		u, err := d.UpdateUser(sam, in)
		if err == nil {
			err = c.JSON(http.StatusOK, u)
		}
		return err
	})
}

// listGroups handles GET /api/groups.
func (s *Server) listGroups(c *echo.Context) error {
	return s.with(c, func(d directory) error {
		gs, err := d.Groups(c.QueryParam("q"))
		if err == nil {
			err = c.JSON(http.StatusOK, gs)
		}
		return err
	})
}

// getGroup handles GET /api/groups/:sam.
func (s *Server) getGroup(c *echo.Context) error {
	return s.with(c, func(d directory) error {
		g, err := d.Group(param(c, "sam"))
		if err == nil {
			err = c.JSON(http.StatusOK, g)
		}
		return err
	})
}

// createGroup handles POST /api/groups.
func (s *Server) createGroup(c *echo.Context) error {
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Type        string `json:"type"`
		Scope       string `json:"scope"`
		OU          string `json:"ou"`
	}
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, msg("invalid data"))
	}
	return s.change(c, "group.create", in.Name, func(d directory) error {
		g, err := d.CreateGroup(in.Name, in.Description, in.Type, in.Scope, in.OU)
		if err == nil {
			err = c.JSON(http.StatusCreated, g)
		}
		return err
	})
}

// listOUs handles GET /api/ous.
func (s *Server) listOUs(c *echo.Context) error {
	return s.with(c, func(d directory) error {
		os, err := d.OUs()
		if err == nil {
			err = c.JSON(http.StatusOK, os)
		}
		return err
	})
}

// createOU handles POST /api/ous.
func (s *Server) createOU(c *echo.Context) error {
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parent      string `json:"parent"`
	}
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, msg("invalid data"))
	}
	return s.change(c, "ou.create", in.Name, func(d directory) error {
		o, err := d.CreateOU(in.Name, in.Description, in.Parent)
		if err == nil {
			err = c.JSON(http.StatusCreated, o)
		}
		return err
	})
}

// deleteOU handles DELETE /api/ous?dn=.
func (s *Server) deleteOU(c *echo.Context) error {
	dn := c.QueryParam("dn")
	err := s.change(c, "ou.delete", dn, func(d directory) error { return d.DeleteOU(dn) })
	if err != nil || committed(c) {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// listComputers handles GET /api/computers.
func (s *Server) listComputers(c *echo.Context) error {
	return s.with(c, func(d directory) error {
		cs, err := d.Computers(c.QueryParam("q"))
		if err == nil {
			err = c.JSON(http.StatusOK, cs)
		}
		return err
	})
}

// listGPOs handles GET /api/gpos: the GPOs with their links and the containers that link or block, read only.
func (s *Server) listGPOs(c *echo.Context) error {
	return s.with(c, func(d directory) error {
		gpos, conts, err := d.GPOs()
		if err == nil {
			err = c.JSON(http.StatusOK, map[string]any{"gpos": gpos, "containers": conts})
		}
		return err
	})
}

// getPolicy handles GET /api/policy.
func (s *Server) getPolicy(c *echo.Context) error {
	return s.with(c, func(d directory) error {
		p, err := d.Policy()
		if err == nil {
			err = c.JSON(http.StatusOK, p)
		}
		return err
	})
}

// setPolicy handles PUT /api/policy.
func (s *Server) setPolicy(c *echo.Context) error {
	var p ad.Policy
	if err := c.Bind(&p); err != nil {
		return c.JSON(http.StatusBadRequest, msg("invalid data"))
	}
	err := s.change(c, "policy.update", "domain", func(d directory) error { return d.SetPolicy(p) })
	if err != nil || committed(c) {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// csvFileNames maps the API name of each CSV file to its file name. The CLI
// import reads these names (and the older grupos.csv, usuarios.csv and computadores.csv).
var csvFileNames = map[string]string{"ous": "ous.csv", "groups": "groups.csv", "users": "users.csv", "computers": "computers.csv"}

// export handles GET /api/export/:file and returns one of the CSV files (ous,
// groups, users, computers).
func (s *Server) export(c *echo.Context) error {
	file := c.Param("file")
	return s.with(c, func(d directory) error {
		set, err := d.Export()
		if err != nil {
			return err
		}
		header, rows := map[string][]string{"ous": ad.OUsHeader, "groups": ad.GroupsHeader, "users": ad.UsersHeader, "computers": ad.ComputersHeader}[file],
			map[string][][]string{"ous": set.OUs, "groups": set.Groups, "users": set.Users, "computers": set.Computers}[file]
		if header == nil {
			return ad.ErrNotFound
		}
		h := c.Response().Header()
		h.Set("Content-Type", "text/csv; charset=utf-8")
		h.Set("Content-Disposition", `attachment; filename="`+csvFileNames[file]+`"`)
		c.Response().WriteHeader(http.StatusOK)
		return ad.WriteCSV(c.Response(), header, rows)
	})
}

// importCSV reads the uploaded CSV files (multipart fields: ous, groups, users,
// computers) and returns the plan; with apply it also runs it (the plan is
// always recomputed on the server).
func (s *Server) importCSV(apply bool) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var data ad.ImportData
		for _, f := range []struct {
			field  string
			header []string
			dst    *[]map[string]string
		}{{"ous", ad.OUsHeader, &data.OUs}, {"groups", ad.GroupsHeader, &data.Groups}, {"users", ad.UsersHeader, &data.Users}, {"computers", ad.ComputersHeader, &data.Computers}} {
			fh, err := c.FormFile(f.field)
			if err != nil {
				continue // optional file
			}
			r, err := fh.Open()
			if err != nil {
				return fail(c, err)
			}
			*f.dst, err = ad.ReadCSV(r, f.header)
			r.Close()
			if err != nil {
				return fail(c, fmt.Errorf("%s: %w", csvFileNames[f.field], err))
			}
		}
		return s.with(c, func(d directory) error {
			plan, err := d.Plan(data)
			if err != nil {
				return err
			}
			var pw map[string]string
			if apply {
				pw = d.Apply(plan, ad.GenPassword)
				for _, a := range plan {
					if a.Kind != "skip" {
						var aerr error
						if !a.OK {
							aerr = errors.New(a.Result)
						}
						audit.Log(sess(c).user, c.RealIP(), "import."+a.Kind, a.Target, aerr)
					}
				}
			}
			return c.JSON(http.StatusOK, map[string]any{"plan": plan, "passwords": pw})
		})
	}
}

// spa serves web/dist with a fallback to index.html (Vue Router routes). The
// Swagger UI files in web/dist/swagger are hidden unless swaggerOn(). The
// document (index.html and every SPA route) is served with the current theme
// already in it (data-theme, data-flat and color-scheme), so the first paint has
// the theme's look; it is never cached, since another operator may change it.
// GET and HEAD.
func spa(e *echo.Echo, dist fs.FS, swaggerOn func() bool, look func() (Theme, bool)) {
	files := http.FileServer(http.FS(dist))
	index, indexErr := fs.ReadFile(dist, "index.html")
	h := func(c *echo.Context) error {
		r := c.Request()
		if strings.HasPrefix(r.URL.Path, "/api/") {
			return c.JSON(http.StatusNotFound, msg("not found"))
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if _, err := fs.Stat(dist, name); name == "" || name == "index.html" || err != nil || (strings.HasPrefix(name, "swagger/") && !swaggerOn()) {
			c.Response().Header().Set("Cache-Control", "no-store")
			if indexErr != nil || len(index) == 0 {
				// built without the SPA (go build before npm run build)
				return c.String(http.StatusServiceUnavailable, "web interface not built (run make build)")
			}
			t, flat := look()
			return c.HTML(http.StatusOK, renderIndex(index, t, flat))
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			c.Response().Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			c.Response().Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(c.Response(), r)
		return nil
	}
	e.GET("/*", h)
	e.HEAD("/*", h)
}

// renderIndex puts the theme in index.html (markers %AD_THEME%, %AD_SCHEME% and %AD_FLAT%).
func renderIndex(index []byte, t Theme, flat bool) string {
	return strings.NewReplacer("%AD_THEME%", t.ID, "%AD_SCHEME%", t.Scheme, "%AD_FLAT%", strconv.FormatBool(flat)).Replace(string(index))
}

// committed reports whether the handler already responded (e.g. fail wrote
// the error).
func committed(c *echo.Context) bool {
	r, _ := echo.UnwrapResponse(c.Response())
	return r != nil && r.Committed
}

// self is this program's path, for the RPC helper process (`ad-manager winrpc`).
func self() string {
	p, err := os.Executable()
	if err != nil {
		return os.Args[0]
	}
	return p
}
