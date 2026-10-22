package agent

import (
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The DC given to dc-join and member becomes the host's nameserver: only an address is accepted.
func TestSetupPlanArgsNameserverIsIP(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		plan SetupPlan
		ok   bool
	}{
		{"join by IP", SetupPlan{Role: "dc-join", Realm: "e.local", Domain: "E", IP: "10.0.0.2", SrcDC: "10.0.0.1"}, true},
		{"join by name", SetupPlan{Role: "dc-join", Realm: "e.local", Domain: "E", IP: "10.0.0.2", SrcDC: "dc01.e.local"}, false},
		{"member by IP", SetupPlan{Role: "member", Realm: "e.local", Domain: "E", DNSDC: "10.0.0.1"}, true},
		{"member by name", SetupPlan{Role: "member", Realm: "e.local", Domain: "E", DNSDC: "dc01.e.local"}, false},
		{"DC with files", SetupPlan{Role: "new-dc", Realm: "e.local", Domain: "E", IP: "10.0.0.2", Files: true}, true},
		{"DC with files, bad root", SetupPlan{Role: "new-dc", Realm: "e.local", Domain: "E", IP: "10.0.0.2", Files: true, ShareRoot: "/srv/../etc"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := tt.plan.Args(); (err == nil) != tt.ok {
				t.Fatalf("Args() err = %v, want ok %v", err, tt.ok)
			}
		})
	}
}

// restore-dc takes the backup file (a .tar.bz2 path, no ..) and needs no password: it comes with the backup.
func TestSetupPlanRestoreDC(t *testing.T) {
	for _, tt := range []struct {
		name   string
		backup string
		ok     bool
	}{
		{"backup file", "/root/dc-20261018.tar.bz2", true},
		{"relative", "dc.tar.bz2", false},
		{"dot dot", "/root/../etc/x.tar.bz2", false},
		{"other format", "/root/dc.tar.gz", false},
		{"option", "/root/--x.tar.bz2;rm", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := SetupPlan{Role: "restore-dc", Realm: "e.local", IP: "10.0.0.2", Backup: tt.backup}
			args, err := p.Args()
			if (err == nil) != tt.ok {
				t.Fatalf("Args() = %q, %v", args, err)
			}
			if tt.ok && (!slices.Contains(args, "--backup") || slices.Contains(args, "--domain") || p.needsPassword()) {
				t.Fatalf("args %q, password %v", args, p.needsPassword())
			}
		})
	}
}

// The old Portuguese role names are still accepted and become the current ones (with the current flags).
func TestSetupPlanOldRoleNames(t *testing.T) {
	t.Parallel()
	for old, want := range map[string]string{"dc-novo": "new-dc", "dc-restaurar": "restore-dc", "membro": "member", "conectar": "connect"} {
		p := SetupPlan{Role: old, Realm: "e.local", Domain: "E", IP: "10.0.0.2", DNSDC: "10.0.0.1", URL: "ldaps://dc.e.local",
			Backup: "/root/dc.tar.bz2", Files: old != "membro"}
		args, err := p.Args()
		if err != nil || args[0] != want {
			t.Fatalf("%s: Args() = %q, %v", old, args, err)
		}
		for _, a := range args {
			if a == "--painel" || a == "--agente" || a == "--arquivos" {
				t.Fatalf("%s: old flag in %q", old, args)
			}
		}
		if p.needsPassword() != (want == "new-dc" || want == "member") {
			t.Fatalf("%s: needsPassword = %v", old, p.needsPassword())
		}
	}
}

// import_ad (the import write modes) goes only with a new domain whose DC also serves files.
func TestSetupPlanImportAD(t *testing.T) {
	t.Parallel()
	dc := func(role string, files bool) SetupPlan {
		return SetupPlan{Role: role, Realm: "e.local", Domain: "E", IP: "10.0.0.2", SrcDC: "10.0.0.1", Files: files, ImportAD: true}
	}
	for _, tt := range []struct {
		name string
		plan SetupPlan
		ok   bool
	}{
		{"new domain with files", dc("new-dc", true), true},
		{"new domain without files", dc("new-dc", false), false},
		{"additional DC", dc("dc-join", true), false},
		{"member", SetupPlan{Role: "member", Realm: "e.local", Domain: "E", DNSDC: "10.0.0.1", ImportAD: true}, false},
	} {
		args, err := tt.plan.Args()
		if (err == nil) != tt.ok || (tt.ok && !slices.Contains(args, "--import-ready")) {
			t.Fatalf("%s: Args() = %q, %v", tt.name, args, err)
		}
		if !tt.ok && err.Error() != "setup: invalid import_ad" {
			t.Fatalf("%s: %v", tt.name, err)
		}
	}
	if args, _ := dc("new-dc", true).withoutImport().Args(); slices.Contains(args, "--import-ready") {
		t.Fatalf("off by default: %q", args)
	}
}

func (p SetupPlan) withoutImport() SetupPlan { p.ImportAD = false; return p }

// samba-setup.py --import-ready: refused outside new-dc --panel --files; import_gates writes one 0600 file (even over
// a looser one, whatever the umask) and the [agent] keys once, and refuses a config whose last table is not [agent].
func TestSetupScriptImportReady(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not installed")
	}
	script, _ := filepath.Abs("../../scripts/samba-setup.py")
	for _, args := range [][]string{
		{"dc-join", "--realm", "e.local", "--panel", "--files", "--import-ready", "--dry-run"},
		{"new-dc", "--realm", "e.local", "--panel", "--import-ready", "--dry-run"},
	} {
		out, err := exec.Command(py, append([]string{"-I", script}, args...)...).CombinedOutput()
		if err == nil || !strings.Contains(string(out), "--import-ready goes with new-dc --panel --files") {
			t.Fatalf("%q: %v %s", args, err, out)
		}
	}
	dir := t.TempDir()
	const prog = `
import importlib.util, os, sys, argparse
spec = importlib.util.spec_from_file_location("setup", sys.argv[1]); m = importlib.util.module_from_spec(spec); spec.loader.exec_module(m)
m.CONFIG, m.IMPORT_PASS = m.Path(sys.argv[2]) / "config.toml", m.Path(sys.argv[2]) / "import.pass"
m.sh = lambda *a, **k: ""
os.umask(0)
m.CONFIG.write_text("[server]\nlisten = 1\n[agent]\ndc_file_server = true\n")
m.IMPORT_PASS.write_text("old"); os.chmod(m.IMPORT_PASS, 0o644)
a = argparse.Namespace(admin="Administrator", realm="example.local", admin_pw="S3cret pw")
m.import_gates(a); m.import_gates(a)
text = m.CONFIG.read_text()
print(oct(os.stat(m.IMPORT_PASS).st_mode & 0o777), repr(m.IMPORT_PASS.read_text()), text.count("_user ="), text.rfind("[agent]") < text.find("gpo_restore_user"))
print('password_migration_user = "Administrator@example.local"' in text, text.count(str(m.IMPORT_PASS)))
m.CONFIG.write_text("[agent]\nlisten = 1\n[samba]\nlog_dir = 2\n")
m.import_gates(a)
`
	out, err := exec.Command(py, "-I", "-c", prog, script, dir).CombinedOutput()
	if err == nil || !strings.Contains(string(out), "0o600 'S3cret pw\\n' 2 True\nTrue 2\n") ||
		!strings.Contains(string(out), "[agent] is not the last section") {
		t.Fatalf("import_gates: %v\n%s", err, out)
	}
}
