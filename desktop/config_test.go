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
	if err != nil || c.Panel.URL != "https://ad.example.local:8443" || c.Panel.User != "joe" || c.Lang != "pt-BR" || !c.hadAgents {
		t.Fatalf("decodeConfig(old) = %+v, %v", c, err)
	}
	path := filepath.Join(t.TempDir(), "desktop.toml")
	if err := saveConfig(path, c); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	for _, k := range []string{"painel", "usuario", "agentes", "nome", "idioma", "agents", "token"} {
		if strings.Contains(string(b), k) {
			t.Fatalf("saved file has the old key %q:\n%s", k, b)
		}
	}
	if again, err := decodeConfig(b); err != nil || again.Panel.User != "joe" || again.hadAgents || again.Lang != "pt-BR" {
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

func TestCheckURLAndSaveConfig(t *testing.T) {
	for _, tt := range []struct {
		url string
		bad bool
	}{{"https://painel:8443", false}, {"http://painel:8443", true}, {"https://", true}} {
		if got := checkURL("Panel URL", tt.url); (got != "") != tt.bad {
			t.Errorf("checkURL(%q) = %q, bad = %v", tt.url, got, tt.bad)
		}
	}

	ok := &config{}
	ok.Panel.URL, ok.Panel.User, ok.Panel.Fingerprint = "https://painel:8443", "joe", "sha256:ab"
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
	if err != nil || needsSetup || got.Panel.URL != ok.Panel.URL || got.Panel.Fingerprint != "sha256:ab" || got.Panel.Insecure {
		t.Fatalf("round trip: %+v needsSetup=%v err=%v", got, needsSetup, err)
	}
}

// TestLegacyAgentTokensRemoved: a config of an older version that still lists file servers with their tokens is read
// without them and rewritten at once (0600) without them, keeping the rest.
func TestLegacyAgentTokensRemoved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop.toml")
	legacy := "lang = \"en\"\n[panel]\nurl = \"https://panel.example.test:8443\"\ninsecure = true\nuser = \"joe\"\n\n" +
		"[[agents]]\nname = \"fs01\"\nurl = \"https://fs01:9443\"\ntoken = \"secret-token\"\ninsecure = true\n"
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	configFlag = path
	t.Cleanup(func() { configFlag, configNotice, configNoticeErr = "", "", "" })
	c, needsSetup, err := loadConfig()
	if err != nil || needsSetup || c.Panel.URL != "https://panel.example.test:8443" || c.Panel.User != "joe" || !c.Panel.Insecure || c.Lang != "en" {
		t.Fatalf("loadConfig = %+v needsSetup=%v err=%v", c, needsSetup, err)
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "secret-token") || strings.Contains(string(b), "agents") {
		t.Fatalf("the tokens stayed on disk:\n%s", b)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 || configNotice == "" || configNoticeErr != "" {
		t.Fatalf("mode %v, notice %q %q", fi.Mode().Perm(), configNotice, configNoticeErr)
	}
}

// TestLegacyAgentTokensRemovedThroughLink: --config pointing to a link rewrites the file it points to (the tokens
// leave the real file) and leaves the link in place.
func TestLegacyAgentTokensRemovedThroughLink(t *testing.T) {
	dir := t.TempDir()
	real, link := filepath.Join(dir, "real.toml"), filepath.Join(dir, "link.toml")
	legacy := "[panel]\nurl = \"https://panel.example.test:8443\"\nuser = \"joe\"\n[[agents]]\nname = \"fs01\"\nurl = \"https://fs01:9443\"\ntoken = \"secret-token\"\n"
	if err := os.WriteFile(real, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skip(err)
	}
	configFlag = link
	t.Cleanup(func() { configFlag, configNotice, configNoticeErr = "", "", "" })
	if _, _, err := loadConfig(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(real)
	fi, err := os.Lstat(link)
	if strings.Contains(string(b), "secret-token") || err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("real file:\n%s\nlink %v %v", b, fi.Mode(), err)
	}
}
