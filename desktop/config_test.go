package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// exampleConfig is the file earlier versions wrote on first run (old Portuguese keys): it is still recognized
// (untouchedExample) so the setup form opens instead.
const exampleConfig = `# ad-manager desktop — panel and file servers
[painel]
url = "https://painel.empresa.local:8443"
insecure = false
usuario = "administrator"

# [[agentes]]
# nome = "fs01"
# url = "https://fs01.empresa.local:9443"
# token = "generated with: sudo ad-manager agent token"
# insecure = false
`

// A desktop.toml with the old Portuguese keys is still read; saving it writes the current keys.
func TestDecodeConfigOldKeys(t *testing.T) {
	old := `idioma = "pt-BR"
[painel]
url = "https://ad.example.local:8443"
usuario = "joe"
[[agentes]]
nome = "fs01"
url = "https://fs01.example.local:9443"
token = "t"
`
	c, err := decodeConfig([]byte(old))
	if err != nil || c.Panel.URL != "https://ad.example.local:8443" || c.Panel.User != "joe" || c.Lang != "pt-BR" ||
		len(c.Agents) != 1 || c.Agents[0].Name != "fs01" || c.Agents[0].Token != "t" {
		t.Fatalf("decodeConfig(old) = %+v, %v", c, err)
	}
	path := filepath.Join(t.TempDir(), "desktop.toml")
	if err := saveConfig(path, c); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	for _, k := range []string{"painel", "usuario", "agentes", "nome", "idioma"} {
		if strings.Contains(string(b), k) {
			t.Fatalf("saved file has the old key %q:\n%s", k, b)
		}
	}
	if again, err := decodeConfig(b); err != nil || again.Panel.User != "joe" || len(again.Agents) != 1 || again.Lang != "pt-BR" {
		t.Fatalf("decodeConfig(saved) = %+v, %v", again, err)
	}
	if !untouchedExample([]byte(exampleConfig)) || !untouchedExample([]byte("[panel]\nurl = \"https://panel.example.local:8443\"\n")) {
		t.Fatal("untouched examples not recognized")
	}
}

// TestResolveConfigPath checks the lookup order: --config, AD_MANAGER_DESKTOP,
// next to the executable, then the per-user file kept from older versions.
func TestResolveConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AD_MANAGER_DESKTOP", "")
	configFlag = ""
	t.Cleanup(func() { configFlag = "" })

	local, user := localConfigPath(), userConfigPath()
	os.Remove(local)
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(local)) })

	if p, create := resolveConfigPath(); p != local || !create {
		t.Fatalf("first run: got %s (create=%v), want %s next to the executable", p, create, local)
	}

	if err := os.MkdirAll(filepath.Dir(user), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(user, []byte(exampleConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if p, _ := resolveConfigPath(); p != local {
		t.Fatalf("untouched per-user example must not win: got %s", p)
	}
	if err := os.WriteFile(user, []byte("[painel]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if p, create := resolveConfigPath(); p != user || create {
		t.Fatalf("existing per-user file: got %s (create=%v), want %s", p, create, user)
	}

	if err := os.MkdirAll(filepath.Dir(local), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local, []byte("[painel]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if p, _ := resolveConfigPath(); p != local {
		t.Fatalf("file next to the executable wins: got %s", p)
	}

	env := filepath.Join(home, "env.toml")
	t.Setenv("AD_MANAGER_DESKTOP", env)
	if p, create := resolveConfigPath(); p != env || !create {
		t.Fatalf("environment variable: got %s (create=%v)", p, create)
	}

	parseFlags([]string{"-psn_0_1234", "--config", filepath.Join(home, "flag.toml")})
	if p, _ := resolveConfigPath(); p != filepath.Join(home, "flag.toml") {
		t.Fatalf("--config: got %s", p)
	}
}

func TestValidateAndSaveConfig(t *testing.T) {
	ok := &config{Agents: []agentConfig{{Name: "fs01", URL: "https://fs01:9443", Token: "x"}}}
	ok.Panel.URL = "https://painel:8443"
	tests := []struct {
		name string
		mod  func(*config)
		bad  bool
	}{
		{"valid", func(*config) {}, false},
		{"panel over plain http", func(c *config) { c.Panel.URL = "http://painel:8443" }, true},
		{"panel without host", func(c *config) { c.Panel.URL = "https://" }, true},
		{"agent without token", func(c *config) { c.Agents[0].Token = "" }, true},
		{"duplicated agent", func(c *config) { c.Agents = append(c.Agents, c.Agents[0]) }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := *ok
			c.Agents = append([]agentConfig(nil), ok.Agents...)
			tt.mod(&c)
			if got := validateConfig(&c); (got != "") != tt.bad {
				t.Errorf("validateConfig() = %q, bad = %v", got, tt.bad)
			}
		})
	}

	path := filepath.Join(t.TempDir(), ".config", "desktop.toml")
	if err := saveConfig(path, ok); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("saved file mode = %v, %v", fi.Mode().Perm(), err)
	}
	configFlag = path
	t.Cleanup(func() { configFlag = "" })
	got, needsSetup, err := loadConfig()
	if err != nil || needsSetup || got.Panel.URL != ok.Panel.URL || len(got.Agents) != 1 || got.Agents[0].Token != "x" {
		t.Fatalf("round trip: %+v needsSetup=%v err=%v", got, needsSetup, err)
	}
}
