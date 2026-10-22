package agent

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"ad-manager/internal/backup"
	"ad-manager/internal/config"

	"github.com/labstack/echo/v5"
)

// SetupListen is where the agent serves the setup wizard (loopback only: the panel on this host calls it).
const SetupListen = "127.0.0.1:9444"

// SetupKeyFile is the key the local panel sends (0640 root:ad-manager, beside the panel's state).
func SetupKeyFile(cfg *config.Config) string { return filepath.Join(cfg.StateDir(), "setup-agent.key") }

// setupScript is samba-setup.py as the package installs it.
var setupScript = "/opt/ad-manager/scripts/samba-setup.py"

// SetupPlan is what the wizard asks for: a role and its parameters, never commands.
type SetupPlan struct {
	Role      string `json:"role"` // new-dc, dc-join, restore-dc, member, connect (old names: see roleAliases)
	Realm     string `json:"realm"`
	Domain    string `json:"domain"`     // NetBIOS
	IP        string `json:"ip"`         // this DC's address (new-dc, dc-join)
	SrcDC     string `json:"src_dc"`     // dc-join
	DNSDC     string `json:"dns_dc"`     // member
	URL       string `json:"url"`        // connect
	Insecure  bool   `json:"insecure"`   // connect
	Admin     string `json:"admin"`      // default Administrator
	Forwarder string `json:"forwarder"`  // default 1.1.1.1
	ShareRoot string `json:"share_root"` // member, or a DC with Files; default /srv/shares
	Files     bool   `json:"files"`      // new-dc, dc-join, restore-dc: this DC also serves files
	Backup    string `json:"backup"`     // restore-dc: the DC backup (.tar.bz2) copied to this server
	ImportAD  bool   `json:"import_ad"`  // new-dc with Files: this DC agent's GPO-import and password-migration write modes
}

var (
	realmRe   = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62})(\.[a-z0-9]([a-z0-9-]{0,62}))+$`)
	netbiosRe = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{0,14}$`)
	adminRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	urlRe     = regexp.MustCompile(`^ldaps?://[A-Za-z0-9.-]+(:[0-9]{1,5})?$`)
	rootRe    = regexp.MustCompile(`^/[A-Za-z0-9._/-]{1,200}$`)
	dcFileRe  = regexp.MustCompile(`^/[A-Za-z0-9._/-]{1,250}\.tar\.bz2$`)
)

// roleAliases maps the old Portuguese role names (sent by older panels) to the current ones.
var roleAliases = map[string]string{"dc-novo": "new-dc", "dc-restaurar": "restore-dc", "membro": "member", "conectar": "connect"}

// normalize puts the role under its current name.
func (p *SetupPlan) normalize() {
	if r, ok := roleAliases[p.Role]; ok {
		p.Role = r
	}
}

func ipv4(s string) bool {
	a, err := netip.ParseAddr(s)
	return err == nil && a.Is4()
}

// Args validates the plan and builds samba-setup.py's arguments (the role first).
func (p SetupPlan) Args() ([]string, error) {
	p.normalize()
	p.Realm = strings.ToLower(strings.TrimSpace(p.Realm))
	p.Domain = strings.ToUpper(strings.TrimSpace(p.Domain))
	if p.Admin == "" {
		p.Admin = "Administrator"
	}
	if p.Forwarder == "" {
		p.Forwarder = "1.1.1.1"
	}
	if p.ShareRoot == "" {
		p.ShareRoot = "/srv/shares"
	}
	bad := func(field string) ([]string, error) { return nil, fmt.Errorf("setup: invalid %s", field) }
	if !realmRe.MatchString(p.Realm) {
		return bad("realm")
	}
	if !adminRe.MatchString(p.Admin) {
		return bad("admin")
	}
	if !ipv4(p.Forwarder) {
		return bad("forwarder")
	}
	args := []string{p.Role, "--realm", p.Realm}
	switch p.Role {
	case "new-dc", "dc-join", "restore-dc":
		if !ipv4(p.IP) {
			return bad("ip")
		}
		if p.Role == "restore-dc" { // the domain, its NetBIOS name and its administrator come from the backup
			if !dcFileRe.MatchString(p.Backup) || strings.Contains(p.Backup, "..") {
				return bad("backup")
			}
			args = append(args, "--ip", p.IP, "--backup", p.Backup, "--panel")
		} else {
			if !netbiosRe.MatchString(p.Domain) {
				return bad("domain")
			}
			args = append(args, "--domain", p.Domain, "--ip", p.IP, "--admin", p.Admin, "--forwarder", p.Forwarder, "--panel")
		}
		if p.Files { // this DC also serves files
			if !rootRe.MatchString(p.ShareRoot) || strings.Contains(p.ShareRoot, "..") {
				return bad("share_root")
			}
			args = append(args, "--files", "--share-root", p.ShareRoot)
		}
		if p.Role == "dc-join" {
			if !ipv4(p.SrcDC) { // it becomes this host's nameserver: an address, not a name that needs DNS
				return bad("src_dc")
			}
			args = append(args, "--src-dc", p.SrcDC)
		}
	case "member":
		if !netbiosRe.MatchString(p.Domain) {
			return bad("domain")
		}
		if !ipv4(p.DNSDC) {
			return bad("dns_dc")
		}
		if !rootRe.MatchString(p.ShareRoot) || strings.Contains(p.ShareRoot, "..") {
			return bad("share_root")
		}
		args = append(args, "--domain", p.Domain, "--dns-dc", p.DNSDC, "--admin", p.Admin, "--share-root", p.ShareRoot, "--agent")
	case "connect":
		if !urlRe.MatchString(p.URL) {
			return bad("url")
		}
		args = append(args, "--url", p.URL)
		if p.Insecure {
			args = append(args, "--insecure")
		}
	default:
		return bad("role")
	}
	if p.ImportAD {
		if p.Role != "new-dc" || !p.Files {
			return bad("import_ad")
		}
		args = append(args, "--import-ready")
	}
	return args, nil
}

// needsPassword: the roles that sign in to a domain (or create one).
func (p SetupPlan) needsPassword() bool {
	p.normalize()
	return p.Role != "connect" && p.Role != "restore-dc"
}

// setupJob is the one setup run of this agent.
type setupJob struct {
	mu       sync.Mutex
	state    string // idle, backup, running, ok, error
	unit     string
	logFile  string
	backupID string
	err      string
	secret   string // the password, masked in what is shown and kept
}

// RunSetup serves the setup wizard's agent side until ctx ends (config setup = true): no queue, no token, only
// 127.0.0.1 and the local key.
func RunSetup(ctx context.Context, cfg *config.Config) error {
	key, err := setupKey(SetupKeyFile(cfg))
	if err != nil {
		return fmt.Errorf("setup key: %w", err)
	}
	job := &setupJob{state: "idle"}
	store := &backup.Store{Dir: backupDir(cfg.Agent)}
	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			host, _, _ := net.SplitHostPort(c.Request().RemoteAddr)
			ip := net.ParseIP(host)
			got := c.Request().Header.Get("X-Setup-Key")
			if ip == nil || !ip.IsLoopback() || subtle.ConstantTimeCompare([]byte(got), []byte(key)) != 1 {
				return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
			}
			return next(c)
		}
	})
	e.GET("/setup/diagnose", func(c *echo.Context) error {
		out, err := exec.CommandContext(c.Request().Context(), "python3", setupScript, "diagnose").Output()
		if err != nil {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": "diagnosis: " + err.Error()})
		}
		return c.JSONBlob(http.StatusOK, out)
	})
	e.POST("/setup/plan", func(c *echo.Context) error { // what the run would do (dry run, nothing changes)
		var p SetupPlan
		if err := c.Bind(&p); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		}
		args, err := p.Args()
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		cmd := exec.CommandContext(c.Request().Context(), "python3", append([]string{setupScript}, append(args, "--dry-run")...)...)
		cmd.Env = append(os.Environ(), "SETUP_JSON=1")
		out, _ := cmd.CombinedOutput()
		return c.JSON(http.StatusOK, map[string]any{"lines": strings.Split(strings.TrimSpace(string(out)), "\n")})
	})
	e.POST("/setup/run", func(c *echo.Context) error {
		var in struct {
			SetupPlan
			Password string `json:"password"`
		}
		if err := c.Bind(&in); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		}
		args, err := in.Args()
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		if in.needsPassword() && (in.Password == "" || strings.ContainsAny(in.Password, "\r\n")) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "setup: the administrator password is required"})
		}
		if err := job.start(store, args, in.Password); err != nil {
			return c.JSON(http.StatusConflict, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusAccepted, map[string]string{"state": "started"})
	})
	e.GET("/setup/status", func(c *echo.Context) error {
		from, _ := strconv.Atoi(c.QueryParam("from"))
		return c.JSON(http.StatusOK, job.status(from))
	})
	srv := &http.Server{Addr: SetupListen, Handler: e, ReadHeaderTimeout: 10 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("agent in setup mode", "listen", SetupListen, "key", SetupKeyFile(cfg))
		errCh <- srv.ListenAndServe()
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shut)
}

// setupKey reads the local key, creating it (32 random bytes, hex) readable by root and the ad-manager group.
func setupKey(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) >= 32 {
		return strings.TrimSpace(string(b)), nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	key := hex.EncodeToString(b)
	if err := os.WriteFile(path, []byte(key+"\n"), 0o640); err != nil {
		return "", err
	}
	if g, err := user.LookupGroup("ad-manager"); err == nil {
		if gid, err := strconv.Atoi(g.Gid); err == nil {
			_ = os.Chown(path, 0, gid)
		}
	} else {
		_ = os.Chmod(path, 0o600) // no panel user here: root only
	}
	return key, nil
}

// start backs up what Samba has here (when it has anything), then runs samba-setup.py as a transient systemd unit
// outside the agent's sandbox, the password on its standard input (a 0600 file in /run removed when it ends) and
// its output appended to a log file.
func (j *setupJob) start(store *backup.Store, args []string, password string) error {
	j.mu.Lock()
	if j.state == "backup" || j.state == "running" {
		j.mu.Unlock()
		return errors.New("a setup is already running")
	}
	stamp := time.Now().Format("20060102-150405")
	j.state, j.err, j.backupID, j.secret = "backup", "", "", password
	j.unit = "ad-manager-setup-" + stamp
	j.logFile = "/var/log/ad-manager/setup-" + stamp + ".log"
	j.mu.Unlock()
	go j.run(store, args, password, stamp)
	return nil
}

func (j *setupJob) fail(err error) {
	j.mu.Lock()
	j.state, j.err = "error", err.Error()
	j.mu.Unlock()
	slog.Warn("setup", "err", err)
}

func (j *setupJob) run(store *backup.Store, args []string, password, stamp string) {
	ctx := context.Background()
	if err := os.MkdirAll(filepath.Dir(j.logFile), 0o750); err != nil {
		j.fail(err)
		return
	}
	// 1. the backup of what is here, before anything changes
	if sambaHere() {
		// a consistent copy: the Samba services stop while the files are copied (their databases change while they
		// run), and come back as they were
		var stopped []string
		startAgain := func() error { // every unit stopped here comes back, whatever happened
			var errs []error
			for _, u := range stopped {
				if out, err := exec.Command("systemctl", "start", u).CombinedOutput(); err != nil {
					errs = append(errs, fmt.Errorf("starting %s again: %w: %s", u, err, strings.TrimSpace(string(out))))
				}
			}
			return errors.Join(errs...)
		}
		for _, u := range []string{"samba-ad-dc", "smbd", "nmbd", "winbind"} {
			if exec.Command("systemctl", "is-active", "--quiet", u).Run() == nil {
				if err := exec.Command("systemctl", "stop", u).Run(); err != nil {
					j.fail(errors.Join(fmt.Errorf("stopping %s for the backup: %w", u, err), startAgain()))
					return
				}
				stopped = append(stopped, u)
			}
		}
		m, err := store.Run(ctx, backup.Exec, backup.Job{Kind: backup.KindSambaFiles, Origin: "setup", Note: "before the setup"}, backup.Sources{})
		if serr := startAgain(); serr != nil {
			err = errors.Join(err, serr) // the setup does not go on over services that did not come back
		}
		if err != nil {
			j.fail(fmt.Errorf("backup before the setup: %w", err))
			return
		}
		j.mu.Lock()
		j.backupID = m.ID
		j.mu.Unlock()
	}
	// 2. the run, outside the sandbox, as its own unit
	run := "/run/ad-manager-setup"
	if err := os.MkdirAll(run, 0o700); err != nil {
		j.fail(err)
		return
	}
	pw := filepath.Join(run, stamp+".pw")
	if err := os.WriteFile(pw, []byte(password+"\n"), 0o600); err != nil {
		j.fail(err)
		return
	}
	defer os.Remove(pw)
	j.mu.Lock()
	j.state = "running"
	j.mu.Unlock()
	cmd := exec.CommandContext(ctx, "systemd-run", append([]string{"--unit=" + j.unit, "--collect", "--wait", "--quiet",
		"--property=StandardInput=file:" + pw, "--property=StandardOutput=append:" + j.logFile, "--property=StandardError=append:" + j.logFile,
		"--setenv=SETUP_JSON=1", "--setenv=DEBIAN_FRONTEND=noninteractive", "--", "/usr/bin/python3", setupScript}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		j.fail(fmt.Errorf("samba-setup.py: %v %s", err, strings.TrimSpace(string(out))))
		return
	}
	j.mu.Lock()
	j.state = "ok"
	j.mu.Unlock()
	if len(args) > 0 && (args[0] == "member" || slices.Contains(args, "--files")) {
		member := args[0] == "member"
		// samba-setup.py left the agent's restart to here (restarting it there would end this job): once the wizard
		// has read the result, the agent comes back with its file-server configuration and this host's setup-only
		// panel goes (a file server is managed from the domain's panel)
		go func() {
			time.Sleep(20 * time.Second)
			if member { // a DC keeps its panel
				_ = exec.Command("systemctl", "disable", "--now", "ad-manager").Run()
			}
			_ = exec.Command("systemctl", "restart", "ad-manager-agent").Run()
		}()
	}
}

// sambaHere tells whether Samba left anything on this server.
func sambaHere() bool {
	for _, p := range []string{"/etc/samba/smb.conf", "/var/lib/samba/private", "/etc/krb5.conf"} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// SetupStatus is what the wizard polls.
type SetupStatus struct {
	State    string   `json:"state"`
	Backup   string   `json:"backup,omitempty"`
	Error    string   `json:"error,omitempty"`
	Lines    []string `json:"lines"` // from the requested line on
	Next     int      `json:"next"`  // ask from here next time
	LogFile  string   `json:"log_file,omitempty"`
	Finished bool     `json:"finished"`
}

// tokenLine is the line samba-setup.py prints the new agent token on (shown once): masked like the password.
var tokenLine = regexp.MustCompile(`^\s{2,}[A-Za-z0-9_-]{32,}\s*$`)

func (j *setupJob) status(from int) SetupStatus {
	j.mu.Lock()
	st := SetupStatus{State: j.state, Backup: j.backupID, Error: j.err, LogFile: j.logFile, Lines: []string{},
		Finished: j.state == "ok" || j.state == "error"}
	file, secret := j.logFile, j.secret
	j.mu.Unlock()
	if file == "" {
		return st
	}
	f, err := os.Open(file)
	if err != nil {
		return st
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	n := 0
	for sc.Scan() {
		if n >= from && len(st.Lines) < 2000 {
			l := sc.Text()
			if secret != "" {
				l = strings.ReplaceAll(l, secret, "***")
			}
			if tokenLine.MatchString(l) {
				l = "   *** (agent token: see the server's terminal or generate another one)"
			}
			st.Lines = append(st.Lines, l)
		}
		n++
	}
	st.Next = from + len(st.Lines)
	return st
}

// statusJSON is for logs.
func (s SetupStatus) String() string { b, _ := json.Marshal(s); return string(b) }
