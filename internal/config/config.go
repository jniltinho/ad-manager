// Package config loads config.toml and applies AD_* environment overrides.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"ad-manager/internal/ad"

	"github.com/pelletier/go-toml/v2"
)

// Config is the complete configuration.
type Config struct {
	// Setup: a fresh installation (the package's config.toml): the panel serves only the setup wizard and the agent
	// only its loopback setup server, until the wizard writes the real configuration (docs/installation.md)
	Setup  bool      `toml:"setup"`
	AD     ad.Config `toml:"ad"`
	Server Server    `toml:"server"`
	DB     DB        `toml:"db"`
	Redis  Redis     `toml:"redis"`
	Agent  Agent     `toml:"agent"`
	Samba  Samba     `toml:"samba"`
	File   string    `toml:"-"` // the file read ("" when none: defaults and environment)
}

// Samba points to the local Samba installation, read by the Logs page (panel
// on a DC) and by the agent (file servers).
type Samba struct {
	LogDir string `toml:"log_dir"` // Samba's "log file" directory
}

// DB is the local SQLite database (registered agents, source AD profiles).
type DB struct {
	Path string `toml:"path"`
}

// Redis is the task queue (asynq). An empty Addr disables the queue and /asynqmon.
type Redis struct {
	Addr     string `toml:"addr"`
	Password string `toml:"password"` // prefer AD_REDIS_PASSWORD
	DB       int    `toml:"db"`
	TLS      bool   `toml:"tls"`
}

// Agent configures the file server daemon.
type Agent struct {
	Listen     string   `toml:"listen"`
	CertFile   string   `toml:"cert_file"`
	KeyFile    string   `toml:"key_file"`
	TokenFile  string   `toml:"token_file"` // SHA-256 of the token
	Queue      string   `toml:"queue"`
	ShareRoots []string `toml:"share_roots"`
	SelfSigned bool     `toml:"self_signed"` // without cert_file: generate and reuse a self-signed one
	// the import write modes of an agent on a domain controller (GPO restore/import and the password migration),
	// switched on without any secret on disk: the administrator credential comes from the panel's database with
	// each GPO task (docs/password-migration.md). The two pairs of keys below are the former way, still accepted.
	ImportModes bool `toml:"import_modes"`
	// explicit write mode of an agent on a domain controller (otherwise read only): restoring a GPO from a backup,
	// always as a new GPO, as this account (it creates GPOs: Domain Admins on Samba) with its password in a file
	// that only root reads (never in the configuration)
	GPORestoreUser         string `toml:"gpo_restore_user"`
	GPORestorePasswordFile string `toml:"gpo_restore_password_file"`
	// dedicated, default-off write mode for the password-material migration (docs/migration-runbook.md): a
	// target-domain administrator and its password in a 0600 file that only root reads, authorizing the agent to
	// read the source's password hashes over DRSUAPI and set them on the local directory. Separate from the GPO
	// restore gate: the GPO account never authorizes this.
	PasswordMigrationUser         string `toml:"password_migration_user"`
	PasswordMigrationPasswordFile string `toml:"password_migration_password_file"`
	// backups of this server (docs/backup.md): archives, manifests and schedules; default /var/backups/ad-manager
	BackupDir string `toml:"backup_dir"`
	// largest backup accepted by the upload (Backup page → send), in MiB; default 20480 (20 GiB)
	BackupUploadMaxMiB int64 `toml:"backup_upload_max_mib"`
	// run the sync worker without a systemd unit around it (a container without systemd); off: a sync runs only
	// confined to its destination
	SyncUnconfined bool `toml:"sync_unconfined"`
	// a domain controller that also serves files (docs/dc-file-server.md): the agent of a DC changes files only with
	// this on and Windows ACLs, registry shares = yes and share roots clear of Samba's directories
	DCFileServer bool `toml:"dc_file_server"`
}

// Server configures the web panel.
type Server struct {
	Listen         string `toml:"listen"`
	CertFile       string `toml:"cert_file"`
	KeyFile        string `toml:"key_file"`
	SessionMinutes int    `toml:"session_minutes"`
	AuditLog       string `toml:"audit_log"`
	AccessLog      string `toml:"access_log"`  // HTTP requests of the API (Logs → HTTP); default: beside audit_log
	TrustProxy     bool   `toml:"trust_proxy"` // honor X-Forwarded-For from a loopback proxy
	SelfSigned     bool   `toml:"self_signed"` // without cert_file: generate and reuse a self-signed one
}

// StateDir returns where the service keeps its state: the SQLite directory, or
// /opt/ad-manager/data (from the package) when the database path is relative.
func (c *Config) StateDir() string {
	d := filepath.Dir(c.DB.Path)
	if !filepath.IsAbs(d) {
		if fi, err := os.Stat("/opt/ad-manager/data"); err == nil && fi.IsDir() {
			return "/opt/ad-manager/data"
		}
	}
	return d
}

// Load reads the configuration file. An empty path searches ./config.toml and
// /opt/ad-manager/config.toml; a missing file means defaults plus environment only.
func Load(path string) (*Config, error) {
	c := &Config{
		AD:     ad.Config{URL: "ldaps://dc1.example.local", Domain: "example.local"},
		Server: Server{Listen: "127.0.0.1:8443", SessionMinutes: 30, AuditLog: "audit.log"},
		DB:     DB{Path: "ad-manager.db"},
		Agent:  Agent{Listen: "0.0.0.0:9443", TokenFile: "/opt/ad-manager/agent.token", ShareRoots: []string{"/srv/shares"}},
		Samba:  Samba{LogDir: "/var/log/samba"},
	}
	candidates := []string{path}
	if path == "" {
		candidates = []string{"config.toml", "/opt/ad-manager/config.toml"}
	}
	for _, p := range candidates {
		b, err := os.ReadFile(p)
		if errors.Is(err, os.ErrNotExist) && path == "" {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := toml.Unmarshal(b, c); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		c.File = p
		break
	}
	env := func(k string, dst *string) {
		if v := os.Getenv(k); v != "" {
			*dst = v
		}
	}
	env("AD_URL", &c.AD.URL)
	env("AD_DOMAIN", &c.AD.Domain)
	env("AD_BASE_DN", &c.AD.BaseDN)
	env("AD_CA_FILE", &c.AD.CAFile)
	env("AD_ADMIN_GROUP", &c.AD.AdminGroup)
	env("AD_LISTEN", &c.Server.Listen)
	env("AD_DB", &c.DB.Path)
	env("AD_REDIS_ADDR", &c.Redis.Addr)
	env("AD_REDIS_PASSWORD", &c.Redis.Password)
	env("AD_AGENT_LISTEN", &c.Agent.Listen)
	env("AD_AGENT_TOKEN_FILE", &c.Agent.TokenFile)
	env("AD_SAMBA_LOG_DIR", &c.Samba.LogDir)
	if v, err := strconv.ParseBool(os.Getenv("AD_SELF_SIGNED")); err == nil {
		c.Server.SelfSigned, c.Agent.SelfSigned = v, v
	}
	if v, err := strconv.ParseBool(os.Getenv("AD_INSECURE_SKIP_VERIFY")); err == nil {
		c.AD.InsecureSkipVerify = v
	}
	if c.Server.AccessLog == "" && c.Server.AuditLog != "" {
		c.Server.AccessLog = filepath.Join(filepath.Dir(c.Server.AuditLog), "access.log")
	}
	if c.Agent.Queue == "" {
		h, _ := os.Hostname()
		c.Agent.Queue = "fs-" + h
	}
	if c.AD.Domain == "" || c.AD.URL == "" {
		return nil, errors.New("ad.url and ad.domain are required")
	}
	return c, nil
}
