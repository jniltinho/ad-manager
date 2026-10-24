package shares

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"ad-manager/internal/sambabridge"
)

func TestValidation(t *testing.T) {
	root := t.TempDir()
	m := &Manager{Roots: []string{root}}
	if err := os.Symlink("/etc", filepath.Join(root, "shortcut")); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		err  error
	}{
		{"Financeiro", nil}, {"Comercial Matriz", nil}, {"Notas.Fiscais", nil}, {"Notas Fiscais$", nil}, {"a$b", ErrInvalid},
		{"sysvol", ErrInvalid}, {"IPC$", ErrInvalid}, {"global", ErrInvalid}, {"a/b", ErrInvalid}, {"", ErrInvalid}, {"[x]", ErrInvalid},
	}
	for _, tt := range tests {
		t.Run("name "+tt.name, func(t *testing.T) {
			if err := ValidName(tt.name); !errors.Is(err, tt.err) {
				t.Fatalf("ValidName(%q) = %v", tt.name, err)
			}
		})
	}
	paths := []struct {
		name, p string
		ok      bool
	}{
		{"inside the root", root + "/financeiro", true},
		{"the root itself", root, false},
		{"outside", "/etc", false},
		{"with ..", root + "/../etc", false},
		{"relative", "financeiro", false},
		{"through a symbolic link", root + "/shortcut/x", false},
	}
	for _, tt := range paths {
		t.Run("path "+tt.name, func(t *testing.T) {
			_, err := m.SafePath(tt.p)
			if (err == nil) != tt.ok {
				t.Fatalf("SafePath(%q) = %v", tt.p, err)
			}
		})
	}
	for _, g := range []string{`EMPRESA\Financeiro`, "Domain Users", `EMPRESA\Comercial Matriz`} {
		if err := ValidGroup(g); err != nil {
			t.Errorf("ValidGroup(%q) = %v", g, err)
		}
	}
	for _, g := range []string{`x"y`, "a,b", "g\n", `A\B\C`, "@g", "a:b", "--configfile=/x"} {
		if ValidGroup(g) == nil {
			t.Errorf("ValidGroup(%q) should reject", g)
		}
	}
}

func TestCreateShareCommands(t *testing.T) {
	root := t.TempDir()
	var calls []string
	m := &Manager{Roots: []string{root}, Run: func(_ context.Context, name string, args ...string) (string, error) {
		if name == "testparm" { // a standalone server: no Domain Admins added
			return "standalone server\n", nil
		}
		calls = append(calls, name+" "+strings.Join(args, "|"))
		if name == "getent" {
			return `EMPRESA\financeiro:x:11103:` + "\n", nil
		}
		return "", nil
	}, Chown: func(string, int, int) error { return nil }}
	err := m.CreateShare(context.Background(), ShareReq{Name: "Financeiro", Path: root + "/fin", Comment: "Equipe",
		RW: []string{`EMPRESA\Financeiro`}, RO: []string{`EMPRESA\Diretoria`}, CreatePath: true})
	if err != nil {
		t.Fatal(err)
	}
	{
		want := []string{
			`getent group|--|EMPRESA\Financeiro`,
			`setfacl -m|g:EMPRESA\Financeiro:rwx,d:g:EMPRESA\Financeiro:rwx,g:EMPRESA\Diretoria:rx,d:g:EMPRESA\Diretoria:rx|--|/proc/`,
			"net conf|import|",
		}
		for i := range want {
			if i >= len(calls) || !strings.HasPrefix(calls[i], want[i]) {
				t.Fatalf("command %d = %q, want prefix %q (all: %q)", i, calls, want[i], calls)
			}
		}
		calls = nil
		_ = slices.Equal[[]string]
	}
}

func TestSplitGroups(t *testing.T) {
	for in, want := range map[string][]string{
		`@"EMPRESA\Comercial Matriz" @"EMPRESA\Fin"`: {`EMPRESA\Comercial Matriz`, `EMPRESA\Fin`},
		`@EMPRESA\Financeiro @EMPRESA\Diretoria`:     {`EMPRESA\Financeiro`, `EMPRESA\Diretoria`},
		`+financeiro, @"DOM\x y"`:                    {"financeiro", `DOM\x y`},
	} {
		if got, _ := splitGroups(in); !slices.Equal(got, want) {
			t.Errorf("splitGroups(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUsersInList(t *testing.T) {
	g, u := splitGroups(`@"EMPRESA\Design" "EMPRESA\ana.lima" ana`)
	if !slices.Equal(g, []string{`EMPRESA\Design`}) || !slices.Equal(u, []string{`EMPRESA\ana.lima`, "ana"}) {
		t.Fatalf("groups %q users %q", g, u)
	}
	if got := smbList([]string{`D\g`}, []string{`D\u`}); got != `@"D\g" "D\u"` {
		t.Fatalf("smbList = %s", got)
	}
}

func TestCommentDoesNotBecomeOption(t *testing.T) {
	m := &Manager{Roots: []string{"/srv/shares"}}
	r := ShareReq{Name: "X", Path: "/srv/shares/x", Comment: "--configfile=/srv/shares/evil.conf", RW: []string{"g"}}
	if err := m.ValidateShare(&r); err == nil {
		t.Fatal("a comment starting with - should be rejected")
	}
}

func TestParseConf(t *testing.T) {
	out := `[global]
	workgroup = EMPRESA

[Financeiro]
	path = /srv/shares/fin
	comment = Equipe
	valid users = @"EMPRESA\Financeiro" @"EMPRESA\Diretoria"
	write list = @"EMPRESA\Financeiro"
	read only = yes
	browseable = no
`
	got := parseConf(out, "")
	if len(got) != 1 {
		t.Fatalf("%+v", got)
	}
	s := got[0]
	if s.Name != "Financeiro" || s.Path != "/srv/shares/fin" || s.Browse || !s.ReadOnly ||
		!slices.Equal(s.RW, []string{`EMPRESA\Financeiro`}) || !slices.Equal(s.RO, []string{`EMPRESA\Diretoria`}) {
		t.Fatalf("parse = %+v", s)
	}
}

// A newline in the path would inject parameters into the smb.conf import.
func TestSafePathRejectsControlCharacters(t *testing.T) {
	m := &Manager{Roots: []string{"/srv/shares"}}
	if _, err := m.SafePath("/srv/shares/x\npath = /etc\nforce user = root"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
}

// fakeConf keeps shares in memory like Samba's registry API.
type fakeConf struct {
	shares map[string][][2]string
	vfs    []string
}

func (f *fakeConf) ListShares(context.Context) ([]sambabridge.ShareConf, error) {
	var out []sambabridge.ShareConf
	for n, p := range f.shares {
		out = append(out, sambabridge.ShareConf{Name: n, Params: p})
	}
	return out, nil
}

func (f *fakeConf) SetShare(_ context.Context, name string, params [][2]string, vfs []string) (*sambabridge.ShareConf, error) {
	if vfs != nil {
		params = append(params, [2]string{"vfs objects", strings.Join(append(vfs, "acl_xattr"), " ")})
	}
	f.shares[name], f.vfs = params, vfs
	return &sambabridge.ShareConf{Name: name, Params: params}, nil
}

func (f *fakeConf) DeleteShare(_ context.Context, name string) error {
	delete(f.shares, name)
	return nil
}

func TestSharesThroughRegistryAPI(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "fin"), 0o755); err != nil {
		t.Fatal(err)
	}
	conf := &fakeConf{shares: map[string][][2]string{}}
	m := &Manager{Roots: []string{root}, Run: func(_ context.Context, name string, _ ...string) (string, error) {
		if name == "testparm" { // a standalone server: no Domain Admins added
			return "standalone server\n", nil
		}
		t.Fatal("net conf must not run")
		return "", nil
	}, Conf: conf}
	req := ShareReq{Name: "Financeiro", Path: filepath.Join(root, "fin"), RW: []string{`EMPRESA\Financeiro`},
		RO: []string{`EMPRESA\Diretoria`}, Recycle: true, ABE: true, Audit: true}
	if err := m.CreateShare(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(conf.vfs, []string{"full_audit", "recycle"}) {
		t.Fatalf("vfs: %v", conf.vfs)
	}
	list, err := m.List(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %+v %v", list, err)
	}
	s := list[0]
	if !s.Recycle || !s.ABE || !s.Audit || !slices.Equal(s.RW, []string{`EMPRESA\Financeiro`}) || !slices.Equal(s.RO, []string{`EMPRESA\Diretoria`}) {
		t.Fatalf("share: %+v", s)
	}
	row := CSVRow(s)
	back := FromCSV(func() map[string]string {
		m := map[string]string{}
		for i, h := range CSVHeader {
			m[h] = row[i]
		}
		return m
	}())
	if !back.Recycle || !back.ABE || !back.Audit {
		t.Fatalf("CSV round trip: %+v", back)
	}
	if err := m.DeleteShare(context.Background(), "Financeiro"); err != nil || len(conf.shares) != 0 {
		t.Fatalf("delete: %v %v", err, conf.shares)
	}
}

func TestNetConfWithoutVFS(t *testing.T) {
	root := t.TempDir()
	m := &Manager{Roots: []string{root}, Run: func(context.Context, string, ...string) (string, error) { return "", nil }}
	err := m.CreateShare(context.Background(), ShareReq{Name: "X", Path: root + "/x", RW: []string{"g"}, CreatePath: false, Recycle: true})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid (recycle needs python3-samba)", err)
	}
}

// This domain's Domain Admins always get in (valid users and write list), once, and are not shown among the share's
// groups — unless nobody else is there; another domain's Domain Admins are another group.
func TestDomainAdminsImplicit(t *testing.T) {
	t.Parallel()
	const da = `ORIGEM\domain admins`
	params := func(r ShareReq) map[string]string {
		p, _ := shareParams(r, da)
		got := map[string]string{}
		for _, kv := range p {
			got[kv[0]] = kv[1]
		}
		return got
	}
	got := params(ShareReq{Name: "Financeiro", Path: "/srv/shares/fin", RW: []string{`ORIGEM\Financeiro`}, RO: []string{`ORIGEM\Diretoria`}})
	if got["valid users"] != `@"ORIGEM\Financeiro" @"ORIGEM\Diretoria" @"ORIGEM\domain admins"` ||
		got["write list"] != `@"ORIGEM\Financeiro" @"ORIGEM\domain admins"` {
		t.Fatalf("params %q", got)
	}
	if got := params(ShareReq{Name: "A", Path: "/srv/shares/a", Full: []string{`ORIGEM\Domain Admins`}}); got["valid users"] != `@"ORIGEM\Domain Admins"` {
		t.Fatalf("added twice: %s", got["valid users"])
	}
	if got := params(ShareReq{Name: "B", Path: "/srv/shares/b", RW: []string{`OUTRO\Domain Admins`}}); got["valid users"] != `@"OUTRO\Domain Admins" @"ORIGEM\domain admins"` {
		t.Fatalf("another domain's group: %s", got["valid users"])
	}
	s := parseConf("[Financeiro]\n\tvalid users = @\"ORIGEM\\Financeiro\" @\"ORIGEM\\Diretoria\" @\"ORIGEM\\domain admins\"\n\twrite list = @\"ORIGEM\\Financeiro\" @\"ORIGEM\\domain admins\"\n", da)[0]
	if !slices.Equal(s.RW, []string{`ORIGEM\Financeiro`}) || !slices.Equal(s.RO, []string{`ORIGEM\Diretoria`}) {
		t.Fatalf("parse RW %q RO %q", s.RW, s.RO)
	}
	only := parseConf("[Adm]\n\tvalid users = @\"ORIGEM\\domain admins\"\n\twrite list = @\"ORIGEM\\domain admins\"\n", da)[0]
	if !slices.Equal(only.RW, []string{da}) {
		t.Fatalf("an administrators-only share must read back with them: %q", only.RW)
	}
}

// TestParseConfEmpty: a server with no share lists [] (JSON), not null: the panel maps the reply.
func TestParseConfEmpty(t *testing.T) {
	b, err := json.Marshal(parseConf("", ""))
	if err != nil || string(b) != "[]" {
		t.Fatalf("got %s %v, want []", b, err)
	}
}

// On a DC only the administrative shares are reserved: a migrated server's hidden data share ("Faturamento
// Sefaz$") is created like any other.
func TestReservedOnDC(t *testing.T) {
	dc := &Manager{DC: true, Reserved: []string{"profiles"}}
	for name, want := range map[string]bool{
		"C$": true, "e$": true, "ADMIN$": true, "IPC$": true, "print$": true, "sysvol": true, "NETLOGON": true,
		"profiles": true, "Faturamento Sefaz$": false, "Notas Fiscais$": false, "AB$": false, "Financeiro": false,
	} {
		if got := dc.reserved(name); got != want {
			t.Errorf("reserved(%q) on a DC = %v, want %v", name, got, want)
		}
	}
	if (&Manager{}).reserved("C$") {
		t.Error("a file server (not a DC) reserves nothing itself")
	}
}
