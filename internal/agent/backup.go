package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"ad-manager/internal/backup"
	"ad-manager/internal/config"

	"github.com/hibiken/asynq"
	"github.com/labstack/echo/v5"
)

// backupDir is where this server's backups go.
func backupDir(ac config.Agent) string {
	if ac.BackupDir != "" {
		return ac.BackupDir
	}
	return "/var/backups/ad-manager"
}

// backupTimeout bounds one backup (data folders can be large).
const backupTimeout = 6 * time.Hour

// Schedule is a recurring backup of this server, kept by the agent.
type Schedule struct {
	ID            string           `json:"id"`
	Kind          string           `json:"kind"`
	Cron          string           `json:"cron"` // "0 2 * * *" (min hour day month weekday), or @daily, @weekly
	Folders       []string         `json:"folders,omitempty"`
	FullEveryDays int              `json:"full_every_days,omitempty"`
	Retention     backup.Retention `json:"retention"`
	Enabled       bool             `json:"enabled"`
}

var scheduleID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// schedulesFile keeps the schedules beside the backups (0600).
func (a *Agent) schedulesFile() string { return filepath.Join(a.backups.Dir, "schedules.json") }

// validateBackup keeps each kind on its role (the DC backup on a DC, the file server ones on a file server) and the
// data folders inside the share roots.
func (a *Agent) validateBackup(j *backup.Job) error {
	switch j.Kind {
	case backup.KindDC:
		if a.role != roleDC {
			return errors.New("the dc backup runs on a domain controller")
		}
	case backup.KindFileConfig, backup.KindFileData, backup.KindAll:
		if a.readOnly() {
			return errors.New("this backup runs on a file server")
		}
	default:
		return fmt.Errorf("unknown backup kind %q", j.Kind)
	}
	if j.Kind == backup.KindAll { // every share root, as it really is
		j.Folders = nil
		for _, r := range a.cfg.ShareRoots {
			if real, err := filepath.EvalSymlinks(r); err == nil {
				j.Folders = append(j.Folders, real)
			}
		}
		if len(j.Folders) == 0 {
			return errors.New("this server has no share roots")
		}
		return nil
	}
	if j.Kind != backup.KindFileData {
		j.Folders = nil
		return nil
	}
	if len(j.Folders) == 0 || len(j.Folders) > 50 {
		return errors.New("fileserver-data: 1 to 50 folders")
	}
	for i, f := range j.Folders {
		f = filepath.Clean(f)
		// the real path (a link anywhere on the way could lead out of the roots), checked and then archived
		real, err := filepath.EvalSymlinks(f)
		if !filepath.IsAbs(f) || err != nil || !a.underRoots(real) {
			return fmt.Errorf("%q is outside the share roots", f)
		}
		j.Folders[i] = real
	}
	return nil
}

// jobsOf turns a checked job into the backups it makes: "all" is the configuration plus a full backup of the share
// roots' data.
func jobsOf(j backup.Job) []backup.Job {
	if j.Kind != backup.KindAll {
		return []backup.Job{j}
	}
	cfg, data := j, j
	cfg.Kind, cfg.Folders = backup.KindFileConfig, nil
	data.Kind, data.Full = backup.KindFileData, true
	return []backup.Job{cfg, data}
}

// underRoots tells whether the real path p is a share root or inside one (roots resolved too).
func (a *Agent) underRoots(p string) bool {
	for _, r := range a.cfg.ShareRoots {
		if rr, err := filepath.EvalSymlinks(r); err == nil {
			r = rr
		}
		r = filepath.Clean(r)
		if p == r || strings.HasPrefix(p, r+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (a *Agent) backupSources() backup.Sources {
	return backup.Sources{Roots: a.cfg.ShareRoots, ACLHistory: a.mgr.History}
}

// loadSchedules registers the saved schedules in the scheduler (replacing the ones registered before).
func (a *Agent) loadSchedules() error {
	list, err := a.readSchedules()
	if err != nil {
		return err
	}
	return a.register(list)
}

func (a *Agent) readSchedules() ([]Schedule, error) {
	b, err := os.ReadFile(a.schedulesFile())
	if errors.Is(err, os.ErrNotExist) {
		return []Schedule{}, nil
	}
	if err != nil {
		return nil, err
	}
	var list []Schedule
	return list, json.Unmarshal(b, &list)
}

// register replaces the scheduler's entries with list (the enabled ones); a bad one stops it all, nothing changed.
func (a *Agent) register(list []Schedule) error {
	a.muSched.Lock()
	defer a.muSched.Unlock()
	var ids []string
	undo := func() {
		for _, id := range ids {
			_ = a.scheduler.Unregister(id)
		}
	}
	for _, sc := range list {
		if !sc.Enabled {
			continue
		}
		// "all" stays "all" here: the task takes the share roots as they are when it runs
		payload, _ := json.Marshal(backup.Job{Kind: sc.Kind, Origin: "schedule:" + sc.ID, Folders: sc.Folders,
			FullEveryDays: sc.FullEveryDays, Retention: sc.Retention})
		id, err := a.scheduler.Register(sc.Cron, asynq.NewTask(TypeBackup, payload), asynq.Queue(a.cfg.Queue),
			asynq.MaxRetry(3), asynq.Timeout(backupTimeout), asynq.Retention(7*24*time.Hour))
		if err != nil {
			undo()
			return fmt.Errorf("schedule %s: %w", sc.ID, err)
		}
		ids = append(ids, id)
	}
	for _, id := range a.schedIDs {
		_ = a.scheduler.Unregister(id)
	}
	a.schedIDs = ids
	return nil
}

// backupRoutes: list, run now, delete, download, schedules.
func (a *Agent) backupRoutes(api *echo.Group) {
	bad := func(c *echo.Context, code int, err error) error {
		return c.JSON(code, map[string]string{"error": err.Error()})
	}
	api.GET("/backups", func(c *echo.Context) error {
		list, err := a.backups.List()
		if err != nil {
			return bad(c, http.StatusInternalServerError, err)
		}
		return c.JSON(http.StatusOK, map[string]any{"backups": list, "dir": a.backups.Dir, "dc": a.role == roleDC,
			"kinds": a.backupKinds(), "roots": a.cfg.ShareRoots, "restore": a.backups.LastRestore(),
			"upload_kinds": a.uploadKinds(), "upload_max_mib": a.uploadMax() >> 20})
	})
	api.POST("/backups/:id/restore", func(c *echo.Context) error {
		var j backup.RestoreJob
		if err := c.Bind(&j); err != nil {
			return bad(c, http.StatusBadRequest, errors.New("invalid JSON"))
		}
		j.ID = c.Param("id")
		m, err := a.backups.Get(j.ID)
		if err != nil {
			return bad(c, http.StatusNotFound, backup.ErrNotFound)
		}
		if err := a.validateRestore(&j, m); err != nil {
			return bad(c, http.StatusBadRequest, err)
		}
		payload, _ := json.Marshal(j)
		info, err := a.client.EnqueueContext(c.Request().Context(), asynq.NewTask(TypeRestore, payload), asynq.Queue(a.cfg.Queue),
			asynq.MaxRetry(3), asynq.Timeout(backupTimeout), asynq.Retention(7*24*time.Hour))
		if err != nil {
			return bad(c, http.StatusServiceUnavailable, fmt.Errorf("queue unavailable: %w", err))
		}
		return c.JSON(http.StatusAccepted, TaskInfo{ID: info.ID, Queue: info.Queue, Type: info.Type, State: info.State.String()})
	})
	// upload: the archive is the request body (streamed to disk, never held in memory)
	api.POST("/backups/upload", func(c *echo.Context) error {
		kind := c.QueryParam("kind")
		if !slices.Contains(a.uploadKinds(), kind) {
			return bad(c, http.StatusBadRequest, fmt.Errorf("a %q backup cannot be sent to this server", kind))
		}
		_ = http.NewResponseController(c.Response()).SetReadDeadline(time.Now().Add(backupTimeout))
		m, err := a.backups.Import(c.Request().Body, kind, c.QueryParam("by"), a.uploadMax())
		if errors.Is(err, backup.ErrBusy) {
			return bad(c, http.StatusConflict, err)
		}
		if err != nil {
			return bad(c, http.StatusBadRequest, err)
		}
		return c.JSON(http.StatusOK, m)
	})
	api.POST("/backups", func(c *echo.Context) error {
		var j backup.Job
		if err := c.Bind(&j); err != nil {
			return bad(c, http.StatusBadRequest, errors.New("invalid JSON"))
		}
		j.Origin = "manual"
		if err := a.validateBackup(&j); err != nil {
			return bad(c, http.StatusBadRequest, err)
		}
		payload, _ := json.Marshal(j)
		info, err := a.client.EnqueueContext(c.Request().Context(), asynq.NewTask(TypeBackup, payload), asynq.Queue(a.cfg.Queue),
			asynq.MaxRetry(3), asynq.Timeout(backupTimeout), asynq.Retention(7*24*time.Hour))
		if err != nil {
			return bad(c, http.StatusServiceUnavailable, fmt.Errorf("queue unavailable: %w", err))
		}
		return c.JSON(http.StatusAccepted, TaskInfo{ID: info.ID, Queue: info.Queue, Type: info.Type, State: info.State.String()})
	})
	api.DELETE("/backups/:id", func(c *echo.Context) error {
		ids, err := a.backups.Delete(c.Param("id"))
		if errors.Is(err, backup.ErrBusy) {
			return bad(c, http.StatusConflict, err)
		}
		if errors.Is(err, backup.ErrNotFound) {
			return bad(c, http.StatusNotFound, err)
		}
		if err != nil {
			return bad(c, http.StatusInternalServerError, err)
		}
		return c.JSON(http.StatusOK, map[string]any{"deleted": ids})
	})
	api.GET("/backups/:id/download", func(c *echo.Context) error {
		f, m, err := a.backups.Open(c.Param("id"))
		if err != nil {
			return bad(c, http.StatusNotFound, backup.ErrNotFound)
		}
		defer f.Close()
		c.Response().Header().Set("Content-Disposition", `attachment; filename="`+m.File+`"`)
		c.Response().Header().Set("X-Backup-SHA256", m.SHA256)
		return c.Stream(http.StatusOK, "application/octet-stream", f)
	})
	api.GET("/backup-schedules", func(c *echo.Context) error {
		list, err := a.readSchedules()
		if err != nil {
			return bad(c, http.StatusInternalServerError, err)
		}
		return c.JSON(http.StatusOK, list)
	})
	api.PUT("/backup-schedules", func(c *echo.Context) error {
		var list []Schedule
		if err := c.Bind(&list); err != nil || len(list) > 50 {
			return bad(c, http.StatusBadRequest, errors.New("invalid JSON"))
		}
		seen := map[string]bool{}
		for i := range list {
			sc := &list[i]
			if !scheduleID.MatchString(sc.ID) || seen[sc.ID] {
				return bad(c, http.StatusBadRequest, fmt.Errorf("schedule id %q: lowercase letters, digits and -, unique", sc.ID))
			}
			seen[sc.ID] = true
			j := backup.Job{Kind: sc.Kind, Folders: sc.Folders}
			if err := a.validateBackup(&j); err != nil {
				return bad(c, http.StatusBadRequest, fmt.Errorf("schedule %s: %w", sc.ID, err))
			}
			if sc.Kind != backup.KindAll {
				sc.Folders = j.Folders
			} else {
				sc.Folders = nil // resolved when it runs
			}
			if sc.Retention.Chains < 0 || sc.Retention.Days < 0 || sc.FullEveryDays < 0 {
				return bad(c, http.StatusBadRequest, fmt.Errorf("schedule %s: negative retention", sc.ID))
			}
		}
		// one change at a time: the file is written first (a failure changes nothing), then the scheduler; if the
		// scheduler refuses, the previous file comes back
		a.muPut.Lock()
		defer a.muPut.Unlock()
		if err := os.MkdirAll(a.backups.Dir, 0o700); err != nil {
			return bad(c, http.StatusInternalServerError, err)
		}
		prev, _ := os.ReadFile(a.schedulesFile())
		b, _ := json.MarshalIndent(list, "", "  ")
		if err := writeFile0600(a.schedulesFile(), b); err != nil {
			return bad(c, http.StatusInternalServerError, err)
		}
		if err := a.register(list); err != nil {
			if prev != nil {
				_ = writeFile0600(a.schedulesFile(), prev)
			} else {
				_ = os.Remove(a.schedulesFile())
			}
			return bad(c, http.StatusBadRequest, err)
		}
		return c.JSON(http.StatusOK, list)
	})
}

// writeFile0600 replaces p atomically with b (0600).
func writeFile0600(p string, b []byte) error {
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// hostRun runs a restoration's commands outside the agent's sandbox (ProtectSystem=full keeps /etc read-only to
// the agent, and a restoration writes smb.conf and /etc/samba), each as a transient unit that systemd-run waits
// for, when the agent runs under systemd.
func hostRun(ctx context.Context, name string, args ...string) (string, error) {
	if os.Getenv("INVOCATION_ID") == "" {
		return backup.Exec(ctx, name, args...)
	}
	// not cancelled with ctx: killing systemd-run would leave its command running unseen (a Samba unit stopped and
	// never counted, a move half done); a restoration's step always runs to its end
	return backup.Exec(context.WithoutCancel(ctx), "systemd-run", append([]string{"--wait", "--collect", "--quiet", "--pipe", "--", name}, args...)...)
}

// uploadKinds are the kinds this server accepts as a sent backup (the ones it can restore).
func (a *Agent) uploadKinds() []string {
	if a.readOnly() {
		return []string{backup.KindSambaFiles}
	}
	return []string{backup.KindFileConfig, backup.KindFileData, backup.KindSambaFiles}
}

func (a *Agent) uploadMax() int64 {
	if a.cfg.BackupUploadMaxMiB > 0 {
		return a.cfg.BackupUploadMaxMiB << 20
	}
	return 20 << 30
}

// validateRestore keeps a restoration on what this server can take: the file server kinds on a file server, the
// original folders still inside the share roots, and, for anything that changes what is in use, the backup's id
// typed back as the confirmation (the panel asks the operator for the server's name before sending it).
func (a *Agent) validateRestore(j *backup.RestoreJob, m backup.Manifest) error {
	switch m.Kind {
	case backup.KindFileData, backup.KindFileConfig:
		if a.readOnly() {
			return errors.New("this backup is restored on a file server")
		}
	case backup.KindSambaFiles:
	case backup.KindDC:
		return errors.New("a domain controller backup is restored by the initial setup on a new server (role: restore a DC), never over a DC in use")
	default:
		return fmt.Errorf("a %s backup is not restored by an agent", m.Kind)
	}
	if m.Status != "ok" {
		return errors.New("only a finished backup can be restored")
	}
	if j.Target != "" && j.Target != backup.TargetNew && j.Target != backup.TargetOriginal {
		return fmt.Errorf("unknown target %q", j.Target)
	}
	if m.Kind == backup.KindFileData {
		folders := j.Folders
		if len(folders) == 0 {
			folders = m.Folders
		}
		if len(folders) > 50 {
			return errors.New("too many folders")
		}
		for i, f := range folders {
			folders[i] = filepath.Clean(f)
			if j.Target == backup.TargetOriginal && !a.restorable(folders[i]) {
				return fmt.Errorf("%q is not inside the share roots of this server: restore it to a new folder", f)
			}
		}
		if len(j.Folders) > 0 {
			j.Folders = folders
		}
		if j.Target != backup.TargetOriginal && len(a.cfg.ShareRoots) == 0 {
			return errors.New("this server has no share roots to restore into")
		}
	} else {
		j.Target, j.Folders = "", nil
	}
	if a.role == roleDC && m.Kind == backup.KindFileConfig {
		// a DC's smb.conf and registry hold the domain's own shares: only the share roots' ACLs come back here
		for _, p := range j.Parts {
			if p != backup.PartACLs {
				return errors.New("on a domain controller only the permissions of the share roots are restored from a configuration backup")
			}
		}
	}
	if j.Overwrites(m.Kind) && j.Confirm != m.ID {
		return errors.New("confirm the restoration: it replaces what is in use")
	}
	return nil
}

// restorable tells whether folder f can be written back where it was: inside a share root (but not the root of the
// restorations), with no link on the way (the part of it that still exists resolves to itself).
func (a *Agent) restorable(f string) bool {
	if !filepath.IsAbs(f) || !a.underRoots(f) {
		return false
	}
	for _, r := range a.cfg.ShareRoots {
		for _, d := range []string{backup.RestoreDir, backup.LegacyRestoreDir} {
			if d = filepath.Join(filepath.Clean(r), d); f == d || strings.HasPrefix(f, d+"/") {
				return false
			}
		}
	}
	p := f
	for {
		if _, err := os.Lstat(p); err == nil {
			real, err := filepath.EvalSymlinks(p)
			return err == nil && real == p
		}
		if p == "/" {
			return false
		}
		p = filepath.Dir(p)
	}
}

// backupKinds are the kinds this server makes.
func (a *Agent) backupKinds() []string {
	if a.role == roleDC {
		if a.files { // a DC that also serves files: the domain and the files
			return []string{backup.KindDC, backup.KindAll, backup.KindFileConfig, backup.KindFileData}
		}
		return []string{backup.KindDC}
	}
	if a.readOnly() {
		return []string{}
	}
	return []string{backup.KindAll, backup.KindFileConfig, backup.KindFileData}
}
