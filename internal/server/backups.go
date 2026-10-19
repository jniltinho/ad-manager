package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ad-manager/internal/audit"
	"ad-manager/internal/backup"
	"ad-manager/internal/store"

	"github.com/labstack/echo/v5"
)

// backupRoutes: the Backup page. The agents make and keep their servers' backups (and schedules); the panel lists
// them, asks for a backup, deletes, streams a download, and makes its own (database, configuration, audit log).
func (s *Server) backupRoutes(p *echo.Group) {
	p.GET("/backups", s.listBackups)
	p.POST("/backups/panel", s.panelBackup)
	p.DELETE("/backups/panel/:bid", s.panelBackupDelete)
	p.GET("/backups/panel/:bid/download", s.panelBackupDownload)
	p.POST("/agents/:id/backups", s.agentBackupRun)
	p.DELETE("/agents/:id/backups/:bid", s.agentBackupDelete)
	p.GET("/agents/:id/backups/:bid/download", s.agentBackupDownload)
	p.POST("/backups/panel/:bid/restore", s.panelBackupRestore)
	p.POST("/backups/panel/upload", s.panelBackupUpload)
	p.POST("/agents/:id/backups/:bid/restore", s.agentBackupRestore)
	p.POST("/agents/:id/backups/upload", s.agentBackupUpload)
	p.GET("/agents/:id/backup-schedules", s.agentSchedules)
	p.PUT("/agents/:id/backup-schedules", s.agentSchedules)
}

// panelStore is the panel's own backup folder (beside its database): one store, so its lock keeps one backup at a
// time.
func (s *Server) panelStore() *backup.Store {
	s.panelBackupsOnce.Do(func() { s.panelBackups = &backup.Store{Dir: filepath.Join(s.cfg.StateDir(), "backups")} })
	return s.panelBackups
}

// listBackups handles GET /api/backups: the panel's backups and each agent's (an agent that does not answer comes
// with its error).
func (s *Server) listBackups(c *echo.Context) error {
	type server struct {
		ID      int64             `json:"id"` // 0: the panel
		Name    string            `json:"name"`
		DC      bool              `json:"dc"`
		Kinds   []string          `json:"kinds"`
		Roots   []string          `json:"roots,omitempty"`
		Backups []backup.Manifest `json:"backups"`
		Error   string            `json:"error,omitempty"`
		// the last restoration, what may be sent here and up to what size
		Restore      *backup.RestoreStatus `json:"restore,omitempty"`
		UploadKinds  []string              `json:"upload_kinds"`
		UploadMaxMiB int64                 `json:"upload_max_mib,omitempty"`
	}
	out := []server{}
	if s.store != nil {
		list, err := s.panelStore().List()
		ps := server{Name: "panel", Kinds: []string{backup.KindPanel}, Backups: list, Restore: s.panelStore().LastRestore(),
			UploadKinds: []string{backup.KindPanel}, UploadMaxMiB: panelUploadMax >> 20}
		if err != nil {
			ps.Error = err.Error()
		}
		out = append(out, ps)
		agents, err := s.store.Agents()
		if err != nil {
			return fail(c, err)
		}
		for _, a := range agents {
			sv := server{ID: a.ID, Name: a.Name, Backups: []backup.Manifest{}, Kinds: []string{}, UploadKinds: []string{}}
			st, b, err := agentDo(c.Request().Context(), &a, http.MethodGet, a.URL+"/api/backups", nil)
			if err == nil && st != http.StatusOK {
				err = agentErr(st, b)
			}
			if err == nil {
				err = json.Unmarshal(b, &sv)
				sv.ID, sv.Name = a.ID, a.Name
			}
			if err != nil {
				sv.Error = err.Error()
			}
			out = append(out, sv)
		}
	}
	return c.JSON(http.StatusOK, out)
}

// agentByParam is the agent of :id (on an error it has answered).
func (s *Server) agentByParam(c *echo.Context) (*store.Agent, error) {
	if s.store == nil {
		return nil, noStore(c)
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	a, err := s.store.Agent(id)
	if err != nil {
		return nil, c.JSON(http.StatusNotFound, msg("server not registered"))
	}
	return a, nil
}

// agentBackupRun handles POST /api/agents/:id/backups {kind, folders}: queued on the agent as the operator's.
func (s *Server) agentBackupRun(c *echo.Context) error {
	a, err := s.agentByParam(c)
	if a == nil {
		return err
	}
	var in struct {
		Kind    string   `json:"kind"`
		Folders []string `json:"folders"`
		Full    bool     `json:"full"`
	}
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, msg("invalid request"))
	}
	body, _ := json.Marshal(backup.Job{Kind: in.Kind, Folders: in.Folders, Full: in.Full, By: sess(c).user})
	st, out, err := agentDo(c.Request().Context(), a, http.MethodPost, a.URL+"/api/backups", body)
	if err == nil && st != http.StatusAccepted {
		err = agentErr(st, out)
	}
	audit.Log(sess(c).user, c.RealIP(), "backup.run", a.Name+":"+in.Kind, err)
	if err != nil {
		return c.JSON(http.StatusBadGateway, msg("server "+a.Name+": "+err.Error()))
	}
	return c.JSONBlob(http.StatusAccepted, out)
}

// agentBackupDelete handles DELETE /api/agents/:id/backups/:bid.
func (s *Server) agentBackupDelete(c *echo.Context) error {
	a, err := s.agentByParam(c)
	if a == nil {
		return err
	}
	bid := c.Param("bid")
	if !backup.ValidID(bid) {
		return c.JSON(http.StatusBadRequest, msg("backup not found"))
	}
	st, out, err := agentDo(c.Request().Context(), a, http.MethodDelete, a.URL+"/api/backups/"+url.PathEscape(bid), nil)
	if err == nil && st != http.StatusOK {
		err = agentErr(st, out)
	}
	audit.Log(sess(c).user, c.RealIP(), "backup.delete", a.Name+":"+bid, err)
	if err != nil {
		return c.JSON(http.StatusBadGateway, msg("server "+a.Name+": "+err.Error()))
	}
	return c.JSONBlob(http.StatusOK, out)
}

// agentBackupDownload handles GET /api/agents/:id/backups/:bid/download, streamed from the agent (never kept on
// the panel).
func (s *Server) agentBackupDownload(c *echo.Context) error {
	a, err := s.agentByParam(c)
	if a == nil {
		return err
	}
	bid := c.Param("bid")
	if !backup.ValidID(bid) {
		return c.JSON(http.StatusBadRequest, msg("backup not found"))
	}
	_ = http.NewResponseController(c.Response()).SetWriteDeadline(time.Now().Add(6 * time.Hour)) // large archives
	req, err := http.NewRequestWithContext(c.Request().Context(), http.MethodGet, a.URL+"/api/backups/"+url.PathEscape(bid)+"/download", nil)
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
	audit.Log(sess(c).user, c.RealIP(), "backup.download", a.Name+":"+bid, err)
	if err != nil {
		return c.JSON(http.StatusBadGateway, msg("server "+a.Name+": "+err.Error()))
	}
	defer resp.Body.Close()
	h := c.Response().Header()
	for _, k := range []string{"Content-Disposition", "Content-Length", "X-Backup-SHA256"} {
		if v := resp.Header.Get(k); v != "" {
			h.Set(k, v)
		}
	}
	return c.Stream(http.StatusOK, "application/octet-stream", resp.Body)
}

// agentSchedules handles GET and PUT /api/agents/:id/backup-schedules (kept by the agent).
func (s *Server) agentSchedules(c *echo.Context) error {
	a, err := s.agentByParam(c)
	if a == nil {
		return err
	}
	var body []byte
	if c.Request().Method == http.MethodPut {
		if body, err = io.ReadAll(io.LimitReader(c.Request().Body, 1<<20)); err != nil {
			return c.JSON(http.StatusBadRequest, msg("invalid request body"))
		}
	}
	st, out, err := agentDo(c.Request().Context(), a, c.Request().Method, a.URL+"/api/backup-schedules", body)
	if err == nil && st != http.StatusOK {
		err = agentErr(st, out)
	}
	if c.Request().Method == http.MethodPut {
		audit.Log(sess(c).user, c.RealIP(), "backup.schedule", a.Name+": "+string(bytes.TrimSpace(body)), err)
	}
	if err != nil {
		return c.JSON(http.StatusBadGateway, msg("server "+a.Name+": "+err.Error()))
	}
	return c.JSONBlob(http.StatusOK, out)
}

// panelBackup handles POST /api/backups/panel: the database (VACUUM INTO), config.toml without secrets, audit log.
func (s *Server) panelBackup(c *echo.Context) error {
	if s.store == nil {
		return noStore(c)
	}
	run := func(_ context.Context, name string, args ...string) (string, error) {
		if name != "sqlite-vacuum" || len(args) != 2 {
			return "", errors.New("the panel backup runs no command")
		}
		return "", s.store.VacuumInto(args[1])
	}
	m, err := s.panelStore().Run(c.Request().Context(), run, backup.Job{Kind: backup.KindPanel, By: sess(c).user},
		backup.Sources{Database: s.cfg.DB.Path, Config: s.cfg.File, AuditLog: s.cfg.Server.AuditLog})
	audit.Log(sess(c).user, c.RealIP(), "backup.run", "panel:"+m.ID, err)
	if errors.Is(err, backup.ErrBusy) {
		return c.JSON(http.StatusConflict, msg(err.Error()))
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, msg(err.Error()))
	}
	return c.JSON(http.StatusOK, m)
}

// panelBackupDelete handles DELETE /api/backups/panel/:bid.
func (s *Server) panelBackupDelete(c *echo.Context) error {
	if s.store == nil {
		return noStore(c)
	}
	ids, err := s.panelStore().Delete(c.Param("bid"))
	audit.Log(sess(c).user, c.RealIP(), "backup.delete", "panel:"+c.Param("bid"), err)
	if errors.Is(err, backup.ErrBusy) {
		return c.JSON(http.StatusConflict, msg(err.Error()))
	}
	if errors.Is(err, backup.ErrNotFound) {
		return c.JSON(http.StatusNotFound, msg(err.Error()))
	}
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"deleted": ids})
}

// panelBackupDownload handles GET /api/backups/panel/:bid/download.
func (s *Server) panelBackupDownload(c *echo.Context) error {
	if s.store == nil {
		return noStore(c)
	}
	f, m, err := s.panelStore().Open(c.Param("bid"))
	audit.Log(sess(c).user, c.RealIP(), "backup.download", "panel:"+c.Param("bid"), err)
	if err != nil {
		return c.JSON(http.StatusNotFound, msg(backup.ErrNotFound.Error()))
	}
	defer f.Close()
	c.Response().Header().Set("Content-Disposition", `attachment; filename="`+m.File+`"`)
	c.Response().Header().Set("X-Backup-SHA256", m.SHA256)
	return c.Stream(http.StatusOK, "application/octet-stream", f)
}

// restoreRequest is what the Backup page sends to restore: confirm is the end of the backup's id (ConfirmCode),
// typed by the operator, required whenever the restoration replaces what is in use.
type restoreRequest struct {
	Target  string   `json:"target"`
	Folders []string `json:"folders"`
	Parts   []string `json:"parts"`
	Confirm string   `json:"confirm"`
}

// agentBackupRestore handles POST /api/agents/:id/backups/:bid/restore: checked here (the typed confirmation),
// queued on the agent (which checks it again against its server).
func (s *Server) agentBackupRestore(c *echo.Context) error {
	a, err := s.agentByParam(c)
	if a == nil {
		return err
	}
	bid := c.Param("bid")
	var in restoreRequest
	if !backup.ValidID(bid) || c.Bind(&in) != nil {
		return c.JSON(http.StatusBadRequest, msg("invalid request"))
	}
	j := backup.RestoreJob{Target: in.Target, Folders: in.Folders, Parts: in.Parts, By: sess(c).user}
	if j.Overwrites(backup.KindOf(bid)) {
		if in.Confirm != backup.ConfirmCode(bid) {
			return c.JSON(http.StatusBadRequest, msg("confirm the restoration: type the end of the backup's id"))
		}
		j.Confirm = bid
	}
	body, _ := json.Marshal(j)
	st, out, err := agentDo(c.Request().Context(), a, http.MethodPost, a.URL+"/api/backups/"+url.PathEscape(bid)+"/restore", body)
	if err == nil && st != http.StatusAccepted {
		err = agentErr(st, out)
	}
	what := a.Name + ":" + bid
	if j.Target != "" {
		what += " → " + j.Target
	}
	if len(j.Parts) > 0 {
		what += " (" + strings.Join(j.Parts, ", ") + ")"
	}
	audit.Log(sess(c).user, c.RealIP(), "backup.restore", what, err)
	if err != nil {
		return c.JSON(http.StatusBadGateway, msg("server "+a.Name+": "+err.Error()))
	}
	return c.JSONBlob(http.StatusAccepted, out)
}

// agentBackupUpload handles POST /api/agents/:id/backups/upload?kind=…: the body (the archive) streamed to the
// agent, which checks and keeps it (nothing stays on the panel).
func (s *Server) agentBackupUpload(c *echo.Context) error {
	a, err := s.agentByParam(c)
	if a == nil {
		return err
	}
	kind := c.QueryParam("kind")
	rc := http.NewResponseController(c.Response())
	_ = rc.SetReadDeadline(time.Now().Add(6 * time.Hour)) // large archives
	_ = rc.SetWriteDeadline(time.Now().Add(6 * time.Hour))
	target := a.URL + "/api/backups/upload?kind=" + url.QueryEscape(kind) + "&by=" + url.QueryEscape(sess(c).user)
	req, err := http.NewRequestWithContext(c.Request().Context(), http.MethodPost, target, c.Request().Body)
	if err != nil {
		return fail(c, err)
	}
	req.ContentLength = c.Request().ContentLength
	req.Header.Set("Authorization", "Bearer "+a.Token)
	req.Header.Set("Content-Type", "application/octet-stream")
	var out []byte
	st := 0
	resp, err := agentClient(a).Do(req)
	if err == nil {
		out, _ = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if st = resp.StatusCode; st != http.StatusOK {
			err = agentErr(st, out)
		}
	}
	var m backup.Manifest
	_ = json.Unmarshal(out, &m)
	audit.Log(sess(c).user, c.RealIP(), "backup.upload", a.Name+":"+kind+" "+m.ID+" "+m.SHA256, err)
	if err != nil {
		return c.JSON(http.StatusBadGateway, msg("server "+a.Name+": "+err.Error()))
	}
	return c.JSONBlob(http.StatusOK, out)
}

// panelUploadMax bounds a panel backup sent to the panel.
const panelUploadMax = 4 << 30

// panelBackupUpload handles POST /api/backups/panel/upload?kind=panel.
func (s *Server) panelBackupUpload(c *echo.Context) error {
	if s.store == nil {
		return noStore(c)
	}
	rc := http.NewResponseController(c.Response())
	_ = rc.SetReadDeadline(time.Now().Add(time.Hour))
	_ = rc.SetWriteDeadline(time.Now().Add(time.Hour))
	m, err := s.panelStore().Import(c.Request().Body, c.QueryParam("kind"), sess(c).user, panelUploadMax)
	if err == nil { // the database must open as the panel's, or the sent backup goes
		tmp := filepath.Join(s.panelStore().Dir, "."+m.ID+".db")
		err = s.panelStore().ExtractFile(m.ID, "ad-manager.db", tmp, panelUploadMax)
		if err == nil {
			err = store.CheckDB(tmp)
		}
		os.Remove(tmp)
		if err != nil {
			_, _ = s.panelStore().Delete(m.ID)
		}
	}
	audit.Log(sess(c).user, c.RealIP(), "backup.upload", "panel:"+m.ID+" "+m.SHA256, err)
	if errors.Is(err, backup.ErrBusy) {
		return c.JSON(http.StatusConflict, msg(err.Error()))
	}
	if err != nil {
		return c.JSON(http.StatusBadRequest, msg(err.Error()))
	}
	return c.JSON(http.StatusOK, m)
}

// panelBackupRestore handles POST /api/backups/panel/:bid/restore {confirm}: the current database is backed up,
// the backup's database is put beside it and the panel restarts (systemd brings it back), swapping them before it
// opens the database (store.ApplyPendingRestore). The configuration and the audit log are not restored.
func (s *Server) panelBackupRestore(c *echo.Context) error {
	if s.store == nil {
		return noStore(c)
	}
	bid := c.Param("bid")
	var in restoreRequest
	if !backup.ValidID(bid) || backup.KindOf(bid) != backup.KindPanel || c.Bind(&in) != nil {
		return c.JSON(http.StatusBadRequest, msg("invalid request"))
	}
	if in.Confirm != backup.ConfirmCode(bid) {
		return c.JSON(http.StatusBadRequest, msg("confirm the restoration: type the end of the backup's id"))
	}
	if !s.panelRestoreMu.TryLock() {
		return c.JSON(http.StatusConflict, msg("a restoration of the panel is already running"))
	}
	restarting := false
	defer func() {
		if !restarting { // a staged restoration keeps the lock until the panel exits
			s.panelRestoreMu.Unlock()
		}
	}()
	staged := s.cfg.DB.Path + store.RestoreSuffix
	tmp, err := os.CreateTemp(filepath.Dir(staged), filepath.Base(staged)+"-*")
	if err != nil {
		return fail(c, err)
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	err = s.panelStore().ExtractFile(bid, "ad-manager.db", tmp.Name(), 4<<30)
	if err == nil {
		err = store.CheckDB(tmp.Name())
	}
	var pre backup.Manifest
	if err == nil {
		pre, err = s.panelStore().Run(c.Request().Context(), func(_ context.Context, name string, args ...string) (string, error) {
			if name != "sqlite-vacuum" || len(args) != 2 {
				return "", errors.New("the panel backup runs no command")
			}
			return "", s.store.VacuumInto(args[1])
		}, backup.Job{Kind: backup.KindPanel, Origin: "pre-restore", By: sess(c).user, Note: "before restoring " + bid},
			backup.Sources{Database: s.cfg.DB.Path, Config: s.cfg.File, AuditLog: s.cfg.Server.AuditLog})
	}
	if err == nil {
		err = os.Rename(tmp.Name(), staged)
	}
	audit.Log(sess(c).user, c.RealIP(), "backup.restore", "panel:"+bid, err)
	if err != nil {
		if errors.Is(err, backup.ErrBusy) {
			return c.JSON(http.StatusConflict, msg(err.Error()))
		}
		return c.JSON(http.StatusInternalServerError, msg(err.Error()))
	}
	restarting = true
	go func() {
		time.Sleep(time.Second) // the answer goes out first
		slog.Warn("restarting to put the restored database in place", "backup", bid, "pre_backup", pre.ID)
		os.Exit(3) // Restart=on-failure brings the panel back
	}()
	return c.JSON(http.StatusAccepted, map[string]any{"restart": true, "pre_backup": pre.ID})
}
