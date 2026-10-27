// Package config is the desktop's desktop.toml: the same file, keys, legacy keys and lookup order as the Rust and
// the archived Fyne apps, so either opens the other's. It holds no secret (the password is asked at sign-in), is
// written atomically with mode 0600 and refused when readable by others, and keeps the keys and sections this app
// does not use (the Rust app's [graphics] gpu_backend) when it rewrites the file.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Config is what the desktop reads and writes in desktop.toml.
type Config struct {
	URL         string // [panel] url
	User        string // [panel] user
	Fingerprint string // [panel] fingerprint: the pinned certificate of a self-signed panel ("sha256:<hex>")
	Insecure    bool   // [panel] insecure: a legacy "accept any certificate", until the next tested Save
	Lang        string // lang: "pt-BR" or "en"; empty = the panel's default
	Theme       string // [appearance] theme: this computer's theme; empty = the panel's
	Flat        string // [appearance] flat: "flat" or "depth"; empty = the panel's
	SafeGPU     bool   // [graphics] safe: start the webview with GPU acceleration off

	raw       map[string]any // the whole file, rewritten with the fields above
	hadAgents bool           // the file listed file servers with tokens ([[agents]], no longer used)
}

// File is a desktop.toml on disk.
type File struct {
	Path   string
	Create bool   // no file yet: it is written at the first Save, next to the executable when that folder is writable
	Notice string // said once after sign-in (the agent tokens of an older file were removed)
}

// exampleURLs are the panel URLs of the examples earlier versions wrote on first run: a file with one of them and no
// agents was never filled in.
var exampleURLs = []string{"https://panel.example.local:8443", "https://painel.empresa.local:8443", "https://painel.empresa.net:8443"}

// ErrLoose is a desktop.toml readable by group or others (Unix).
var ErrLoose = errors.New("must be private")

// Decode reads a desktop.toml: the current keys, and for each part left empty the Portuguese keys of versions up to
// 0.x ([painel] usuario, [[agentes]], idioma).
func Decode(b []byte) (*Config, error) {
	raw := map[string]any{}
	if err := toml.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	str := func(m map[string]any, k string) string { s, _ := m[k].(string); return s }
	table := func(k string) map[string]any { t, _ := raw[k].(map[string]any); return t }
	c := &Config{raw: raw}
	if p := table("panel"); p != nil {
		c.URL, c.User, c.Fingerprint = str(p, "url"), str(p, "user"), str(p, "fingerprint")
		c.Insecure, _ = p["insecure"].(bool)
	}
	if old := table("painel"); old != nil && c.URL == "" && c.User == "" && !c.Insecure {
		c.URL, c.User = str(old, "url"), str(old, "usuario")
		c.Insecure, _ = old["insecure"].(bool)
	}
	c.Lang = str(raw, "lang")
	if c.Lang == "" {
		c.Lang = str(raw, "idioma")
	}
	if a := table("appearance"); a != nil {
		c.Theme, c.Flat = str(a, "theme"), str(a, "flat")
	}
	if g := table("graphics"); g != nil {
		c.SafeGPU, _ = g["safe"].(bool)
	}
	agents, _ := raw["agents"].([]any)
	agentes, _ := raw["agentes"].([]any)
	c.hadAgents = len(agents) > 0 || len(agentes) > 0
	return c, nil
}

// Encode is the file for c: its raw keys with the fields of c over them, the legacy Portuguese keys and the agent
// tokens dropped.
func (c *Config) Encode() ([]byte, error) {
	raw := map[string]any{}
	for k, v := range c.raw {
		raw[k] = v
	}
	for _, k := range []string{"painel", "agentes", "idioma", "agents"} {
		delete(raw, k)
	}
	panel := tableOf(raw, "panel")
	set(panel, "url", c.URL)
	set(panel, "user", c.User)
	set(panel, "fingerprint", c.Fingerprint)
	if c.Insecure && c.Fingerprint == "" {
		panel["insecure"] = true
	} else {
		delete(panel, "insecure")
	}
	set(raw, "lang", c.Lang)
	app := tableOf(raw, "appearance")
	set(app, "theme", c.Theme)
	set(app, "flat", c.Flat)
	gr := tableOf(raw, "graphics")
	if c.SafeGPU {
		gr["safe"] = true
	} else {
		delete(gr, "safe")
	}
	for _, k := range []string{"appearance", "graphics"} {
		if t, _ := raw[k].(map[string]any); len(t) == 0 {
			delete(raw, k)
		}
	}
	b, err := toml.Marshal(raw)
	if err != nil {
		return nil, err
	}
	return append([]byte("# ad-manager desktop — written by the Configure screen (can be edited by hand)\n"), b...), nil
}

func tableOf(raw map[string]any, k string) map[string]any {
	t, ok := raw[k].(map[string]any)
	if !ok {
		t = map[string]any{}
	} else {
		t = cloneMap(t)
	}
	raw[k] = t
	return t
}

func cloneMap(m map[string]any) map[string]any {
	c := make(map[string]any, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

func set(m map[string]any, k, v string) {
	if v == "" {
		delete(m, k)
	} else {
		m[k] = v
	}
}

// localPath is .config/desktop.toml next to the executable; none inside a macOS .app (writing there would break its
// signature).
func localPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	if strings.Contains(exe, ".app/Contents/MacOS/") {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), ".config", "desktop.toml")
}

// userPath is the per-user file: ~/.config/ad-manager (Linux), ~/Library/Application Support/ad-manager (macOS),
// %AppData%\ad-manager (Windows).
func userPath() string {
	d, _ := os.UserConfigDir()
	return filepath.Join(d, "ad-manager", "desktop.toml")
}

// Resolve picks the file, in order: flag (--config), the AD_MANAGER_DESKTOP variable, .config/desktop.toml next to
// the executable, then the per-user file when it was filled in.
func Resolve(flag string) File {
	for _, p := range []string{flag, os.Getenv("AD_MANAGER_DESKTOP")} {
		if p != "" {
			_, err := os.Stat(p)
			return File{Path: p, Create: os.IsNotExist(err)}
		}
	}
	local, user := localPath(), userPath()
	if local != "" {
		if _, err := os.Stat(local); err == nil {
			return File{Path: local}
		}
	}
	// the per-user file counts only when the user filled it in: an untouched example of an older version does not
	// hide the local one
	if b, err := os.ReadFile(user); err == nil && !untouched(b) {
		return File{Path: user}
	}
	if local != "" && writableDir(filepath.Dir(filepath.Dir(local))) {
		return File{Path: local, Create: true}
	}
	_, err := os.Stat(user)
	return File{Path: user, Create: os.IsNotExist(err)}
}

func untouched(b []byte) bool {
	c, err := Decode(b)
	return err == nil && !c.hadAgents && slices.Contains(exampleURLs, c.URL)
}

func writableDir(dir string) bool {
	f, err := os.CreateTemp(dir, ".ad-manager-write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

// Load reads f. A file that is missing, untouched or without a panel URL is "unconfigured" (Configure opens; only
// preferences may be in it). A file with agent tokens is rewritten without them, with a notice for after sign-in.
func Load(f *File) (c *Config, unconfigured bool, err error) {
	if f.Create {
		return &Config{User: "administrator"}, true, nil
	}
	b, err := os.ReadFile(f.Path)
	if err != nil {
		return nil, false, err
	}
	// on Windows the mode does not reflect the ACL (the user's profile is already private)
	if fi, err := os.Stat(f.Path); err == nil && runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		return nil, false, fmt.Errorf("%s %w and has mode %#o: run chmod 600", f.Path, ErrLoose, fi.Mode().Perm())
	}
	if c, err = Decode(b); err != nil {
		return nil, false, fmt.Errorf("%s: %w", f.Path, err)
	}
	if c.User == "" {
		c.User = "administrator"
	}
	if c.hadAgents {
		f.Notice = "File-server tokens are no longer used: they were removed from the configuration file."
		if err := Save(f, c); err != nil {
			f.Notice = "File-server tokens are no longer used, but they could not be removed from the configuration file: " + err.Error()
		}
		c.hadAgents = false
	}
	return c, untouched(b) || c.URL == "", nil
}

// Save writes c to f atomically (a private temporary file renamed over it), mode 0600; a --config link is followed
// and the file it points to rewritten.
func Save(f *File, c *Config) error {
	path := f.Path
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	b, err := c.Encode()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".desktop-*.toml") // 0600
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	f.Create = false
	return nil
}
