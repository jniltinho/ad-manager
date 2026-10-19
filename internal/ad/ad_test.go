package ad

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-ldap/ldap/v3"
)

func TestBindName(t *testing.T) {
	for in, want := range map[string][2]string{
		"joao":             {"joao@empresa.net", "joao"},
		"joao@empresa.net": {"joao@empresa.net", "joao"},
		`EMPRESA\joao`:     {"joao@empresa.net", "joao"},
		" administrator ":  {"administrator@empresa.net", "administrator"},
	} {
		b, s := BindName(in, "empresa.net")
		if b != want[0] || s != want[1] {
			t.Errorf("BindName(%q) = %q,%q; want %q", in, b, s, want)
		}
	}
}

func TestSIDAndRID(t *testing.T) {
	// S-1-5-21-1111111111-2222222222-3333333333-512
	b := []byte{0x01, 0x05, 0x00, 0x00, 0x00, 0x00, 0x00, 0x05, 0x15, 0x00, 0x00, 0x00, 0xc7, 0x35, 0x3a, 0x42,
		0x8e, 0x6b, 0x74, 0x84, 0x55, 0xa1, 0xae, 0xc6, 0x00, 0x02, 0x00, 0x00}
	if got := sidString(b); got != "S-1-5-21-1111111111-2222222222-3333333333-512" {
		t.Errorf("sidString = %s", got)
	}
	if rid(b) != 512 {
		t.Errorf("rid = %d", rid(b))
	}
	if rid(nil) != 0 {
		t.Error("an invalid SID must count as protected (rid 0)")
	}
}

func TestEncodePwd(t *testing.T) {
	if got := encodePwd("ab"); got != "\"\x00a\x00b\x00\"\x00" {
		t.Errorf("encodePwd = %q", got)
	}
}

func TestFileTime(t *testing.T) {
	if !fileTime("0").IsZero() || !fileTime("9223372036854775807").IsZero() {
		t.Error("0 and max must mean 'never'")
	}
	if got := fileTime("116444736000000000"); !got.Equal(time.Unix(0, 0)) {
		t.Errorf("epoch = %v", got)
	}
}

func TestPolicyUnits(t *testing.T) {
	day := 24 * time.Hour
	if v := fromUnits(42, day); v != "-36288000000000" {
		t.Errorf("42 days = %s", v)
	}
	if n := toUnits("-36288000000000", day); n != 42 {
		t.Errorf("round trip = %d", n)
	}
	if n := toUnits("-9223372036854775808", day); n != 0 {
		t.Errorf("never = %d", n)
	}
}

func TestDiff(t *testing.T) {
	a := &Inventory{Source: "a", Users: []User{{Sam: "joao", SID: "S-1"}}, Members: map[string][]string{"g": {"joao", "ADM$"}},
		Computers: []Computer{{Name: "ADM", IsDC: true}}}
	b := &Inventory{Source: "b", Users: []User{{Sam: "joao", SID: "S-1"}}, Members: map[string][]string{"g": {"JOAO", "DC01$"}},
		Computers: []Computer{{Name: "DC01", IsDC: true}}}
	if d := Diff(a, b); len(d) != 0 {
		t.Errorf("DCs and letter case should not produce differences: %v", d)
	}
	b.Users[0].SID = "S-2"
	if d := Diff(a, b); len(d) != 2 {
		t.Errorf("a different SID should produce 2 lines: %v", d)
	}
}

func TestPasswordRequiresTLS(t *testing.T) {
	c := &Conn{cfg: &Config{Domain: "empresa.net"}, tls: false, Me: "admin"}
	if err := c.setPassword("CN=x", "Abc-123", false); !errors.Is(err, ErrNeedsTLS) {
		t.Fatalf("expected ErrNeedsTLS, got %v", err)
	}
	if err := c.ChangeOwnPassword("a", "b"); !errors.Is(err, ErrNeedsTLS) {
		t.Fatalf("expected ErrNeedsTLS, got %v", err)
	}
	if _, err := c.CreateUser(UserInput{Sam: "joao", Name: "João"}, "x", "", false); !errors.Is(err, ErrNeedsTLS) {
		t.Fatalf("expected ErrNeedsTLS, got %v", err)
	}
}

func TestGenPassword(t *testing.T) {
	for range 200 {
		p := GenPassword()
		if len(p) != 16 || !strings.ContainsAny(p, "ABCDEFGHJKLMNPQRSTUVWXYZ") || !strings.ContainsAny(p, "abcdefghijkmnopqrstuvwxyz") ||
			!strings.ContainsAny(p, "23456789") || !strings.ContainsAny(p, "@#%+=!?") || strings.ContainsAny(p, "0O1lI") {
			t.Fatalf("weak or ambiguous password: %q", p)
		}
	}
}

func TestReadCSV(t *testing.T) {
	tests := []struct {
		name, in string
		rows     int
		err      bool
	}{
		{"semicolon with BOM", "\ufefflogin;name\njoao;João\n;empty\n", 1, false},
		{"comma", "login,name\njoao,João\nmaria,Maria\n", 2, false},
		{"older Portuguese column", "login;nome\njoao;João\n", 1, false},
		{"missing key column", "name\nJoão\n", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := ReadCSV(strings.NewReader(tt.in), UsersHeader)
			if (err != nil) != tt.err || len(rows) != tt.rows {
				t.Fatalf("rows=%d err=%v", len(rows), err)
			}
			if tt.rows > 0 && rows[0]["name"] != "João" {
				t.Fatalf("name = %q", rows[0]["name"])
			}
		})
	}
}

func TestOuChain(t *testing.T) {
	tests := []struct {
		name, in string
		want     []string
	}{
		{"child and parent", "OU=Filha,OU=Pai", []string{"OU=Pai", "OU=Filha,OU=Pai"}},
		{"escaped comma", `OU=Vendas\, Sul`, []string{`OU=Vendas\, Sul`}},
		{"container does not become an OU", "CN=Users", nil},
		{"OU inside a container", "OU=X,CN=Users", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ouChain(tt.in); !slices.Equal(got, tt.want) {
				t.Fatalf("ouChain(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
	if n, p := splitRDN(`OU=Vendas\, Sul,OU=Pai`); n != "Vendas, Sul" || p != "OU=Pai" {
		t.Fatalf("splitRDN = %q, %q", n, p)
	}
}

func TestRODCProtected(t *testing.T) {
	if !isDC(0x5001000) || !isDC(0x82000) || isDC(0x1000) {
		t.Fatal("isDC must recognize DCs and RODCs but not workstations")
	}
}

// A Windows builtin group carries the domain local bit too: it is builtin; a well-known group stays protected.
func TestToGroupBuiltin(t *testing.T) {
	sid := func(rid uint32) string { // S-1-5-21-1-2-3-<rid>
		b := []byte{1, 5, 0, 0, 0, 0, 0, 5, 21, 0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3, 0, 0, 0}
		return string(append(b, byte(rid), byte(rid>>8), byte(rid>>16), byte(rid>>24)))
	}
	for _, tt := range []struct {
		name, groupType string
		rid             uint32
		scope           string
		protected       bool
	}{
		{"Hyper-V Administrators", "-2147483643", 578, "builtin", true},
		{"Key Admins", "-2147483646", 526, "global", true},
		{"Financeiro", "-2147483646", 1105, "global", false},
	} {
		e := ldap.NewEntry("CN="+tt.name+",DC=x", map[string][]string{"sAMAccountName": {tt.name}, "groupType": {tt.groupType}, "objectSid": {sid(tt.rid)}})
		if g := toGroup(e); g.Scope != tt.scope || g.Protected != tt.protected {
			t.Errorf("%s: scope %s protected %v", tt.name, g.Scope, g.Protected)
		}
	}
}
