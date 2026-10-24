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

// samba-setup.py --import-ready: refused outside new-dc --panel --files; import_gates writes import_modes once (no
// secret on disk), hands the credential to `ad-manager import-credential` as the service user with the password on
// stdin only (never an argument), removes a former import.pass, and refuses a config whose last table is not [agent].
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
import importlib.util, os, sys, argparse, types
spec = importlib.util.spec_from_file_location("setup", sys.argv[1]); m = importlib.util.module_from_spec(spec); spec.loader.exec_module(m)
d = m.Path(sys.argv[2])
m.CONFIG, m.IMPORT_PASS, m.OPT = d / "config.toml", d / "import.pass", d
m.sh = lambda *a, **k: ""
calls = []
def run(cmd, input=None, **k):
    calls.append((cmd, input))
    return types.SimpleNamespace(returncode=0, stdout="", stderr="")
m.subprocess.run = run
m.CONFIG.write_text("[server]\nlisten = 1\n[agent]\ndc_file_server = true\n")
m.IMPORT_PASS.write_text("old")
a = argparse.Namespace(admin="Administrator", realm="example.local", admin_pw="S3cret pw")
m.import_gates(a); m.import_gates(a)
text = m.CONFIG.read_text()
print("modes", text.count("import_modes = true"), text.rfind("[agent]") < text.find("import_modes"), "_user" in text)
cmd, inp = calls[0]
print("cli", cmd[:3], "import-credential" in cmd, "S3cret pw" in " ".join(cmd), repr(inp))
print("old file", m.IMPORT_PASS.exists())
m.CONFIG.write_text("[agent]\nlisten = 1\n[samba]\nlog_dir = 2\n")
m.import_gates(a)
`
	out, err := exec.Command(py, "-I", "-c", prog, script, dir).CombinedOutput()
	want := "modes 1 True False\ncli ['runuser', '-u', 'ad-manager'] True False 'S3cret pw\\n'\nold file False\n"
	if err == nil || !strings.Contains(string(out), want) || !strings.Contains(string(out), "[agent] is not the last section") {
		t.Fatalf("import_gates: %v\n%s", err, out)
	}
}

// samba-setup.py: a file-serving DC's agent (beside the panel, registered as https://127.0.0.1:9443) listens on the
// loopback only; a separate file server's listens on the network for its panel; an existing [agent] is kept.
func TestSetupScriptAgentListen(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not installed")
	}
	script, _ := filepath.Abs("../../scripts/samba-setup.py")
	dir := t.TempDir()
	const prog = `
import importlib.util, sys, argparse
spec = importlib.util.spec_from_file_location("setup", sys.argv[1]); m = importlib.util.module_from_spec(spec); spec.loader.exec_module(m)
d = m.Path(sys.argv[2])
m.CONFIG, m.OPT = d / "config.toml", d
(d / "agent.token").write_text("hash\n")
m.sh = lambda *a, **k: ""
m.smbconf_global = lambda *a, **k: None
a = argparse.Namespace(share_root=str(d / "shares"), new_token=False)
m.CONFIG.write_text('[server]\nlisten = "0.0.0.0:8443"\n')
m.dc_files(a)
print("dc", 'listen = "127.0.0.1:9443"' in m.CONFIG.read_text(), 'listen = "0.0.0.0:8443"' in m.CONFIG.read_text())
m.CONFIG.write_text('[agent]\nlisten = "0.0.0.0:9443"\n')
m.dc_files(a)
print("kept", m.CONFIG.read_text() == '[agent]\nlisten = "0.0.0.0:9443"\n')
print("fileserver", 'listen = "0.0.0.0:9443"' in m.AGENT_CONF)
`
	out, err := exec.Command(py, "-I", "-c", prog, script, dir).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "dc True True\n") || !strings.Contains(string(out), "kept True\n") ||
		!strings.Contains(string(out), "fileserver True\n") {
		t.Fatalf("agent listen: %v\n%s", err, out)
	}
}

// A DC role starts the panel last: when the DC agent part fails, the panel is never (re)started, so the wizard (still
// served by the setup-mode panel) shows the error instead of a finished setup.
func TestSetupScriptPanelLast(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not installed")
	}
	script, _ := filepath.Abs("../../scripts/samba-setup.py")
	const prog = `
import importlib.util, sys
spec = importlib.util.spec_from_file_location("setup", sys.argv[1]); m = importlib.util.module_from_spec(spec); spec.loader.exec_module(m)
calls = []
def rec(name, fail=False):
    def f(*a, **k):
        calls.append(name + ("(start=False)" if k.get("start") is False else ""))
        if fail: m.fail("registering the agent in the panel: boom")
    return f
for n in ("check_dc_root", "dc_new", "panel", "import_gates", "panel_start"): setattr(m, n, rec(n))
m.dc_files = rec("dc_files", fail=sys.argv[2] == "fail")
sys.argv = ["samba-setup.py", "new-dc", "--realm", "e.local", "--domain", "E", "--ip", "10.0.0.2", "--panel", "--files", "--import-ready", "--dry-run"]
try:
    m.main()
finally:
    print("CALLS", calls)
`
	for _, tt := range []struct{ mode, want string }{
		{"ok", "CALLS ['check_dc_root', 'dc_new', 'panel(start=False)', 'dc_files', 'import_gates', 'panel_start']"},
		{"fail", "CALLS ['check_dc_root', 'dc_new', 'panel(start=False)', 'dc_files']"},
	} {
		out, _ := exec.Command(py, "-I", "-c", prog, script, tt.mode).CombinedOutput()
		if !strings.Contains(string(out), tt.want) {
			t.Fatalf("%s: want %s\n%s", tt.mode, tt.want, out)
		}
	}
}
