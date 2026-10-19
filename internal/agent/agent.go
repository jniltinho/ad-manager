// Package agent is the file server daemon: a token-authenticated HTTPS API
// that validates requests and enqueues them (asynq/Redis); the worker in the
// same process runs the tasks with the shares package.
package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ad-manager/internal/backup"
	"ad-manager/internal/buildinfo"
	"ad-manager/internal/config"
	"ad-manager/internal/sambabridge"
	"ad-manager/internal/sambadc"
	"ad-manager/internal/sambalog"
	"ad-manager/internal/shares"
	"ad-manager/internal/tlsutil"

	"github.com/hibiken/asynq"
	"github.com/labstack/echo/v5"
	emw "github.com/labstack/echo/v5/middleware"
)

// Queue task types.
const (
	TypeFolder      = "folder:create"
	TypeShare       = "share:create"
	TypeShareDelete = "share:delete"
	TypeACLRestore  = "acl:restore"
	TypeBackup      = "backup:run"
	TypeRestore     = "backup:restore"
)

// RedisOpt builds the asynq connection options from the configuration.
func RedisOpt(r config.Redis) asynq.RedisClientOpt {
	o := asynq.RedisClientOpt{Addr: r.Addr, Password: r.Password, DB: r.DB}
	if r.TLS {
		host, _, _ := net.SplitHostPort(r.Addr)
		o.TLSConfig = &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	}
	return o
}

// GenerateToken returns a random token and its SHA-256 (hex), the content of
// the agent's token_file.
func GenerateToken() (token, hash string) {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // crypto/rand.Read does not fail (Go ≥ 1.24)
	token = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(sum[:])
}

// NewToken generates a token, stores only its SHA-256 (hex, mode 0600) and
// returns the token.
func NewToken(path string) (string, error) {
	tok, hash := GenerateToken()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(hash+"\n"), 0o600); err != nil {
		return "", err
	}
	return tok, os.Rename(tmp, path)
}

// loadHash reads the token hash written by NewToken.
func loadHash(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("agent token (%s): %w — generate it with `ad-manager agent token`", path, err)
	}
	h, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(h) != sha256.Size {
		return nil, fmt.Errorf("%s does not contain a valid hash", path)
	}
	return h, nil
}

// Agent ties together the API, the queue and the executor.
type Agent struct {
	cfg    config.Agent
	files  bool             // folders, shares, ACLs, syncs and data backups allowed (fileMode)
	why    string           // why not
	logDir string           // Samba log directory (Logs page)
	caps   sambabridge.Caps // what the Samba Python API offers on this server
	mgr    *shares.Manager
	client *asynq.Client
	insp   *asynq.Inspector
	hash   []byte

	// ponytail: 5-minute cache of `du` (large folders are slow); switch to an
	// incremental walk or filesystem quotas if this becomes too heavy.
	muSize  sync.Mutex
	sizes   map[string]int64
	sizesAt time.Time

	// server role of smb.conf: anything but a file server role (or "" when unreadable) makes the agent read only
	role string
	// realm of smb.conf (the AD domain), reported so the panel picks a DC of its own domain
	realm string

	backups   *backup.Store
	sync      *syncState // sync jobs: running runs and destinations being written
	scheduler *asynq.Scheduler
	muSched   sync.Mutex
	muPut     sync.Mutex // one PUT of the schedules at a time (file and scheduler together)
	schedIDs  []string   // entries registered in the scheduler

	// on a domain controller: the last consistency check, and the one running (dbcheck can take minutes on a large
	// directory; requests wait for it without holding the lock, and it runs to its end even if they give up)
	muChecks sync.Mutex
	checks   *sambadc.Checks
	running  chan struct{}

	privateDir string // a DC's private dir, hidden from the sync worker too
}

// roleDC is smb.conf's server role on a Samba AD domain controller.
const roleDC = "active directory domain controller"

// errReadOnlyDC refuses every change when the agent runs on a domain controller (or cannot tell).
var errReadOnlyDC = fmt.Errorf("%w: the agent is read only on a domain controller", shares.ErrInvalid)

// readOnly reports a server whose files the agent must not change: a domain controller without its file mode
// (fileMode), or a role that is not known.
func (a *Agent) readOnly() bool { return !a.files }

// gpoRestoreReady tells whether the explicit write mode is set up: an account and its password file, a regular
// file not readable by group or others.
func (a *Agent) gpoRestoreReady() error {
	if a.cfg.GPORestoreUser == "" || a.cfg.GPORestorePasswordFile == "" {
		return errors.New("GPO restore is off on this agent (gpo_restore_user and gpo_restore_password_file)")
	}
	fi, err := os.Stat(a.cfg.GPORestorePasswordFile)
	if err != nil {
		return fmt.Errorf("GPO restore: %w", err)
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm()&0o077 != 0 {
		return errors.New("GPO restore: the password file must be a regular file with mode 0600")
	}
	return nil
}

// testparm reads one smb.conf parameter from testparm's standard output (its notices go to stderr).
func testparm(ctx context.Context, name string) string {
	out, err := exec.CommandContext(ctx, "testparm", "-s", "--parameter-name="+name).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// testparmSection reads one parameter of a share section of smb.conf.
func testparmSection(ctx context.Context, section, name string) string {
	out, err := exec.CommandContext(ctx, "testparm", "-s", "--section-name="+section, "--parameter-name="+name).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// serverRole reads smb.conf's server role: from the Samba helper, else from testparm (no python3-samba).
func serverRole(ctx context.Context, caps sambabridge.Caps) string {
	role := caps.Role
	if role == "" {
		role = testparm(ctx, "server role")
	}
	if strings.EqualFold(role, "auto") { // smb.conf's default: decided by "security" (a DC always names its role)
		return roleFromSecurity(testparm(ctx, "security"))
	}
	return role
}

// roleFromSecurity is what Samba makes of "server role = auto": user → standalone, ads or domain → member;
// anything else stays unknown (read only).
func roleFromSecurity(sec string) string {
	switch strings.ToLower(strings.TrimSpace(sec)) {
	case "user":
		return "standalone server"
	case "ads", "domain":
		return "member server"
	}
	return "auto"
}

// tokenOK compares the token's hash in constant time.
func (a *Agent) tokenOK(tok string) bool {
	sum := sha256.Sum256([]byte(tok))
	return subtle.ConstantTimeCompare(sum[:], a.hash) == 1
}

// Run starts the worker and the API and serves until ctx is done.
func Run(ctx context.Context, cfg *config.Config, insecureDev bool) error {
	if cfg.Setup {
		return RunSetup(ctx, cfg)
	}
	if cfg.Redis.Addr == "" {
		return errors.New("the agent needs [redis] addr")
	}
	ac := cfg.Agent
	host, _, err := net.SplitHostPort(ac.Listen)
	if err != nil {
		return fmt.Errorf("agent.listen: %w", err)
	}
	ip := net.ParseIP(host)
	tlsCfg, fp, err := tlsutil.Config(ac.CertFile, ac.KeyFile, ac.SelfSigned, cfg.StateDir())
	if err != nil {
		return err
	}
	tlsOn := tlsCfg != nil
	if !tlsOn && !(insecureDev && (host == "localhost" || (ip != nil && ip.IsLoopback()))) {
		return errors.New("the agent requires HTTPS: set agent.cert_file/key_file or self_signed = true (HTTP only with --insecure-dev on loopback)")
	}
	hash, err := loadHash(ac.TokenFile)
	if err != nil {
		return err
	}
	ro := RedisOpt(cfg.Redis)
	bridge := &sambabridge.Bridge{}
	cctx, ccancel := context.WithTimeout(ctx, 20*time.Second)
	caps := bridge.Capabilities(cctx)
	ccancel()
	bridge.GoRead = caps.GoReadable()
	rctx, rcancel := context.WithTimeout(ctx, 20*time.Second)
	role, realm := serverRole(rctx, caps), strings.ToLower(testparm(rctx, "realm"))
	rcancel()
	a := &Agent{cfg: ac, logDir: cfg.Samba.LogDir, caps: caps, role: role, realm: realm, client: asynq.NewClient(ro), insp: asynq.NewInspector(ro),
		hash: hash, mgr: NewManager(cfg, bridge, caps), backups: &backup.Store{Dir: backupDir(ac)},
		sync: &syncState{running: map[string]context.CancelFunc{}, trees: map[string]string{}, active: map[string]string{}}}
	defer a.client.Close()
	fctx, fcancel := context.WithTimeout(ctx, 30*time.Second)
	a.files, a.why = a.fileMode(fctx)
	if a.role == roleDC && a.files {
		a.mgr.DC, a.mgr.Reserved = true, staticShares("/etc/samba/smb.conf")
		a.privateDir = testparm(fctx, "private dir")
	}
	fcancel()
	if a.why != "" {
		slog.Info("files off on this server", "why", a.why)
	}
	// the backup schedules live on the agent (they run with the panel off): registered from the saved file
	// schedules in the server's own time zone ("0 1 * * *" is 01:00 here, not 01:00 UTC)
	a.scheduler = asynq.NewScheduler(ro, &asynq.SchedulerOpts{Logger: slogAdapter{}, Location: time.Local})
	if err := a.loadSchedules(); err != nil {
		slog.Warn("backup schedules", "err", err)
	}
	if err := a.scheduler.Start(); err != nil {
		return fmt.Errorf("backup scheduler: %w", err)
	}
	defer a.scheduler.Shutdown()

	// ponytail: Concurrency 1 serializes every change on the server (low volume);
	// use per-share locks if there are ever many simultaneous tasks.
	worker := asynq.NewServer(ro, asynq.Config{Queues: map[string]int{ac.Queue: 1}, Concurrency: 1,
		Logger: slogAdapter{}, ShutdownTimeout: 20 * time.Second})
	if err := worker.Start(a.mux()); err != nil {
		return fmt.Errorf("worker asynq: %w", err)
	}
	defer worker.Shutdown()
	// syncs on a queue of their own, one at a time: a long sync does not hold backups and folders back
	if !a.readOnly() {
		if list, err := a.syncJobs(); err != nil {
			slog.Warn("sync jobs", "err", err)
		} else if err := a.registerSyncs(list); err != nil {
			slog.Warn("sync schedules", "err", err)
		}
		sm := asynq.NewServeMux()
		sm.HandleFunc(TypeSync, a.handleSync)
		syncWorker := asynq.NewServer(ro, asynq.Config{Queues: map[string]int{a.syncQueue(): 1}, Concurrency: 1,
			Logger: slogAdapter{}, ShutdownTimeout: 20 * time.Second})
		if err := syncWorker.Start(sm); err != nil {
			return fmt.Errorf("sync worker asynq: %w", err)
		}
		defer syncWorker.Shutdown()
	}

	srv := &http.Server{Addr: ac.Listen, Handler: a.Routes(), TLSConfig: tlsCfg, ReadHeaderTimeout: 10 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("agent listening", "listen", ac.Listen, "queue", ac.Queue, "roots", ac.ShareRoots, "tls", tlsOn, "sha256", fp, "version", buildinfo.Version,
			"nt_acl", caps.Usable(), "samba", caps.Samba, "samba_api_error", caps.Error, "role", role, "read_only", a.readOnly())
		if tlsOn {
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

// NewManager builds the shares manager of this server: Windows ACLs and the
// registry shares through the Samba bridge when caps allow it, with the ACL
// change history kept in the state directory.
func NewManager(cfg *config.Config, bridge *sambabridge.Bridge, caps sambabridge.Caps) *shares.Manager {
	m := &shares.Manager{Roots: cfg.Agent.ShareRoots, Run: shares.Exec, ACL: bridge, NTACL: caps.Usable(),
		History: filepath.Join(cfg.StateDir(), "acl-history.jsonl")}
	if caps.NTACL { // python3-samba: shares through Samba's registry API; else `net conf`
		m.Conf = bridge
	}
	return m
}

// mux binds each task type to its executor. Validation errors are not retried.
func (a *Agent) mux() *asynq.ServeMux {
	m := asynq.NewServeMux()
	handle := func(kind string, fn func(ctx context.Context, payload []byte) error) {
		m.HandleFunc(kind, func(ctx context.Context, t *asynq.Task) error {
			var err error
			if a.readOnly() { // a task queued before the server became a DC (or its role became unreadable)
				err = errReadOnlyDC
			} else {
				err = fn(ctx, t.Payload())
			}
			slog.Info("task", "type", kind, "id", t.ResultWriter().TaskID(), "err", err)
			if errors.Is(err, shares.ErrInvalid) {
				return fmt.Errorf("%w: %w", err, asynq.SkipRetry)
			}
			if err == nil {
				_, _ = t.ResultWriter().Write([]byte("ok"))
			}
			return err
		})
	}
	handle(TypeFolder, func(ctx context.Context, p []byte) error {
		var r shares.FolderReq
		if err := json.Unmarshal(p, &r); err != nil {
			return fmt.Errorf("%w: %v", shares.ErrInvalid, err)
		}
		return a.mgr.CreateFolder(ctx, r)
	})
	handle(TypeShare, func(ctx context.Context, p []byte) error {
		var r shares.ShareReq
		if err := json.Unmarshal(p, &r); err != nil {
			return fmt.Errorf("%w: %v", shares.ErrInvalid, err)
		}
		return a.mgr.CreateShare(ctx, r)
	})
	handle(TypeACLRestore, func(ctx context.Context, p []byte) error {
		var r shares.RestoreReq
		if err := json.Unmarshal(p, &r); err != nil {
			return fmt.Errorf("%w: %v", shares.ErrInvalid, err)
		}
		acl, err := a.mgr.RestoreACL(ctx, r)
		if err == nil {
			slog.Info("acl restored", "path", r.Path, "from", r.Time, "propagated", acl.Propagated, "skipped", acl.Skipped)
		}
		return err
	})
	// backups are allowed on a DC too (they only read): validateBackup keeps each kind on its role
	m.HandleFunc(TypeBackup, func(ctx context.Context, t *asynq.Task) error {
		var j backup.Job
		err := json.Unmarshal(t.Payload(), &j)
		if err == nil {
			err = a.validateBackup(&j)
		}
		if err != nil {
			slog.Info("task", "type", TypeBackup, "id", t.ResultWriter().TaskID(), "err", err)
			return fmt.Errorf("%w: %w", err, asynq.SkipRetry)
		}
		// "all": the configuration, then a full backup of the share roots as they are now (resolved by validateBackup)
		var ids []string
		for _, jj := range jobsOf(j) {
			bm, err := a.backups.Run(ctx, backup.Exec, jj, a.backupSources())
			slog.Info("task", "type", TypeBackup, "id", t.ResultWriter().TaskID(), "backup", bm.ID, "kind", jj.Kind, "origin", jj.Origin, "err", err)
			if errors.Is(err, backup.ErrBusy) && len(ids) == 0 {
				return err // retried later (nothing made yet)
			}
			if err != nil {
				return fmt.Errorf("%w: %w", err, asynq.SkipRetry) // the manifest records the failure
			}
			ids = append(ids, bm.ID)
		}
		_, _ = t.ResultWriter().Write([]byte(strings.Join(ids, " ")))
		return nil
	})
	m.HandleFunc(TypeRestore, func(ctx context.Context, t *asynq.Task) error {
		var j backup.RestoreJob
		err := json.Unmarshal(t.Payload(), &j)
		var bm backup.Manifest
		if err == nil {
			bm, err = a.backups.Get(j.ID)
		}
		if err == nil {
			err = a.validateRestore(&j, bm) // again: the server may have changed since it was queued
		}
		if err != nil {
			slog.Info("task", "type", TypeRestore, "id", t.ResultWriter().TaskID(), "backup", j.ID, "err", err)
			return fmt.Errorf("%w: %w", err, asynq.SkipRetry)
		}
		// a restoration over the original folders shares the tree lock with the syncs (never both on one tree)
		if bm.Kind == backup.KindFileData && j.Target == backup.TargetOriginal {
			folders := j.Folders
			if len(folders) == 0 {
				folders = bm.Folders
			}
			for i, f := range folders {
				if err := a.lockTree(filepath.Clean(f), "restore "+j.ID); err != nil {
					for _, g := range folders[:i] {
						a.unlockTree(filepath.Clean(g))
					}
					return err // retried: the sync ends first
				}
			}
			defer func() {
				for _, f := range folders {
					a.unlockTree(filepath.Clean(f))
				}
			}()
		}
		st, err := a.backups.Restore(ctx, hostRun, j, a.backupSources())
		slog.Info("task", "type", TypeRestore, "id", t.ResultWriter().TaskID(), "backup", j.ID, "target", st.Target, "dest", st.Dest, "err", err)
		if errors.Is(err, backup.ErrBusy) {
			return err // a backup is running: retried later
		}
		if err != nil {
			return fmt.Errorf("%w: %w", err, asynq.SkipRetry) // restore.json records the failure
		}
		return nil
	})
	handle(TypeShareDelete, func(ctx context.Context, p []byte) error {
		var r struct{ Name string }
		if err := json.Unmarshal(p, &r); err != nil {
			return fmt.Errorf("%w: %v", shares.ErrInvalid, err)
		}
		return a.mgr.DeleteShare(ctx, r.Name)
	})
	return m
}

// taskTimeout bounds a task: a folder whose ACL changes passes it on to everything under it (sambabridge).
const taskTimeout = 30 * time.Minute

// TaskReq is the body of POST /api/tasks.
type TaskReq struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// TaskInfo is the state of a task.
type TaskInfo struct {
	ID      string `json:"id"`
	Queue   string `json:"queue"`
	Type    string `json:"type"`
	State   string `json:"state"`
	Retried int    `json:"retried"`
	LastErr string `json:"last_error,omitempty"`
	Result  string `json:"result,omitempty"`
}

// validate checks the request before enqueueing it (the worker checks again).
func (a *Agent) validate(t TaskReq) error {
	if a.readOnly() {
		return errReadOnlyDC
	}
	switch t.Type {
	case TypeFolder:
		var r shares.FolderReq
		if err := json.Unmarshal(t.Payload, &r); err != nil {
			return fmt.Errorf("%w: %v", shares.ErrInvalid, err)
		}
		return a.mgr.ValidateFolder(&r)
	case TypeShare:
		var r shares.ShareReq
		if err := json.Unmarshal(t.Payload, &r); err != nil {
			return fmt.Errorf("%w: %v", shares.ErrInvalid, err)
		}
		return a.mgr.ValidateShare(&r)
	case TypeACLRestore:
		var r shares.RestoreReq
		if err := json.Unmarshal(t.Payload, &r); err != nil {
			return fmt.Errorf("%w: %v", shares.ErrInvalid, err)
		}
		return a.mgr.ValidateRestore(&r)
	case TypeShareDelete:
		var r struct{ Name string }
		if err := json.Unmarshal(t.Payload, &r); err != nil {
			return fmt.Errorf("%w: %v", shares.ErrInvalid, err)
		}
		return shares.ValidName(r.Name)
	}
	return fmt.Errorf("%w: task type %q", shares.ErrInvalid, t.Type)
}

// Routes builds the agent API.
func (a *Agent) Routes() *echo.Echo {
	e := echo.New()
	e.Use(emw.Recover())
	e.GET("/api/health", func(c *echo.Context) error {
		return c.JSON(http.StatusOK, map[string]any{"status": "ok", "queue": a.cfg.Queue, "version": buildinfo.Version,
			"nt_acl": a.caps.Usable(), "samba": a.caps.Samba, "dc": a.role == roleDC, "read_only": a.readOnly(), "realm": a.realm,
			"gpo_restore": a.role == roleDC && a.gpoRestoreReady() == nil, "files": a.files, "files_why": a.why})
	})
	api := e.Group("/api", a.bearer)
	api.GET("/shares", func(c *echo.Context) error {
		list, err := a.mgr.List(c.Request().Context())
		if err != nil {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		}
		a.markFullControl(c.Request().Context(), list)
		return c.JSON(http.StatusOK, list)
	})
	api.GET("/sizes", func(c *echo.Context) error {
		a.muSize.Lock()
		defer a.muSize.Unlock()
		if a.sizes == nil || time.Since(a.sizesAt) > 5*time.Minute || c.QueryParam("refresh") == "1" {
			ctx, cancel := context.WithTimeout(c.Request().Context(), 2*time.Minute)
			defer cancel()
			sz, err := a.mgr.Sizes(ctx)
			if err != nil {
				return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
			}
			a.sizes, a.sizesAt = sz, time.Now()
		}
		return c.JSON(http.StatusOK, map[string]any{"sizes": a.sizes, "at": a.sizesAt})
	})
	// Windows ACL of a folder (read through Samba by the embedded helper)
	api.GET("/acl", func(c *echo.Context) error {
		acl, err := a.mgr.ReadACL(c.Request().Context(), c.QueryParam("path"))
		switch {
		case errors.Is(err, sambabridge.ErrUnavailable):
			return c.JSON(http.StatusNotImplemented, map[string]string{"error": err.Error()})
		case errors.Is(err, shares.ErrInvalid):
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		case err != nil:
			return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, acl)
	})
	// the folder's recorded ACL changes (newest first), to choose one to restore (task acl:restore)
	api.GET("/acl/history", func(c *echo.Context) error {
		list, err := a.mgr.ACLHistory(c.QueryParam("path"))
		switch {
		case errors.Is(err, sambabridge.ErrUnavailable):
			return c.JSON(http.StatusNotImplemented, map[string]string{"error": err.Error()})
		case errors.Is(err, shares.ErrInvalid):
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		case err != nil:
			return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, list)
	})
	// connected sessions and open files; disconnecting needs the pid and the session it serves
	api.GET("/sessions", func(c *echo.Context) error {
		st, err := a.mgr.Status(c.Request().Context())
		if err != nil {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, st)
	})
	api.POST("/sessions/close", func(c *echo.Context) error {
		if a.readOnly() {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": errReadOnlyDC.Error()})
		}
		var r shares.CloseReq
		if err := c.Bind(&r); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		}
		if a.role == roleDC {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": errDCSessions.Error()})
		}
		closed, err := a.mgr.CloseSession(c.Request().Context(), r)
		switch {
		case errors.Is(err, shares.ErrInvalid):
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		case err != nil:
			return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		}
		for _, s := range closed {
			slog.Info("session closed", "user", s.User, "machine", s.Machine, "pid", s.PID, "session", s.ID)
		}
		return c.JSON(http.StatusOK, map[string]any{"closed": closed})
	})
	// domain controller health, read only: replication and the consistency checks (cached 10 min; refresh=1 reruns)
	dcOnly := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if a.role != roleDC {
				return c.JSON(http.StatusNotFound, map[string]string{"error": "not a domain controller"})
			}
			return next(c)
		}
	}
	api.GET("/dc/replication", func(c *echo.Context) error {
		r, err := sambadc.ReadReplication(c.Request().Context(), sambadc.Runner(shares.Exec))
		if err != nil {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, r)
	}, dcOnly)
	api.GET("/dc/checks", func(c *echo.Context) error {
		a.muChecks.Lock()
		cached, done := a.checks, a.running
		if cached != nil && time.Since(cached.At) < 10*time.Minute && c.QueryParam("refresh") != "1" {
			a.muChecks.Unlock()
			return c.JSON(http.StatusOK, cached)
		}
		if done == nil { // one run at a time, on its own deadline (a request that gives up does not cut it short)
			done = make(chan struct{})
			a.running = done
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				defer cancel()
				res := sambadc.RunChecks(ctx, sambadc.Runner(shares.Exec))
				a.muChecks.Lock()
				a.checks, a.running = res, nil
				a.muChecks.Unlock()
				close(done)
			}()
		}
		a.muChecks.Unlock()
		select {
		case <-done:
			a.muChecks.Lock()
			res := a.checks
			a.muChecks.Unlock()
			return c.JSON(http.StatusOK, res)
		case <-c.Request().Context().Done():
			return c.Request().Context().Err()
		}
	}, dcOnly)
	// a GPO exported with samba-tool gpo backup (container and SYSVOL as XML), as a .tar.gz; read only
	api.GET("/dc/gpos/:guid/backup", func(c *echo.Context) error {
		ctx, cancel := context.WithTimeout(c.Request().Context(), 2*time.Minute)
		defer cancel()
		// this DC itself (samba-tool would otherwise look for any DC of the domain), with its machine account
		self := strings.ToLower(testparm(ctx, "netbios name") + "." + a.realm)
		policies := ""
		if sv := testparmSection(ctx, "sysvol", "path"); sv != "" {
			policies = filepath.Join(sv, a.realm, "Policies")
		}
		b, err := sambadc.BackupGPO(ctx, sambadc.Runner(shares.Exec), self, policies, c.Param("guid"))
		switch {
		case errors.Is(err, sambadc.ErrInvalidGUID):
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		case err != nil:
			return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		}
		return c.Blob(http.StatusOK, "application/gzip", b)
	}, dcOnly)
	// the one change an agent makes on a DC, in its explicit write mode (gpo_restore_user): a GPO restored from a
	// backup as a new GPO ({name, archive: base64 of the .tar.gz}); the panel audits who asked
	api.POST("/dc/gpos/restore", func(c *echo.Context) error {
		if err := a.gpoRestoreReady(); err != nil {
			return c.JSON(http.StatusForbidden, map[string]string{"error": err.Error()})
		}
		var in struct {
			Name    string `json:"name"`
			Archive []byte `json:"archive"`
		}
		if err := json.NewDecoder(io.LimitReader(c.Request().Body, sambadc.MaxBackup*2)).Decode(&in); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		}
		ctx, cancel := context.WithTimeout(c.Request().Context(), 2*time.Minute)
		defer cancel()
		self := strings.ToLower(testparm(ctx, "netbios name") + "." + a.realm)
		run := sambadc.WithEnv("PASSWD_FILE=" + a.cfg.GPORestorePasswordFile)
		guid, err := sambadc.RestoreGPO(ctx, run, self, a.cfg.GPORestoreUser, in.Name, in.Archive)
		switch {
		case errors.Is(err, sambadc.ErrInvalidGPOName), errors.Is(err, sambadc.ErrInvalidBackup):
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		case err != nil:
			return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		}
		slog.Info("GPO restored", "name", in.Name, "guid", guid)
		return c.JSON(http.StatusOK, map[string]string{"guid": guid, "dc": self})
	}, dcOnly)
	// GPOs of another domain's DC copied here as new GPOs (the panel's "From another AD"), in the explicit write
	// mode: {source: {host, user, password}, gpos: [{guid, name}]}; the source password reaches samba-tool through
	// a 0600 PASSWD_FILE removed at the end, never an argument or a log
	api.POST("/dc/gpos/import", func(c *echo.Context) error {
		if err := a.gpoRestoreReady(); err != nil {
			return c.JSON(http.StatusForbidden, map[string]string{"error": err.Error()})
		}
		var in struct {
			Source struct {
				Host     string `json:"host"`
				User     string `json:"user"`
				Password string `json:"password"`
			} `json:"source"`
			GPOs []struct {
				GUID string `json:"guid"`
				Name string `json:"name"`
			} `json:"gpos"`
		}
		if err := json.NewDecoder(io.LimitReader(c.Request().Body, 1<<20)).Decode(&in); err != nil || len(in.GPOs) == 0 || len(in.GPOs) > 500 ||
			in.Source.Password == "" || strings.ContainsAny(in.Source.Password, "\r\n") {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		}
		ctx, cancel := context.WithTimeout(c.Request().Context(), 15*time.Minute)
		defer cancel()
		dir, err := os.MkdirTemp("", "ad-manager-srcpass-*")
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		defer os.RemoveAll(dir)
		pass := filepath.Join(dir, "pass")
		if err := os.WriteFile(pass, []byte(in.Source.Password), 0o600); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		self := strings.ToLower(testparm(ctx, "netbios name") + "." + a.realm)
		runSrc := sambadc.WithEnv("PASSWD_FILE=" + pass)
		runDst := sambadc.WithEnv("PASSWD_FILE=" + a.cfg.GPORestorePasswordFile)
		type result struct {
			GUID    string `json:"guid"`
			Name    string `json:"name"`
			NewGUID string `json:"new_guid,omitempty"`
			Error   string `json:"error,omitempty"`
		}
		out := make([]result, 0, len(in.GPOs))
		for _, g := range in.GPOs {
			r := result{GUID: g.GUID, Name: g.Name}
			var err error
			if r.NewGUID, err = sambadc.ImportGPO(ctx, runSrc, runDst, self, in.Source.Host, in.Source.User, a.cfg.GPORestoreUser, g.GUID, g.Name); err != nil {
				r.Error = err.Error()
			} else {
				slog.Info("GPO imported", "source", in.Source.Host, "guid", g.GUID, "name", g.Name, "new_guid", r.NewGUID)
			}
			out = append(out, r)
		}
		return c.JSON(http.StatusOK, out)
	}, dcOnly)
	api.GET("/samba/logs", sambalog.ListHandler(a.logDir))
	api.GET("/samba/logs/:name", sambalog.ReadHandler(a.logDir, "name"))
	api.GET("/dirs", func(c *echo.Context) error {
		ds, err := a.mgr.Dirs(c.QueryParam("path"))
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, map[string]any{"roots": a.cfg.ShareRoots, "dirs": ds})
	})
	api.POST("/tasks", func(c *echo.Context) error {
		var t TaskReq
		if err := c.Bind(&t); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		}
		if err := a.validate(t); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		info, err := a.client.EnqueueContext(c.Request().Context(), asynq.NewTask(t.Type, t.Payload),
			asynq.Queue(a.cfg.Queue), asynq.MaxRetry(5), asynq.Retention(7*24*time.Hour), asynq.Timeout(taskTimeout))
		if err != nil {
			return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "queue unavailable: " + err.Error()})
		}
		return c.JSON(http.StatusAccepted, TaskInfo{ID: info.ID, Queue: info.Queue, Type: info.Type, State: info.State.String()})
	})
	api.GET("/tasks/:id", func(c *echo.Context) error {
		ti, err := a.insp.GetTaskInfo(a.cfg.Queue, c.Param("id"))
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, TaskInfo{ID: ti.ID, Queue: ti.Queue, Type: ti.Type, State: ti.State.String(),
			Retried: ti.Retried, LastErr: ti.LastErr, Result: string(ti.Result)})
	})
	a.backupRoutes(api)
	a.syncRoutes(api)
	// No asynqmon here: with a shared Redis it would expose the other servers'
	// queues. The monitor lives in the panel, only for a logged-in administrator.
	return e
}

// markFullControl fills Share.Full/FullUsers from each folder's Windows ACL:
// smb.conf only knows "write", the ACL tells Modify from Full control.
// The ACLs come in one batch (a Samba Python process per folder made a DC
// with 29 shares take 4.3 s to list them), and each account name is resolved
// once per listing, the shares 8 at a time.
func (a *Agent) markFullControl(ctx context.Context, list []shares.Share) {
	if !a.mgr.NTACL {
		return
	}
	paths := make([]string, len(list))
	for i := range list {
		paths[i] = list[i].Path
	}
	acls := a.mgr.FolderACLs(ctx, paths)
	var mu sync.Mutex
	sids := map[string]string{} // "user/name" → SID ("" when unknown)
	sid := func(name string, user bool) string {
		k := fmt.Sprint(user, "/", name)
		mu.Lock()
		s, ok := sids[k]
		mu.Unlock()
		if ok {
			return s
		}
		s, err := a.mgr.SID(ctx, name, user)
		if err != nil {
			s = ""
		}
		mu.Lock()
		sids[k] = s
		mu.Unlock()
		return s
	}
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i := range list {
		acl := acls[list[i].Path]
		if acl == nil {
			continue
		}
		sem <- struct{}{}
		wg.Go(func() { // each goroutine writes only its own share
			defer func() { <-sem }()
			markShareFull(&list[i], acl, sid)
		})
	}
	wg.Wait()
}

// markShareFull moves the accounts with Full control on the share's folder (acl) from RW to Full.
func markShareFull(sh *shares.Share, acl *sambabridge.ACL, sid func(string, bool) string) {
	// by SID (names may come with or without the domain); an inherit-only
	// ACE (0x08) does not apply to the folder itself
	full := map[string]bool{}
	for _, e := range acl.ACEs {
		if e.Type == "allow" && e.Access == "full" && e.Flags&0x08 == 0 {
			full[e.SID] = true
		}
	}
	isFull := func(name string, user bool) bool {
		s := sid(name, user)
		return s != "" && full[s]
	}
	for _, g := range sh.RW {
		if isFull(g, false) {
			sh.Full = append(sh.Full, g)
		}
	}
	for _, u := range sh.RWUsers {
		if isFull(u, true) {
			sh.FullUsers = append(sh.FullUsers, u)
		}
	}
	// smb.conf only knows "write list": full control members move out of RW
	sh.RW = shares.Without(sh.RW, sh.Full)
	sh.RWUsers = shares.Without(sh.RWUsers, sh.FullUsers)
}

// bearer is the middleware that requires a valid Bearer token.
func (a *Agent) bearer(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		tok, ok := strings.CutPrefix(c.Request().Header.Get("Authorization"), "Bearer ")
		if !ok || !a.tokenOK(tok) {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid token"})
		}
		return next(c)
	}
}

// slogAdapter forwards asynq logs to slog.
type slogAdapter struct{}

func (slogAdapter) Debug(args ...any) {}
func (slogAdapter) Info(args ...any)  { slog.Info(fmt.Sprint(args...)) }
func (slogAdapter) Warn(args ...any)  { slog.Warn(fmt.Sprint(args...)) }
func (slogAdapter) Error(args ...any) { slog.Error(fmt.Sprint(args...)) }
func (slogAdapter) Fatal(args ...any) { slog.Error(fmt.Sprint(args...)); os.Exit(1) }
