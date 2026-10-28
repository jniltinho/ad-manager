//go:build linux

package sambadc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"ad-manager/internal/gosamba/drs"
)

// fakeSource and fakeEnv stand in for the source DC and the local ldb tools.
type fakeSource struct {
	sid, realm string
	users      []srcUser
	secrets    map[string]*drs.Secrets // by sAMAccountName
	errs       map[string]error
	tamper     map[string][2]string // sam → the name and SID the DRS reply carries instead
	read       []string
}

func (s *fakeSource) SID() string      { return s.sid }
func (s *fakeSource) Realm() string    { return s.realm }
func (s *fakeSource) Users() []srcUser { return s.users }
func (s *fakeSource) Close()           {}
func (s *fakeSource) Secrets(_ context.Context, guid []byte) (*drs.Secrets, error) {
	sam := string(guid)
	s.read = append(s.read, sam)
	if err := s.errs[sam]; err != nil {
		return nil, err
	}
	name, sid := sam, s.sid+"-1105"
	if t, ok := s.tamper[sam]; ok { // what the sealed reply really holds
		name, sid = t[0], t[1]
	}
	sec := s.secrets[sam]
	if sec == nil {
		return &drs.Secrets{SAMAccountName: name, SID: sid}, nil
	}
	return &drs.Secrets{NTHash: bytes.Clone(sec.NTHash), Supplemental: bytes.Clone(sec.Supplemental), SAMAccountName: name, SID: sid}, nil
}

type written struct {
	dn       string
	nt, supp []byte
}

type fakeEnv struct {
	sid, realm string
	local      map[string]localUser
	state      map[string][2][]byte // dn → current NT hash, supplementalCredentials
	refuseSupp bool                 // Samba rejects any Kerberos blob
	src        *fakeSource
	writes     []written
}

func (e *fakeEnv) localDomain(context.Context) (string, string, string, error) {
	return e.sid, e.realm, "DC=example,DC=net", nil
}
func (e *fakeEnv) localUsers(context.Context, string) (map[string]localUser, error) {
	return e.local, nil
}
func (e *fakeEnv) currentState(_ context.Context, dn string) ([]byte, []byte, error) {
	s := e.state[dn]
	return bytes.Clone(s[0]), bytes.Clone(s[1]), nil
}
func (e *fakeEnv) write(_ context.Context, dn string, nt, supp []byte, pls int64) error {
	if pls <= 0 {
		return errors.New("pwdLastSet must be explicit and non-zero")
	}
	if e.refuseSupp && len(supp) > 0 {
		return errors.New("ldbmodify: exit status 1: supplementalCredentialsBlob memcmp differ")
	}
	e.writes = append(e.writes, written{dn, bytes.Clone(nt), bytes.Clone(supp)})
	return nil
}
func (e *fakeEnv) openSource(context.Context, PwSource, string) (source, error) { return e.src, nil }

// a real Kerberos blob of a throwaway user on the test Samba (EMPRESA.TEST) and its rebuilt form, from the parser's
// parity fixtures
func kerberosBlob(t *testing.T) (blob, rebuilt []byte) {
	b, err := os.ReadFile("../gosamba/suppcreds/testdata/parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var cs []struct{ Name, Blob, Rebuilt string }
	if err := json.Unmarshal(b, &cs); err != nil {
		t.Fatal(err)
	}
	blob, _ = hex.DecodeString(cs[0].Blob)
	rebuilt, _ = hex.DecodeString(cs[0].Rebuilt)
	return blob, rebuilt
}

func newFake(t *testing.T) *fakeEnv {
	kb, rebuilt := kerberosBlob(t)
	nt := bytes.Repeat([]byte{0x11}, 16)
	clear := func() []byte {
		// the parity blob with a CLEARTEXT package is the next-to-last case
		b, _ := os.ReadFile("../gosamba/suppcreds/testdata/parity.json")
		var cs []struct{ Name, Blob string }
		_ = json.Unmarshal(b, &cs)
		for _, c := range cs {
			if c.Name == "synthetic cleartext" {
				v, _ := hex.DecodeString(c.Blob)
				return v
			}
		}
		t.Fatal("no cleartext fixture")
		return nil
	}()
	u := func(sam string) srcUser { return srcUser{Sam: sam, GUID: []byte(sam)} }
	src := &fakeSource{sid: "S-1-5-21-1-1-1", realm: "EMPRESA.TEST",
		users: []srcUser{u("kerb"), u("ntlm"), u("same"), u("nopw"), u("clear"), u("missing"), u("Administrator"), u("broken")},
		secrets: map[string]*drs.Secrets{
			"kerb": {NTHash: nt, Supplemental: kb}, "ntlm": {NTHash: nt}, "same": {NTHash: nt, Supplemental: kb},
			"clear": {NTHash: nt, Supplemental: clear},
		},
		errs: map[string]error{"broken": errors.New("DRSGetNCChanges: Windows error 0x00000005 timeout? deadline exceeded")},
	}
	return &fakeEnv{sid: "S-1-5-21-2-2-2", realm: "EMPRESA.TEST", src: src,
		local: map[string]localUser{
			"kerb": {DN: "CN=kerb"}, "ntlm": {DN: "CN=ntlm"}, "same": {DN: "CN=same", PwdLastSet: 1},
			"nopw": {DN: "CN=nopw"}, "clear": {DN: "CN=clear"}, "administrator": {DN: "CN=Administrator", Protected: true},
			"broken": {DN: "CN=broken"},
		},
		state: map[string][2][]byte{"CN=same": {nt, rebuilt}},
	}
}

func outcomes(r *PwResult) map[string]string {
	m := map[string]string{}
	for _, u := range r.Users {
		m[u.Sam] = u.Outcome
	}
	return m
}

func TestMigrateApply(t *testing.T) {
	e := newFake(t)
	r, err := migrate(context.Background(), e, true, PwSource{}, "pw", e.sid)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"kerb": "set", "ntlm": "ntlm_only", "same": "same", "nopw": "no_password", "clear": "rejected",
		"missing": "missing", "Administrator": "builtin", "broken": "error"}
	for k, v := range want {
		if outcomes(r)[k] != v {
			t.Errorf("%s: %s, want %s (%v)", k, outcomes(r)[k], v, r.Users)
		}
	}
	if r.Counts["set"] != 1 || r.Counts["error"] != 1 || r.DestSID != e.sid || r.SourceSID != e.src.sid {
		t.Errorf("counts or SIDs: %+v", r)
	}
	// only kerb (with its rebuilt Kerberos blob) and ntlm (no blob: stale keys removed) were written
	_, rebuilt := kerberosBlob(t)
	if len(e.writes) != 2 || e.writes[0].dn != "CN=kerb" || !bytes.Equal(e.writes[0].supp, rebuilt) ||
		e.writes[1].dn != "CN=ntlm" || e.writes[1].supp != nil {
		t.Errorf("writes: %d", len(e.writes))
	}
	// the built-in, the missing user and their secrets were never read
	for _, s := range e.src.read {
		if s == "Administrator" || s == "missing" {
			t.Errorf("read the secrets of %s", s)
		}
	}
}

func TestMigrateFallbackAndPlan(t *testing.T) {
	e := newFake(t)
	e.refuseSupp = true
	r, err := migrate(context.Background(), e, true, PwSource{}, "pw", e.sid)
	if err != nil || outcomes(r)["kerb"] != "ntlm_only" {
		t.Fatalf("fallback: %v %v", outcomes(r), err)
	}
	for _, u := range r.Users {
		if u.Sam == "kerb" && !strings.Contains(u.Reason, "Samba rejected the copied Kerberos keys") {
			t.Errorf("fallback reason %q", u.Reason)
		}
	}
	e = newFake(t)
	r, err = migrate(context.Background(), e, false, PwSource{}, "pw", e.sid)
	if err != nil || r.Counts["would_set"] != 6 || len(e.src.read) != 0 || len(e.writes) != 0 {
		t.Fatalf("plan read or wrote secrets: %v reads=%d writes=%d %v", r.Counts, len(e.src.read), len(e.writes), err)
	}
}

func TestMigrateRefusals(t *testing.T) {
	e := newFake(t)
	for name, f := range map[string]func() (string, error){
		"no pin": func() (string, error) {
			_, err := migrate(context.Background(), e, true, PwSource{}, "pw", "")
			return "", err
		},
		"wrong pin": func() (string, error) {
			_, err := migrate(context.Background(), e, true, PwSource{}, "pw", "S-1-5-21-9")
			return "", err
		},
		"same domain": func() (string, error) {
			e2 := newFake(t)
			e2.src.sid = e2.sid
			_, err := migrate(context.Background(), e2, true, PwSource{}, "pw", e2.sid)
			return "", err
		},
		"rights": func() (string, error) {
			e2 := newFake(t)
			e2.src.errs["kerb"] = errors.New("DRSGetNCChanges: DRS error 0x00002105")
			_, err := migrate(context.Background(), e2, true, PwSource{}, "pw", e2.sid)
			if len(e2.writes) != 0 {
				return "", errors.New("wrote before the refusal")
			}
			return "", err
		},
	} {
		if _, err := f(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if len(e.writes) != 0 {
		t.Error("a refused run wrote")
	}
}

// The material reaches ldbmodify only on stdin: never in its arguments.
func TestWriteKeepsSecretsOffArgv(t *testing.T) {
	nt := bytes.Repeat([]byte{0xA5}, 16)
	supp := []byte("secret-kerberos-blob")
	var args []string
	var stdin []byte
	e := &ldbEnv{db: "/var/lib/samba/private/sam.ldb", run: func(_ context.Context, name string, a []string, in []byte) ([]byte, error) {
		args, stdin = append([]string{name}, a...), bytes.Clone(in)
		return nil, nil
	}}
	if err := e.write(context.Background(), "CN=Ana Lima,OU=Matriz,DC=example,DC=net", nt, supp, 133000000000000000); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, s := range []string{hex.EncodeToString(nt), base64.StdEncoding.EncodeToString(nt), string(supp), base64.StdEncoding.EncodeToString(supp)} {
		if strings.Contains(joined, s) {
			t.Fatalf("secret in argv: %q", joined)
		}
	}
	if joined != "ldbmodify -H /var/lib/samba/private/sam.ldb --controls="+bypassControl {
		t.Errorf("argv %q", joined)
	}
	if !bytes.Contains(stdin, []byte("unicodePwd:: "+base64.StdEncoding.EncodeToString(nt))) ||
		!bytes.Contains(stdin, []byte("supplementalCredentials:: "+base64.StdEncoding.EncodeToString(supp))) ||
		!bytes.Contains(stdin, []byte("pwdLastSet: 133000000000000000")) {
		t.Errorf("LDIF:\n%s", stdin)
	}
	// NTLM-only: supplementalCredentials replaced with no value, so no stale key stays
	_ = e.write(context.Background(), "CN=x", nt, nil, 1)
	if !bytes.Contains(stdin, []byte("replace: supplementalCredentials\n-\n")) {
		t.Errorf("NTLM-only LDIF does not clear the Kerberos keys:\n%s", stdin)
	}
}

func TestParseLDIF(t *testing.T) {
	out := "# record 1\ndn: CN=Ana,CN=Users,DC=example,DC=net\nsAMAccountName: ana\nobjectSid: S-1-5-21-1-2-3-1105\n" +
		"unicodePwd:: AAECAwQFBgcICQoL\n DA0ODw==\npwdLastSet: 1\n\n# record 2\ndn:: Q049Sm/Do28sREM9ZXhhbXBsZSxEQz1uZXQ=\n" +
		"sAMAccountName: joao\n\n# Referral\nref: ldap://x\n\n# returned 3 records\n"
	rs, err := parseLDIF([]byte(out))
	if err != nil || len(rs) != 2 {
		t.Fatalf("%d records, %v", len(rs), err)
	}
	if rs[0].first("sAMAccountName") != "ana" || len(rs[0].value("unicodepwd")) != 16 || rs[1].dn != "CN=João,DC=example,DC=net" {
		t.Errorf("parsed %+v", rs)
	}
	if rid, ok := ridOf(rs[0].first("objectSid")); !ok || rid != 1105 {
		t.Errorf("rid %d", rid)
	}
	if dnsOf("DC=example,DC=net") != "example.net" {
		t.Error("dnsOf")
	}
}

// The LDAP listing is not trusted: a reply for another account, or from another domain, stops the run.
func TestMigrateTamperedListing(t *testing.T) {
	for name, tw := range map[string][2]string{
		"another account":        {"bia", "S-1-5-21-1-1-1-1106"},
		"another domain":         {"kerb", "S-1-5-21-6-6-6-1105"},
		"the destination domain": {"kerb", "S-1-5-21-2-2-2-1105"},
	} {
		e := newFake(t)
		e.src.tamper = map[string][2]string{"kerb": tw}
		if _, err := migrate(context.Background(), e, true, PwSource{}, "pw", e.sid); err == nil || len(e.writes) != 0 {
			t.Errorf("%s: err=%v writes=%d", name, err, len(e.writes))
		}
	}
}

// A refusal before any read is missing rights (nothing written); after a read succeeded it is that user's error.
func TestMigrateRightsOnlyBeforeFirstRead(t *testing.T) {
	e := newFake(t)
	e.src.errs["clear"] = errors.New("win32: ERROR_DS_DRA_BAD_DN (0x000020f7)") // "clear" comes after "broken"…
	delete(e.src.errs, "broken")
	r, err := migrate(context.Background(), e, true, PwSource{}, "pw", e.sid)
	if err != nil || outcomes(r)["clear"] != "error" || outcomes(r)["broken"] != "ntlm_only" && outcomes(r)["broken"] != "no_password" {
		t.Fatalf("mid-run BAD_DN: %v %v", outcomes(r), err)
	}
	e = newFake(t)
	e.src.errs["broken"] = errors.New("win32: ERROR_DS_DRA_BAD_DN (0x000020f7)") // "broken" is the first user read
	if _, err := migrate(context.Background(), e, true, PwSource{}, "pw", e.sid); !errors.Is(err, ErrRights) || len(e.writes) != 0 {
		t.Fatalf("first BAD_DN: %v, %d writes", err, len(e.writes))
	}
}

func TestMigrateCancelled(t *testing.T) {
	e := newFake(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := migrate(ctx, e, true, PwSource{}, "pw", e.sid); err == nil || len(e.writes) != 0 {
		t.Fatalf("cancelled run: %v, %d writes", err, len(e.writes))
	}
}

// same needs a non-zero pwdLastSet, the same NT hash and the intended Kerberos state; anything else is written again.
func TestMigrateIdempotency(t *testing.T) {
	_, rebuilt := kerberosBlob(t)
	nt := bytes.Repeat([]byte{0x11}, 16)
	for name, tc := range map[string]struct {
		pls         int64
		state       [2][]byte
		wantOutcome string
	}{
		"migrated":             {1, [2][]byte{nt, rebuilt}, "same"},
		"pwdLastSet zero":      {0, [2][]byte{nt, rebuilt}, "set"},
		"other hash":           {1, [2][]byte{bytes.Repeat([]byte{0x22}, 16), rebuilt}, "set"},
		"stale keys":           {1, [2][]byte{nt, []byte("old keys")}, "set"},
		"NTLM-only (terminal)": {1, [2][]byte{nt, nil}, "same"},
	} {
		e := newFake(t)
		e.local["same"] = localUser{DN: "CN=same", PwdLastSet: tc.pls}
		e.state["CN=same"] = tc.state
		r, err := migrate(context.Background(), e, true, PwSource{}, "pw", e.sid)
		if err != nil || outcomes(r)["same"] != tc.wantOutcome {
			t.Errorf("%s: %s, want %s (%v)", name, outcomes(r)["same"], tc.wantOutcome, err)
		}
	}
}

// The database path comes from Samba's configuration or nothing: no default path behind a failed testparm.
func TestLdbEnvFailsClosed(t *testing.T) {
	for name, run := range map[string]runner{
		"testparm fails": func(context.Context, string, []string, []byte) ([]byte, error) { return nil, errors.New("exit 1") },
		"relative path":  func(context.Context, string, []string, []byte) ([]byte, error) { return []byte("private\n"), nil },
		"no database": func(context.Context, string, []string, []byte) ([]byte, error) {
			return []byte(t.TempDir() + "\n"), nil
		},
	} {
		if e, err := newLdbEnvWith(context.Background(), run); err == nil {
			t.Errorf("%s: accepted %s", name, e.db)
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/sam.ldb", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	e, err := newLdbEnvWith(context.Background(), func(context.Context, string, []string, []byte) ([]byte, error) { return []byte(dir + "/\n"), nil })
	if err != nil || e.db != dir+"/sam.ldb" {
		t.Fatalf("%v %v", e, err)
	}
}

// Malformed ldbsearch output is an error, never "no credentials" (which could read as a false "same").
func TestLdbOutputErrors(t *testing.T) {
	for _, out := range []string{"dn: CN=a\nunicodePwd:: !!notbase64\n", "dn: CN=a\ngarbage line\n"} {
		if _, err := parseLDIF([]byte(out)); err == nil {
			t.Errorf("accepted %q", out)
		}
	}
	e := &ldbEnv{db: "/x", run: func(context.Context, string, []string, []byte) ([]byte, error) {
		return []byte("dn: CN=a\nunicodePwd:: !!\n"), nil
	}}
	if _, _, err := e.currentState(context.Background(), "CN=a"); err == nil {
		t.Error("currentState accepted malformed output")
	}
	// a write refused by Samba gives an allowlisted reason, never the tool's text (which might quote anything)
	e.run = func(context.Context, string, []string, []byte) ([]byte, error) {
		return nil, errors.New("ldbmodify: exit status 1: SENTINEL-SECRET supplementalCredentialsBlob memcmp")
	}
	err := e.write(context.Background(), "CN=a", []byte("n"), nil, 1)
	if err == nil || strings.Contains(reasonOf(err), "SENTINEL") {
		t.Errorf("reason %q", reasonOf(err))
	}
}
