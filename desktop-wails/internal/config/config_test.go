package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func write(t *testing.T, body string, mode os.FileMode) *File {
	t.Helper()
	p := filepath.Join(t.TempDir(), "desktop.toml")
	if err := os.WriteFile(p, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	return &File{Path: p}
}

func TestSharedWithTheRustApp(t *testing.T) {
	f := write(t, `lang = "pt-BR"
[panel]
url = "https://panel.domain.local:8443"
user = "carol"
fingerprint = "sha256:ab"
[appearance]
theme = "matrix"
flat = "depth"
[graphics]
gpu_backend = "dx12"
`, 0o600)
	c, unconfigured, err := Load(f)
	if err != nil || unconfigured {
		t.Fatalf("load: %v %v", unconfigured, err)
	}
	if c.URL != "https://panel.domain.local:8443" || c.User != "carol" || c.Fingerprint != "sha256:ab" || c.Lang != "pt-BR" ||
		c.Theme != "matrix" || c.Flat != "depth" {
		t.Fatalf("fields: %+v", c)
	}
	c.SafeGPU = true
	c.Theme = ""
	if err := Save(f, c); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(f.Path)
	for _, want := range []string{"gpu_backend = 'dx12'", "safe = true", "flat = 'depth'", "fingerprint = 'sha256:ab'"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("saved file lacks %q:\n%s", want, b)
		}
	}
	if strings.Contains(string(b), "theme") {
		t.Errorf("an empty theme was written:\n%s", b)
	}
	if st, _ := os.Stat(f.Path); runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
}

func TestLegacyKeysAndAgentTokens(t *testing.T) {
	f := write(t, `idioma = "pt-BR"
[painel]
url = "https://painel.local:8443"
usuario = "joao"
insecure = true
[[agentes]]
nome = "fs1"
token = "secret-token"
`, 0o600)
	c, _, err := Load(f)
	if err != nil || c.URL != "https://painel.local:8443" || c.User != "joao" || !c.Insecure || c.Lang != "pt-BR" {
		t.Fatalf("legacy: %+v %v", c, err)
	}
	if !strings.Contains(f.Notice, "no longer used") {
		t.Fatalf("notice: %q", f.Notice)
	}
	b, _ := os.ReadFile(f.Path)
	if strings.Contains(string(b), "secret-token") || strings.Contains(string(b), "[painel]") || !strings.Contains(string(b), "insecure = true") {
		t.Fatalf("rewritten file:\n%s", b)
	}
	// a pin replaces the legacy insecure
	c.Fingerprint = "sha256:cd"
	_ = Save(f, c)
	if b, _ = os.ReadFile(f.Path); strings.Contains(string(b), "insecure") {
		t.Fatalf("insecure kept beside a pin:\n%s", b)
	}
}

func TestLoosePermissionsRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("on Windows the mode does not reflect the ACL")
	}
	f := write(t, "[panel]\nurl = \"https://p:8443\"\n", 0o644)
	_, _, err := Load(f)
	if !errors.Is(err, ErrLoose) || !strings.Contains(err.Error(), "must be private and has mode 0644: run chmod 600") {
		t.Fatalf("loose: %v", err)
	}
}

func TestFirstRunAndPreferencesOnly(t *testing.T) {
	c, unconfigured, err := Load(&File{Path: filepath.Join(t.TempDir(), "none.toml"), Create: true})
	if err != nil || !unconfigured || c.User != "administrator" {
		t.Fatalf("first run: %+v %v %v", c, unconfigured, err)
	}
	f := write(t, "lang = \"pt-BR\"\n[appearance]\ntheme = \"matrix\"\n", 0o600)
	c, unconfigured, err = Load(f)
	if err != nil || !unconfigured || c.Theme != "matrix" {
		t.Fatalf("preferences only: %+v %v %v", c, unconfigured, err)
	}
}

func TestResolveOrder(t *testing.T) {
	dir := t.TempDir()
	flag := filepath.Join(dir, "flag.toml")
	if f := Resolve(flag); f.Path != flag || !f.Create {
		t.Fatalf("flag: %+v", f)
	}
	env := filepath.Join(dir, "env.toml")
	t.Setenv("AD_MANAGER_DESKTOP", env)
	if f := Resolve(""); f.Path != env {
		t.Fatalf("env: %+v", f)
	}
}
