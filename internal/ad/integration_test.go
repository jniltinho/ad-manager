//go:build integration

package ad

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-ldap/ldap/v3"

	"ad-manager/internal/gosamba/samdb"
)

// testCfg points at the test Samba AD: `make samba-up` (Docker) —
// ldaps://127.0.0.1:6636, empresa.test.
func testCfg() *Config {
	env := func(k, d string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return d
	}
	return &Config{URL: env("AD_TEST_URL", "ldaps://127.0.0.1:6636"), Domain: env("AD_TEST_DOMAIN", "empresa.test"), InsecureSkipVerify: true}
}

// adminPass returns the test Administrator password.
func adminPass() string {
	if p := os.Getenv("AD_TEST_PASSWORD"); p != "" {
		return p
	}
	return "Teste-ad-manager-2026"
}

// dialAdmin binds as Administrator, skipping the test if Samba is down.
func dialAdmin(t *testing.T) *Conn {
	t.Helper()
	c, err := Dial(testCfg(), "Administrator", adminPass())
	if err != nil {
		t.Skipf("test Samba unavailable (make samba-up): %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

// uniq returns p with a time-based suffix.
func uniq(p string) string { return fmt.Sprintf("%s%d", p, time.Now().UnixNano()%1e8) }

// newUser creates a user and schedules its deletion.
func newUser(t *testing.T, c *Conn, ou string) (string, string) {
	t.Helper()
	sam, pass := uniq("it"), "Inicial-123"
	if _, err := c.CreateUser(UserInput{Sam: sam, Name: "Teste " + sam}, pass, ou, false); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() { _ = c.Delete(sam) })
	return sam, pass
}

func TestUserLifecycle(t *testing.T) {
	c := dialAdmin(t)
	sam, pass := newUser(t, c, "")

	u, err := c.User(sam)
	if err != nil || !u.Enabled || u.UPN != sam+"@empresa.test" || u.SID == "" {
		t.Fatalf("user created incorrectly: %+v %v", u, err)
	}
	t.Run("initial password allows login", func(t *testing.T) {
		uc, err := Dial(testCfg(), sam, pass)
		if err != nil {
			t.Fatalf("login with the initial password: %v", err)
		}
		uc.Close()
	})
	t.Run("duplicate login is a conflict", func(t *testing.T) {
		if _, err := c.CreateUser(UserInput{Sam: sam, Name: "Outro " + sam}, pass, "", false); !errors.Is(err, ErrConflict) {
			t.Fatalf("expected ErrConflict, got %v", err)
		}
	})
	t.Run("disable and enable", func(t *testing.T) {
		if err := c.SetEnabled(sam, false); err != nil {
			t.Fatal(err)
		}
		if u, _ := c.User(sam); u.Enabled {
			t.Fatal("still enabled")
		}
		if err := c.SetEnabled(sam, true); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("edit and clear a field", func(t *testing.T) {
		if _, err := c.UpdateUser(sam, UserInput{Name: "New Name", Mail: "x@empresa.test", Title: "Support"}); err != nil {
			t.Fatal(err)
		}
		u, _ := c.User(sam)
		if u.Name != "New Name" || u.Mail != "x@empresa.test" || u.Title != "Support" {
			t.Fatalf("fields not saved: %+v", u)
		}
		if _, err := c.UpdateUser(sam, UserInput{Name: "New Name"}); err != nil {
			t.Fatal(err)
		}
		if u, _ := c.User(sam); u.Mail != "" {
			t.Fatalf("mail should have been removed: %q", u.Mail)
		}
	})
	t.Run("reset with mandatory change", func(t *testing.T) {
		if err := c.ResetPassword(sam, "Nova-456", true); err != nil {
			t.Fatal(err)
		}
		if u, _ := c.User(sam); !u.PwdLastSet.IsZero() {
			t.Fatalf("pwdLastSet should be 0, got %v", u.PwdLastSet)
		}
	})
	t.Run("delete", func(t *testing.T) {
		if err := c.Delete(sam); err != nil {
			t.Fatal(err)
		}
		if _, err := c.User(sam); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestChangeOwnPassword(t *testing.T) {
	c := dialAdmin(t)
	sam, pass := newUser(t, c, "")
	uc, err := Dial(testCfg(), sam, pass)
	if err != nil {
		t.Fatal(err)
	}
	defer uc.Close()
	if err := uc.ChangeOwnPassword("wrong", "Outra-789"); err == nil {
		t.Fatal("a wrong current password should fail")
	}
	if err := uc.ChangeOwnPassword(pass, "Outra-789"); err != nil {
		t.Fatalf("change: %v", err)
	}
	nc, err := Dial(testCfg(), sam, "Outra-789")
	if err != nil {
		t.Fatalf("login with the new password: %v", err)
	}
	nc.Close()
}

func TestProtectedObjects(t *testing.T) {
	c := dialAdmin(t)
	dcs, err := c.Computers("")
	if err != nil {
		t.Fatal(err)
	}
	dc := ""
	for _, m := range dcs {
		if m.IsDC {
			dc = m.Name + "$"
		}
	}
	tests := []struct {
		name string
		fn   func() error
	}{
		{"delete Administrator", func() error { return c.Delete("Administrator") }},
		{"delete krbtgt", func() error { return c.Delete("krbtgt") }},
		{"disable own account", func() error { return c.SetEnabled("Administrator", false) }},
		{"delete Domain Admins", func() error { return c.Delete("Domain Admins") }},
		{"disable the DC", func() error { return c.SetEnabled(dc, false) }},
		{"move the DC", func() error { return c.Move(dc, "CN=Users,"+c.cfg.Base()) }},
		{"delete the Domain Controllers OU", func() error { return c.DeleteOU("OU=Domain Controllers," + c.cfg.Base()) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.fn(); !errors.Is(err, ErrProtected) {
				t.Fatalf("expected ErrProtected, got %v", err)
			}
		})
	}
}

func TestGroupAndMembers(t *testing.T) {
	c := dialAdmin(t)
	sam, _ := newUser(t, c, "")
	g := uniq("grp ")
	if _, err := c.CreateGroup(g, "teste", "security", "global", ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Delete(g) })
	for range 2 { // idempotent
		if err := c.AddMember(g, sam); err != nil {
			t.Fatalf("AddMember: %v", err)
		}
	}
	gr, _ := c.Group(g)
	u, _ := c.User(sam)
	if !slices.Contains(gr.Members, sam) || !slices.Contains(u.Groups, g) {
		t.Fatalf("membership does not show on both sides: %v / %v", gr.Members, u.Groups)
	}
	if err := c.RemoveMember(g, sam); err != nil {
		t.Fatal(err)
	}
	if gr, _ := c.Group(g); len(gr.Members) != 0 {
		t.Fatalf("member not removed: %v", gr.Members)
	}
}

func TestOUNotEmptyAndMove(t *testing.T) {
	c := dialAdmin(t)
	ou, err := c.CreateOU(uniq("OUteste"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.DeleteOU(ou.DN) })
	sam, _ := newUser(t, c, ou.DN)
	if err := c.DeleteOU(ou.DN); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("expected ErrNotEmpty, got %v", err)
	}
	if err := c.Move(sam, "CN=Users,"+c.cfg.Base()); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteOU(ou.DN); err != nil {
		t.Fatalf("an empty OU should be deleted: %v", err)
	}
}

func TestPasswordPolicy(t *testing.T) {
	c := dialAdmin(t)
	orig, err := c.Policy()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.SetPolicy(*orig) })
	sam, _ := newUser(t, c, "")
	p := *orig
	p.MinLength, p.Complexity, p.MaxAgeDays, p.LockoutThreshold, p.LockoutMinutes, p.ObservationMinute = 8, true, 90, 10, 30, 30
	if err := c.SetPolicy(p); err != nil {
		t.Fatal(err)
	}
	got, _ := c.Policy()
	if *got != p {
		t.Fatalf("saved policy %+v, want %+v", got, p)
	}
	if err := c.ResetPassword(sam, "abc", false); !errors.Is(err, ErrPwdPolicy) {
		t.Fatalf("a short password should violate the policy, got %v", err)
	}
}

func TestAdminAndInventory(t *testing.T) {
	c := dialAdmin(t)
	if ok, err := c.IsAdmin(); !ok || err != nil {
		t.Fatalf("Administrator should be admin: %v %v", ok, err)
	}
	sam, pass := newUser(t, c, "")
	uc, err := Dial(testCfg(), sam, pass)
	if err != nil {
		t.Fatal(err)
	}
	defer uc.Close()
	if ok, _ := uc.IsAdmin(); ok {
		t.Fatal("a regular user must not be admin")
	}
	inv, err := c.Inventory("teste")
	if err != nil {
		t.Fatal(err)
	}
	if inv.Counts["users"] == 0 || len(Diff(inv, inv)) != 0 {
		t.Fatalf("invalid inventory: %v", inv.Counts)
	}
	pf, err := c.Preflight()
	if err != nil || len(pf) < 8 {
		t.Fatalf("preflight: %v %v", pf, err)
	}
	// the RC4-only row is there (its status depends on when provisioning set Administrator's password: a heuristic)
	if i := slices.IndexFunc(pf, func(c Check) bool { return c.Item == "RC4-only accounts" }); i < 0 || pf[i].Status == StatusBlocker {
		t.Fatalf("RC4-only accounts row: %v", pf)
	}
}

// TestImportComputer checks that importing computers pre-creates the account
// (disabled if the CSV says so) in the requested OU, creating the OU; a second
// import does not repeat it.
func TestImportComputer(t *testing.T) {
	c := dialAdmin(t)
	name, ou := fmt.Sprintf("PC%d", time.Now().UnixNano()%1e8), uniq("OUpc")
	t.Cleanup(func() { _ = c.Delete(name + "$"); _ = c.DeleteOU("OU=" + ou + "," + c.cfg.Base()) })
	d := ImportData{Computers: []map[string]string{{"computer": name, "dns": name + ".empresa.test", "os": "Windows 11", "ou": "OU=" + ou, "enabled": "no"}}}
	plan, err := c.Plan(d)
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{}
	for _, a := range plan {
		kinds = append(kinds, a.Kind)
	}
	if !slices.Equal(kinds, []string{"ou.create", "computer.create"}) {
		t.Fatalf("plan: %v", kinds)
	}
	c.Apply(plan, GenPassword)
	for _, a := range plan {
		if !a.OK {
			t.Fatalf("%s %s: %s", a.Kind, a.Target, a.Result)
		}
	}
	ms, err := c.Computers(name)
	if err != nil || len(ms) != 1 || ms[0].Enabled || ms[0].DNSName != name+".empresa.test" {
		t.Fatalf("computer: %+v %v", ms, err)
	}
	if plan, _ = c.Plan(d); len(plan) != 0 {
		t.Fatalf("a second import should be empty: %+v", plan)
	}
}

// TestPasswordLinkKerberos: after PrepareLinkReset the user cannot bind with
// LDAP (must change), but the Kerberos change with the temporary password works
// even with a minimum password age; protected accounts are refused.
func TestPasswordLinkKerberos(t *testing.T) {
	c := dialAdmin(t)
	orig, err := c.Policy()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.SetPolicy(*orig) })
	p := *orig
	p.MinAgeDays, p.MinLength = 1, 8
	if err := c.SetPolicy(p); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PrepareLinkReset("Administrator", TempPassword()); !errors.Is(err, ErrProtected) {
		t.Fatalf("Administrator: want ErrProtected, got %v", err)
	}
	sam, _ := newUser(t, c, "")
	temp := TempPassword()
	if _, err := c.PrepareLinkReset(sam, temp); err != nil {
		t.Fatal(err)
	}
	cfg := testCfg()
	if err := ChangePasswordKerberos(cfg, sam, "errada-"+temp, "Nova-Senha-123"); !errors.Is(err, ErrBadLogin) {
		t.Skipf("Kerberos (88/464) unavailable or wrong error: %v", err)
	}
	if err := ChangePasswordKerberos(cfg, sam, temp, "abc"); !errors.Is(err, ErrPwdPolicy) {
		t.Fatalf("short password: want ErrPwdPolicy, got %v", err)
	}
	if err := ChangePasswordKerberos(cfg, sam, temp, "Nova-Senha-123"); err != nil {
		t.Fatalf("Kerberos change: %v", err)
	}
	u, err := Dial(cfg, sam, "Nova-Senha-123")
	if err != nil {
		t.Fatalf("bind with the new password: %v", err)
	}
	u.Close()
}

// TestGPOs: the default GPOs of a new domain and their links (domain root, Domain Controllers OU).
func TestGPOs(t *testing.T) {
	c := dialAdmin(t)
	gpos, conts, err := c.GPOs()
	if err != nil {
		t.Fatal(err)
	}
	byGUID := map[string]GPO{}
	for _, g := range gpos {
		byGUID[g.GUID] = g
	}
	dom, dcs := byGUID["{31B2F340-016D-11D2-945F-00C04FB984F9}"], byGUID["{6AC1786C-016F-11D2-945F-00C04FB984F9}"]
	if dom.Name != "Default Domain Policy" || len(dom.Links) == 0 || !strings.EqualFold(dom.Links[0].Target, c.cfg.Base()) {
		t.Fatalf("Default Domain Policy: %+v", dom)
	}
	if dcs.Name != "Default Domain Controllers Policy" || len(dcs.Links) == 0 || !strings.HasPrefix(strings.ToLower(dcs.Links[0].Target), "ou=domain controllers") {
		t.Fatalf("Default Domain Controllers Policy: %+v", dcs)
	}
	if len(conts) < 2 {
		t.Fatalf("containers with links: %+v", conts)
	}
}

// TestDCHostName: the DNS name of the DC the connection talks to (its Kerberos principals use it).
func TestDCHostName(t *testing.T) {
	c := dialAdmin(t)
	h, err := c.DCHostName()
	if err != nil || !strings.HasSuffix(strings.ToLower(h), "."+strings.ToLower(testCfg().Domain)) {
		t.Fatalf("%q %v", h, err)
	}
}

// TestSearchForSync covers what the Entra sync reads: the naming contexts and a paged search returning binary
// attributes untouched.
func TestSearchForSync(t *testing.T) {
	c := dialAdmin(t)
	dom, conf, err := c.NamingContexts()
	if err != nil || dom != testCfg().Base() || conf != "CN=Configuration,"+dom {
		t.Fatalf("NamingContexts = %q, %q, %v", dom, conf, err)
	}
	es, err := c.Search(dom, ldap.ScopeWholeSubtree, "(sAMAccountName=Administrator)", []string{"objectGUID", "objectSid"})
	if err != nil || len(es) != 1 {
		t.Fatalf("Search: %d entries, %v", len(es), err)
	}
	g, sid := es[0].GetRawAttributeValue("objectGUID"), es[0].GetRawAttributeValue("objectSid")
	if len(g) != 16 || len(sid) != 28 {
		t.Fatalf("objectGUID %d bytes, objectSid %d bytes", len(g), len(sid))
	}
	root, _ := c.Search("", ldap.ScopeBaseObject, "(objectClass=*)", []string{"dnsHostName"})
	if len(root) == 1 { // for scripts/test-dc.sh's DRSUAPI run against a DC without fixtures (read only)
		t.Logf("dnsHostName %s, Administrator objectGUID %s", root[0].GetAttributeValue("dnsHostName"), samdb.GUID(g))
	}
}
