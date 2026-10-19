package ad

import (
	"bytes"
	"crypto/rand"
	"encoding/csv"
	"fmt"
	"io"
	"math/big"
	"slices"
	"sort"
	"strings"

	"github.com/go-ldap/ldap/v3"
)

// CSV export/import (";" and a UTF-8 BOM, which Excel opens in any locale). OU
// paths are relative to the domain root ("OU=Sales", "CN=Users"), so an
// export from example.net imports into a domain with another name. Passwords
// are not exportable: new accounts get a generated password and must change it
// at first logon.

// File headers. The column names are part of the file format.
var (
	UsersHeader     = []string{"login", "name", "given_name", "surname", "email", "phone", "title", "department", "description", "enabled", "ou", "groups"}
	GroupsHeader    = []string{"group", "type", "scope", "description", "ou", "members"}
	OUsHeader       = []string{"ou", "description"}
	ComputersHeader = []string{"computer", "dns", "os", "ou", "enabled"}
)

// csvAliases maps the Portuguese column names of older exports (objects and
// shares files) to the current ones, so those files still import.
var csvAliases = map[string]string{
	"nome": "name", "primeiro_nome": "given_name", "sobrenome": "surname", "telefone": "phone", "cargo": "title",
	"departamento": "department", "descricao": "description", "ativo": "enabled", "grupos": "groups", "grupo": "group",
	"tipo": "type", "escopo": "scope", "membros": "members", "computador": "computer", "sistema": "os",
	"compartilhamento": "share", "caminho": "path", "comentario": "comment", "oculto": "hidden",
	"escrita_grupos": "write_groups", "leitura_grupos": "read_groups", "escrita_usuarios": "write_users",
	"leitura_usuarios": "read_users", "controle_total_grupos": "full_control_groups",
	"controle_total_usuarios": "full_control_users", "lixeira": "recycle_bin", "auditoria": "audit",
}

// csvNo reports whether a CSV yes/no value means no (empty is neither; nao and
// não come from older Portuguese files).
func csvNo(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "no", "n", "false", "0", "nao", "não":
		return true
	}
	return false
}

// CSVSet holds the export files as rows (without header).
type CSVSet struct {
	Users, Groups, OUs, Computers [][]string
}

// rel strips the domain root from a DN.
func (c *Conn) rel(dn string) string {
	return strings.TrimSuffix(strings.TrimSuffix(dn, c.cfg.Base()), ",")
}

// abs is the inverse of rel: it appends the domain root.
func (c *Conn) abs(rel string) string {
	if rel == "" {
		return c.cfg.Base()
	}
	return rel + "," + c.cfg.Base()
}

// parent returns the parent DN (escaped commas are respected).
func parent(dn string) string {
	d, err := ldap.ParseDN(dn)
	if err != nil || len(d.RDNs) < 2 {
		return ""
	}
	parts := make([]string, 0, len(d.RDNs)-1)
	for _, r := range d.RDNs[1:] {
		parts = append(parts, r.Attributes[0].Type+"="+ldap.EscapeDN(r.Attributes[0].Value))
	}
	return strings.Join(parts, ",")
}

// Export reads the domain and returns the four CSV files (without secrets).
func (c *Conn) Export() (*CSVSet, error) {
	inv, err := c.Inventory(c.cfg.URL)
	if err != nil {
		return nil, err
	}
	out := &CSVSet{}
	for _, o := range inv.OUs {
		if strings.HasPrefix(o.DN, "OU=") {
			out.OUs = append(out.OUs, []string{c.rel(o.DN), o.Description})
		}
	}
	for _, g := range inv.Groups {
		scope := g.Scope
		if g.Protected { // a well-known group (RID < 1000): each domain makes its own, an import never creates one
			scope = "builtin"
		}
		out.Groups = append(out.Groups, []string{g.Name, g.Type, scope, g.Description, c.rel(parent(g.DN)), strings.Join(inv.Members[g.Name], "|")})
	}
	for _, u := range inv.Users {
		out.Users = append(out.Users, []string{u.Sam, u.Name, u.GivenName, u.Surname, u.Mail, u.Phone, u.Title, u.Department,
			u.Description, yesNo(u.Enabled), c.rel(parent(u.DN)), strings.Join(u.Groups, "|")})
	}
	for _, m := range inv.Computers {
		if m.IsDC { // controllers come in by joining as a DC, not by import
			continue
		}
		out.Computers = append(out.Computers, []string{m.Name, m.DNSName, m.OS, c.rel(parent(m.DN)), yesNo(m.Enabled)})
	}
	for _, rows := range [][][]string{out.OUs, out.Groups, out.Users, out.Computers} {
		sort.Slice(rows, func(i, j int) bool { return strings.ToLower(rows[i][0]) < strings.ToLower(rows[j][0]) })
	}
	return out, nil
}

// WriteCSV writes header and rows with a BOM and ";" separators.
func WriteCSV(w io.Writer, header []string, rows [][]string) error {
	if _, err := w.Write([]byte("\ufeff")); err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	cw.Comma = ';'
	if err := cw.Write(header); err != nil {
		return err
	}
	if err := cw.WriteAll(rows); err != nil {
		return err
	}
	return cw.Error()
}

// ReadCSV reads a CSV separated by ";" or "," (detected from the header line),
// checks the required key column and returns rows indexed by column name.
// Older Portuguese column names (csvAliases) are read as the current ones.
func ReadCSV(r io.Reader, header []string) ([]map[string]string, error) {
	const max = 10 << 20
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if len(b) > max {
		return nil, fmt.Errorf("%w: CSV larger than 10 MiB", ErrWrongInput)
	}
	b = bytes.TrimPrefix(b, []byte("\ufeff"))
	first, _, _ := bytes.Cut(b, []byte("\n"))
	cr := csv.NewReader(bytes.NewReader(b))
	cr.Comma = ';'
	if bytes.Count(first, []byte(",")) > bytes.Count(first, []byte(";")) {
		cr.Comma = ','
	}
	cr.FieldsPerRecord = -1
	recs, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("%w: invalid CSV: %v", ErrWrongInput, err)
	}
	if len(recs) == 0 {
		return nil, nil
	}
	cols := map[string]int{}
	for i, h := range recs[0] {
		cols[strings.ToLower(strings.TrimSpace(h))] = i
	}
	for old, cur := range csvAliases {
		if i, ok := cols[old]; ok {
			if _, has := cols[cur]; !has {
				cols[cur] = i
			}
		}
	}
	if _, ok := cols[header[0]]; !ok {
		return nil, fmt.Errorf("%w: missing column %q (expected: %s)", ErrWrongInput, header[0], strings.Join(header, ";"))
	}
	out := make([]map[string]string, 0, len(recs)-1)
	for _, rec := range recs[1:] {
		m := map[string]string{}
		for _, h := range header {
			if i, ok := cols[h]; ok && i < len(rec) {
				m[h] = strings.TrimSpace(rec[i])
			}
		}
		if m[header[0]] != "" {
			out = append(out, m)
		}
	}
	return out, nil
}

// Action is one step of the import plan.
// diffLabels names the CSV user columns in the import preview.
var diffLabels = map[string]string{"name": "name", "given_name": "given name", "surname": "surname", "email": "e-mail",
	"phone": "phone", "title": "title", "department": "department", "description": "description"}

type Action struct {
	Kind   string `json:"kind"`   // ou.create, group.create, group.update, user.create, user.update, user.disable, computer.create, member.add, skip
	Target string `json:"target"` // relative OU, group or login
	Detail string `json:"detail"` // what changes (before → after)
	Result string `json:"result,omitempty"`
	OK     bool   `json:"ok"`

	ou       string
	user     UserInput
	group    [3]string // type, scope, description
	enabled  bool
	member   string
	password string
}

// ImportData holds the rows read from the CSV files.
type ImportData struct {
	OUs, Groups, Users, Computers []map[string]string
}

// Plan compares the CSV data with the domain and lists what would be done. It
// only adds and updates: it never deletes objects or removes members.
func (c *Conn) Plan(d ImportData) ([]Action, error) {
	inv, err := c.Inventory(c.cfg.URL)
	if err != nil {
		return nil, err
	}
	ous := map[string]bool{}
	for _, o := range inv.OUs {
		ous[strings.ToLower(normRel(c.rel(o.DN)))] = true
	}
	groups := map[string]Group{}
	for _, g := range inv.Groups {
		groups[strings.ToLower(g.Name)] = g
	}
	users := map[string]User{}
	for _, u := range inv.Users {
		users[strings.ToLower(u.Sam)] = u
	}
	computers := map[string]bool{}
	for _, m := range inv.Computers {
		computers[strings.ToLower(m.Name)+"$"] = true
	}
	var plan []Action

	// OUs: every referenced OU (ous.csv, groups, users) is added with its parent
	// chain, parents before children. RDNs with escaped commas are respected.
	ouDesc := map[string]string{}
	for _, r := range d.OUs {
		ouDesc[strings.ToLower(normRel(r["ou"]))] = r["description"]
	}
	needOU := func(rel string) {
		for _, anc := range ouChain(rel) {
			k := strings.ToLower(anc)
			if ous[k] {
				continue
			}
			ous[k] = true
			plan = append(plan, Action{Kind: "ou.create", Target: anc, Detail: ouDesc[k], ou: anc})
		}
	}
	ouRows := slices.Clone(d.OUs)
	sort.Slice(ouRows, func(i, j int) bool { return len(ouChain(ouRows[i]["ou"])) < len(ouChain(ouRows[j]["ou"])) })
	for _, r := range ouRows {
		needOU(r["ou"])
	}

	// Groups.
	for _, r := range d.Groups {
		name := r["group"]
		g, ok := groups[strings.ToLower(name)]
		switch {
		case !ok && r["scope"] == "builtin":
			plan = append(plan, Action{Kind: "skip", Target: name, Detail: "built-in group missing at the destination"})
		case !ok:
			needOU(r["ou"])
			typ, scope := orDefault(r["type"], "security"), orDefault(r["scope"], "global")
			plan = append(plan, Action{Kind: "group.create", Target: name, Detail: typ + "/" + scope, ou: orDefault(r["ou"], "CN=Users"), group: [3]string{typ, scope, r["description"]}})
			groups[strings.ToLower(name)] = Group{Name: name}
		case r["description"] != "" && r["description"] != g.Description && !g.Protected:
			plan = append(plan, Action{Kind: "group.update", Target: name, Detail: fmt.Sprintf("description: %q → %q", g.Description, r["description"]), group: [3]string{"", "", r["description"]}})
		}
	}

	// Users.
	for _, r := range d.Users {
		sam := r["login"]
		in := UserInput{Sam: sam, Name: orDefault(r["name"], sam), GivenName: r["given_name"], Surname: r["surname"], Mail: r["email"],
			Phone: r["phone"], Title: r["title"], Department: r["department"], Description: r["description"]}
		enabled := !csvNo(r["enabled"])
		u, ok := users[strings.ToLower(sam)]
		switch {
		case ok && u.Protected:
			plan = append(plan, Action{Kind: "skip", Target: sam, Detail: "built-in domain account"})
		case !ok:
			needOU(r["ou"])
			plan = append(plan, Action{Kind: "user.create", Target: sam, Detail: in.Name + " in " + orDefault(r["ou"], "CN=Users"), ou: orDefault(r["ou"], "CN=Users"), user: in, enabled: enabled})
			users[strings.ToLower(sam)] = User{Sam: sam, Enabled: enabled}
		default:
			var diff []string
			cur := map[string]string{"name": u.Name, "given_name": u.GivenName, "surname": u.Surname, "email": u.Mail, "phone": u.Phone,
				"title": u.Title, "department": u.Department, "description": u.Description}
			for _, k := range []string{"name", "given_name", "surname", "email", "phone", "title", "department", "description"} {
				if r[k] != "" && r[k] != cur[k] {
					diff = append(diff, fmt.Sprintf("%s: %q → %q", diffLabels[k], cur[k], r[k]))
				}
			}
			if len(diff) > 0 {
				merged := UserInput{Name: orDefault(r["name"], u.Name), GivenName: orDefault(r["given_name"], u.GivenName), Surname: orDefault(r["surname"], u.Surname),
					Mail: orDefault(r["email"], u.Mail), Phone: orDefault(r["phone"], u.Phone), Title: orDefault(r["title"], u.Title),
					Department: orDefault(r["department"], u.Department), Description: orDefault(r["description"], u.Description)}
				plan = append(plan, Action{Kind: "user.update", Target: sam, Detail: strings.Join(diff, "; "), user: merged})
			}
			if u.Enabled && !enabled {
				plan = append(plan, Action{Kind: "user.disable", Target: sam, Detail: "enabled: yes → no"})
			}
			if !u.Enabled && enabled && r["enabled"] != "" {
				plan = append(plan, Action{Kind: "user.enable", Target: sam, Detail: "enabled: no → yes"})
			}
		}
	}

	// Computers: pre-created account (joining the domain reuses it); existing
	// ones are left as they are.
	for _, r := range d.Computers {
		name := strings.TrimSuffix(r["computer"], "$")
		if computers[strings.ToLower(name)+"$"] {
			continue
		}
		needOU(r["ou"])
		ou := orDefault(r["ou"], "CN=Computers")
		plan = append(plan, Action{Kind: "computer.create", Target: name, Detail: strings.TrimSpace(r["os"] + " in " + ou), ou: ou,
			user: UserInput{Sam: name, Description: r["dns"]}, enabled: !csvNo(r["enabled"])})
		computers[strings.ToLower(name)+"$"] = true
	}

	// Memberships (only the missing ones are added).
	have := map[string]bool{}
	for g, ms := range inv.Members {
		for _, m := range ms {
			have[strings.ToLower(g+"|"+m)] = true
		}
	}
	addMember := func(g, m string) {
		k := strings.ToLower(g + "|" + m)
		// Domain Users (RID 513) is the primary group: the membership does not show in memberOf.
		if have[k] || g == "" || m == "" || strings.HasSuffix(groups[strings.ToLower(g)].SID, "-513") {
			return
		}
		have[k] = true
		_, gok := groups[strings.ToLower(g)]
		_, uok := users[strings.ToLower(m)]
		_, mg := groups[strings.ToLower(m)]
		if !gok || !(uok || mg || computers[strings.ToLower(m)]) {
			plan = append(plan, Action{Kind: "skip", Target: g, Detail: "member " + m + ": group or member missing"})
			return
		}
		plan = append(plan, Action{Kind: "member.add", Target: g, Detail: m, member: m})
	}
	for _, r := range d.Groups {
		for _, m := range splitList(r["members"]) {
			addMember(r["group"], m)
		}
	}
	for _, r := range d.Users {
		for _, g := range splitList(r["groups"]) {
			addMember(g, r["login"])
		}
	}
	return plan, nil
}

// Apply runs the plan in order; errors do not stop the following steps.
// It returns the passwords generated for the created accounts (login → password).
func (c *Conn) Apply(plan []Action, genPassword func() string) map[string]string {
	pw := map[string]string{}
	for i := range plan {
		a := &plan[i]
		var err error
		switch a.Kind {
		case "ou.create":
			name, par := splitRDN(a.ou)
			_, err = c.CreateOU(name, a.Detail, c.absOrBase(par))
		case "group.create":
			_, err = c.CreateGroup(a.Target, a.group[2], a.group[0], a.group[1], c.abs(a.ou))
		case "group.update":
			err = c.SetDescription(a.Target, a.group[2])
		case "user.create":
			a.password = genPassword()
			if _, err = c.CreateUser(a.user, a.password, c.abs(a.ou), true); err == nil {
				pw[a.Target] = a.password
				if !a.enabled {
					err = c.SetEnabled(a.Target, false)
				}
			}
		case "user.update":
			_, err = c.UpdateUser(a.Target, a.user)
		case "user.disable":
			err = c.SetEnabled(a.Target, false)
		case "user.enable":
			err = c.SetEnabled(a.Target, true)
		case "computer.create":
			err = c.CreateComputer(a.Target, a.user.Description, c.abs(a.ou), a.enabled)
		case "member.add":
			err = c.AddMember(a.Target, a.member)
		case "skip":
			a.Result = "skipped"
			continue
		}
		a.OK = err == nil
		a.Result = "ok"
		if err != nil {
			a.Result = err.Error()
		}
	}
	return pw
}

// ouChain returns the OU ancestors of a relative path, from the top down to
// the leaf, already normalized ("OU=Parent", "OU=Child,OU=Parent"). Containers
// (CN=) and invalid paths produce no OUs.
func ouChain(rel string) []string {
	d, err := ldap.ParseDN(rel)
	if err != nil || rel == "" {
		return nil
	}
	var out []string
	for i := len(d.RDNs) - 1; i >= 0; i-- {
		if !strings.EqualFold(d.RDNs[i].Attributes[0].Type, "OU") {
			return out // nothing is created below a CN=
		}
		out = append(out, rdnString(d.RDNs[i:]))
	}
	return out
}

// rdnString formats RDNs with uppercase types and escaped values.
func rdnString(rdns []*ldap.RelativeDN) string {
	parts := make([]string, len(rdns))
	for i, r := range rdns {
		parts[i] = strings.ToUpper(r.Attributes[0].Type) + "=" + ldap.EscapeDN(r.Attributes[0].Value)
	}
	return strings.Join(parts, ",")
}

// normRel normalizes a relative DN for comparison.
func normRel(rel string) string {
	d, err := ldap.ParseDN(rel)
	if err != nil {
		return rel
	}
	return rdnString(d.RDNs)
}

// splitRDN splits the first RDN (decoded value) from the rest of the path.
func splitRDN(rel string) (name, parent string) {
	d, err := ldap.ParseDN(rel)
	if err != nil || len(d.RDNs) == 0 {
		return rel, ""
	}
	return d.RDNs[0].Attributes[0].Value, rdnString(d.RDNs[1:])
}

// absOrBase is abs, with an empty path meaning the domain root.
func (c *Conn) absOrBase(rel string) string {
	if rel == "" {
		return c.cfg.Base()
	}
	return c.abs(rel)
}

// orDefault returns v, or d when v is empty.
func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

// splitList splits a "|"-separated list, dropping empty items.
func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, "|") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// GenPassword generates a 16-character password containing all 4 character
// classes (meets AD complexity), without ambiguous characters.
func GenPassword() string {
	groups := []string{"ABCDEFGHJKLMNPQRSTUVWXYZ", "abcdefghijkmnopqrstuvwxyz", "23456789", "@#%+=!?"}
	all := strings.Join(groups, "")
	n := func(max int) int {
		v, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
		if err != nil {
			panic(err) // crypto/rand does not fail on supported systems
		}
		return int(v.Int64())
	}
	pick := func(s string) byte { return s[n(len(s))] }
	b := make([]byte, 0, 16)
	for _, g := range groups {
		b = append(b, pick(g))
	}
	for len(b) < 16 {
		b = append(b, pick(all))
	}
	for i := len(b) - 1; i > 0; i-- {
		j := n(i + 1)
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}

// SetToData converts an export (rows) into import data (maps by column) — used
// to import straight from another AD without going through files.
func SetToData(set *CSVSet) ImportData {
	conv := func(header []string, rows [][]string) []map[string]string {
		out := make([]map[string]string, 0, len(rows))
		for _, r := range rows {
			m := make(map[string]string, len(header))
			for i, h := range header {
				if i < len(r) {
					m[h] = r[i]
				}
			}
			out = append(out, m)
		}
		return out
	}
	return ImportData{OUs: conv(OUsHeader, set.OUs), Groups: conv(GroupsHeader, set.Groups), Users: conv(UsersHeader, set.Users),
		Computers: conv(ComputersHeader, set.Computers)}
}

// yesNo formats a boolean as the CSV values "yes"/"no".
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
