package ad

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
)

// ── Groups ───────────────────────────────────────────────────────────────────

// Group is the view of a group.
type Group struct {
	DN          string   `json:"dn"`
	Name        string   `json:"name"`
	SID         string   `json:"sid"`
	Description string   `json:"description,omitempty"`
	Type        string   `json:"type"`  // security | distribution
	Scope       string   `json:"scope"` // global | domainlocal | universal | builtin
	Protected   bool     `json:"protected"`
	Members     []string `json:"members"` // sAMAccountName of direct members (only filled by Conn.Group)
	MemberCount int      `json:"member_count"`
}

var groupScopes = map[string]int32{"global": 0x2, "domainlocal": 0x4, "universal": 0x8}

const (
	groupSecurity     = int32(-0x80000000)
	groupBuiltinLocal = int32(0x1) // GROUP_TYPE_BUILTIN_LOCAL_GROUP: the groups of CN=Builtin
)

// toGroup converts an LDAP entry into a Group.
func toGroup(e *ldap.Entry) Group {
	gt64, _ := strconv.ParseInt(e.GetAttributeValue("groupType"), 10, 32)
	gt := int32(gt64)
	g := Group{DN: e.DN, Name: e.GetAttributeValue("sAMAccountName"), SID: sidString(e.GetRawAttributeValue("objectSid")), Description: e.GetAttributeValue("description"),
		Type: "distribution", Scope: "builtin", MemberCount: len(e.GetAttributeValues("member")),
		Protected: rid(e.GetRawAttributeValue("objectSid")) < protectedRIDMax}
	if gt&groupSecurity != 0 {
		g.Type = "security"
	}
	for s, bit := range groupScopes {
		if gt&bit != 0 {
			g.Scope = s
		}
	}
	if gt&groupBuiltinLocal != 0 { // Windows sets the domain local bit too: builtin wins
		g.Scope = "builtin"
	}
	return g
}

var groupAttrs = []string{"sAMAccountName", "description", "groupType", "member", "objectSid"}

// Groups lists groups; q filters by name or description.
func (c *Conn) Groups(q string) ([]Group, error) {
	f := "(objectClass=group)"
	if q = strings.TrimSpace(q); q != "" {
		f = fmt.Sprintf("(&(objectClass=group)(|(sAMAccountName=*%[1]s*)(description=*%[1]s*)))", ldap.EscapeFilter(q))
	}
	es, err := c.search(c.cfg.Base(), ldap.ScopeWholeSubtree, f, groupAttrs)
	if err != nil {
		return nil, err
	}
	out := make([]Group, 0, len(es))
	for _, e := range es {
		out = append(out, toGroup(e))
	}
	return out, nil
}

// Group returns the group with its member list (logins).
func (c *Conn) Group(name string) (*Group, error) {
	e, err := c.one(fmt.Sprintf("(&(objectClass=group)(sAMAccountName=%s))", ldap.EscapeFilter(name)), groupAttrs)
	if err != nil {
		return nil, err
	}
	g := toGroup(e)
	// Members via the reverse link: a single search already returns each login.
	ms, err := c.search(c.cfg.Base(), ldap.ScopeWholeSubtree,
		fmt.Sprintf("(memberOf=%s)", ldap.EscapeFilter(e.DN)), []string{"sAMAccountName"})
	if err != nil {
		return nil, err
	}
	g.Members = []string{}
	for _, m := range ms {
		g.Members = append(g.Members, m.GetAttributeValue("sAMAccountName"))
	}
	return &g, nil
}

// CreateGroup creates a group. typ is security|distribution; scope is
// global|domainlocal|universal.
func (c *Conn) CreateGroup(name, description, typ, scope, ouDN string) (*Group, error) {
	bit, ok := groupScopes[scope]
	if strings.TrimSpace(name) == "" || strings.ContainsAny(name, "\"/\\[]:;|=,+*?<>@") || !ok || (typ != "security" && typ != "distribution") {
		return nil, fmt.Errorf("%w: name, type (security|distribution) and scope (global|domainlocal|universal)", ErrWrongInput)
	}
	if _, err := c.object(name); err == nil {
		return nil, ErrConflict
	}
	if typ == "security" {
		bit |= groupSecurity
	}
	if ouDN == "" {
		ouDN = "CN=Users," + c.cfg.Base()
	}
	req := ldap.NewAddRequest("CN="+ldap.EscapeDN(name)+","+ouDN, nil)
	req.Attribute("objectClass", []string{"top", "group"})
	req.Attribute("sAMAccountName", []string{name})
	req.Attribute("groupType", []string{strconv.Itoa(int(bit))})
	if description != "" {
		req.Attribute("description", []string{description})
	}
	if err := c.add(req); err != nil {
		return nil, mapErr(err)
	}
	return c.Group(name)
}

// CreateComputer pre-creates a computer account (without password: joining
// the domain sets it). dns is the dNSHostName (optional).
func (c *Conn) CreateComputer(name, dns, ouDN string, enabled bool) error {
	name = strings.ToUpper(strings.TrimSuffix(strings.TrimSpace(name), "$"))
	if name == "" || len(name) > 15 || strings.ContainsAny(name, "\"/\\[]:;|=,+*?<>@ .") {
		return fmt.Errorf("%w: computer NetBIOS name (up to 15 characters, no spaces or dots)", ErrWrongInput)
	}
	if _, err := c.object(name + "$"); err == nil {
		return ErrConflict
	}
	if ouDN == "" {
		ouDN = "CN=Computers," + c.cfg.Base()
	}
	uac := uacWorkstation
	if !enabled {
		uac |= uacDisabled
	}
	req := ldap.NewAddRequest("CN="+ldap.EscapeDN(name)+","+ouDN, nil)
	req.Attribute("objectClass", []string{"top", "person", "organizationalPerson", "user", "computer"})
	req.Attribute("sAMAccountName", []string{name + "$"})
	req.Attribute("userAccountControl", []string{strconv.Itoa(uac)})
	if dns != "" {
		req.Attribute("dNSHostName", []string{dns})
	}
	return mapErr(c.add(req))
}

// SetDescription changes the description of a user, group or computer.
func (c *Conn) SetDescription(sam, description string) error {
	e, err := c.object(sam)
	if err != nil {
		return err
	}
	m := ldap.NewModifyRequest(e.DN, nil)
	if description == "" {
		m.Replace("description", []string{})
	} else {
		m.Replace("description", []string{description})
	}
	return mapErr(c.modify(m))
}

// AddMember adds member (a user login, group, or computer with "$") to group.
func (c *Conn) AddMember(group, member string) error { return c.member(group, member, true) }

// RemoveMember removes member (a user login, group, or computer with "$")
// from group.
func (c *Conn) RemoveMember(group, member string) error { return c.member(group, member, false) }

// member adds or removes a group membership; both operations are idempotent.

func (c *Conn) member(group, member string, add bool) error {
	g, err := c.one(fmt.Sprintf("(&(objectClass=group)(sAMAccountName=%s))", ldap.EscapeFilter(group)), []string{"dn"})
	if err != nil {
		return fmt.Errorf("group %s: %w", group, err)
	}
	m, err := c.object(member)
	if err != nil {
		return fmt.Errorf("member %s: %w", member, err)
	}
	req := ldap.NewModifyRequest(g.DN, nil)
	if add {
		req.Add("member", []string{m.DN})
	} else {
		req.Delete("member", []string{m.DN})
	}
	err = c.modify(req)
	if add && ldap.IsErrorWithCode(err, ldap.LDAPResultEntryAlreadyExists) {
		return nil // already a member
	}
	if !add && ldap.IsErrorWithCode(err, ldap.LDAPResultUnwillingToPerform) {
		return nil // already not a member (AD answers 53)
	}
	return mapErr(err)
}

// ── OUs ──────────────────────────────────────────────────────────────────────

// OU is an organizational unit (or a default container).
type OU struct {
	DN          string `json:"dn"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// OUs lists the OUs and the default containers where objects are created.
func (c *Conn) OUs() ([]OU, error) {
	es, err := c.search(c.cfg.Base(), ldap.ScopeWholeSubtree,
		"(|(objectClass=organizationalUnit)(&(objectClass=container)(|(cn=Users)(cn=Computers))))",
		[]string{"name", "description"})
	if err != nil {
		return nil, err
	}
	out := make([]OU, 0, len(es))
	for _, e := range es {
		out = append(out, OU{DN: e.DN, Name: e.GetAttributeValue("name"), Description: e.GetAttributeValue("description")})
	}
	return out, nil
}

// CreateOU creates an OU under parentDN (empty means the domain root).
func (c *Conn) CreateOU(name, description, parentDN string) (*OU, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("%w: OU name", ErrWrongInput)
	}
	if parentDN == "" {
		parentDN = c.cfg.Base()
	}
	dn := "OU=" + ldap.EscapeDN(name) + "," + parentDN
	req := ldap.NewAddRequest(dn, nil)
	req.Attribute("objectClass", []string{"top", "organizationalUnit"})
	if description != "" {
		req.Attribute("description", []string{description})
	}
	if err := c.add(req); err != nil {
		return nil, mapErr(err)
	}
	return &OU{DN: dn, Name: name, Description: description}, nil
}

// DeleteOU deletes an empty OU (the server refuses an OU with children).
func (c *Conn) DeleteOU(dn string) error {
	if !strings.HasPrefix(strings.ToUpper(dn), "OU=") || strings.EqualFold(cnOf(dn), "Domain Controllers") {
		return ErrProtected
	}
	return mapErr(c.del(ldap.NewDelRequest(dn, nil)))
}

// ── Computers ────────────────────────────────────────────────────────────────

// Computer is a computer account.
type Computer struct {
	DN        string    `json:"dn"`
	Name      string    `json:"name"`
	Sam       string    `json:"sam"` // NAME$ — use this one in actions
	DNSName   string    `json:"dns_name,omitempty"`
	OS        string    `json:"os,omitempty"`
	OSVersion string    `json:"os_version,omitempty"`
	Enabled   bool      `json:"enabled"`
	IsDC      bool      `json:"is_dc"`
	LastLogon time.Time `json:"last_logon,omitzero"`
}

// Computers lists computer accounts; q filters by name.
func (c *Conn) Computers(q string) ([]Computer, error) {
	f := "(objectClass=computer)"
	if q = strings.TrimSpace(q); q != "" {
		f = fmt.Sprintf("(&(objectClass=computer)(cn=*%s*))", ldap.EscapeFilter(q))
	}
	es, err := c.search(c.cfg.Base(), ldap.ScopeWholeSubtree, f,
		[]string{"cn", "sAMAccountName", "dNSHostName", "operatingSystem", "operatingSystemVersion", "userAccountControl", "lastLogonTimestamp"})
	if err != nil {
		return nil, err
	}
	out := make([]Computer, 0, len(es))
	for _, e := range es {
		uac, _ := strconv.Atoi(e.GetAttributeValue("userAccountControl"))
		out = append(out, Computer{DN: e.DN, Name: e.GetAttributeValue("cn"), Sam: e.GetAttributeValue("sAMAccountName"), DNSName: e.GetAttributeValue("dNSHostName"),
			OS: e.GetAttributeValue("operatingSystem"), OSVersion: e.GetAttributeValue("operatingSystemVersion"),
			Enabled: uac&uacDisabled == 0, IsDC: isDC(uac),
			LastLogon: fileTime(e.GetAttributeValue("lastLogonTimestamp"))})
	}
	return out, nil
}

// ── Password policy ──────────────────────────────────────────────────────────

// Policy is the domain's default password policy. Durations are in days or
// minutes.
type Policy struct {
	Complexity        bool `json:"complexity"`
	MinLength         int  `json:"min_length"`
	History           int  `json:"history"`
	MinAgeDays        int  `json:"min_age_days"`
	MaxAgeDays        int  `json:"max_age_days"` // 0 means never expires
	LockoutThreshold  int  `json:"lockout_threshold"`
	LockoutMinutes    int  `json:"lockout_minutes"`
	ObservationMinute int  `json:"lockout_window_minutes"`
}

var policyAttrs = []string{"pwdProperties", "minPwdLength", "pwdHistoryLength", "minPwdAge", "maxPwdAge",
	"lockoutThreshold", "lockoutDuration", "lockOutObservationWindow"}

// toUnits converts an AD interval (a negative integer in 100ns units) into
// whole units.
func toUnits(s string, unit time.Duration) int {
	v, _ := strconv.ParseInt(s, 10, 64)
	if v == -0x8000000000000000 { // "never"
		return 0
	}
	if v < 0 {
		v = -v
	}
	return int(v * 100 / int64(unit))
}

// fromUnits is the inverse of toUnits.
func fromUnits(n int, unit time.Duration) string {
	if n == 0 {
		return "0"
	}
	return strconv.FormatInt(-int64(n)*int64(unit)/100, 10)
}

// Policy reads the domain policy.
func (c *Conn) Policy() (*Policy, error) {
	es, err := c.search(c.cfg.Base(), ldap.ScopeBaseObject, "(objectClass=*)", policyAttrs)
	if err != nil {
		return nil, err
	}
	e := es[0]
	atoi := func(a string) int { v, _ := strconv.Atoi(e.GetAttributeValue(a)); return v }
	day := 24 * time.Hour
	return &Policy{
		Complexity: atoi("pwdProperties")&1 != 0, MinLength: atoi("minPwdLength"), History: atoi("pwdHistoryLength"),
		MinAgeDays: toUnits(e.GetAttributeValue("minPwdAge"), day), MaxAgeDays: toUnits(e.GetAttributeValue("maxPwdAge"), day),
		LockoutThreshold:  atoi("lockoutThreshold"),
		LockoutMinutes:    toUnits(e.GetAttributeValue("lockoutDuration"), time.Minute),
		ObservationMinute: toUnits(e.GetAttributeValue("lockOutObservationWindow"), time.Minute),
	}, nil
}

// SetPolicy writes the domain policy.
func (c *Conn) SetPolicy(p Policy) error {
	if p.MinLength < 0 || p.MinLength > 255 || p.History < 0 || p.History > 24 || p.MinAgeDays < 0 ||
		p.MaxAgeDays < 0 || p.LockoutThreshold < 0 || p.LockoutMinutes < 0 || p.ObservationMinute < 0 ||
		(p.MaxAgeDays > 0 && p.MinAgeDays >= p.MaxAgeDays) ||
		(p.LockoutMinutes > 0 && p.ObservationMinute > p.LockoutMinutes) {
		return fmt.Errorf("%w: values out of range (history 0-24, minimum age < maximum age, window ≤ lockout duration)", ErrWrongInput)
	}
	cur, err := c.search(c.cfg.Base(), ldap.ScopeBaseObject, "(objectClass=*)", []string{"pwdProperties"})
	if err != nil {
		return err
	}
	props, _ := strconv.Atoi(cur[0].GetAttributeValue("pwdProperties"))
	if p.Complexity {
		props |= 1
	} else {
		props &^= 1
	}
	day := 24 * time.Hour
	maxAge := fromUnits(p.MaxAgeDays, day)
	if p.MaxAgeDays == 0 {
		maxAge = "-9223372036854775808"
	}
	m := ldap.NewModifyRequest(c.cfg.Base(), nil)
	m.Replace("pwdProperties", []string{strconv.Itoa(props)})
	m.Replace("minPwdLength", []string{strconv.Itoa(p.MinLength)})
	m.Replace("pwdHistoryLength", []string{strconv.Itoa(p.History)})
	m.Replace("minPwdAge", []string{fromUnits(p.MinAgeDays, day)})
	m.Replace("maxPwdAge", []string{maxAge})
	m.Replace("lockoutThreshold", []string{strconv.Itoa(p.LockoutThreshold)})
	m.Replace("lockoutDuration", []string{fromUnits(p.LockoutMinutes, time.Minute)})
	m.Replace("lockOutObservationWindow", []string{fromUnits(p.ObservationMinute, time.Minute)})
	return mapErr(c.modify(m))
}
