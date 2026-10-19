package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"time"

	"ad-manager/internal/audit"
	"ad-manager/internal/secretbox"
	"ad-manager/internal/store"

	"github.com/labstack/echo/v5"
)

// syncRoutes: the Sync page. The agents keep and run the sync jobs (folders brought from SMB shares); the panel
// lists them, saves, previews, runs, cancels, reads the logs and audits each change.
func (s *Server) syncRoutes(p *echo.Group) {
	p.GET("/agents/:id/syncs", s.syncProxy("", ""))
	p.PUT("/agents/:id/syncs/:sid", s.syncSave)
	p.DELETE("/agents/:id/syncs/:sid", s.syncDelete)
	p.POST("/agents/:id/syncs/browse", s.syncBrowse)
	p.POST("/agents/:id/syncs/validate", s.syncProxy("", ""))
	p.POST("/agents/:id/syncs/:sid/preview", s.syncProxy("sync.preview", "sid"))
	p.POST("/agents/:id/syncs/:sid/run", s.syncProxy("sync.run", "sid"))
	p.POST("/agents/:id/sync-runs/:run/cancel", s.syncProxy("sync.cancel", "run"))
	p.GET("/agents/:id/sync-runs/:run/log", s.syncProxy("", "run"))
	// the quarantines (what the runs deleted or replaced): list, files, give back, download, empty a job's
	p.GET("/agents/:id/syncs/quarantines", s.syncProxy("", ""))
	p.GET("/agents/:id/syncs/quarantines/:qid/files", s.syncProxy("", "qid"))
	p.POST("/agents/:id/syncs/quarantines/:qid/restore", s.syncProxy("sync.quarantine.restore", "qid"))
	p.GET("/agents/:id/syncs/quarantines/:qid/file", s.syncQuarantineFile)
	p.POST("/agents/:id/syncs/:sid/quarantine/empty", s.syncProxy("sync.quarantine.empty", "sid"))
}

var (
	syncParamRe = regexp.MustCompile(`^[A-Za-z0-9-]{1,40}$`)
	// a quarantine: <job>-<run>
	quarantineRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}-\d{8}T\d{6}Z-[0-9a-f]{8}$`)
)

// syncQuarantineFile handles GET /api/agents/:id/syncs/quarantines/:qid/file?path=: one file of a quarantine,
// streamed from the agent and audited.
func (s *Server) syncQuarantineFile(c *echo.Context) error {
	a, err := s.agentByParam(c)
	if a == nil {
		return err
	}
	qid, rel := c.Param("qid"), c.QueryParam("path")
	if !quarantineRe.MatchString(qid) || rel == "" || len(rel) > 4096 {
		return c.JSON(http.StatusBadRequest, msg("invalid request"))
	}
	_ = http.NewResponseController(c.Response()).SetWriteDeadline(time.Now().Add(6 * time.Hour))
	target := a.URL + "/api/syncs/quarantines/" + url.PathEscape(qid) + "/file?path=" + url.QueryEscape(rel)
	req, err := http.NewRequestWithContext(c.Request().Context(), http.MethodGet, target, nil)
	if err != nil {
		return fail(c, err)
	}
	req.Header.Set("Authorization", "Bearer "+a.Token)
	resp, err := agentClient(a).Do(req)
	if err == nil && resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		err = agentErr(resp.StatusCode, b)
	}
	audit.Log(sess(c).user, c.RealIP(), "sync.quarantine.download", a.Name+":"+qid+" "+rel, err)
	if err != nil {
		return c.JSON(http.StatusBadGateway, msg("server "+a.Name+": "+err.Error()))
	}
	defer resp.Body.Close()
	h := c.Response().Header()
	for _, k := range []string{"Content-Disposition", "Content-Length"} {
		if v := resp.Header.Get(k); v != "" {
			h.Set(k, v)
		}
	}
	return c.Stream(http.StatusOK, "application/octet-stream", resp.Body)
}

// syncProxy passes the request to the agent's same path (in the operator's name) and audits action with the
// job or run named by param; the agent checks everything again.
func (s *Server) syncProxy(action, param string) echo.HandlerFunc {
	return func(c *echo.Context) error {
		a, err := s.agentByParam(c)
		if a == nil {
			return err
		}
		re := syncParamRe
		if param == "qid" {
			re = quarantineRe
		}
		if param != "" && !re.MatchString(c.Param(param)) {
			return c.JSON(http.StatusBadRequest, msg("invalid request"))
		}
		var body []byte
		if c.Request().Method == http.MethodPut || c.Request().Method == http.MethodPost {
			if body, err = io.ReadAll(io.LimitReader(c.Request().Body, 1<<20)); err != nil {
				return c.JSON(http.StatusBadRequest, msg("invalid request body"))
			}
		}
		path := c.Request().URL.Path
		path = path[len("/api/agents/"+c.Param("id")):] // /syncs/… or /sync-runs/…
		target := a.URL + "/api" + path
		if c.Request().Method == http.MethodPost {
			target += "?by=" + url.QueryEscape(sess(c).user)
		}
		ctx := c.Request().Context()
		if action == "sync.quarantine.restore" || action == "sync.quarantine.empty" { // many files: more than 15 s
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, 30*time.Minute)
			defer cancel()
			_ = http.NewResponseController(c.Response()).SetWriteDeadline(time.Now().Add(31 * time.Minute))
		}
		st, out, err := agentDo(ctx, a, c.Request().Method, target, body)
		if err == nil && st >= 300 {
			err = agentErr(st, out)
		}
		if action != "" {
			what := a.Name + ":" + c.Param(param)
			audit.Log(sess(c).user, c.RealIP(), action, what, err)
		}
		if err != nil {
			return c.JSON(http.StatusBadGateway, msg("server "+a.Name+": "+err.Error()))
		}
		if bytes.HasPrefix(out, []byte("{")) || bytes.HasPrefix(out, []byte("[")) {
			return c.JSONBlob(st, out)
		}
		return c.Blob(st, "text/plain; charset=utf-8", out)
	}
}

// syncBox is the key of the sync passwords kept in the panel's database (state folder, 0600).
func (s *Server) syncBox() (*secretbox.Box, error) {
	return secretbox.Load(filepath.Join(s.cfg.StateDir(), "secret.key"))
}

// syncSecretCtx binds a sealed password to the agent's registration (id and creation: an id used again by another
// agent opens nothing) and the job.
func syncSecretCtx(a *store.Agent, job string) string {
	return fmt.Sprintf("sync:%d:%d:%s", a.ID, a.Created.UnixNano(), job)
}

// keptPassword is the password kept for a job ("" when none, or when it no longer opens).
func (s *Server) keptPassword(a *store.Agent, job string) string {
	if s.store == nil || job == "" {
		return ""
	}
	box, err := s.syncBox()
	if err != nil {
		return ""
	}
	sealed, err := s.store.SyncSecret(a.ID, job)
	if err != nil || sealed == nil {
		return ""
	}
	b, err := box.Open(sealed, syncSecretCtx(a, job))
	if err != nil {
		return ""
	}
	return string(b)
}

// keepPassword seals pw for a job (after the agent took the job).
func (s *Server) keepPassword(a *store.Agent, job, pw string) error {
	if s.store == nil {
		return nil
	}
	box, err := s.syncBox()
	if err != nil {
		return err
	}
	return s.store.SetSyncSecret(a.ID, job, box.Seal([]byte(pw), syncSecretCtx(a, job)))
}

// syncSave handles PUT /api/agents/:id/syncs/:sid: an NTLM password typed in the wizard is kept sealed in the
// panel's database and sent to the agent (TLS, its token), which keeps its own sealed copy for the schedules; the
// audit and the answer never carry it.
func (s *Server) syncSave(c *echo.Context) error {
	a, err := s.agentByParam(c)
	if a == nil {
		return err
	}
	job := c.Param("sid")
	if !syncParamRe.MatchString(job) {
		return c.JSON(http.StatusBadRequest, msg("invalid request"))
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request().Body, 1<<20))
	var in map[string]any
	if err != nil || json.Unmarshal(raw, &in) != nil {
		return c.JSON(http.StatusBadRequest, msg("invalid request body"))
	}
	typed, _ := in["password"].(string)
	delete(in, "password")
	src, _ := in["source"].(map[string]any)
	auth, _ := src["auth"].(string)
	pwFile, _ := in["password_file"].(string)
	if auth == "ntlm" && pwFile == "" {
		pw := typed
		if pw == "" {
			pw = s.keptPassword(a, job)
		}
		if pw == "" {
			return c.JSON(http.StatusBadRequest, msg("type the password of the source user"))
		}
		in["password"] = pw
	}
	body, _ := json.Marshal(in)
	st, out, err := agentDo(c.Request().Context(), a, http.MethodPut, a.URL+"/api/syncs/"+url.PathEscape(job), body)
	if err == nil && st != http.StatusOK {
		err = agentErr(st, out)
	}
	switch { // kept only once the agent took the job (a refused edit changes nothing)
	case err != nil:
	case auth == "ntlm" && pwFile == "" && typed != "":
		err = s.keepPassword(a, job, typed)
	case (auth != "ntlm" || pwFile != "") && s.store != nil:
		_ = s.store.DeleteSyncSecret(a.ID, job) // saved with Kerberos or a password file: nothing kept here
	}
	delete(in, "password")
	delete(in, "confirm")
	what, _ := json.Marshal(in)
	audit.Log(sess(c).user, c.RealIP(), "sync.save", a.Name+":"+job+" "+string(what), err)
	if err != nil {
		return c.JSON(http.StatusBadGateway, msg("server "+a.Name+": "+err.Error()))
	}
	return c.JSONBlob(http.StatusOK, out)
}

// syncDelete handles DELETE /api/agents/:id/syncs/:sid (the kept password goes too).
func (s *Server) syncDelete(c *echo.Context) error {
	a, err := s.agentByParam(c)
	if a == nil {
		return err
	}
	job := c.Param("sid")
	if !syncParamRe.MatchString(job) {
		return c.JSON(http.StatusBadRequest, msg("invalid request"))
	}
	st, out, err := agentDo(c.Request().Context(), a, http.MethodDelete, a.URL+"/api/syncs/"+url.PathEscape(job), nil)
	if err == nil && st != http.StatusOK {
		err = agentErr(st, out)
	}
	if err == nil && s.store != nil {
		err = s.store.DeleteSyncSecret(a.ID, job)
	}
	audit.Log(sess(c).user, c.RealIP(), "sync.delete", a.Name+":"+job, err)
	if err != nil {
		return c.JSON(http.StatusBadGateway, msg("server "+a.Name+": "+err.Error()))
	}
	return c.JSONBlob(http.StatusOK, out)
}

// syncBrowse handles POST /api/agents/:id/syncs/browse {source, password?, password_file?, job?}: the shares of
// the source server (no share) or the folders of a share path, listed by the agent with the job's credentials
// (the password typed, or the one kept for job).
func (s *Server) syncBrowse(c *echo.Context) error {
	a, err := s.agentByParam(c)
	if a == nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request().Body, 1<<16))
	var in map[string]any
	if err != nil || json.Unmarshal(raw, &in) != nil {
		return c.JSON(http.StatusBadRequest, msg("invalid request body"))
	}
	typed, _ := in["password"].(string)
	job, _ := in["job"].(string)
	if job != "" && !syncParamRe.MatchString(job) {
		return c.JSON(http.StatusBadRequest, msg("invalid request"))
	}
	src, _ := in["source"].(map[string]any)
	if auth, _ := src["auth"].(string); auth == "ntlm" {
		pw := typed
		if pw == "" {
			pw = s.keptPassword(a, job)
		}
		in["password"] = pw
	}
	body, _ := json.Marshal(in)
	limit := 90 * time.Second
	if size, _ := in["size"].(bool); size { // a count walks the whole tree on the agent
		limit = 6 * time.Minute
		_ = http.NewResponseController(c.Response()).SetWriteDeadline(time.Now().Add(limit))
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), limit)
	defer cancel()
	st, out, err := agentDo(ctx, a, http.MethodPost, a.URL+"/api/syncs/browse", body)
	if err == nil && st != http.StatusOK {
		err = agentErr(st, out)
	}
	if err != nil {
		return c.JSON(http.StatusBadGateway, msg("server "+a.Name+": "+err.Error()))
	}
	return c.JSONBlob(http.StatusOK, out)
}
