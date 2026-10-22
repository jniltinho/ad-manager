package ad

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/go-ldap/ldap/v3"
)

// userAccountControl bits used here.
const (
	uacDisabled     = 0x2
	uacNormal       = 0x200
	uacWorkstation  = 0x1000
	uacServer       = 0x2000
	uacPartialSec   = 0x04000000 // RODC
	uacLockout      = 0x10       // in msDS-User-Account-Control-Computed
	uacDontExpire   = 0x10000
	protectedRIDMax = 1000 // RIDs below this are built-in accounts and groups
)

// User is the view of a user used by the CLI, the API and the inventory.
type User struct {
	DN          string    `json:"dn"`
	Sam         string    `json:"sam"`
	SID         string    `json:"sid"`
	UPN         string    `json:"upn,omitempty"`
	Name        string    `json:"name"`
	GivenName   string    `json:"given_name,omitempty"`
	Surname     string    `json:"surname,omitempty"`
	Mail        string    `json:"mail,omitempty"`
	Phone       string    `json:"phone,omitempty"`
	Title       string    `json:"title,omitempty"`
	Department  string    `json:"department,omitempty"`
	Description string    `json:"description,omitempty"`
	Enabled     bool      `json:"enabled"`
	Locked      bool      `json:"locked"`
	NoExpire    bool      `json:"password_never_expires"`
	Protected   bool      `json:"protected"`
	Groups      []string  `json:"groups,omitempty"` // sAMAccountName of the direct groups
	LastLogon   time.Time `json:"last_logon,omitzero"`
	PwdLastSet  time.Time `json:"pwd_last_set,omitzero"`
	Created     time.Time `json:"created,omitzero"`
}

// UserInput holds the editable fields of a user.
type UserInput struct {
	Sam         string `json:"sam"`
	Name        string `json:"name"`
	GivenName   string `json:"given_name"`
	Surname     string `json:"surname"`
	Mail        string `json:"mail"`
	Phone       string `json:"phone"`
	Title       string `json:"title"`
	Department  string `json:"department"`
	Description string `json:"description"`
}

var userAttrs = []string{"sAMAccountName", "userPrincipalName", "displayName", "cn", "givenName", "sn",
	"mail", "telephoneNumber", "title", "department", "description", "userAccountControl",
	"lockoutTime", "msDS-User-Account-Control-Computed", "memberOf", "lastLogonTimestamp", "pwdLastSet", "whenCreated", "objectSid"}

const userFilter = "(&(objectCategory=person)(objectClass=user))"

// isDC reports a domain controller, including an RODC.
func isDC(uac int) bool { return uac&(uacServer|uacPartialSec) != 0 }

// toUser converts an LDAP entry into a User; groupSam maps lowercase group
// DNs to their sAMAccountName.
func toUser(e *ldap.Entry, groupSam map[string]string) User {
	uac, _ := strconv.Atoi(e.GetAttributeValue("userAccountControl"))
	name := e.GetAttributeValue("displayName")
	if name == "" {
		name = e.GetAttributeValue("cn")
	}
	u := User{
		DN: e.DN, Sam: e.GetAttributeValue("sAMAccountName"), SID: sidString(e.GetRawAttributeValue("objectSid")), UPN: e.GetAttributeValue("userPrincipalName"),
		Name: name, GivenName: e.GetAttributeValue("givenName"), Surname: e.GetAttributeValue("sn"),
		Mail: e.GetAttributeValue("mail"), Phone: e.GetAttributeValue("telephoneNumber"),
		Title: e.GetAttributeValue("title"), Department: e.GetAttributeValue("department"),
		Description: e.GetAttributeValue("description"),
		Enabled:     uac&uacDisabled == 0, NoExpire: uac&uacDontExpire != 0,
		Locked:     locked(e),
		Protected:  rid(e.GetRawAttributeValue("objectSid")) < protectedRIDMax,
		LastLogon:  fileTime(e.GetAttributeValue("lastLogonTimestamp")),
		PwdLastSet: fileTime(e.GetAttributeValue("pwdLastSet")),
		Created:    genTime(e.GetAttributeValue("whenCreated")),
	}
	for _, g := range e.GetAttributeValues("memberOf") {
		if sam, ok := groupSam[strings.ToLower(g)]; ok {
			u.Groups = append(u.Groups, sam)
		}
	}
	return u
}

// locked uses the bit computed by the DC (lockoutTime stays set after the
// lockout expires); without that attribute it falls back to lockoutTime.
func locked(e *ldap.Entry) bool {
	if v := e.GetAttributeValue("msDS-User-Account-Control-Computed"); v != "" {
		n, _ := strconv.Atoi(v)
		return n&uacLockout != 0
	}
	return !fileTime(e.GetAttributeValue("lockoutTime")).IsZero()
}

// groupSams returns lowercase DN → sAMAccountName for every group.
func (c *Conn) groupSams() (map[string]string, error) {
	es, err := c.search(c.cfg.Base(), ldap.ScopeWholeSubtree, "(objectClass=group)", []string{"sAMAccountName"})
	if err != nil {
		return nil, err
	}
	m := make(map[string]string, len(es))
	for _, e := range es {
		m[strings.ToLower(e.DN)] = e.GetAttributeValue("sAMAccountName")
	}
	return m, nil
}

// Users lists users; q filters by login, name or e-mail (substring match).
func (c *Conn) Users(q string) ([]User, error) {
	f := userFilter
	if q = strings.TrimSpace(q); q != "" {
		e := ldap.EscapeFilter(q)
		f = fmt.Sprintf("(&%s(|(sAMAccountName=*%s*)(displayName=*%s*)(cn=*%s*)(mail=*%s*)))", userFilter, e, e, e, e)
	}
	es, err := c.search(c.cfg.Base(), ldap.ScopeWholeSubtree, f, userAttrs)
	if err != nil {
		return nil, err
	}
	gs, err := c.groupSams()
	if err != nil {
		return nil, err
	}
	out := make([]User, 0, len(es))
	for _, e := range es {
		out = append(out, toUser(e, gs))
	}
	return out, nil
}

// User fetches a user by login.
func (c *Conn) User(sam string) (*User, error) {
	e, err := c.one(fmt.Sprintf("(&%s(sAMAccountName=%s))", userFilter, ldap.EscapeFilter(sam)), userAttrs)
	if err != nil {
		return nil, err
	}
	gs, err := c.groupSams()
	if err != nil {
		return nil, err
	}
	u := toUser(e, gs)
	return &u, nil
}

// CreateUser creates the user in the OU (full DN; empty means CN=Users), sets
// the password and enables the account. If setting the password fails, the
// created object is removed.
func (c *Conn) CreateUser(in UserInput, password, ouDN string, mustChange bool) (*User, error) {
	if !validSam(in.Sam) || strings.TrimSpace(in.Name) == "" {
		return nil, fmt.Errorf("%w: login and name are required; login without spaces or \"/\\[]:;|=,+*?<>", ErrWrongInput)
	}
	if !c.tls {
		return nil, ErrNeedsTLS
	}
	if _, err := c.object(in.Sam); err == nil {
		return nil, ErrConflict
	}
	if ouDN == "" {
		ouDN = "CN=Users," + c.cfg.Base()
	}
	dn := "CN=" + ldap.EscapeDN(in.Name) + "," + ouDN
	req := ldap.NewAddRequest(dn, nil)
	req.Attribute("objectClass", []string{"top", "person", "organizationalPerson", "user"})
	req.Attribute("sAMAccountName", []string{in.Sam})
	req.Attribute("userPrincipalName", []string{in.Sam + "@" + c.cfg.Domain})
	req.Attribute("userAccountControl", []string{strconv.Itoa(uacNormal | uacDisabled)})
	req.Attribute("displayName", []string{in.Name})
	for attr, v := range in.attrs() {
		if v != "" {
			req.Attribute(attr, []string{v})
		}
	}
	if err := c.add(req); err != nil {
		return nil, mapErr(err)
	}
	if err := c.setPassword(dn, password, mustChange); err != nil {
		_ = c.del(ldap.NewDelRequest(dn, nil))
		return nil, err
	}
	if err := c.setUAC(dn, uacDisabled, false); err != nil {
		return nil, err
	}
	return c.User(in.Sam)
}

// attrs maps the optional fields to their LDAP attributes.
func (in UserInput) attrs() map[string]string {
	return map[string]string{"givenName": in.GivenName, "sn": in.Surname, "mail": in.Mail,
		"telephoneNumber": in.Phone, "title": in.Title, "department": in.Department, "description": in.Description}
}

// UpdateUser writes the editable fields (an empty value removes the attribute).
func (c *Conn) UpdateUser(sam string, in UserInput) (*User, error) {
	u, err := c.User(sam)
	if err != nil {
		return nil, err
	}
	m := ldap.NewModifyRequest(u.DN, nil)
	if in.Name != "" {
		m.Replace("displayName", []string{in.Name})
	}
	for attr, v := range in.attrs() {
		if v != "" {
			m.Replace(attr, []string{v})
		} else {
			m.Replace(attr, []string{}) // an empty replace removes it; it does not fail if already absent
		}
	}
	if err := c.modify(m); err != nil {
		return nil, mapErr(err)
	}
	return c.User(sam)
}

// SetEnabled enables or disables a user or computer (sam with "$").
func (c *Conn) SetEnabled(sam string, enabled bool) error {
	e, err := c.object(sam)
	if err != nil {
		return err
	}
	if !enabled && c.protected(sam, e) {
		return ErrProtected
	}
	return c.setUAC(e.DN, uacDisabled, !enabled)
}

// Unlock clears a failed-attempts lockout.
func (c *Conn) Unlock(sam string) error {
	e, err := c.object(sam)
	if err != nil {
		return err
	}
	m := ldap.NewModifyRequest(e.DN, nil)
	m.Replace("lockoutTime", []string{"0"})
	return mapErr(c.modify(m))
}

// Move moves an object (user, group, computer) to another OU.
func (c *Conn) Move(sam, ouDN string) error {
	e, err := c.object(sam)
	if err != nil {
		return err
	}
	if uac, _ := strconv.Atoi(e.GetAttributeValue("userAccountControl")); isDC(uac) {
		return ErrProtected
	}
	dn, err := ldap.ParseDN(e.DN)
	if err != nil {
		return err
	}
	rdn := dn.RDNs[0].Attributes[0].Type + "=" + ldap.EscapeDN(dn.RDNs[0].Attributes[0].Value)
	return mapErr(c.modDN(ldap.NewModifyDNRequest(e.DN, rdn, true, ouDN)))
}

// Delete deletes a user, group or computer, refusing protected objects and the
// operator's own account.
func (c *Conn) Delete(sam string) error {
	e, err := c.object(sam)
	if err != nil {
		return err
	}
	if c.protected(sam, e) {
		return ErrProtected
	}
	// computers have children (e.g. RID Set, services): tree delete.
	return mapErr(c.del(ldap.NewDelRequest(e.DN, []ldap.Control{&ldap.ControlString{ControlType: "1.2.840.113556.1.4.805", Criticality: true}})))
}

// ResetPassword sets a new password (administrative reset).
func (c *Conn) ResetPassword(sam, password string, mustChange bool) error {
	e, err := c.object(sam)
	if err != nil {
		return err
	}
	return c.setPassword(e.DN, password, mustChange)
}

// ChangeOwnPassword changes the operator's own password (requires the current
// one).
func (c *Conn) ChangeOwnPassword(oldPwd, newPwd string) error {
	if !c.tls {
		return ErrNeedsTLS
	}
	e, err := c.object(c.Me)
	if err != nil {
		return err
	}
	m := ldap.NewModifyRequest(e.DN, nil)
	m.Delete("unicodePwd", []string{encodePwd(oldPwd)})
	m.Add("unicodePwd", []string{encodePwd(newPwd)})
	return pwdErr(c.modify(m))
}

// setPassword replaces unicodePwd; mustChange forces a change at next logon.
func (c *Conn) setPassword(dn, password string, mustChange bool) error {
	if !c.tls {
		return ErrNeedsTLS
	}
	if password == "" {
		return fmt.Errorf("%w: empty password", ErrWrongInput)
	}
	m := ldap.NewModifyRequest(dn, nil)
	m.Replace("unicodePwd", []string{encodePwd(password)})
	if mustChange {
		m.Replace("pwdLastSet", []string{"0"})
	}
	return pwdErr(c.modify(m))
}

// setUAC sets or clears a userAccountControl bit.
func (c *Conn) setUAC(dn string, bit int, on bool) error {
	es, err := c.search(dn, ldap.ScopeBaseObject, "(objectClass=*)", []string{"userAccountControl"})
	if err != nil {
		return err
	}
	if len(es) == 0 {
		return ErrNotFound
	}
	uac, _ := strconv.Atoi(es[0].GetAttributeValue("userAccountControl"))
	if on {
		uac |= bit
	} else {
		uac &^= bit
	}
	m := ldap.NewModifyRequest(dn, nil)
	m.Replace("userAccountControl", []string{strconv.Itoa(uac)})
	return mapErr(c.modify(m))
}

// object finds any object by sAMAccountName (user, group, computer).
func (c *Conn) object(sam string) (*ldap.Entry, error) {
	return c.one(fmt.Sprintf("(sAMAccountName=%s)", ldap.EscapeFilter(sam)), []string{"objectSid", "objectClass", "userAccountControl"})
}

// protected reports built-in accounts and groups (RID < 1000), domain
// controllers and the operator's own account.
func (c *Conn) protected(sam string, e *ldap.Entry) bool {
	uac, _ := strconv.Atoi(e.GetAttributeValue("userAccountControl"))
	return strings.EqualFold(sam, c.Me) || isDC(uac) || rid(e.GetRawAttributeValue("objectSid")) < protectedRIDMax
}

// encodePwd encodes unicodePwd: the quoted password in UTF-16LE.
func encodePwd(p string) string {
	u := utf16.Encode([]rune(`"` + p + `"`))
	b := make([]byte, len(u)*2)
	for i, r := range u {
		binary.LittleEndian.PutUint16(b[i*2:], r)
	}
	return string(b)
}

// rid returns the last sub-authority of a binary SID (0 if invalid, which
// counts as protected).
func rid(sid []byte) uint32 {
	if len(sid) < 12 {
		return 0
	}
	return binary.LittleEndian.Uint32(sid[len(sid)-4:])
}

// sidString formats a binary SID as S-1-5-21-...
func sidString(b []byte) string {
	if len(b) < 8 || len(b) != 8+4*int(b[1]) {
		return ""
	}
	auth := uint64(0)
	for _, x := range b[2:8] {
		auth = auth<<8 | uint64(x)
	}
	s := fmt.Sprintf("S-%d-%d", b[0], auth)
	for i := 8; i < len(b); i += 4 {
		s += fmt.Sprintf("-%d", binary.LittleEndian.Uint32(b[i:]))
	}
	return s
}

// toFileTime converts a time into an AD FILETIME string (100ns ticks since
// 1601); the zero time returns "" (nothing to write).
func toFileTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return strconv.FormatInt((t.UTC().Unix()+11644473600)*1e7+int64(t.Nanosecond())/100, 10)
}

// SetImportMetadata carries the source's non-secret object metadata to the
// matching local object (by sAMAccountName): the last-logon timestamp of a
// user or computer and, for a computer, its operating system. Only the values
// present are written: a zero last-logon (never logged on) and an empty OS
// write nothing, so a re-import never clears a value it did not receive, and a
// last-logon is set only when it is newer than the one already there (a
// re-import never moves it backwards). An object missing locally (not imported)
// is skipped, not an error, so the import can run objects first and this after.
func (c *Conn) SetImportMetadata(sam string, lastLogon time.Time, os, osVersion string) error {
	e, err := c.one(fmt.Sprintf("(sAMAccountName=%s)", ldap.EscapeFilter(sam)),
		[]string{"lastLogonTimestamp", "operatingSystem", "operatingSystemVersion"})
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	m := ldap.NewModifyRequest(e.DN, nil)
	n := 0
	if ft := toFileTime(lastLogon); ft != "" {
		// never move backwards: write only when the source value is newer than the target's current one
		newN, _ := strconv.ParseInt(ft, 10, 64)
		curN, _ := strconv.ParseInt(e.GetAttributeValue("lastLogonTimestamp"), 10, 64)
		if newN > curN {
			m.Replace("lastLogonTimestamp", []string{ft})
			n++
		}
	}
	if os != "" && !strings.EqualFold(os, e.GetAttributeValue("operatingSystem")) {
		m.Replace("operatingSystem", []string{os})
		n++
		if osVersion != "" {
			m.Replace("operatingSystemVersion", []string{osVersion})
		}
	}
	if n == 0 {
		return nil
	}
	return mapErr(c.modify(m))
}

// fileTime converts a FILETIME (100ns since 1601) into a time; 0 or "never"
// becomes the zero time.
func fileTime(s string) time.Time {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v <= 0 || v == 0x7FFFFFFFFFFFFFFF {
		return time.Time{}
	}
	return time.Unix(v/1e7-11644473600, (v%1e7)*100).UTC()
}

// genTime parses an LDAP GeneralizedTime.
func genTime(s string) time.Time {
	t, _ := time.Parse("20060102150405.0Z", s)
	return t
}

// cnOf returns the value of the first RDN.
func cnOf(dn string) string {
	if d, err := ldap.ParseDN(dn); err == nil && len(d.RDNs) > 0 {
		return d.RDNs[0].Attributes[0].Value
	}
	return dn
}

// validSam reports whether s is an acceptable sAMAccountName.
func validSam(s string) bool {
	return s != "" && len(s) <= 20 && !strings.ContainsAny(s, " \"/\\[]:;|=,+*?<>@")
}
