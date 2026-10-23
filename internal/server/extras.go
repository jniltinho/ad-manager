package server

import (
	"ad-manager/internal/importad"
	"ad-manager/internal/shares"
	"ad-manager/internal/winrpc"
	"ad-manager/internal/winshares"
	"bytes"
	"cmp"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"ad-manager/internal/ad"
	"ad-manager/internal/agent"
	"ad-manager/internal/audit"
	"ad-manager/internal/sambadc"
	"ad-manager/internal/store"

	"github.com/hibiken/asynqmon"
	"github.com/labstack/echo/v5"
)

// extras registers agents, shares, import from another AD and asynqmon.
// Without SQLite configured these routes answer 503.
func (s *Server) extras(e *echo.Echo, p *echo.Group) {
	p.GET("/agents", s.listAgents)
	p.POST("/agents", s.saveAgent)
	// a new agent token: the token is registered here, its hash goes to the file server
	p.POST("/agents/token", func(c *echo.Context) error {
		tok, hash := agent.GenerateToken()
		audit.Log(sess(c).user, c.RealIP(), "agent.token", "", nil)
		return c.JSON(http.StatusOK, map[string]string{"token": tok, "hash": hash,
			"command": "echo " + hash + " | sudo tee /opt/ad-manager/agent.token >/dev/null && sudo chmod 600 /opt/ad-manager/agent.token && sudo systemctl restart ad-manager-agent"})
	})
	p.PUT("/agents/:id", s.saveAgent)
	p.DELETE("/agents/:id", s.deleteAgent)
	p.GET("/agents/:id/health", s.proxy(http.MethodGet, "/api/health", ""))
	p.GET("/agents/:id/shares", s.proxy(http.MethodGet, "/api/shares", ""))
	p.GET("/agents/:id/dirs", s.proxy(http.MethodGet, "/api/dirs", ""))
	p.GET("/agents/:id/sizes", s.proxy(http.MethodGet, "/api/sizes", ""))
	p.GET("/agents/:id/acl", s.proxy(http.MethodGet, "/api/acl", ""))
	p.GET("/agents/:id/acl/history", s.proxy(http.MethodGet, "/api/acl/history", ""))
	p.POST("/agents/:id/tasks", s.proxy(http.MethodPost, "/api/tasks", "task"))
	p.GET("/agents/:id/tasks/:tid", s.proxy(http.MethodGet, "/api/tasks/", ""))
	p.GET("/agents/:id/sessions", s.proxy(http.MethodGet, "/api/sessions", ""))
	p.POST("/agents/:id/sessions/close", s.proxy(http.MethodPost, "/api/sessions/close", "session.close"))
	p.GET("/agents/:id/dc/replication", s.proxy(http.MethodGet, "/api/dc/replication", ""))
	p.GET("/agents/:id/dc/checks", s.proxy(http.MethodGet, "/api/dc/checks", ""))
	p.GET("/gpos/:guid/backup", s.gpoBackup)
	s.backupRoutes(p)
	s.syncRoutes(p)
	p.POST("/gpos/restore", s.gpoRestore)
	p.GET("/agents/:id/samba/logs", s.proxy(http.MethodGet, "/api/samba/logs", ""))
	// :tid is the path suffix the proxy appends (here the log file name)
	p.GET("/agents/:id/samba/logs/:tid", s.proxy(http.MethodGet, "/api/samba/logs/", ""))
	p.GET("/agents/:id/export", s.exportShares)
	p.POST("/agents/:id/import/plan", s.importShares(false))
	p.POST("/agents/:id/import/apply", s.importShares(true))

	p.GET("/ad-sources", func(c *echo.Context) error {
		if s.store == nil {
			return noStore(c)
		}
		list, err := s.store.Sources()
		if err != nil {
			return fail(c, err)
		}
		return c.JSON(http.StatusOK, list)
	})
	p.DELETE("/ad-sources/:id", func(c *echo.Context) error {
		if s.store == nil {
			return noStore(c)
		}
		id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
		if err := s.store.DeleteSource(id); err != nil {
			return fail(c, err)
		}
		return c.NoContent(http.StatusNoContent)
	})
	p.POST("/import/ad/plan", s.importAD(false))
	p.POST("/import/ad/apply", s.importAD(true))
	p.POST("/import/ad/passwords", s.importADPasswords)

	// asynqmon with its original layout, only for a logged-in administrator.
	if s.cfg.Redis.Addr != "" {
		mon := asynqmon.New(asynqmon.Options{RootPath: "/asynqmon", RedisConnOpt: agent.RedisOpt(s.cfg.Redis)})
		h := func(c *echo.Context) error {
			// the asynqmon SPA uses inline scripts and Google fonts
			c.Response().Header().Set("Content-Security-Policy",
				"default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com data:; img-src 'self' data:; frame-ancestors 'none'")
			mon.ServeHTTP(c.Response(), c.Request())
			return nil
		}
		e.Any("/asynqmon", h, s.authPage)
		e.Any("/asynqmon/*", h, s.authPage)
	}
}

// dcAgent is the first registered agent whose health says dc with this panel's realm (GPO GUIDs, the default ones,
// repeat across domains) and, when restore is set, GPO restore on (its explicit write mode).
func (s *Server) dcAgent(ctx context.Context, restore bool) (*store.Agent, error) {
	need := ""
	if restore {
		need = "gpo_restore"
	}
	return s.dcAgentCap(ctx, need)
}

// dcAgentCap is the first registered agent on a domain controller of this panel's realm that has the named
// capability on (its dedicated write mode): "gpo_restore", "password_migration", or "" for any DC.
func (s *Server) dcAgentCap(ctx context.Context, need string) (*store.Agent, error) {
	list, err := s.store.Agents()
	if err != nil {
		return nil, err
	}
	for i := range list {
		a := &list[i]
		st, out, err := agentDo(ctx, a, http.MethodGet, a.URL+"/api/health", nil)
		var h struct {
			DC                bool
			Realm             string
			GPORestore        bool `json:"gpo_restore"`
			PasswordMigration bool `json:"password_migration"`
		}
		if err != nil || st != http.StatusOK || json.Unmarshal(out, &h) != nil || !h.DC || !strings.EqualFold(h.Realm, s.cfg.AD.Domain) {
			continue
		}
		switch need {
		case "gpo_restore":
			if !h.GPORestore {
				continue
			}
		case "password_migration":
			if !h.PasswordMigration {
				continue
			}
		}
		return a, nil
	}
	return nil, nil
}

// importADPasswords handles POST /api/import/ad/passwords: the password-material migration of a new-domain
// cutover. It finds the DC agent of this domain with the dedicated password-migration write mode on and forwards
// the request; plan reads no secret, apply reads the source hashes over a sealed read-only DRSUAPI and sets them on
// this domain's matching users. The source replication password travels to the agent in the JSON body (TLS) and
// never leaves its memory; the answer and the audit (password.migrate) carry only counts, never any secret.
func (s *Server) importADPasswords(c *echo.Context) error {
	var in struct {
		URL      string `json:"url"`
		User     string `json:"user"`
		Password string `json:"password"`
		Apply    bool   `json:"apply"`
	}
	if err := c.Bind(&in); err != nil || in.URL == "" || in.User == "" || in.Password == "" {
		return c.JSON(http.StatusBadRequest, msg("enter the source DC URL, the replication account and its password"))
	}
	u, err := url.Parse(in.URL)
	if err != nil || u.Hostname() == "" {
		return c.JSON(http.StatusBadRequest, msg("invalid source DC URL"))
	}
	if s.store == nil {
		return noStore(c)
	}
	if in.Apply { // one object at a time over DRS: the answer must outlive the server's 60 s write timeout
		_ = http.NewResponseController(c.Response()).SetWriteDeadline(time.Now().Add(35 * time.Minute))
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), 32*time.Minute)
	defer cancel()
	// pin the destination by its domain SID, read through the operator's own authenticated connection — independent
	// of the agent, which is selected only by realm (the two domains share it)
	var destSID string
	if err := s.with(c, func(d directory) error {
		var e error
		destSID, e = d.DomainSID()
		return e
	}); err != nil {
		audit.Log(sess(c).user, c.RealIP(), "password.migrate", u.Hostname(), err)
		return fail(c, err)
	}
	res, err := s.runPasswordMigration(c, ctx, u.Hostname(), in.User, in.Password, destSID, in.Apply)
	if err != nil {
		if errors.Is(err, errNoPwAgent) {
			return c.JSON(http.StatusServiceUnavailable, msg(err.Error()))
		}
		return c.JSON(http.StatusBadGateway, msg(err.Error()))
	}
	return c.JSON(http.StatusOK, map[string]any{"users": res.Users, "counts": res.Counts, "applied": in.Apply})
}

// errNoPwAgent is returned when no DC agent of this domain has the password-migration write mode on.
var errNoPwAgent = errors.New("no agent on a domain controller of this domain with the password migration mode on (password_migration_user)")

// runPasswordMigration finds the DC agent of this domain with the password-migration write mode on and runs the
// plan (apply=false) or apply there over a sealed read-only DRSUAPI. The source replication password travels to the
// agent in the TLS body and never leaves its memory; the audit (password.migrate) records only counts, never a
// secret. destSID pins the destination, read independently through the operator's own connection. It is shared by
// the standalone route and the "From another AD" import.
func (s *Server) runPasswordMigration(c *echo.Context, ctx context.Context, host, user, password, destSID string, apply bool) (*sambadc.PwResult, error) {
	if s.store == nil {
		err := errors.New("local database (db.path) not configured")
		audit.Log(sess(c).user, c.RealIP(), "password.migrate", host, err)
		return nil, err
	}
	a, err := s.dcAgentCap(ctx, "password_migration")
	if err == nil && a == nil {
		err = errNoPwAgent
	}
	if err != nil {
		audit.Log(sess(c).user, c.RealIP(), "password.migrate", host, err)
		return nil, err
	}
	body, _ := json.Marshal(map[string]any{"source": map[string]string{"host": host, "user": user},
		"password": password, "apply": apply, "dest_sid": destSID})
	st, out, err := agentDo(ctx, a, http.MethodPost, a.URL+"/api/dc/passwords/migrate", body)
	if err == nil && st != http.StatusOK {
		err = agentErr(st, out)
	}
	var res sambadc.PwResult
	if err == nil && json.Unmarshal(out, &res) != nil {
		err = errors.New("invalid answer")
	}
	target := host
	if err == nil { // counts only, never a user's secret or even a per-user list in the audit
		target = fmt.Sprintf("%s → this domain (%v)", host, res.Counts)
	}
	audit.Log(sess(c).user, c.RealIP(), "password.migrate", target, err)
	if err != nil {
		return nil, fmt.Errorf("server %s: %w", a.Name, err)
	}
	return &res, nil
}

// agentErr is the error of an agent's non-200 answer.
func agentErr(st int, out []byte) error {
	var e struct{ Error string }
	_ = json.Unmarshal(out, &e)
	if e.Error == "" {
		e.Error = http.StatusText(st)
	}
	return errors.New(e.Error)
}

// gpoBackup handles GET /api/gpos/:guid/backup: the GPO exported by the agent of a domain controller of this
// panel's domain (dcAgent), as a .tar.gz download. Audited as gpo.backup.
func (s *Server) gpoBackup(c *echo.Context) error {
	if s.store == nil {
		return noStore(c)
	}
	guid := c.Param("guid")
	ctx := c.Request().Context()
	a, err := s.dcAgent(ctx, false)
	if err != nil {
		return fail(c, err)
	}
	if a == nil {
		return c.JSON(http.StatusServiceUnavailable, msg("no agent on a domain controller of this domain (install ad-manager agent on a DC and register it)"))
	}
	bctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	st, out, err := agentDo(bctx, a, http.MethodGet, a.URL+"/api/dc/gpos/"+url.PathEscape(guid)+"/backup", nil)
	if err == nil && st != http.StatusOK {
		err = agentErr(st, out)
	}
	audit.Log(sess(c).user, c.RealIP(), "gpo.backup", a.Name+":"+guid, err)
	if err != nil {
		return c.JSON(http.StatusBadGateway, msg("server "+a.Name+": "+err.Error()))
	}
	name := "gpo-" + strings.Trim(guid, "{}") + "-" + time.Now().Format("20060102-1504") + ".tar.gz"
	c.Response().Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	return c.Blob(http.StatusOK, "application/gzip", out)
}

// gpoRestore handles POST /api/gpos/restore {name, archive: base64 of a backup .tar.gz}: a new GPO (new GUID, not
// linked) created from the backup by the agent of a DC in its explicit write mode. The name must not be in use.
// The answer lists the other domain controllers, whose SYSVOL Samba does not replicate. Audited as gpo.restore.
func (s *Server) gpoRestore(c *echo.Context) error {
	if s.store == nil {
		audit.Log(sess(c).user, c.RealIP(), "gpo.restore", "", errors.New("local database (db.path) not configured"))
		return noStore(c)
	}
	var in struct {
		Name    string `json:"name"`
		Archive []byte `json:"archive"`
	}
	target := ""
	refuse := func(code int, err error) error { // every attempt is audited, refusals included
		audit.Log(sess(c).user, c.RealIP(), "gpo.restore", target, err)
		return c.JSON(code, msg(err.Error()))
	}
	if err := json.NewDecoder(io.LimitReader(c.Request().Body, sambadc.MaxBackup*2)).Decode(&in); err != nil {
		return refuse(http.StatusBadRequest, errors.New("invalid request"))
	}
	target = in.Name
	if err := sambadc.ValidGPOName(in.Name); err != nil {
		return refuse(http.StatusBadRequest, err)
	}
	if len(in.Archive) == 0 || len(in.Archive) > sambadc.MaxBackup {
		return refuse(http.StatusBadRequest, sambadc.ErrInvalidBackup)
	}
	var dcs []string
	inUse := false
	dirErr := errors.New("directory unavailable")
	err := s.with(c, func(d directory) (err error) {
		defer func() { dirErr = err }()
		gpos, _, err := d.GPOs()
		if err != nil {
			return err
		}
		for _, g := range gpos {
			inUse = inUse || strings.EqualFold(g.Name, in.Name)
		}
		cs, err := d.Computers("")
		for _, k := range cs {
			if k.IsDC {
				dcs = append(dcs, strings.ToLower(k.DNSName))
			}
		}
		return err
	})
	if err != nil || committed(c) { // s.with wrote the directory's error
		audit.Log(sess(c).user, c.RealIP(), "gpo.restore", target, cmp.Or(dirErr, errors.New("directory unavailable")))
		return err
	}
	if inUse {
		return refuse(http.StatusConflict, errors.New("a GPO with this name already exists"))
	}
	ctx := c.Request().Context()
	a, err := s.dcAgent(ctx, true)
	if err != nil {
		audit.Log(sess(c).user, c.RealIP(), "gpo.restore", target, err)
		return fail(c, err)
	}
	if a == nil {
		return refuse(http.StatusServiceUnavailable, errors.New("no agent on a domain controller of this domain with GPO restore on (gpo_restore_user)"))
	}
	target = a.Name + ":" + in.Name
	body, _ := json.Marshal(in)
	rctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	st, out, err := agentDo(rctx, a, http.MethodPost, a.URL+"/api/dc/gpos/restore", body)
	var res struct {
		GUID string `json:"guid"`
		DC   string `json:"dc"`
	}
	if err == nil && st != http.StatusOK {
		err = agentErr(st, out)
	}
	if err == nil && (json.Unmarshal(out, &res) != nil || res.GUID == "") {
		err = errors.New("invalid answer")
	}
	if err == nil {
		target += " → " + res.GUID
	}
	audit.Log(sess(c).user, c.RealIP(), "gpo.restore", target, err)
	if err != nil {
		return c.JSON(http.StatusBadGateway, msg("server "+a.Name+": "+err.Error()))
	}
	sync := []string{}
	for _, d := range dcs {
		if d != "" && d != strings.ToLower(res.DC) {
			sync = append(sync, d)
		}
	}
	return c.JSON(http.StatusOK, map[string]any{"guid": res.GUID, "dc": res.DC, "sync": sync})
}

// noStore answers 503 when no local database is configured.
func noStore(c *echo.Context) error {
	return c.JSON(http.StatusServiceUnavailable, msg("local database (db.path) not configured"))
}

// listAgents handles GET /api/agents.
func (s *Server) listAgents(c *echo.Context) error {
	if s.store == nil {
		return noStore(c)
	}
	list, err := s.store.Agents()
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(http.StatusOK, list)
}

// saveAgent handles POST /api/agents and PUT /api/agents/:id.
func (s *Server) saveAgent(c *echo.Context) error {
	if s.store == nil {
		return noStore(c)
	}
	var in struct {
		Name     string `json:"name"`
		URL      string `json:"url"`
		Token    string `json:"token"`
		Insecure bool   `json:"insecure"`
	}
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, msg("invalid data"))
	}
	u, err := url.Parse(in.URL)
	if strings.TrimSpace(in.Name) == "" || err != nil || u.Scheme != "https" || u.Host == "" {
		return c.JSON(http.StatusBadRequest, msg("enter the agent's name and https:// URL"))
	}
	a := &store.Agent{Name: strings.TrimSpace(in.Name), URL: strings.TrimRight(in.URL, "/"), Token: in.Token, Insecure: in.Insecure}
	if id := c.Param("id"); id != "" {
		a.ID, _ = strconv.ParseInt(id, 10, 64)
	} else if in.Token == "" {
		return c.JSON(http.StatusBadRequest, msg("enter the token generated by `ad-manager agent token`"))
	}
	err = s.store.SaveAgent(a)
	audit.Log(sess(c).user, c.RealIP(), "agent.save", a.Name, err)
	if err != nil {
		return fail(c, fmt.Errorf("%w: %v", ad.ErrConflict, err))
	}
	return c.JSON(http.StatusOK, a)
}

// deleteAgent handles DELETE /api/agents/:id.
func (s *Server) deleteAgent(c *echo.Context) error {
	if s.store == nil {
		return noStore(c)
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	err := s.store.DeleteAgent(id)
	audit.Log(sess(c).user, c.RealIP(), "agent.delete", c.Param("id"), err)
	if err != nil {
		return fail(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// proxy forwards the call to the agent with its token. A non-empty action
// audits the call (task POST).
func (s *Server) proxy(method, path, action string) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if s.store == nil {
			return noStore(c)
		}
		id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
		a, err := s.store.Agent(id)
		if err != nil {
			return c.JSON(http.StatusNotFound, msg("server not registered"))
		}
		target := a.URL + path
		if tid := c.Param("tid"); tid != "" {
			target += url.PathEscape(tid)
		}
		if q := c.Request().URL.RawQuery; q != "" {
			target += "?" + q
		}
		var body []byte
		if method == http.MethodPost {
			if body, err = io.ReadAll(io.LimitReader(c.Request().Body, 1<<20)); err != nil {
				return c.JSON(http.StatusBadRequest, msg("invalid request body"))
			}
		}
		ctx := c.Request().Context()
		if slow := map[string]time.Duration{"/api/sizes": 2 * time.Minute, "/api/dc/checks": 10 * time.Minute}[path]; slow > 0 {
			var cancel context.CancelFunc // du on large folders, dbcheck on a large directory
			ctx, cancel = context.WithTimeout(ctx, slow)
			defer cancel()
			// the server's WriteTimeout (60 s) would cut the answer before the agent's: this one may write until then
			_ = http.NewResponseController(c.Response()).SetWriteDeadline(time.Now().Add(slow + 15*time.Second))
		}
		status, out, err := agentDo(ctx, a, method, target, body)
		if action != "" {
			var t agent.TaskReq
			var name struct {
				Name string `json:"name"`
				Path string `json:"path"`
			}
			if action == "task" { // the task's own type and target
				_ = json.Unmarshal(body, &t)
				_ = json.Unmarshal(t.Payload, &name)
			} else { // the route's action; the target only from the agent's validated reply
				t.Type = action
				var r struct {
					Closed []struct{ User, Machine string }
				}
				if err == nil && json.Unmarshal(out, &r) == nil {
					var who []string
					for _, s := range r.Closed {
						who = append(who, s.User+"@"+s.Machine)
					}
					name.Name = strings.Join(who, ", ")
				}
			}
			var aerr error = err
			if err == nil && status >= 300 {
				aerr = errors.New(http.StatusText(status))
			}
			audit.Log(sess(c).user, c.RealIP(), t.Type, a.Name+":"+strings.TrimSpace(name.Name+" "+name.Path), aerr)
		}
		if err != nil {
			return c.JSON(http.StatusBadGateway, msg("server "+a.Name+" unavailable: "+err.Error()))
		}
		return c.Blob(status, "application/json", out)
	}
}

// agentClient is the HTTP client for agent a: its certificate choice, and no redirects (the Bearer token must not
// go to another origin or fall back to HTTP).
func agentClient(a *store.Agent) *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: a.Insecure, MinVersion: tls.VersionTLS12}}} //nolint:gosec // per-agent opt-in (self-signed certificate)
}

// agentDo calls the agent with its token (full URL in target). Without a
// deadline in ctx it uses 15 s.
func agentDo(ctx context.Context, a *store.Agent, method, target string, body []byte) (int, []byte, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := agentClient(a).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, maxAgentAnswer+1))
	if err == nil && len(out) > maxAgentAnswer { // never pass on a cut JSON
		return 0, nil, fmt.Errorf("the answer is larger than %d MiB", maxAgentAnswer>>20)
	}
	return resp.StatusCode, out, err
}

// maxAgentAnswer is the largest agent answer the panel reads into memory to pass on.
const maxAgentAnswer = 4 << 20

// importAD reads another AD and plans or applies on the current domain: its users, groups, OUs and computers
// (objects, the default), its GPOs (new, unlinked GPOs made by the DC agent in its explicit write mode) and the
// shares of one of its file servers (recreated on a registered agent, as the "Folders" import).
func (s *Server) importAD(apply bool) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var in struct {
			URL       string `json:"url"`
			Domain    string `json:"domain"`
			User      string `json:"user"`
			Password  string `json:"password"`
			Insecure  bool   `json:"insecure"`
			SaveAs    string `json:"save_as"`
			Objects   *bool  `json:"objects"` // default true
			GPOs      bool   `json:"gpos"`
			DNS       bool   `json:"dns"`
			Dynamic   bool   `json:"include_dynamic"` // DNS: also the records clients registered
			Zones     bool   `json:"create_zones"`    // DNS: create the zones missing here (zone.create)
			Passwords bool   `json:"passwords"`       // new-domain cutover: migrate the users' password hashes (DC agent, DRSUAPI)
			Shares    struct {
				Host  string `json:"host"`  // file server of the source domain
				Agent int64  `json:"agent"` // target agent
				Map   string `json:"map"`   // E:\=/srv/shares
			} `json:"shares"`
		}
		if err := c.Bind(&in); err != nil || in.URL == "" || in.Domain == "" || in.User == "" || in.Password == "" {
			return c.JSON(http.StatusBadRequest, msg("enter the source AD URL, domain, user and password"))
		}
		objects := in.Objects == nil || *in.Objects
		if (apply && (in.GPOs || in.DNS)) || in.Passwords { // the DC agent copies GPOs for up to 15 minutes, each DNS
			// record is one RPC call, and the password migration replicates one object at a time: the answer must
			// outlive the server's 60 s write timeout
			_ = http.NewResponseController(c.Response()).SetWriteDeadline(time.Now().Add(35 * time.Minute))
		}
		withShares := in.Shares.Host != ""
		var winPre, linuxDst string
		if withShares {
			var ok bool
			winPre, linuxDst, ok = strings.Cut(in.Shares.Map, "=")
			if winshares.ValidHost(in.Shares.Host) != nil || in.Shares.Agent == 0 || !ok || winPre == "" || !strings.HasPrefix(linuxDst, "/") {
				return c.JSON(http.StatusBadRequest, msg(`shares: source server (name or address), target server and the map E:\=/srv/shares`))
			}
		}
		if !objects && !in.GPOs && !in.DNS && !withShares && !in.Passwords {
			return c.JSON(http.StatusBadRequest, msg("choose what to import"))
		}
		src, err := importad.Open(in.URL, in.Domain, in.User, in.Password, in.Insecure)
		if err != nil {
			return c.JSON(http.StatusBadGateway, msg("source AD: "+err.Error()))
		}
		defer src.Close()
		if in.SaveAs != "" && s.store != nil { // profile without the password
			_ = s.store.SaveSource(&store.Source{Name: in.SaveAs, URL: in.URL, Domain: in.Domain, User: in.User, Insecure: in.Insecure})
		}
		counts := map[string]int{}
		var data ad.ImportData
		if objects {
			set, err := src.Export()
			if err != nil {
				return fail(c, err)
			}
			data = ad.SetToData(set)
			counts["users"], counts["groups"], counts["ous"], counts["computers"] = len(set.Users), len(set.Groups), len(set.OUs), len(set.Computers)
		}
		var srcGPOs []ad.GPO
		if in.GPOs {
			if srcGPOs, err = src.GPOs(); err != nil {
				return c.JSON(http.StatusBadGateway, msg("source AD: "+err.Error()))
			}
			counts["gpos"] = len(srcGPOs)
		}
		var sharePlan []ad.Action
		var shareReqs map[string]shares.ShareReq
		var shareAgent *store.Agent
		if withShares {
			if shareAgent, sharePlan, shareReqs, err = s.planSourceShares(c, src, in.User, in.Password, in.Shares.Host, in.Shares.Agent,
				map[string]string{winPre: linuxDst}); shareAgent == nil {
				return err
			}
			counts["shares"] = len(sharePlan)
		}
		var srcDNS []ad.DNSRecord
		var srcDCs []string
		if in.DNS {
			var bad int
			if srcDNS, bad, err = src.DNSRecords(); err == nil {
				srcDCs, err = src.DCNames()
			}
			if err != nil {
				return c.JSON(http.StatusBadGateway, msg("source AD: "+err.Error()))
			}
			counts["dns"], counts["dns_unreadable"] = len(srcDNS), bad
		}
		return s.with(c, func(d directory) error {
			var objPlan, gpoPlan []ad.Action
			var dnsPlan importad.DNSPlan
			if objects {
				if objPlan, err = d.Plan(data); err != nil {
					return err
				}
			}
			if in.GPOs {
				here, _, err := d.GPOs()
				if err != nil {
					return err
				}
				gpoPlan = planGPOs(srcGPOs, here)
			}
			if in.DNS {
				here, _, err := d.DNSRecords()
				if err != nil {
					return err
				}
				dcs := slices.Clone(srcDCs)
				cs, err := d.Computers("")
				if err != nil {
					return err
				}
				for _, k := range cs {
					if k.IsDC {
						dcs = append(dcs, k.DNSName)
					}
				}
				if h, err := d.DCHostName(); err == nil {
					dcs = append(dcs, h)
				}
				dnsPlan = importad.PlanDNS(srcDNS, here, dcs, in.Dynamic, in.Zones)
				maps.Copy(counts, dnsPlan.Counts)
			}
			var pw map[string]string
			if apply {
				if objects {
					pw = d.Apply(objPlan, ad.GenPassword)
					for _, a := range objPlan {
						if a.Kind != "skip" {
							var aerr error
							if !a.OK {
								aerr = errors.New(a.Result)
							}
							audit.Log(sess(c).user, c.RealIP(), "import."+a.Kind, a.Target, aerr)
						}
					}
					// carry the non-secret metadata (last-logon of users/computers, OS of computers) to the
					// objects just imported, so the inactivity view and the OS column match the source
					meta, err := src.Metadata()
					if err != nil {
						audit.Log(sess(c).user, c.RealIP(), "import.metadata", "", err)
					}
					for _, m := range meta {
						if e := d.SetImportMetadata(m.Sam, m.LastLogon, m.OS, m.OSVersion); e != nil {
							audit.Log(sess(c).user, c.RealIP(), "import.metadata", m.Sam, e)
						}
					}
					counts["metadata"] = len(meta)
				}
				if in.GPOs {
					s.applyGPOs(c, gpoPlan, src.Host(), sourceUser(in.User, src.NetBIOS()), in.Password)
				}
				if withShares {
					sendShares(c, shareAgent, sharePlan, shareReqs)
				}
				if in.DNS {
					s.applyDNS(c, d, dnsPlan)
				}
			}
			// password migration (new-domain cutover): the DC agent reads the source hashes over a sealed read-only
			// DRSUAPI and sets them here. plan classifies (reads no secret), apply sets. The source credential is the
			// one typed in this form; nothing is stored or logged. Runs for both plan and apply (the agent plan is
			// read-only). A failure here is reported as one row, not a failure of the whole import.
			var pwPlan []ad.Action
			if in.Passwords {
				// renew the response write deadline: earlier stages (objects, GPOs, DNS) may have used much of the
				// first budget, and the migration replicates one object at a time
				_ = http.NewResponseController(c.Response()).SetWriteDeadline(time.Now().Add(35 * time.Minute))
				pctx, cancel := context.WithTimeout(c.Request().Context(), 33*time.Minute)
				defer cancel()
				// a failure in this stage is reported as its own row — it never discards the objects/GPOs/DNS/shares
				// results already written above (and their one-time generated passwords)
				destSID, e := d.DomainSID()
				if e != nil {
					pwPlan = append(pwPlan, ad.Action{Kind: "password", Target: src.Host(), Detail: "destination SID: " + e.Error()})
				} else if res, e := s.runPasswordMigration(c, pctx, src.Host(), sourceUser(in.User, src.NetBIOS()), in.Password, destSID, apply); e != nil {
					pwPlan = append(pwPlan, ad.Action{Kind: "password", Target: src.Host(), Detail: e.Error()})
				} else {
					for _, u := range res.Users {
						ok := u.Outcome != "error" && u.Outcome != "rejected"
						detail := u.Outcome
						if u.Reason != "" { // show the sanitized reason (why ntlm_only/error/rejected), never a secret
							detail = u.Outcome + " — " + u.Reason
						}
						pwPlan = append(pwPlan, ad.Action{Kind: "password", Target: u.Sam, Detail: detail, OK: ok, Result: u.Outcome})
						// a user whose production password was set no longer uses the generated one: drop it from
						// the download so no invalid credential is handed out
						if apply && (u.Outcome == "set" || u.Outcome == "ntlm_only" || u.Outcome == "same") {
							delete(pw, u.Sam)
						}
					}
					for k, v := range res.Counts {
						counts["pw_"+k] = v
					}
				}
			}
			plan := append(append(append(append(append([]ad.Action{}, objPlan...), gpoPlan...), sharePlan...), dnsPlan.Actions...), pwPlan...)
			return c.JSON(http.StatusOK, map[string]any{"plan": plan, "passwords": pw, "counts": counts})
		})
	}
}

// applyDNS writes the zone.create zones and the dns.add records of p on this domain's DC (MS-DNSP with the
// operator's credential, the same rule as POST /api/dns/records: ad.CheckDNSRecord already passed each record in the
// plan), recording each result and audit line.
func (s *Server) applyDNS(c *echo.Context, d directory, p importad.DNSPlan) {
	t := winrpc.Target{Addr: s.dcHost()}
	host, herr := d.DCHostName()
	t.Host = host
	ctx, cred := c.Request().Context(), s.rpcCred(c)
	importad.ApplyDNS(p, func(zone string) error {
		if herr != nil {
			return herr
		}
		return s.rpc.CreateZone(ctx, t, zone, cred)
	}, func(ch importad.DNSChange) error {
		if herr != nil {
			return herr
		}
		return s.rpc.UpdateRecord(ctx, t, ch.Zone, winrpc.Record{Name: ch.Name, Type: ch.Type, TTL: ch.TTL, Data: ch.Data}, false, cred)
	}, func(kind, what string, err error) { audit.Log(sess(c).user, c.RealIP(), kind, what, err) })
}

// sourceUser is the source credential as samba-tool takes it: user@realm as given, otherwise NETBIOS\user.
func sourceUser(user, netbios string) string {
	if strings.ContainsAny(user, `@\`) || netbios == "" {
		return user
	}
	return netbios + `\` + user
}

// planGPOs plans the source GPOs: a name already used here is skipped (the default GPOs always are), the others
// become gpo.import (target: name, detail: source GUID). Links are not copied.
func planGPOs(src, here []ad.GPO) []ad.Action {
	used := map[string]bool{}
	for _, g := range here {
		used[strings.ToLower(g.Name)] = true
	}
	plan := make([]ad.Action, 0, len(src))
	for _, g := range src {
		switch {
		case g.Name == "" || sambadc.ValidGPOName(g.Name) != nil:
			plan = append(plan, ad.Action{Kind: "skip", Target: g.GUID, Detail: "GPO without a usable name"})
		case used[strings.ToLower(g.Name)]:
			plan = append(plan, ad.Action{Kind: "skip", Target: g.Name, Detail: "a GPO with this name already exists"})
		default:
			used[strings.ToLower(g.Name)] = true
			plan = append(plan, ad.Action{Kind: "gpo.import", Target: g.Name, Detail: g.GUID + " (new GPO, not linked)"})
		}
	}
	return plan
}

// applyGPOs has the DC agent in its write mode copy the gpo.import actions of plan from the source DC host,
// recording each result and audit line.
func (s *Server) applyGPOs(c *echo.Context, plan []ad.Action, host, user, password string) {
	type item struct {
		GUID string `json:"guid"`
		Name string `json:"name"`
	}
	var items []item
	idx := map[string]int{}
	for i, a := range plan {
		if a.Kind == "gpo.import" {
			guid, _, _ := strings.Cut(a.Detail, " ")
			items = append(items, item{guid, a.Target})
			idx[strings.ToUpper(guid)] = i
		}
	}
	if len(items) == 0 {
		return
	}
	fail := func(err error) {
		for _, i := range idx {
			plan[i].Result = err.Error()
			audit.Log(sess(c).user, c.RealIP(), "import.gpo.import", plan[i].Target, err)
		}
	}
	if s.store == nil {
		fail(errors.New("local database (db.path) not configured"))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), 16*time.Minute)
	defer cancel()
	a, err := s.dcAgent(ctx, true)
	if err == nil && a == nil {
		err = errors.New("no agent on a domain controller of this domain with GPO restore on (gpo_restore_user)")
	}
	if err != nil {
		fail(err)
		return
	}
	body, _ := json.Marshal(map[string]any{"source": map[string]string{"host": host, "user": user, "password": password}, "gpos": items})
	st, out, err := agentDo(ctx, a, http.MethodPost, a.URL+"/api/dc/gpos/import", body)
	var res []struct {
		GUID    string `json:"guid"`
		NewGUID string `json:"new_guid"`
		Error   string `json:"error"`
	}
	if err == nil && st != http.StatusOK {
		err = agentErr(st, out)
	}
	if err == nil && json.Unmarshal(out, &res) != nil {
		err = errors.New("invalid answer")
	}
	if err != nil {
		fail(fmt.Errorf("server %s: %w", a.Name, err))
		return
	}
	done := map[int]bool{}
	for _, r := range res {
		i, ok := idx[strings.ToUpper(r.GUID)]
		if !ok || done[i] {
			continue
		}
		done[i] = true
		p := &plan[i]
		var aerr error
		if r.Error != "" {
			aerr = errors.New(r.Error)
			p.Result = r.Error
		} else {
			p.OK, p.Result = true, r.NewGUID
		}
		audit.Log(sess(c).user, c.RealIP(), "import.gpo.import", a.Name+":"+p.Target+" → "+r.NewGUID, aerr)
	}
	for _, i := range idx { // a GPO the agent did not answer for
		if !done[i] {
			err := errors.New("no result from the agent")
			plan[i].Result = err.Error()
			audit.Log(sess(c).user, c.RealIP(), "import.gpo.import", a.Name+":"+plan[i].Target, err)
		}
	}
}

// mapIdentities rewrites the names of a share plan from the source domain to this one (SRC\x → DEST\x: the
// objects import keeps the names) and drops, with a warning, any name of another domain or a local one, which
// would not resolve here or, through a trust, could grant the old domain access.
func mapIdentities(p *winshares.SharePlan, srcNB, destNB string) {
	for _, l := range []*[]string{&p.Full, &p.FullUser, &p.RW, &p.RWUsers, &p.RO, &p.ROUsers} {
		out := (*l)[:0]
		for _, n := range *l {
			dom, name, ok := strings.Cut(n, `\`)
			switch {
			case ok && (strings.EqualFold(dom, srcNB) || strings.EqualFold(dom, destNB)):
				out = append(out, destNB+`\`+name)
			default:
				p.Warnings = append(p.Warnings, "not from the source domain, not imported: "+n)
			}
		}
		*l = slices.Compact(out)
	}
}

// planSourceShares reads the shares of the source file server host (rpcclient/smbcacls with the source
// credential in a temporary 0600 file), works out the effective access through the source's groups, and plans
// them on the target agent like the "Folders" import. On an error it has answered the request (nil agent).
func (s *Server) planSourceShares(c *echo.Context, src *importad.Source, user, password, host string, agentID int64, driveMap map[string]string) (*store.Agent, []ad.Action, map[string]shares.ShareReq, error) {
	a, current, err := s.agentSharesByID(c, agentID)
	if a == nil {
		return nil, nil, nil, err
	}
	destNB := strings.ToUpper(s.netbios)
	if destNB == "" {
		return nil, nil, nil, c.JSON(http.StatusServiceUnavailable, msg("the NetBIOS name of this domain is not known yet (log in again)"))
	}
	inv, err := src.Inventory()
	if err != nil {
		return nil, nil, nil, c.JSON(http.StatusBadGateway, msg("source AD: "+err.Error()))
	}
	dir := winshares.Directory{NetBIOS: src.NetBIOS(), Groups: inv.Members, Users: map[string]bool{}}
	for _, u := range inv.Users {
		dir.Users[strings.ToLower(u.Sam)] = true
	}
	auth, err := winshares.AuthFile("", user, password, src.NetBIOS())
	if err != nil {
		return nil, nil, nil, fail(c, err)
	}
	defer os.Remove(auth)
	ctx, cancel := context.WithTimeout(c.Request().Context(), 5*time.Minute)
	defer cancel()
	ws, err := winshares.Collect(ctx, winshares.Exec, host, auth)
	if err != nil {
		return nil, nil, nil, c.JSON(http.StatusBadGateway, msg("shares of "+host+": "+err.Error()))
	}
	foreign := winshares.KeepDomain(ws, src.NetBIOS())
	plans := winshares.Plan(ws, dir, driveMap)
	for i := range plans {
		for _, n := range foreign[strings.ToLower(plans[i].Name)] {
			plans[i].Warnings = append(plans[i].Warnings, "not from the source domain, not imported: "+n)
		}
	}
	var refused []ad.Action
	plans = slices.DeleteFunc(plans, func(p winshares.SharePlan) bool {
		if p.Denies { // a deny entry left out could grant more than the source: not imported
			refused = append(refused, ad.Action{Kind: "skip", Target: p.Name, Detail: "has deny entries: not imported"})
		}
		return p.Denies
	})
	for i := range plans {
		mapIdentities(&plans[i], src.NetBIOS(), destNB)
	}
	rows := rowMaps(shares.CSVHeader, winshares.CSVRows(plans))
	for _, r := range rows { // the source says nothing about these: an existing share keeps its own
		delete(r, "recycle_bin")
		delete(r, "abe")
		delete(r, "audit")
	}
	plan, reqs := planShares(rows, current)
	plan = append(plan, refused...)
	// the access of each new share (full control, write, read) and the warnings, after its path
	extra := map[string]string{}
	for _, p := range plans {
		var parts []string
		for _, l := range []struct {
			label string
			names []string
		}{{"full control", append(slices.Clone(p.Full), p.FullUser...)}, {"write", append(slices.Clone(p.RW), p.RWUsers...)}, {"read", append(slices.Clone(p.RO), p.ROUsers...)}} {
			if len(l.names) > 0 {
				parts = append(parts, l.label+": "+strings.Join(l.names, ", "))
			}
		}
		parts = append(parts, p.Warnings...)
		extra[strings.ToLower(p.Name)] = strings.Join(parts, "; ")
	}
	for i := range plan {
		if e := extra[strings.ToLower(plan[i].Target)]; e != "" && (plan[i].Kind == "share.create" || plan[i].Kind == "share.update") {
			plan[i].Detail = plan[i].Detail + " — " + e
		}
	}
	return a, plan, reqs, nil
}
