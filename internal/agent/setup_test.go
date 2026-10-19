package agent

import (
	"slices"
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
