//go:build linux

package sambadc

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
	krbclient "github.com/jcmturner/gokrb5/v8/client"
	krbconfig "github.com/jcmturner/gokrb5/v8/config"

	"ad-manager/internal/ad"
	"ad-manager/internal/gosamba/drs"
	"ad-manager/internal/gosamba/samdb"
	"ad-manager/internal/winrpc"
)

// bypassControl is DSDB_CONTROL_BYPASS_PASSWORD_HASH_OID as ldb's tools take it: honoured only on the local
// database (over LDAP Samba refuses an NT hash: "it's not allowed to set the NT hash password directly").
const bypassControl = "local_oid:1.3.6.1.4.1.7165.4.3.12:0"

// runner runs one of Samba's ldb tools with stdin and returns its stdout; its stderr goes only into the error.
type runner func(ctx context.Context, name string, args []string, stdin []byte) ([]byte, error)

func execRunner(ctx context.Context, name string, args []string, stdin []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8"}
	cmd.Dir = "/"
	cmd.Stdin = bytes.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		// stderr is Samba's message (no secret is ever on the command line or in the LDIF it echoes); callers only
		// ever map it to an allowlisted reason
		return nil, fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

// ldbEnv is pwEnv on this DC: the local sam.ldb through ldbsearch/ldbmodify, the source over LDAP and DRSUAPI.
type ldbEnv struct {
	run runner
	db  string // the local sam.ldb
}

// newLdbEnv finds the local sam.ldb in Samba's own configuration (the private dir smb.conf names, as samba-tool
// opens it). It fails closed: no configuration or no database there is an error, never a default path that may hold
// another database.
func newLdbEnv(ctx context.Context) (pwEnv, error) { return newLdbEnvWith(ctx, execRunner) }

func newLdbEnvWith(ctx context.Context, run runner) (*ldbEnv, error) {
	e := &ldbEnv{run: run}
	out, err := e.run(ctx, "testparm", []string{"-s", "--parameter-name=private dir"}, nil)
	d := strings.TrimSpace(string(out))
	if err != nil || !strings.HasPrefix(d, "/") || strings.ContainsAny(d, "\n\x00") {
		return nil, errors.Join(errors.New("Samba's private dir is unknown (testparm)"), err)
	}
	e.db = strings.TrimSuffix(d, "/") + "/sam.ldb"
	if st, err := os.Stat(e.db); err != nil || !st.Mode().IsRegular() {
		return nil, fmt.Errorf("no Samba database at %s", e.db)
	}
	return e, nil
}

func (e *ldbEnv) search(ctx context.Context, base, scope, filter string, attrs ...string) ([]ldifRecord, error) {
	args := append([]string{"-H", e.db, "-s", scope, "-b", base, filter}, attrs...)
	out, err := e.run(ctx, "ldbsearch", args, nil)
	if err != nil {
		return nil, err
	}
	return parseLDIF(out)
}

func (e *ldbEnv) localDomain(ctx context.Context) (string, string, string, error) {
	r, err := e.search(ctx, "", "base", "(objectClass=*)", "defaultNamingContext")
	if err != nil || len(r) != 1 || r[0].first("defaultNamingContext") == "" {
		return "", "", "", errors.Join(errors.New("no default naming context"), err)
	}
	base := r[0].first("defaultNamingContext")
	d, err := e.search(ctx, base, "base", "(objectClass=*)", "objectSid")
	if err != nil || len(d) != 1 || !strings.HasPrefix(d[0].first("objectSid"), "S-1-5-21-") {
		return "", "", "", errors.Join(errors.New("no domain SID"), err)
	}
	return d[0].first("objectSid"), strings.ToUpper(dnsOf(base)), base, nil
}

func (e *ldbEnv) localUsers(ctx context.Context, base string) (map[string]localUser, error) {
	rs, err := e.search(ctx, base, "sub", "(sAMAccountName=*)", "sAMAccountName", "objectSid", "sAMAccountType", "pwdLastSet")
	if err != nil {
		return nil, err
	}
	out := make(map[string]localUser, len(rs))
	for _, r := range rs {
		sam := r.first("sAMAccountName")
		if sam == "" {
			continue
		}
		rid, ok := ridOf(r.first("objectSid"))
		atype, _ := strconv.ParseUint(r.first("sAMAccountType"), 10, 32)
		pls, _ := strconv.ParseInt(r.first("pwdLastSet"), 10, 64)
		out[strings.ToLower(sam)] = localUser{DN: r.dn, PwdLastSet: pls,
			Protected: !ok || protectedRIDs[rid] || atype == samMachine || atype == samTrust || atype == samInterdom}
	}
	return out, nil
}

func (e *ldbEnv) currentState(ctx context.Context, dn string) ([]byte, []byte, error) {
	out, err := e.run(ctx, "ldbsearch", []string{"-H", e.db, "-s", "base", "-b", dn, "(objectClass=*)", "unicodePwd", "supplementalCredentials"}, nil)
	defer clear(out) // the values in base64
	if err != nil {
		return nil, nil, err
	}
	rs, err := parseLDIF(out)
	if err != nil || len(rs) != 1 {
		return nil, nil, errors.Join(errors.New("user not found"), err)
	}
	return rs[0].value("unicodePwd"), rs[0].value("supplementalCredentials"), nil
}

func (e *ldbEnv) write(ctx context.Context, dn string, nt, supp []byte, pwdLastSet int64) error {
	ldif := modifyLDIF(dn, nt, supp, pwdLastSet)
	defer clear(ldif)
	_, err := e.run(ctx, "ldbmodify", []string{"-H", e.db, "--controls=" + bypassControl}, ldif)
	return err
}

// modifyLDIF is the one-record LDIF ldbmodify reads on stdin: the NT hash, the Kerberos blob (or none, so no stale
// keys stay), an explicit pwdLastSet. Values and the DN go base64 (::), encoded straight into the buffer the caller
// clears (no string copy of a secret).
func modifyLDIF(dn string, nt, supp []byte, pwdLastSet int64) []byte {
	enc := base64.StdEncoding
	b := make([]byte, 0, 256+len(dn)*2+enc.EncodedLen(len(nt))+enc.EncodedLen(len(supp)))
	b64 := func(v []byte) {
		n := len(b)
		b = append(b, make([]byte, enc.EncodedLen(len(v)))...)
		enc.Encode(b[n:], v)
	}
	b = append(b, "dn:: "...)
	b64([]byte(dn))
	b = append(b, "\nchangetype: modify\nreplace: unicodePwd\nunicodePwd:: "...)
	b64(nt)
	b = append(b, "\n-\nreplace: supplementalCredentials\n"...)
	if len(supp) > 0 {
		b = append(b, "supplementalCredentials:: "...)
		b64(supp)
		b = append(b, '\n')
	}
	b = append(b, "-\nreplace: pwdLastSet\npwdLastSet: "...)
	b = strconv.AppendInt(b, pwdLastSet, 10)
	return append(b, "\n-\n"...)
}

// ldifRecord is one entry of ldbsearch's output.
type ldifRecord struct {
	dn    string
	attrs map[string][][]byte
}

func (r ldifRecord) value(name string) []byte {
	for k, v := range r.attrs {
		if strings.EqualFold(k, name) && len(v) > 0 {
			return v[0]
		}
	}
	return nil
}

func (r ldifRecord) first(name string) string { return string(r.value(name)) }

// parseLDIF reads ldbsearch's LDIF: folded lines (a leading space continues the previous one), "name: text" and
// "name:: base64" values, "#" comments, records separated by blank lines.
func parseLDIF(out []byte) ([]ldifRecord, error) {
	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		l := strings.TrimSuffix(sc.Text(), "\r")
		if strings.HasPrefix(l, " ") && len(lines) > 0 && lines[len(lines)-1] != "" {
			lines[len(lines)-1] += l[1:]
			continue
		}
		lines = append(lines, l)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	var recs []ldifRecord
	var cur *ldifRecord
	for _, l := range append(lines, "") {
		if l == "" || strings.HasPrefix(l, "#") {
			if l == "" && cur != nil {
				recs = append(recs, *cur)
				cur = nil
			}
			continue
		}
		name, val, ok := strings.Cut(l, ":")
		if !ok {
			return nil, fmt.Errorf("ldif: unexpected line")
		}
		var v []byte
		if strings.HasPrefix(val, ":") {
			d, err := base64.StdEncoding.DecodeString(strings.TrimSpace(val[1:]))
			if err != nil {
				return nil, fmt.Errorf("ldif: bad base64 in %s", name)
			}
			v = d
		} else {
			v = []byte(strings.TrimPrefix(val, " "))
		}
		if strings.EqualFold(name, "dn") {
			if cur != nil {
				recs = append(recs, *cur)
			}
			cur = &ldifRecord{dn: string(v), attrs: map[string][][]byte{}}
			continue
		}
		if cur == nil {
			continue // a referral line or anything outside an entry
		}
		cur.attrs[name] = append(cur.attrs[name], v)
	}
	return recs, nil
}

// dnsOf turns DC=example,DC=local into example.local.
func dnsOf(dn string) string {
	var parts []string
	for _, rdn := range strings.Split(dn, ",") {
		if k, v, ok := strings.Cut(strings.TrimSpace(rdn), "="); ok && strings.EqualFold(k, "DC") {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, ".")
}

// ldapSource is the source domain: users and SID over LDAP (ldap:// + NTLM, as a 2008 DC needs), secrets over a
// Kerberos-sealed DRSUAPI to the same address.
type ldapSource struct {
	sid, realm string
	users      []srcUser
	reader     *drs.Reader
}

func (s *ldapSource) SID() string      { return s.sid }
func (s *ldapSource) Realm() string    { return s.realm }
func (s *ldapSource) Users() []srcUser { return s.users }
func (s *ldapSource) Secrets(ctx context.Context, guid []byte) (*drs.Secrets, error) {
	return s.reader.Secrets(ctx, guid)
}
func (s *ldapSource) Close() { _ = s.reader.Close(context.Background()) }

func (e *ldbEnv) openSource(ctx context.Context, src PwSource, password string) (source, error) {
	host := src.Host
	if net.ParseIP(host) == nil && (host == "" || strings.ContainsAny(host, "/:@ \\")) {
		return nil, errors.New("invalid source host")
	}
	// rootDSE (anonymous): the domain and the DC's DNS name, which the Kerberos bind needs as its SPN host
	l, err := ldap.DialURL("ldap://"+net.JoinHostPort(host, "389"), ldap.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}))
	if err != nil {
		return nil, err
	}
	l.SetTimeout(30 * time.Second)
	stop := context.AfterFunc(ctx, func() { l.Close() })
	root, err := l.Search(ldap.NewSearchRequest("", ldap.ScopeBaseObject, ldap.NeverDerefAliases, 0, 30, false,
		"(objectClass=*)", []string{"defaultNamingContext", "dnsHostName", "dsServiceName"}, nil))
	stop()
	l.Close()
	if err != nil || len(root.Entries) != 1 {
		return nil, errors.Join(errors.New("source rootDSE unreadable"), ctx.Err(), err)
	}
	base, dcName := root.Entries[0].GetAttributeValue("defaultNamingContext"), root.Entries[0].GetAttributeValue("dnsHostName")
	dsa := root.Entries[0].GetAttributeValue("dsServiceName")
	domain := dnsOf(base)
	if domain == "" || dcName == "" {
		return nil, errors.New("source rootDSE without a domain or DNS name")
	}
	_, sam := ad.BindName(src.User, domain)
	c, err := sourceLDAP(host, dcName, strings.ToUpper(domain), sam, password)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	defer context.AfterFunc(ctx, func() { c.Close() })() // a cancelled run does not wait for the listing
	// the listing only proposes accounts: each one's name and SID are checked again in its sealed DRS reply
	d, err := c.Search(ldap.NewSearchRequest(base, ldap.ScopeBaseObject, ldap.NeverDerefAliases, 0, 30, false,
		"(objectClass=*)", []string{"objectSid"}, nil))
	if err != nil || len(d.Entries) != 1 {
		return nil, errors.Join(errors.New("source domain SID unreadable"), err)
	}
	sid := samdb.SID(d.Entries[0].GetRawAttributeValue("objectSid"))
	if !strings.HasPrefix(sid, "S-1-5-21-") {
		return nil, errors.New("source domain SID unreadable")
	}
	us, err := c.SearchWithPaging(ldap.NewSearchRequest(base, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		"(sAMAccountType=805306368)", []string{"sAMAccountName", "objectSid", "objectGUID"}, nil), 500)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// the DRS bind's service principal is the DC's replication SPN (its NTDS Settings GUID), which only a DC of this
	// domain holds: the rootDSE that named it is anonymous, but a ticket for that SPN is useless to anyone else
	nt, err := c.Search(ldap.NewSearchRequest(dsa, ldap.ScopeBaseObject, ldap.NeverDerefAliases, 0, 30, false,
		"(objectClass=nTDSDSA)", []string{"objectGUID"}, nil))
	if err != nil || len(nt.Entries) != 1 || len(nt.Entries[0].GetRawAttributeValue("objectGUID")) != 16 {
		return nil, errors.Join(errors.New("source DC's NTDS Settings unreadable"), err)
	}
	spn := "E3514235-4B06-11D1-AB04-00C04FC2DCD2/" + samdb.GUID(nt.Entries[0].GetRawAttributeValue("objectGUID")) + "/" + domain
	s := &ldapSource{sid: sid, realm: strings.ToUpper(domain)}
	for _, en := range us.Entries {
		rid, ok := samdb.RID(en.GetRawAttributeValue("objectSid"))
		guid := en.GetRawAttributeValue("objectGUID")
		if !ok || len(guid) != 16 || protectedRIDs[rid] {
			continue
		}
		s.users = append(s.users, srcUser{Sam: en.GetAttributeValue("sAMAccountName"), GUID: guid})
	}
	s.reader, err = drs.Dial(ctx, winrpc.Target{Addr: host, Host: dcName, SPN: spn},
		winrpc.Cred{Realm: strings.ToUpper(domain), KDC: net.JoinHostPort(host, "88"), User: sam, Password: password})
	if err != nil {
		return nil, err
	}
	return s, nil
}

// sourceLDAP binds without the password ever travelling: NTLM over plain LDAP, as a 2008 DC takes it; else Kerberos
// (GSSAPI) over LDAPS, as Samba's "ldap server require strong auth" and a Windows DC with LDAP signing want (go-ldap's
// NTLM bind is Microsoft's own, which Samba does not offer). The certificate is not checked: the listing is only a
// proposal, verified account by account in the sealed DRS reply.
func sourceLDAP(host, dcName, realm, sam, password string) (*ldap.Conn, error) {
	dialer := ldap.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second})
	var errs []error
	if l, err := ldap.DialURL("ldap://"+net.JoinHostPort(host, "389"), dialer); err != nil {
		errs = append(errs, err)
	} else {
		l.SetTimeout(60 * time.Second)
		if err = l.NTLMBind(realm, sam, password); err == nil {
			return l, nil
		}
		l.Close()
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			return nil, errors.New("source authentication failed")
		}
		errs = append(errs, err)
	}
	l, err := ldap.DialURL("ldaps://"+net.JoinHostPort(host, "636"), dialer, ldap.DialWithTLSConfig(&tls.Config{InsecureSkipVerify: true})) //nolint:gosec // see above
	if err != nil {
		return nil, errors.Join(append([]error{errors.New("source LDAP unreachable")}, append(errs, err)...)...)
	}
	l.SetTimeout(60 * time.Second)
	cfg := krbconfig.New()
	cfg.LibDefaults.DefaultRealm = realm
	cfg.LibDefaults.DNSLookupKDC = false
	cfg.LibDefaults.UDPPreferenceLimit = 1 // TCP
	cfg.Realms = []krbconfig.Realm{{Realm: realm, KDC: []string{net.JoinHostPort(host, "88")}}}
	cl := krbclient.NewWithPassword(sam, realm, password, cfg, krbclient.DisablePAFXFAST(true))
	defer cl.Destroy()
	st, ok := l.TLSConnectionState()
	if !ok || len(st.PeerCertificates) == 0 {
		l.Close()
		return nil, errors.New("source LDAPS without a certificate")
	}
	gc, err := newCBGSSAPI(cl, st.PeerCertificates[0])
	if err == nil {
		err = l.GSSAPIBind(gc, "ldap/"+dcName, "")
	}
	if err != nil {
		l.Close()
		return nil, errors.Join(append([]error{errors.New("source LDAP bind failed")}, append(errs, err)...)...)
	}
	return l, nil
}
