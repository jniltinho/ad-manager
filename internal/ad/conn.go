// Package ad talks to Active Directory (Windows or Samba) over LDAP(S),
// always with the identity of the operator. Every protection rule lives here,
// on the server side, not only in the UI.
package ad

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
)

// Config describes how to reach the domain.
type Config struct {
	URL                string `toml:"url"`                  // ldaps://host or ldap://host
	Domain             string `toml:"domain"`               // example.local
	BaseDN             string `toml:"base_dn"`              // empty means derived from Domain
	StartTLS           bool   `toml:"start_tls"`            // with ldap://
	CAFile             string `toml:"ca_file"`              // CA of the DC certificate
	InsecureSkipVerify bool   `toml:"insecure_skip_verify"` // lab only
	AdminGroup         string `toml:"admin_group"`          // group allowed to use the panel
}

// NetBIOS returns the domain's short name (EXAMPLE), used in winbind group
// names (EXAMPLE\Finance). It is read from the domain's crossRef; without
// access it falls back to the first label of the DNS name.
func (c *Conn) NetBIOS() string {
	root, err := c.search("", ldap.ScopeBaseObject, "(objectClass=*)", []string{"configurationNamingContext"})
	if err == nil && len(root) > 0 {
		es, err := c.search("CN=Partitions,"+root[0].GetAttributeValue("configurationNamingContext"), ldap.ScopeSingleLevel,
			fmt.Sprintf("(&(objectClass=crossRef)(nCName=%s))", ldap.EscapeFilter(c.cfg.Base())), []string{"nETBIOSName"})
		if err == nil && len(es) > 0 && es[0].GetAttributeValue("nETBIOSName") != "" {
			return es[0].GetAttributeValue("nETBIOSName")
		}
	}
	return strings.ToUpper(strings.SplitN(c.cfg.Domain, ".", 2)[0])
}

// Base returns the domain's root DN.
func (c *Config) Base() string {
	if c.BaseDN != "" {
		return c.BaseDN
	}
	parts := strings.Split(c.Domain, ".")
	for i, p := range parts {
		parts[i] = "DC=" + p
	}
	return strings.Join(parts, ",")
}

// Errors that the CLI and the API translate into messages and HTTP codes.
var (
	ErrNotFound   = errors.New("object not found")
	ErrConflict   = errors.New("an object with this name already exists")
	ErrProtected  = errors.New("protected system object: operation refused")
	ErrNotEmpty   = errors.New("the OU is not empty")
	ErrNeedsTLS   = errors.New("changes require a TLS connection (ldaps:// or start_tls); without TLS it is read-only")
	ErrBadLogin   = errors.New("invalid user or password")
	ErrPwdPolicy  = errors.New("the password does not meet the domain policy")
	ErrWrongInput = errors.New("invalid data")
)

// Conn is an authenticated connection.
type Conn struct {
	l   *ldap.Conn
	cfg *Config
	tls bool
	Me  string // sAMAccountName of whoever bound
}

// BindName normalizes "user", "user@domain" and "DOM\user" into the implicit
// UPN used for the bind, and also returns the sAMAccountName.
func BindName(login, domain string) (bind, sam string) {
	login = strings.TrimSpace(login)
	if i := strings.LastIndex(login, `\`); i >= 0 {
		login = login[i+1:]
	}
	if i := strings.Index(login, "@"); i >= 0 {
		return login, login[:i]
	}
	return login + "@" + domain, login
}

// Dial connects and binds with the operator's credentials.
func Dial(cfg *Config, login, password string) (*Conn, error) {
	if password == "" {
		return nil, ErrBadLogin // a bind without password becomes an anonymous bind in AD
	}
	tc := &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify} //nolint:gosec // explicit opt-in for the lab
	if u := strings.TrimPrefix(strings.TrimPrefix(cfg.URL, "ldaps://"), "ldap://"); u != "" {
		host, _, err := net.SplitHostPort(u)
		if err != nil {
			host = u
		}
		tc.ServerName = host
	}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read ca_file: %w", err)
		}
		tc.RootCAs = x509.NewCertPool()
		tc.RootCAs.AppendCertsFromPEM(pem)
	}

	l, err := ldap.DialURL(cfg.URL,
		ldap.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}),
		ldap.DialWithTLSConfig(tc))
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", cfg.URL, err)
	}
	l.SetTimeout(30 * time.Second)
	secure := strings.HasPrefix(cfg.URL, "ldaps://")
	if !secure && cfg.StartTLS {
		if err := l.StartTLS(tc); err != nil {
			l.Close()
			return nil, fmt.Errorf("starttls: %w", err)
		}
		secure = true
	}
	bind, sam := BindName(login, cfg.Domain)
	doBind := func() error { return l.Bind(bind, password) }
	if !secure {
		// Without TLS (e.g. a legacy DC without a certificate): NTLM, so the
		// password never travels in clear text. Read-only in practice: writes
		// require TLS.
		doBind = func() error { return l.NTLMBind(cfg.Domain, sam, password) }
	}
	if err := doBind(); err != nil {
		l.Close()
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			return nil, ErrBadLogin
		}
		return nil, fmt.Errorf("bind: %w", err)
	}
	// The UPN may differ from the login (sAMAccountName): ask the server who we are.
	if who, err := l.WhoAmI(nil); err == nil {
		if i := strings.LastIndex(who.AuthzID, `\`); i >= 0 {
			sam = who.AuthzID[i+1:]
		}
	}
	return &Conn{l: l, cfg: cfg, tls: secure, Me: sam}, nil
}

// Close closes the connection.
func (c *Conn) Close() { c.l.Close() }

// Domain returns the configured DNS domain.
func (c *Conn) Domain() string { return c.cfg.Domain }

// add, modify, del and modDN are the only write paths: without TLS the
// connection is read-only (e.g. a legacy DC via NTLM).
func (c *Conn) add(r *ldap.AddRequest) error {
	if !c.tls {
		return ErrNeedsTLS
	}
	return c.l.Add(r)
}

func (c *Conn) modify(r *ldap.ModifyRequest) error {
	if !c.tls {
		return ErrNeedsTLS
	}
	return c.l.Modify(r)
}

func (c *Conn) del(r *ldap.DelRequest) error {
	if !c.tls {
		return ErrNeedsTLS
	}
	return c.l.Del(r)
}

func (c *Conn) modDN(r *ldap.ModifyDNRequest) error {
	if !c.tls {
		return ErrNeedsTLS
	}
	return c.l.ModifyDN(r)
}

// search runs a paged search (AD truncates at 1000 entries without paging).
func (c *Conn) search(base string, scope int, filter string, attrs []string) ([]*ldap.Entry, error) {
	req := ldap.NewSearchRequest(base, scope, ldap.NeverDerefAliases, 0, 0, false, filter, attrs, nil)
	res, err := c.l.SearchWithPaging(req, 500)
	if err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultNoSuchObject) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return res.Entries, nil
}

// one fetches exactly one object.
func (c *Conn) one(filter string, attrs []string) (*ldap.Entry, error) {
	es, err := c.search(c.cfg.Base(), ldap.ScopeWholeSubtree, filter, attrs)
	if err != nil {
		return nil, err
	}
	if len(es) == 0 {
		return nil, ErrNotFound
	}
	return es[0], nil
}

// mapErr translates common LDAP result codes into the package errors.
func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case ldap.IsErrorWithCode(err, ldap.LDAPResultEntryAlreadyExists):
		return ErrConflict
	case ldap.IsErrorWithCode(err, ldap.LDAPResultNoSuchObject):
		return ErrNotFound
	case ldap.IsErrorWithCode(err, ldap.LDAPResultNotAllowedOnNonLeaf):
		return ErrNotEmpty
	}
	return err
}

// pwdErr is mapErr for password operations: a constraint violation means the
// password policy rejected it.
func pwdErr(err error) error {
	if ldap.IsErrorWithCode(err, ldap.LDAPResultConstraintViolation) || strings.Contains(fmt.Sprint(err), "0000052D") {
		return ErrPwdPolicy
	}
	return mapErr(err)
}

// IsAdmin reports whether the logged-in operator is a member (direct or
// nested) of the administrative group. Without admin_group configured it uses
// the SID <domain>-512 (Domain Admins), read from tokenGroups — independent of
// the group's name.
func (c *Conn) IsAdmin() (bool, error) {
	if c.cfg.AdminGroup != "" {
		g, err := c.one(fmt.Sprintf("(&(objectClass=group)(sAMAccountName=%s))", ldap.EscapeFilter(c.cfg.AdminGroup)), []string{"dn"})
		if err != nil {
			return false, fmt.Errorf("administrative group %q: %w", c.cfg.AdminGroup, err)
		}
		_, err = c.one(fmt.Sprintf("(&(sAMAccountName=%s)(memberOf:1.2.840.113556.1.4.1941:=%s))",
			ldap.EscapeFilter(c.Me), ldap.EscapeFilter(g.DN)), []string{"dn"})
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return err == nil, err
	}
	u, err := c.object(c.Me)
	if err != nil {
		return false, err
	}
	es, err := c.search(u.DN, ldap.ScopeBaseObject, "(objectClass=*)", []string{"tokenGroups"})
	if err != nil || len(es) == 0 {
		return false, err
	}
	dom, err := c.search(c.cfg.Base(), ldap.ScopeBaseObject, "(objectClass=*)", []string{"objectSid"})
	if err != nil {
		return false, err
	}
	want := append(append([]byte{}, dom[0].GetRawAttributeValue("objectSid")...), 0, 2, 0, 0) // RID 512 LE
	want[1]++                                                                                 // one more sub-authority
	for _, sid := range es[0].GetRawAttributeValues("tokenGroups") {
		if string(sid) == string(want) {
			return true, nil
		}
	}
	return false, nil
}

// DCHostName is the DNS name of the domain controller this connection talks to (rootDSE dnsHostName): the name its
// Kerberos service principals use.
func (c *Conn) DCHostName() (string, error) {
	es, err := c.search("", ldap.ScopeBaseObject, "(objectClass=*)", []string{"dnsHostName"})
	if err != nil {
		return "", err
	}
	if len(es) == 0 || es[0].GetAttributeValue("dnsHostName") == "" {
		return "", fmt.Errorf("%w: rootDSE without dnsHostName", ErrWrongInput)
	}
	return es[0].GetAttributeValue("dnsHostName"), nil
}
