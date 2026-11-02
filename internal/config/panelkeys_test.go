package config

import (
	"strings"
	"testing"
)

const commented = `# the panel
[server]
listen = "0.0.0.0:8443" # all addresses

[ad]
# the DC
url = "ldaps://dc1.example.local"   # first DC
domain = "example.local"

[agent]
share_roots = ["/srv/shares"]
`

func TestSetKeysKeepsTheRest(t *testing.T) {
	keys := map[string]any{"ad.url": " ldaps://dc2.example.local ", "ad.admin_group": "Panel Admins", "ad.start_tls": false}
	c := Config{}
	c.AD.Domain = "example.local"
	if err := c.SetPanelKeys(keys); err != nil {
		t.Fatal(err)
	}
	out, err := SetKeys(commented, keys)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckEdit([]byte(commented), []byte(out), keys); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"# the DC\n", `listen = "0.0.0.0:8443" # all addresses`, "url = 'ldaps://dc2.example.local'\n",
		"domain = \"example.local\"\nadmin_group = 'Panel Admins'\nstart_tls = false\n", `share_roots = ["/srv/shares"]`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestSetKeysAddsTheTable(t *testing.T) {
	keys := map[string]any{"ad.url": "ldap://dc1.lab.test", "ad.domain": "lab.test"}
	out, err := SetKeys("setup = false\n", keys)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckEdit([]byte("setup = false\n"), []byte(out), keys); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestSetPanelKeysRefuses(t *testing.T) {
	base := Config{}
	base.AD.URL, base.AD.Domain = "ldaps://dc1.example.local", "example.local"
	for name, keys := range map[string]map[string]any{
		"agent key":   {"agent.sync_unconfined": true},
		"agent path":  {"agent.share_roots": "/"},
		"redis":       {"redis.addr": "evil:6379"},
		"bad url":     {"ad.url": "https://dc1"},
		"bad type":    {"ad.start_tls": "yes"},
		"newline":     {"ad.admin_group": "a\nb"},
		"relative CA": {"ad.ca_file": "ca.pem"},
		"empty":       {},
	} {
		c := base
		if err := c.SetPanelKeys(keys); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestCheckEditCatchesOtherChanges(t *testing.T) {
	keys := map[string]any{"ad.url": "ldaps://dc2.example.local"}
	edited := strings.Replace(commented, "dc1", "dc2", 1)
	edited = strings.Replace(edited, "/srv/shares", "/", 1)
	if CheckEdit([]byte(commented), []byte(edited), keys) == nil {
		t.Fatal("a change outside the keys passed")
	}
}

func TestEditKeepsMultilineValues(t *testing.T) {
	old := "[ad]\nurl = \"ldaps://dc1.example.local\"\nnote = \"\"\"\nurl = kept\n\"\"\"\n"
	b, err := Edit([]byte(old), map[string]any{"ad.url": "ldaps://dc2.example.local"})
	if err == nil && strings.Contains(string(b), "url = kept") {
		return // edited around the multiline value
	}
	if err == nil {
		t.Fatalf("the multiline value changed:\n%s", b)
	}
}

func TestEditUsesDefaults(t *testing.T) {
	// no ad.domain: Load's default applies, as when the panel runs
	if _, err := Edit([]byte("[ad]\nurl = \"ldaps://dc1.example.local\"\n"), map[string]any{"ad.url": "ldaps://dc2.example.local"}); err != nil {
		t.Fatal(err)
	}
}

func TestCAFileMustBeASmallFile(t *testing.T) {
	c := defaults()
	for _, p := range []string{"/dev/zero", "/", "/nonexistent.pem"} {
		if err := c.SetPanelKeys(map[string]any{"ad.ca_file": p}); err == nil {
			t.Errorf("%s accepted", p)
		}
	}
}
