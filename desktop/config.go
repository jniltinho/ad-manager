package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// config is the desktop.toml file (mode 0600: it holds agent tokens); see
// resolveConfigPath for where it lives. Files written by older versions use
// Portuguese keys: decodeConfig still reads them (legacyConfig).
type config struct {
	Panel struct {
		URL      string `toml:"url"`
		Insecure bool   `toml:"insecure"`
		User     string `toml:"user"`
	} `toml:"panel"`
	Agents []agentConfig `toml:"agents"`
	Lang   string        `toml:"lang,omitempty"` // "pt-BR" or "en"; empty = panel default
}

// agentConfig is a file server reached directly through its agent token.
type agentConfig struct {
	Name     string `toml:"name"`
	URL      string `toml:"url"`
	Token    string `toml:"token"`
	Insecure bool   `toml:"insecure"`
}

// legacyConfig is desktop.toml as versions up to 0.x wrote it ([painel] usuario, [[agentes]] nome, idioma).
type legacyConfig struct {
	Panel struct {
		URL      string `toml:"url"`
		Insecure bool   `toml:"insecure"`
		User     string `toml:"usuario"`
	} `toml:"painel"`
	Agents []struct {
		Name     string `toml:"nome"`
		URL      string `toml:"url"`
		Token    string `toml:"token"`
		Insecure bool   `toml:"insecure"`
	} `toml:"agentes"`
	Lang string `toml:"idioma"`
}

// decodeConfig reads a desktop.toml: the current keys, and for each part left empty the old Portuguese keys.
func decodeConfig(b []byte) (*config, error) {
	c := &config{}
	if err := toml.Unmarshal(b, c); err != nil {
		return nil, err
	}
	var old legacyConfig
	if err := toml.Unmarshal(b, &old); err != nil {
		return nil, err
	}
	if c.Panel.URL == "" && c.Panel.User == "" && !c.Panel.Insecure {
		c.Panel.URL, c.Panel.Insecure, c.Panel.User = old.Panel.URL, old.Panel.Insecure, old.Panel.User
	}
	if len(c.Agents) == 0 {
		for _, a := range old.Agents {
			c.Agents = append(c.Agents, agentConfig{Name: a.Name, URL: a.URL, Token: a.Token, Insecure: a.Insecure})
		}
	}
	if c.Lang == "" {
		c.Lang = old.Lang
	}
	return c, nil
}

// exampleURLs are the panel URLs of the examples written on first run by earlier versions (and of this version's
// placeholder): a file with one of them and no agents was never filled in.
var exampleURLs = []string{"https://panel.example.local:8443", "https://painel.empresa.local:8443", "https://painel.empresa.net:8443"}

// configFlag is the --config command-line value (empty when not given).
var configFlag string

// parseFlags reads --config path (also -config, --config=path). Other
// arguments (e.g. -psn_… from old macOS launchers) are ignored.
func parseFlags(args []string) {
	for i := 0; i < len(args); i++ {
		a := strings.TrimPrefix(args[i], "-")
		switch {
		case a == "-config" || a == "config":
			if i+1 < len(args) {
				configFlag = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "-config=") || strings.HasPrefix(a, "config="):
			configFlag = a[strings.Index(a, "=")+1:]
		}
	}
}

// localConfigPath is .config/desktop.toml next to the executable. Inside a
// macOS .app bundle there is none: writing there would break the signature.
func localConfigPath() string {
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

// userConfigPath is the per-user location used up to 0.3 (~/.config/ad-manager).
func userConfigPath() string {
	d, _ := os.UserConfigDir()
	return filepath.Join(d, "ad-manager", "desktop.toml")
}

// resolveConfigPath picks the config file, in order: --config, the
// AD_MANAGER_DESKTOP variable, .config/desktop.toml next to the executable, then
// the per-user file when it was filled in. create reports that the file does not exist yet and should
// be written: next to the executable when that folder is writable, otherwise in
// the per-user location.
func resolveConfigPath() (path string, create bool) {
	for _, p := range []string{configFlag, os.Getenv("AD_MANAGER_DESKTOP")} {
		if p != "" {
			_, err := os.Stat(p)
			return p, os.IsNotExist(err)
		}
	}
	local, user := localConfigPath(), userConfigPath()
	if local != "" {
		if _, err := os.Stat(local); err == nil {
			return local, false
		}
	}
	// the per-user file only counts when the user filled it in: an untouched
	// example written by an older version should not hide the local one
	if b, err := os.ReadFile(user); err == nil && !untouchedExample(b) {
		return user, false
	}
	if local != "" && writableDir(filepath.Dir(filepath.Dir(local))) {
		return local, true
	}
	_, err := os.Stat(user)
	return user, os.IsNotExist(err)
}

// untouchedExample tells whether b is still an example written on first run by an earlier version: an example
// panel URL and no agents.
func untouchedExample(b []byte) bool {
	c, err := decodeConfig(b)
	return err == nil && len(c.Agents) == 0 && slices.Contains(exampleURLs, c.Panel.URL)
}

// writableDir tells whether a file can be created in dir.
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

// configPath is the file in use (set by loadConfig).
var configPath string

// loadConfig reads the config file. needsSetup is true when there is no file
// yet or it is still the untouched example: the app then opens the setup form
// (the file is only written when the form is saved).
func loadConfig() (c *config, needsSetup bool, err error) {
	p, create := resolveConfigPath()
	configPath = p
	if create {
		c = &config{}
		c.Panel.User = "administrator"
		return c, true, nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, false, err
	}
	// on Windows the os.Stat mode does not reflect the DACL (the user profile is already private)
	if fi, err := os.Stat(p); err == nil && runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		return nil, false, fmt.Errorf("%s holds tokens and has mode %v: run chmod 600", p, fi.Mode().Perm())
	}
	if c, err = decodeConfig(b); err != nil {
		return nil, false, fmt.Errorf("%s: %w", p, err)
	}
	if c.Panel.User == "" {
		c.Panel.User = "administrator"
	}
	return c, untouchedExample(b) || c.Panel.URL == "", nil
}

// saveConfig writes c to path (0600, through a temp file and rename so a
// crash never leaves half a file).
func saveConfig(path string, c *config) error {
	b, err := toml.Marshal(c)
	if err != nil {
		return err
	}
	b = append([]byte("# ad-manager desktop — written by the Settings screen (can be edited by hand)\n"), b...)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".desktop-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
