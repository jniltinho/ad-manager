//go:build linux

package shares

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"ad-manager/internal/sambabridge"
)

// fakeACL records the Windows ACL the manager asks for.
type fakeACL struct {
	got       sambabridge.FolderACL
	sddl      string
	propagate bool
	rels      []string // the paths ReinheritPaths was asked to reapply
}

func (f *fakeACL) SetFolderACL(_ context.Context, dir *os.File, req sambabridge.FolderACL) (*sambabridge.ACL, error) {
	if dir == nil || dir.Name() != req.Path {
		return nil, errors.New("folder descriptor missing")
	}
	f.got = req
	return &sambabridge.ACL{SDDL: "O:DAG:DAD:P(A;OICI;FA;;;BA)"}, nil
}

func (f *fakeACL) GetACL(context.Context, *os.File, string, []string, string) (*sambabridge.ACL, error) {
	return &sambabridge.ACL{SDDL: "O:DAG:DAD:P(A;OICI;FA;;;SY)"}, nil
}

func (f *fakeACL) SetSDDL(_ context.Context, dir *os.File, path string, _ []string, sddl string, propagate bool) (*sambabridge.ACL, error) {
	if dir == nil || dir.Name() != path {
		return nil, errors.New("folder descriptor missing")
	}
	f.sddl, f.propagate = sddl, propagate
	return &sambabridge.ACL{SDDL: sddl, Propagated: 3}, nil
}

func (f *fakeACL) Reapply(ctx context.Context, dir *os.File, path string, roots []string, sddl string) (*sambabridge.ACL, error) {
	return f.SetSDDL(ctx, dir, path, roots, sddl, true)
}

func (f *fakeACL) ReinheritPaths(_ context.Context, dir *os.File, path string, _ []string, rels []string) (*sambabridge.ACL, error) {
	if dir == nil || dir.Name() != path {
		return nil, errors.New("folder descriptor missing")
	}
	f.rels = rels
	return &sambabridge.ACL{Propagated: len(rels)}, nil
}

// winbind resolves EMPRESA names; local names only exist in getent.
func fakeWinbind(_ context.Context, name string, args ...string) (string, error) {
	switch {
	case name == "wbinfo" && args[0] == "--own-domain":
		return "EMPRESA\n", nil
	case name == "wbinfo" && args[0] == "--name-to-sid" && strings.HasPrefix(args[1], `EMPRESA\`):
		return "S-1-5-21-1-2-3-" + map[string]string{`EMPRESA\Domain Admins`: "512", `EMPRESA\Financeiro`: "1103",
			`EMPRESA\Diretoria`: "1104", `EMPRESA\ana`: "1105"}[args[1]] + " SID_DOM_GROUP (2)\n", nil
	case name == "getent" && args[0] == "group" && args[2] == "local":
		return "local:x:3001:\n", nil
	case name == "getent" && args[0] == "passwd" && args[2] == "joao":
		return "joao:x:3002:3002::/home/joao:/bin/bash\n", nil
	}
	return "", errors.New("not found")
}

func TestCreateFolderNTACL(t *testing.T) {
	root := t.TempDir()
	acl := &fakeACL{}
	m := &Manager{Roots: []string{root}, Run: fakeWinbind, ACL: acl, NTACL: true, History: filepath.Join(t.TempDir(), "acl.jsonl")}
	path := filepath.Join(root, "Financeiro")
	err := m.CreateFolder(context.Background(), FolderReq{Path: path, Owner: `EMPRESA\Financeiro`,
		Full: []string{`EMPRESA\Diretoria`}, RW: []string{`EMPRESA\Financeiro`}, ROUsers: []string{`EMPRESA\ana`}})
	if err != nil {
		t.Fatal(err)
	}
	g := acl.got
	if g.Path != path || g.Owner != "S-1-5-21-1-2-3-512" || g.Group != "S-1-5-21-1-2-3-1103" {
		t.Fatalf("owner/group: %+v", g)
	}
	want := []sambabridge.Entry{{SID: "S-1-5-21-1-2-3-1104", Access: "full"}, {SID: "S-1-5-21-1-2-3-1103", Access: "rw"},
		{SID: "S-1-5-21-1-2-3-1105", Access: "ro"}}
	if len(g.Entries) != len(want) {
		t.Fatalf("entries: %+v", g.Entries)
	}
	for i := range want {
		if g.Entries[i] != want[i] {
			t.Fatalf("entry %d: got %+v, want %+v", i, g.Entries[i], want[i])
		}
	}
	data, err := os.ReadFile(m.History)
	if err != nil {
		t.Fatal(err)
	}
	var line map[string]string
	if err := json.Unmarshal(data, &line); err != nil || line["path"] != path || line["after"] == "" || line["before"] != "" {
		t.Fatalf("history: %s (%v)", data, err)
	}
	if fi, _ := os.Stat(m.History); fi.Mode().Perm() != 0o600 {
		t.Fatalf("history mode %v", fi.Mode().Perm())
	}
}

func TestSID(t *testing.T) {
	m := &Manager{Run: fakeWinbind}
	for _, tt := range []struct {
		name, in string
		user     bool
		want     string
		invalid  bool
	}{
		{name: "domain group via winbind", in: `EMPRESA\Financeiro`, want: "S-1-5-21-1-2-3-1103"},
		{name: "local group becomes a Unix SID", in: "local", want: "S-1-22-2-3001"},
		{name: "local user becomes a Unix SID", in: "joao", user: true, want: "S-1-22-1-3002"},
		{name: "unresolved domain name never falls back to getent", in: `OUTRO\Financeiro`, invalid: true},
		{name: "unknown local name", in: "ninguem", invalid: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := m.SID(context.Background(), tt.in, tt.user)
			if tt.invalid {
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("got %q, %v; want ErrInvalid", got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("got %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestDomainAdminsStandalone(t *testing.T) {
	m := &Manager{Run: func(context.Context, string, ...string) (string, error) { return "", errors.New("no winbind") }}
	if got, _ := m.domainAdmins(context.Background()); got != "S-1-22-1-0" {
		t.Fatalf("got %q, want root's Unix SID", got)
	}
}

// A renamed Domain Admins on a DC: the domain SID from wbinfo -D with RID 512.
func TestDomainAdminsRenamedOnDC(t *testing.T) {
	m := &Manager{DC: true, Run: func(_ context.Context, name string, args ...string) (string, error) {
		switch {
		case name == "wbinfo" && args[0] == "--own-domain":
			return "EMPRESA\n", nil
		case name == "wbinfo" && args[0] == "-D":
			return "Name              : EMPRESA\nSID               : S-1-5-21-1-2-3\n", nil
		}
		return "", errors.New("not found")
	}}
	if got, err := m.domainAdmins(context.Background()); got != "S-1-5-21-1-2-3-512" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestRestoreACL(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Financeiro")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	hist := filepath.Join(t.TempDir(), "acl.jsonl")
	lines := `{"time":"2026-10-01T10:00:00Z","path":"` + path + `","before":"O:DAG:DAD:P(A;OICI;FA;;;BA)","after":""}
{"time":"2026-10-01T10:00:00Z","path":"` + path + `","before":"","after":"O:DAG:DAD:P(A;OICI;0x1301bf;;;DU)"}
{"time":"2026-10-02T10:00:00Z","path":"` + root + `/Outra","before":"O:DAG:DAD:(A;;FA;;;BA)","after":""}
not json
`
	if err := os.WriteFile(hist, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	acl := &fakeACL{}
	m := &Manager{Roots: []string{root}, ACL: acl, NTACL: true, History: hist}

	list, err := m.ACLHistory(path)
	if err != nil || len(list) != 2 || list[0].After == "" || list[1].Before == "" {
		t.Fatalf("history: %+v, %v", list, err)
	}
	for _, tt := range []struct {
		name string
		req  RestoreReq
	}{
		{name: "time not recorded", req: RestoreReq{Path: path, Time: "2026-10-03T10:00:00Z"}},
		{name: "another folder's time", req: RestoreReq{Path: path, Time: "2026-10-02T10:00:00Z"}},
		{name: "outside the roots", req: RestoreReq{Path: "/etc", Time: "2026-10-01T10:00:00Z"}},
		{name: "through a link", req: RestoreReq{Path: filepath.Join(root, "link"), Time: "2026-10-01T10:00:00Z"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := m.RestoreACL(context.Background(), tt.req); !errors.Is(err, ErrInvalid) {
				t.Fatalf("got %v, want ErrInvalid", err)
			}
		})
	}
	got, err := m.RestoreACL(context.Background(), RestoreReq{Path: path, Time: "2026-10-01T10:00:00Z"})
	if err != nil || acl.sddl != "O:DAG:DAD:P(A;OICI;FA;;;BA)" || !acl.propagate || got.Propagated != 3 {
		t.Fatalf("restore: %+v %v (sddl %q, propagate %v)", got, err, acl.sddl, acl.propagate)
	}
	// the ACL replaced by the restore is in the history (it can be undone), then the restored one
	list, _ = m.ACLHistory(path)
	if len(list) != 4 || list[1].Before != "O:DAG:DAD:P(A;OICI;FA;;;SY)" || list[0].After != "O:DAG:DAD:P(A;OICI;FA;;;BA)" {
		t.Fatalf("history after restore: %+v", list)
	}
}

func TestReinheritPaths(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Financeiro")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	acl := &fakeACL{}
	m := &Manager{Roots: []string{root}, ACL: acl, NTACL: true}

	// no transferred paths: no bridge call, no work
	if got, err := m.ReinheritPaths(context.Background(), path, nil); err != nil || got.Propagated != 0 || acl.rels != nil {
		t.Fatalf("empty: %+v %v (rels %v)", got, err, acl.rels)
	}
	// the transferred paths are passed through to the bridge
	rels := []string{"a.txt", "sub/b.txt"}
	got, err := m.ReinheritPaths(context.Background(), path, rels)
	if err != nil || got.Propagated != len(rels) || len(acl.rels) != 2 || acl.rels[1] != "sub/b.txt" {
		t.Fatalf("passthrough: %+v %v (rels %v)", got, err, acl.rels)
	}
	// a destination reached through a link, or outside the roots, is refused before any bridge call
	for _, p := range []string{filepath.Join(root, "link"), "/etc"} {
		if _, err := m.ReinheritPaths(context.Background(), p, rels); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: got %v, want ErrInvalid", p, err)
		}
	}
}

func TestEditPropagates(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Financeiro")
	acl := &fakeACL{}
	m := &Manager{Roots: []string{root}, Run: fakeWinbind, ACL: acl, NTACL: true}
	req := FolderReq{Path: path, RW: []string{`EMPRESA\Financeiro`}}
	if err := m.CreateFolder(context.Background(), req); err != nil || acl.got.Propagate {
		t.Fatalf("new folder: propagate %v, %v", acl.got.Propagate, err)
	}
	if err := syscall.Setxattr(path, "security.NTACL", []byte("x"), 0); err != nil {
		t.Skipf("no xattr support for security.* here: %v", err)
	}
	if err := m.CreateFolder(context.Background(), req); err != nil || !acl.got.Propagate {
		t.Fatalf("existing folder: propagate %v, %v", acl.got.Propagate, err)
	}
}

func TestACLHistoryWindow(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Financeiro")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i := range 120 {
		fmt.Fprintf(&b, `{"time":"t%03d","path":%q,"before":"O:BAG:BAD:(A;;FA;;;BA)"}`+"\n", i, path)
		fmt.Fprintf(&b, `{"time":"x%03d","path":%q,"before":"other"}`+"\n", i, filepath.Join(root, "Outra"))
	}
	hist := filepath.Join(t.TempDir(), "acl.jsonl")
	if err := os.WriteFile(hist, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	acl := &fakeACL{}
	m := &Manager{Roots: []string{root}, ACL: acl, NTACL: true, History: hist}
	list, err := m.ACLHistory(path)
	if err != nil || len(list) != maxHistory || list[0].Time != "t119" || list[maxHistory-1].Time != "t070" {
		t.Fatalf("window: %d entries, first %v, last %v, %v", len(list), list[0].Time, list[len(list)-1].Time, err)
	}
	// a change older than the window can still be restored
	if _, err := m.RestoreACL(context.Background(), RestoreReq{Path: path, Time: "t003"}); err != nil || acl.sddl != "O:BAG:BAD:(A;;FA;;;BA)" {
		t.Fatalf("restore of an old change: %v (%q)", err, acl.sddl)
	}
}

// On a member, a Domain Admins that does not resolve (winbind down) is an error, never a share without them; on a
// standalone server there is none to add.
func TestAdminsName(t *testing.T) {
	run := func(role string) func(context.Context, string, ...string) (string, error) {
		return func(_ context.Context, name string, args ...string) (string, error) {
			if name == "testparm" {
				return role + "\n", nil
			}
			return "", errors.New("winbind down")
		}
	}
	if n, err := (&Manager{Run: run("standalone server")}).adminsName(context.Background()); n != "" || err != nil {
		t.Fatalf("standalone: %q %v", n, err)
	}
	if _, err := (&Manager{Run: run("member server")}).adminsName(context.Background()); err == nil {
		t.Fatal("a member without winbind must refuse")
	}
}

// TestFolderACLs: without the helper's batch each folder is read on its own; a folder outside the roots or missing
// has no entry.
func TestFolderACLs(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	if err := os.Mkdir(a, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &Manager{Roots: []string{root}, ACL: &fakeACL{}, NTACL: true}
	got := m.FolderACLs(context.Background(), []string{a, filepath.Join(root, "missing"), "/etc"})
	if len(got) != 1 || got[a] == nil {
		t.Fatalf("got %v", got)
	}
}
