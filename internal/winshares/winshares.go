// Package winshares reads the shares of a Windows server (Samba's rpcclient
// and smbcacls, read-only) and computes the effective access of each group and
// user — share permission ∩ NTFS ACL of the share root — so the agent can
// recreate them on a Samba server.
package winshares

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Access levels.
const (
	levelNone  = 0
	levelRead  = 1
	levelWrite = 2 // Change / Modify
	levelFull  = 3 // Full control (also changes permissions and owner)
)

// Runner runs a command (replaceable in tests).
type Runner func(ctx context.Context, name string, args ...string) (string, error)

// Exec is the real Runner.
func Exec(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// ACE is a permission resolved to a principal name.
type ACE struct {
	Principal string `json:"principal"` // DOMAIN\name, BUILTIN\..., Everyone
	Level     int    `json:"level"`     // 1 read, 2 write (change), 3 full control
	Denied    bool   `json:"denied,omitempty"`
}

// Share is a share read from Windows.
type Share struct {
	Name     string `json:"name"`
	WinPath  string `json:"win_path"`
	Comment  string `json:"comment,omitempty"`
	ShareACL []ACE  `json:"share_acl"`
	NTFS     []ACE  `json:"ntfs_acl"`
	// ShareDeny: the share permission has a deny entry (parseShareSD keeps only the allowed ones)
	ShareDeny bool `json:"share_deny,omitempty"`
}

// isSpecial reports shares that are not migrated (administrative, logon,
// printers).
func isSpecial(name, p string) bool {
	u := strings.ToUpper(name)
	return p == "" || u == "IPC$" || u == "ADMIN$" || u == "PRINT$" || u == "NETLOGON" || u == "SYSVOL" ||
		(len(u) == 2 && u[1] == '$') // C$, E$
}

// Collect reads every data share from the server.
func Collect(ctx context.Context, run Runner, host, authFile string) ([]Share, error) {
	out, err := run(ctx, "rpcclient", "-A", authFile, host, "-c", "netshareenumall")
	if err != nil {
		return nil, err
	}
	// a name with a quote or ; would break out of rpcclient's -c command line: such shares are not read
	shares := slices.DeleteFunc(parseEnum(out), func(s Share) bool { return strings.ContainsAny(s.Name, "\";\\") })
	var sids []string
	sidACL := map[string][][2]string{} // share → [(sid, mask)]
	for _, s := range shares {
		o, err := run(ctx, "rpcclient", "-A", authFile, host, "-c", "netsharegetinfo \""+s.Name+"\" 502")
		if err != nil {
			return nil, fmt.Errorf("permission of %s: %w", s.Name, err)
		}
		aces := parseShareSD(o)
		sidACL[s.Name] = aces
		shares[slices.IndexFunc(shares, func(x Share) bool { return x.Name == s.Name })].ShareDeny = strings.Contains(o, "ACCESS DENIED")
		for _, a := range aces {
			if !slices.Contains(sids, a[0]) {
				sids = append(sids, a[0])
			}
		}
	}
	names := map[string]string{}
	if len(sids) > 0 {
		o, err := run(ctx, "rpcclient", "-A", authFile, host, "-c", "lookupsids "+strings.Join(sids, " "))
		if err != nil {
			return nil, fmt.Errorf("lookupsids: %w", err)
		}
		names = parseLookup(o)
	}
	for i := range shares {
		for _, a := range sidACL[shares[i].Name] {
			n := names[a[0]]
			if n == "" {
				n = a[0]
			}
			m, _ := strconv.ParseUint(strings.TrimPrefix(a[1], "0x"), 16, 32)
			shares[i].ShareACL = append(shares[i].ShareACL, ACE{Principal: n, Level: maskLevel(uint32(m))})
		}
		o, err := run(ctx, "smbcacls", "-A", authFile, "//"+host+"/"+shares[i].Name, "")
		if err != nil {
			return nil, fmt.Errorf("NTFS ACL of %s: %w", shares[i].Name, err)
		}
		shares[i].NTFS = parseCacls(o)
	}
	return shares, nil
}

// Paths reads the server's data shares and their folders (lowercase name → E:\Folder), with rpcclient (port 0:
// SMB's own).
func Paths(ctx context.Context, run Runner, host string, port int, authFile string) (map[string]string, error) {
	if err := ValidHost(host); err != nil {
		return nil, err
	}
	args := []string{"-A", authFile, host, "-c", "netshareenumall"}
	if port > 0 && port != 445 {
		args = append([]string{"-p", strconv.Itoa(port)}, args...)
	}
	out, err := run(ctx, "rpcclient", args...)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, s := range parseEnum(out) {
		m[strings.ToLower(s.Name)] = s.WinPath
	}
	return m, nil
}

var reRIDHex = regexp.MustCompile(`^[0-9A-F]{8}$`)

var reField = regexp.MustCompile(`^\s*(netname|remark|path):\s*(.*)$`)

// parseEnum parses `netshareenumall` output, dropping special shares.
func parseEnum(out string) []Share {
	var list []Share
	var cur *Share
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		m := reField.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		switch m[1] {
		case "netname":
			list = append(list, Share{Name: strings.TrimSpace(m[2])})
			cur = &list[len(list)-1]
		case "remark":
			if cur != nil {
				cur.Comment = strings.TrimSpace(m[2])
			}
		case "path":
			if cur != nil {
				cur.WinPath = strings.TrimSpace(m[2])
			}
		}
	}
	return slices.DeleteFunc(list, func(s Share) bool { return isSpecial(s.Name, s.WinPath) })
}

var (
	reMask = regexp.MustCompile(`Permissions:\s*(0x[0-9a-fA-F]+)`)
	reSID  = regexp.MustCompile(`SID:\s*(S-[0-9-]+)`)
)

// parseShareSD returns (SID, mask) for each "ACCESS ALLOWED" ACE.
func parseShareSD(out string) [][2]string {
	var r [][2]string
	for _, block := range strings.Split(out, "ACE\n")[1:] {
		if !strings.Contains(block, "ACCESS ALLOWED") {
			continue
		}
		m, s := reMask.FindStringSubmatch(block), reSID.FindStringSubmatch(block)
		if m != nil && s != nil {
			r = append(r, [2]string{s[1], m[1]})
		}
	}
	return r
}

// parseLookup parses "S-1-5-... DOM\name (type)" lines.
func parseLookup(out string) map[string]string {
	r := map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		f := strings.SplitN(strings.TrimSpace(l), " ", 2)
		if len(f) == 2 && strings.HasPrefix(f[0], "S-") {
			n := f[1]
			if i := strings.LastIndex(n, " ("); i >= 0 {
				n = n[:i] // drop the " (type)"
			}
			n = strings.TrimSpace(n)
			r[f[0]] = strings.TrimPrefix(n, `\`) // "\Everyone" → "Everyone"
		}
	}
	return r
}

// maskLevel returns full control for GENERIC_ALL or the complete Full control
// mask (a partial one, e.g. write + WRITE_DAC only, stays write), write for FILE_WRITE_DATA/APPEND/GENERIC_WRITE and read for
// FILE_READ_DATA/GENERIC_READ.
func maskLevel(m uint32) int {
	switch {
	case m&0x10000000 != 0, m&0x1F01FF == 0x1F01FF:
		return levelFull
	case m&(0x2|0x4|0x40000000) != 0:
		return levelWrite
	case m&(0x1|0x80000000|0x20000000) != 0:
		return levelRead
	}
	return levelNone
}

// parseCacls parses smbcacls "ACL:NAME:ALLOWED/FLAGS/RIGHTS" lines.
// It skips CREATOR OWNER and inherit-only ACEs (IO): they do not apply to the root.
func parseCacls(out string) []ACE {
	var r []ACE
	for _, l := range strings.Split(out, "\n") {
		if !strings.HasPrefix(l, "ACL:") {
			continue
		}
		body := strings.TrimPrefix(l, "ACL:")
		i := strings.LastIndex(body, ":")
		if i < 0 {
			continue
		}
		name, rest := body[:i], strings.Split(body[i+1:], "/")
		if len(rest) != 3 {
			continue
		}
		if rest[0] == "DENIED" { // any deny (inherit-only, or of rights without a level such as WRITE_DAC) is kept:
			// the caller must know there is one
			r = append(r, ACE{Principal: name, Level: rightsLevel(rest[2]), Denied: true})
			continue
		}
		if name == "CREATOR OWNER" || strings.Contains(rest[1], "IO") {
			continue
		}
		nv := rightsLevel(rest[2])
		if nv == levelNone {
			continue
		}
		r = append(r, ACE{Principal: name, Level: nv, Denied: rest[0] == "DENIED"})
	}
	return r
}

// rightsLevel maps an smbcacls rights field to an access level.
func rightsLevel(d string) int {
	switch d {
	case "FULL":
		return levelFull
	case "CHANGE":
		return levelWrite
	case "READ", "R":
		return levelRead
	}
	if strings.HasPrefix(d, "0x") {
		m, _ := strconv.ParseUint(d[2:], 16, 32)
		return maskLevel(uint32(m))
	}
	if strings.ContainsAny(d, "WD") {
		return levelWrite
	}
	if strings.Contains(d, "R") {
		return levelRead
	}
	return levelNone
}

// SharePlan is a share as it will be recreated on Samba.
type SharePlan struct {
	Name     string   `json:"name"`
	WinPath  string   `json:"win_path"`
	Path     string   `json:"path"`
	Comment  string   `json:"comment"`
	RW       []string `json:"rw_groups"`
	RO       []string `json:"ro_groups"`
	RWUsers  []string `json:"rw_users"`
	ROUsers  []string `json:"ro_users"`
	Full     []string `json:"full_groups,omitempty"` // Full control (share and NTFS)
	FullUser []string `json:"full_users,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	// Denies: the share or its root has a deny entry, which the effective access above leaves out (it could grant
	// more than the source); the panel does not import such a share
	Denies bool `json:"denies,omitempty"`
}

// Directory tells which names are groups and who is a member of whom (logins
// without the domain prefix).
type Directory struct {
	NetBIOS string
	Groups  map[string][]string // group → direct members
	Users   map[string]bool     // user logins
}

// short strips the DOMAIN\ prefix.
func (d Directory) short(p string) string {
	if i := strings.Index(p, `\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// universal reports principals that apply to every authenticated user.
func universal(p string) bool {
	switch strings.ToLower(p) {
	case "everyone", `nt authority\authenticated users`, `builtin\users`:
		return true
	}
	return strings.HasSuffix(strings.ToLower(p), `\domain users`)
}

// isAdministrative reports principals that need not be listed (Domain Admins
// is always added).
func isAdministrative(p string) bool {
	l := strings.ToLower(p)
	return l == `builtin\administrators` || l == `nt authority\system` || strings.HasSuffix(l, `\administrator`) ||
		strings.HasSuffix(l, `\domain admins`) || strings.HasSuffix(l, `\enterprise admins`)
}

// contains reports whether group (or someone nested inside it) is p.
func (d Directory) contains(group, p string, seen map[string]bool) bool {
	if strings.EqualFold(group, p) {
		return true
	}
	if seen[strings.ToLower(group)] {
		return false
	}
	seen[strings.ToLower(group)] = true
	for _, m := range d.Groups[group] {
		if d.contains(m, p, seen) {
			return true
		}
	}
	return false
}

// ntfs returns the NTFS level of p (or of a group containing p).
func (d Directory) ntfs(acl []ACE, p string) int {
	nv := levelNone
	for _, a := range acl {
		if a.Denied {
			continue
		}
		g := d.short(a.Principal)
		if universal(a.Principal) || d.contains(g, d.short(p), map[string]bool{}) {
			nv = max(nv, a.Level)
		}
	}
	return nv
}

// Plan computes the effective access and the Linux path of each share.
// driveMap replaces a Windows prefix (e.g. `E:\` → /srv/shares).
func Plan(shares []Share, d Directory, driveMap map[string]string) []SharePlan {
	var out []SharePlan
	for _, s := range shares {
		p := SharePlan{Name: s.Name, WinPath: s.WinPath, Path: linuxPath(s.WinPath, driveMap), Comment: s.Comment}
		effective := map[string]int{}
		var univ int
		for _, a := range s.ShareACL {
			if universal(a.Principal) {
				univ = max(univ, a.Level)
			}
		}
		// candidates: whoever appears in the share or in NTFS; each one's level is
		// the lower of the two sides (counting groups that contain it).
		// ponytail: does not cross distinct groups (user in X on the share and in Y
		// on NTFS); expand per user if that shows up in the preview warnings.
		cands := append(slices.Clone(s.ShareACL), s.NTFS...)
		for _, c := range cands {
			if c.Denied {
				continue // warned below
			}
			if isAdministrative(c.Principal) {
				continue
			}
			if strings.HasPrefix(c.Principal, "S-") || reRIDHex.MatchString(d.short(c.Principal)) {
				// deleted account: smbcacls shows the SID or the RID in hexadecimal
				if !slices.Contains(p.Warnings, "deleted account ignored: "+c.Principal) {
					p.Warnings = append(p.Warnings, "deleted account ignored: "+c.Principal)
				}
				continue
			}
			sh := univ
			for _, a := range s.ShareACL {
				if strings.EqualFold(a.Principal, c.Principal) || d.contains(d.short(a.Principal), d.short(c.Principal), map[string]bool{}) {
					sh = max(sh, a.Level)
				}
			}
			nv := min(sh, d.ntfs(s.NTFS, c.Principal))
			name := c.Principal
			if universal(name) {
				name = d.NetBIOS + `\Domain Users`
			}
			effective[name] = max(effective[name], nv)
		}
		for _, a := range s.NTFS {
			if a.Denied {
				p.Warnings = append(p.Warnings, "deny ACE not migrated: "+a.Principal)
				p.Denies = true
			}
		}
		if s.ShareDeny {
			p.Warnings = append(p.Warnings, "deny entry in the share permission not migrated")
			p.Denies = true
		}
		effective[d.NetBIOS+`\Domain Admins`] = levelFull // domain administrators, always
		for name, nv := range effective {
			isUser := d.Users[strings.ToLower(d.short(name))]
			switch {
			case nv == levelFull && isUser:
				p.FullUser = append(p.FullUser, name)
			case nv == levelFull:
				p.Full = append(p.Full, name)
			case nv == levelWrite && isUser:
				p.RWUsers = append(p.RWUsers, name)
			case nv == levelWrite:
				p.RW = append(p.RW, name)
			case nv == levelRead && isUser:
				p.ROUsers = append(p.ROUsers, name)
			case nv == levelRead:
				p.RO = append(p.RO, name)
			}
		}
		for _, l := range []*[]string{&p.RW, &p.RO, &p.RWUsers, &p.ROUsers, &p.Full, &p.FullUser} {
			sort.Strings(*l)
		}
		// Domain Admins (full control) last: the first write group becomes the
		// folder's owner group
		p.Full = append(slices.DeleteFunc(p.Full, func(g string) bool { return strings.HasSuffix(g, `\Domain Admins`) }), d.NetBIOS+`\Domain Admins`)
		out = append(out, p)
	}
	// parents before children (nested shares: Comercial\Contrato…)
	sort.SliceStable(out, func(i, j int) bool { return strings.Count(out[i].Path, "/") < strings.Count(out[j].Path, "/") })
	return out
}

// linuxPath maps a Windows path to its Linux path using driveMap.
func linuxPath(win string, driveMap map[string]string) string {
	for pre, dst := range driveMap {
		if len(win) >= len(pre) && strings.EqualFold(win[:len(pre)], pre) {
			return path.Join(dst, strings.ReplaceAll(win[len(pre):], `\`, "/"))
		}
	}
	// another drive: /srv/shares/<letter>/...
	if len(win) >= 3 && win[1] == ':' {
		base := "/srv/shares"
		for _, dst := range driveMap {
			base = dst
			break
		}
		return path.Join(base, strings.ToLower(win[:1]), strings.ReplaceAll(win[3:], `\`, "/"))
	}
	return win
}
