package ad

import (
	"fmt"
	"net"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
)

// Check is one item of the preflight check.
type Check struct {
	Item   string `json:"item"`
	Status string `json:"status"` // ok, StatusWarning or StatusBlocker
	Detail string `json:"detail"`
}

// Check statuses besides "ok".
const (
	StatusWarning = "warning"
	StatusBlocker = "blocker"
)

// Ports probes a DC's ports (no credentials needed).
func Ports(host string) []Check {
	var out []Check
	for _, p := range []struct {
		port int
		name string
	}{{53, "DNS"}, {88, "Kerberos"}, {135, "RPC"}, {389, "LDAP"}, {445, "SMB"}, {636, "LDAPS"}, {3268, "Global catalog"}} {
		c := Check{Item: fmt.Sprintf("port %d (%s)", p.port, p.name), Status: "ok", Detail: "open"}
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(p.port)), 3*time.Second)
		if err != nil {
			c.Status, c.Detail = StatusBlocker, err.Error()
			if p.port == 636 || p.port == 3268 {
				c.Status = StatusWarning
			}
		} else {
			conn.Close()
		}
		out = append(out, c)
	}
	return out
}

var levels = map[string]string{"0": "2000", "1": "2003 interim", "2": "2003", "3": "2008", "4": "2008 R2",
	"5": "2012", "6": "2012 R2", "7": "2016"}

var schemaVersions = map[string]string{"13": "2000", "30": "2003", "31": "2003 R2", "44": "2008", "47": "2008 R2",
	"56": "2012", "69": "2012 R2", "87": "2016", "88": "2019/2022"}

// Preflight reads the domain state relevant to joining Samba as a DC.
func (c *Conn) Preflight() ([]Check, error) {
	base := c.cfg.Base()
	root, err := c.search("", ldap.ScopeBaseObject, "(objectClass=*)",
		[]string{"configurationNamingContext", "schemaNamingContext", "domainFunctionality", "forestFunctionality", "dnsHostName"})
	if err != nil {
		return nil, err
	}
	r := root[0]
	cfgNC, schemaNC := r.GetAttributeValue("configurationNamingContext"), r.GetAttributeValue("schemaNamingContext")
	out := []Check{{Item: "authentication", Status: "ok", Detail: c.Me + " on " + r.GetAttributeValue("dnsHostName")}}

	lvl := func(item, v string) {
		st := "ok"
		if n, _ := strconv.Atoi(v); n < 3 { // rehearsal: Samba 4.23 replicated a 2008 domain; anything lower is untested
			st = StatusBlocker
		}
		out = append(out, Check{Item: item, Status: st, Detail: levels[v] + " (" + v + ")"})
	}
	lvl("domain functional level", r.GetAttributeValue("domainFunctionality"))
	lvl("forest functional level", r.GetAttributeValue("forestFunctionality"))

	if ck, ok := c.rc4Only(base); ok {
		out = append(out, ck)
	}

	sv, err := c.search(schemaNC, ldap.ScopeBaseObject, "(objectClass=*)", []string{"objectVersion"})
	if err == nil {
		v := sv[0].GetAttributeValue("objectVersion")
		out = append(out, Check{Item: "schema", Status: "ok", Detail: schemaVersions[v] + " (objectVersion " + v + ")"})
	}
	if ex, err := c.search(schemaNC, ldap.ScopeSingleLevel, "(cn=ms-Exch-Schema-Version-Pt)", []string{"rangeUpper"}); err == nil {
		st, d := "ok", "absent"
		if len(ex) > 0 {
			st, d = StatusWarning, "present (rangeUpper "+ex[0].GetAttributeValue("rangeUpper")+"): validate it in the rehearsal"
		}
		out = append(out, Check{Item: "schema Exchange", Status: st, Detail: d})
	}

	for _, f := range []struct{ role, dn string }{
		{"FSMO PDC", base}, {"FSMO RID", "CN=RID Manager$,CN=System," + base},
		{"FSMO Infrastructure", "CN=Infrastructure," + base}, {"FSMO Schema", schemaNC},
		{"FSMO Domain naming", "CN=Partitions," + cfgNC},
	} {
		es, err := c.search(f.dn, ldap.ScopeBaseObject, "(objectClass=*)", []string{"fSMORoleOwner"})
		if err != nil || len(es) == 0 {
			out = append(out, Check{Item: f.role, Status: StatusWarning, Detail: fmt.Sprint(err)})
			continue
		}
		out = append(out, Check{Item: f.role, Status: "ok", Detail: serverOf(es[0].GetAttributeValue("fSMORoleOwner"))})
	}

	dcs, err := c.search("CN=Sites,"+cfgNC, ldap.ScopeWholeSubtree, "(objectClass=nTDSDSA)", []string{"dn"})
	if err == nil {
		names := make([]string, 0, len(dcs))
		for _, d := range dcs {
			names = append(names, serverOf(d.DN))
		}
		st := "ok"
		if len(dcs) == 1 {
			st = StatusWarning // no redundancy: the Samba join is the first extra DC
		}
		out = append(out, Check{Item: "domain controllers", Status: st, Detail: strings.Join(names, ", ")})
	}
	return out, nil
}

// rc4Only finds the enabled users whose password predates the domain's AES Kerberos keys: a password set before the
// domain reached the 2008 level (or while the account was still local, before dcpromo) has only the NT hash, so
// RC4 is its only Kerberos key — on the source and, after a dc-join or a password migration, on Samba too. Ubuntu's
// MIT Kerberos refuses RC4 by default. Microsoft's heuristic: pwdLastSet older than the creation of the Read-only
// Domain Controllers group (RID 521), created when a 2008 DC first holds the PDC role — a heuristic: a password set
// later on a remaining 2003 DC is missed. Users that must change their password (pwdLastSet 0) are left out: they
// get new keys at their next logon. ok is false when that group is unreadable (a 2003 domain is a blocker already).
func (c *Conn) rc4Only(base string) (Check, bool) {
	sid, err := c.DomainSID()
	if err != nil {
		return Check{}, false
	}
	g, err := c.search(base, ldap.ScopeWholeSubtree, "(objectSid="+sid+"-521)", []string{"whenCreated"})
	if err != nil || len(g) == 0 {
		return Check{}, false
	}
	since := genTime(g[0].GetAttributeValue("whenCreated"))
	if since.IsZero() {
		return Check{}, false
	}
	// Key Admins (RID 526) appears when a 2016 DC holds the PDC role: created with the RODC group, the domain was born
	// on 2016 or later, whose local accounts already carry AES keys before dcpromo (checked on the 2019 and 2022 labs)
	if ka, err := c.search(base, ldap.ScopeWholeSubtree, "(objectSid="+sid+"-526)", []string{"whenCreated"}); err == nil &&
		len(ka) > 0 && bornOn2016(since, genTime(ka[0].GetAttributeValue("whenCreated"))) {
		return Check{Item: "RC4-only accounts", Status: "ok", Detail: "none: the domain was created by Windows Server 2016 or later"}, true
	}
	us, err := c.search(base, ldap.ScopeWholeSubtree, "(&(objectCategory=person)(objectClass=user)"+
		"(!(userAccountControl:1.2.840.113556.1.4.803:=2))(pwdLastSet>=1)(pwdLastSet<="+toFileTime(since)+"))",
		[]string{"sAMAccountName"})
	if err != nil {
		return Check{}, false
	}
	names := make([]string, 0, len(us))
	for _, u := range us {
		names = append(names, u.GetAttributeValue("sAMAccountName"))
	}
	return rc4Check(names), true
}

// bornOn2016 reports whether the Key Admins group was created together with the RODC group (the same dcpromo, within
// the hour): the domain's first DC was Windows Server 2016 or later.
func bornOn2016(rodc, keyAdmins time.Time) bool {
	return !keyAdmins.IsZero() && keyAdmins.Sub(rodc).Abs() < time.Hour
}

// rc4Check is the preflight row for the users with RC4-only Kerberos keys.
func rc4Check(names []string) Check {
	if len(names) == 0 {
		return Check{Item: "RC4-only accounts", Status: "ok", Detail: "none found: no enabled user's password predates the domain's 2008 level"}
	}
	sort.Strings(names)
	shown := names
	if len(shown) > 10 {
		shown = shown[:10]
	}
	d := fmt.Sprintf("%d enabled user(s) whose password predates the domain's 2008 level (likely RC4-only Kerberos keys): %s",
		len(names), strings.Join(shown, ", "))
	if len(names) > len(shown) {
		d += ", …"
	}
	return Check{Item: "RC4-only accounts", Status: StatusWarning,
		Detail: d + "; reset their passwords before the cutover (Ubuntu's Kerberos refuses RC4)"}
}

// serverOf extracts the server name from "CN=NTDS Settings,CN=ADM,CN=Servers,...".
func serverOf(dn string) string {
	d, err := ldap.ParseDN(dn)
	if err != nil || len(d.RDNs) < 2 {
		return dn
	}
	return d.RDNs[1].Attributes[0].Value
}

// Inventory is the domain snapshot used to compare source and destination. It
// contains no password attribute.
type Inventory struct {
	Domain    string              `json:"domain"`
	Source    string              `json:"source"`
	Taken     time.Time           `json:"taken"`
	Counts    map[string]int      `json:"counts"`
	Users     []User              `json:"users"`
	Groups    []Group             `json:"groups"`
	OUs       []OU                `json:"ous"`
	Computers []Computer          `json:"computers"`
	Policy    *Policy             `json:"policy"`
	Members   map[string][]string `json:"members"` // group → logins
}

// Inventory collects users, groups (with members), OUs, computers and the
// password policy.
func (c *Conn) Inventory(source string) (*Inventory, error) {
	inv := &Inventory{Domain: c.cfg.Domain, Source: source, Taken: time.Now().UTC(), Members: map[string][]string{}}
	var err error
	if inv.Users, err = c.Users(""); err != nil {
		return nil, err
	}
	if inv.Groups, err = c.Groups(""); err != nil {
		return nil, err
	}
	if inv.OUs, err = c.OUs(); err != nil {
		return nil, err
	}
	if inv.Computers, err = c.Computers(""); err != nil {
		return nil, err
	}
	if inv.Policy, err = c.Policy(); err != nil {
		return nil, err
	}
	// Members: a DN→login index avoids one search per group.
	byDN := map[string]string{}
	all, err := c.search(c.cfg.Base(), ldap.ScopeWholeSubtree, "(sAMAccountName=*)", []string{"sAMAccountName", "memberOf"})
	if err != nil {
		return nil, err
	}
	for _, e := range all {
		byDN[strings.ToLower(e.DN)] = e.GetAttributeValue("sAMAccountName")
	}
	for _, e := range all {
		for _, g := range e.GetAttributeValues("memberOf") {
			if name, ok := byDN[strings.ToLower(g)]; ok {
				inv.Members[name] = append(inv.Members[name], e.GetAttributeValue("sAMAccountName"))
			}
		}
	}
	for k := range inv.Members {
		sort.Strings(inv.Members[k])
	}
	inv.Counts = map[string]int{"users": len(inv.Users), "groups": len(inv.Groups), "ous": len(inv.OUs), "computers": len(inv.Computers)}
	return inv, nil
}

// Diff lists what exists in only one of the inventories (accounts, groups,
// OUs, computers and memberships). An empty list means they are equivalent.
// DCs are ignored (the destination always has different DCs) and OUs are
// compared by DN.
func Diff(a, b *Inventory) []string {
	var out []string
	cmp := func(kind string, x, y []string) {
		for _, s := range x {
			if !slices.Contains(y, s) {
				out = append(out, fmt.Sprintf("%s only in %s: %s", kind, a.Source, s))
			}
		}
		for _, s := range y {
			if !slices.Contains(x, s) {
				out = append(out, fmt.Sprintf("%s only in %s: %s", kind, b.Source, s))
			}
		}
	}
	keys := func(n int, f func(int) string) []string {
		s := make([]string, 0, n)
		for i := range n {
			if k := f(i); k != "" {
				s = append(s, strings.ToLower(k))
			}
		}
		return s
	}
	cmp("user", keys(len(a.Users), func(i int) string { return a.Users[i].Sam + " " + a.Users[i].SID }), keys(len(b.Users), func(i int) string { return b.Users[i].Sam + " " + b.Users[i].SID }))
	cmp("group", keys(len(a.Groups), func(i int) string { return a.Groups[i].Name + " " + a.Groups[i].SID }), keys(len(b.Groups), func(i int) string { return b.Groups[i].Name + " " + b.Groups[i].SID }))
	cmp("OU", keys(len(a.OUs), func(i int) string { return a.OUs[i].DN }), keys(len(b.OUs), func(i int) string { return b.OUs[i].DN }))
	nonDC := func(cs []Computer) func(int) string {
		return func(i int) string {
			if cs[i].IsDC {
				return ""
			}
			return cs[i].Name
		}
	}
	cmp("computer", keys(len(a.Computers), nonDC(a.Computers)), keys(len(b.Computers), nonDC(b.Computers)))
	// DC names change between source and destination: leave them out of the
	// membership comparison.
	dcs := map[string]bool{}
	for _, cs := range [][]Computer{a.Computers, b.Computers} {
		for _, c := range cs {
			if c.IsDC {
				dcs[strings.ToLower(c.Name)+"$"] = true
			}
		}
	}
	members := func(ms []string) []string {
		out := []string{}
		for _, m := range lower(ms) {
			if !dcs[m] {
				out = append(out, m)
			}
		}
		return out
	}
	for g, ms := range a.Members {
		cmp("member of "+g, members(ms), members(b.Members[g]))
	}
	for g, ms := range b.Members {
		if _, ok := a.Members[g]; !ok {
			cmp("member of "+g, nil, members(ms))
		}
	}
	sort.Strings(out)
	return out
}

// lower returns a lowercase copy of s.
func lower(s []string) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[i] = strings.ToLower(v)
	}
	return out
}
