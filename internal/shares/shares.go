// Package shares creates folders and SMB shares on a Samba file server that is
// a domain member (shares live in the registry configuration: Samba's smbconf
// API through the helper, or `net conf` without python3-samba). It runs as root
// in the agent, so every input is validated here, before and after the queue.
package shares

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"ad-manager/internal/sambabridge"
)

// ErrInvalid reports input rejected by validation.
var ErrInvalid = errors.New("invalid input")

// Runner runs a command without a shell (separate arguments).
type Runner func(ctx context.Context, name string, args ...string) (string, error)

// Exec is the real Runner.
func Exec(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// ACLBridge writes and reads Windows (NT) ACLs through Samba (the embedded
// Python helper, internal/sambabridge).
type ACLBridge interface {
	SetFolderACL(ctx context.Context, dir *os.File, req sambabridge.FolderACL) (*sambabridge.ACL, error)
	GetACL(ctx context.Context, dir *os.File, path string, roots []string, service string) (*sambabridge.ACL, error)
	SetSDDL(ctx context.Context, dir *os.File, path string, roots []string, sddl string, propagate bool) (*sambabridge.ACL, error)
	Reapply(ctx context.Context, dir *os.File, path string, roots []string, sddl string) (*sambabridge.ACL, error)
	ReinheritPaths(ctx context.Context, dir *os.File, path string, roots []string, rels []string) (*sambabridge.ACL, error)
}

// Manager holds the allowed roots and the command runner.
type Manager struct {
	Roots []string
	Run   Runner
	Chown func(path string, uid, gid int) error // nil means fchown on the opened folder (tests replace it)
	// ACL writes Windows ACLs when NTACL is true (python3-samba present and
	// vfs objects = acl_xattr); otherwise folders get POSIX ACLs (setfacl).
	ACL   ACLBridge
	NTACL bool
	// History is the JSON-lines file where each Windows ACL change records the
	// SDDL before and after (empty disables it).
	History string
	// Kill ends process pid once check (run with the process pinned) passes; nil means killPinned (tests replace it).
	Kill func(pid int, check func() error) error
	// Conf edits the shares through Samba's registry API (python3-samba); nil means `net conf`.
	Conf ConfBridge
	// DC: a domain controller that also serves files — owners never fall back to root, and Reserved (the shares
	// smb.conf itself defines: sysvol, netlogon…) and the administrative names ($) are never created or deleted.
	DC       bool
	Reserved []string
}

// reserved tells whether name is a share the agent must leave alone on a DC.
func (m *Manager) reserved(name string) bool {
	if !m.DC {
		return false
	}
	// the administrative shares (a drive's C$, ADMIN$, IPC$, print$), not a hidden data share such as
	// "Notas Fiscais$" (a migrated Windows server's hidden shares are data)
	if adminShare.MatchString(name) || strings.EqualFold(name, "sysvol") || strings.EqualFold(name, "netlogon") {
		return true
	}
	for _, r := range m.Reserved {
		if strings.EqualFold(r, name) {
			return true
		}
	}
	return false
}

// ConfBridge reads and writes shares in Samba's registry configuration (the embedded helper).
type ConfBridge interface {
	ListShares(ctx context.Context) ([]sambabridge.ShareConf, error)
	SetShare(ctx context.Context, name string, params [][2]string, vfs []string) (*sambabridge.ShareConf, error)
	DeleteShare(ctx context.Context, name string) error
}

// ACLChange is one line of the ACL history: the folder's SDDL before a change (Before) or right after it (After).
type ACLChange struct {
	Time   string `json:"time"`
	Path   string `json:"path"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

// RestoreReq asks to put back a folder's ACL as it was before the change recorded at Time.
type RestoreReq struct {
	Path string `json:"path"`
	Time string `json:"time"`
}

// Share is a published share.
type Share struct {
	Name    string   `json:"name"`
	Path    string   `json:"path"`
	Comment string   `json:"comment,omitempty"`
	RW      []string `json:"rw_groups"`
	RO      []string `json:"ro_groups"`
	RWUsers []string `json:"rw_users,omitempty"`
	ROUsers []string `json:"ro_users,omitempty"`
	// Full lists who has Windows "Full control" (read from the folder's NT ACL;
	// they are also in RW for smb.conf)
	Full      []string `json:"full_groups,omitempty"`
	FullUsers []string `json:"full_users,omitempty"`
	Browse    bool     `json:"browseable"`
	ReadOnly  bool     `json:"read_only"`
	Recycle   bool     `json:"recycle"` // deleted files go to .recycle/<user> (vfs recycle)
	ABE       bool     `json:"abe"`     // access-based enumeration: what the user cannot read is hidden
	Audit     bool     `json:"audit"`   // changes logged to syslog (vfs full_audit)
}

// FolderReq requests a folder with an owner group and ACLs.
type FolderReq struct {
	Path    string   `json:"path"`
	Owner   string   `json:"owner_group"` // DOMAIN\group (optional)
	RW      []string `json:"rw_groups"`
	RO      []string `json:"ro_groups"`
	RWUsers []string `json:"rw_users"` // users (personal folders)
	ROUsers []string `json:"ro_users"`
	// Full control (Windows): like RW plus changing permissions and taking
	// ownership; on a POSIX-only server it is the same as RW.
	Full      []string `json:"full_groups,omitempty"`
	FullUsers []string `json:"full_users,omitempty"`
}

// ShareReq requests a share to be created (or recreated); CreatePath creates
// the folder first.
type ShareReq struct {
	Name       string   `json:"name"`
	Path       string   `json:"path"`
	Comment    string   `json:"comment"`
	RW         []string `json:"rw_groups"`
	RO         []string `json:"ro_groups"`
	RWUsers    []string `json:"rw_users"`
	ROUsers    []string `json:"ro_users"`
	Full       []string `json:"full_groups,omitempty"`
	FullUsers  []string `json:"full_users,omitempty"`
	Hidden     bool     `json:"hidden"`
	CreatePath bool     `json:"create_path"`
	Recycle    bool     `json:"recycle"`
	ABE        bool     `json:"abe"`
	Audit      bool     `json:"audit"`
}

var (
	// "$" only at the end: hidden share (Windows convention, e.g. "Notas Fiscais$")
	shareName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,78}\$?$`)
	reserved  = []string{"global", "homes", "printers", "print$", "ipc$", "admin$", "sysvol", "netlogon"}
	// adminShare: Windows' administrative shares (a drive letter's, ADMIN$, IPC$, print$)
	adminShare = regexp.MustCompile(`(?i)^([a-z]|admin|ipc|print)\$$`)
)

// ValidName rejects names outside the allowed pattern and names reserved by
// Samba/Windows.
func ValidName(n string) error {
	if !shareName.MatchString(n) || slices.Contains(reserved, strings.ToLower(n)) {
		return fmt.Errorf("%w: share name %q (letters, numbers, space . _ -; reserved: %s)", ErrInvalid, n, strings.Join(reserved, ", "))
	}
	return nil
}

// ValidGroup accepts "DOMAIN\group" or "group" made of letters, digits, space
// and . _ -.
func ValidGroup(g string) error {
	if g == "" || len(g) > 256 || strings.HasPrefix(g, "-") || strings.Count(g, `\`) > 1 || strings.ContainsAny(g, `"'@,:;=+*?<>|/[]`) {
		return fmt.Errorf("%w: group %q", ErrInvalid, g)
	}
	for _, r := range g {
		if unicode.IsControl(r) {
			return fmt.Errorf("%w: group %q", ErrInvalid, g)
		}
	}
	return nil
}

// SafePath requires an absolute path inside a root, without "..", and without
// a symbolic link in any existing component. It returns the cleaned path.
func (m *Manager) SafePath(p string) (string, error) {
	// control characters (newline) would inject parameters into smb.conf
	if !filepath.IsAbs(p) || strings.Contains(p, "..") || strings.ContainsFunc(p, unicode.IsControl) {
		return "", fmt.Errorf("%w: path %q must be absolute and without ..", ErrInvalid, p)
	}
	p = filepath.Clean(p)
	var root string
	for _, r := range m.Roots {
		r = filepath.Clean(r)
		if p != r && strings.HasPrefix(p, r+string(filepath.Separator)) {
			root = r
		}
	}
	if root == "" {
		return "", fmt.Errorf("%w: %q is outside the allowed roots %v", ErrInvalid, p, m.Roots)
	}
	for cur := p; len(cur) >= len(root); cur = filepath.Dir(cur) {
		fi, err := os.Lstat(cur)
		if err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%w: %q is a symbolic link", ErrInvalid, cur)
		}
		if cur == root {
			break
		}
	}
	return p, nil
}

// validGroups validates every group or user name in the lists.
func validGroups(gs ...[]string) error {
	for _, list := range gs {
		for _, g := range list {
			if err := ValidGroup(g); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateFolder checks a folder request and normalizes its path.
func (m *Manager) ValidateFolder(r *FolderReq) error {
	p, err := m.SafePath(r.Path)
	if err != nil {
		return err
	}
	r.Path = p
	if r.Owner != "" {
		if err := ValidGroup(r.Owner); err != nil {
			return err
		}
	}
	return validGroups(r.RW, r.RO, r.RWUsers, r.ROUsers, r.Full, r.FullUsers)
}

// ValidateShare checks a share request and normalizes its path.
func (m *Manager) ValidateShare(r *ShareReq) error {
	if err := ValidName(r.Name); err != nil {
		return err
	}
	if m.reserved(r.Name) {
		return fmt.Errorf("%w: %q is a share of the domain controller itself", ErrInvalid, r.Name)
	}
	if strings.ContainsAny(r.Comment, "\n\r[]") || strings.HasPrefix(r.Comment, "-") || len(r.Comment) > 200 {
		return fmt.Errorf("%w: comment", ErrInvalid)
	}
	p, err := m.SafePath(r.Path)
	if err != nil {
		return err
	}
	r.Path = p
	if len(r.RW)+len(r.RO)+len(r.RWUsers)+len(r.ROUsers)+len(r.Full)+len(r.FullUsers) == 0 {
		return fmt.Errorf("%w: give at least one read or write group or user", ErrInvalid)
	}
	if m.Conf == nil && (r.Recycle || r.Audit) { // before anything changes on disk
		return fmt.Errorf("%w: the recycle bin and the file audit need python3-samba on the file server", ErrInvalid)
	}
	return validGroups(r.RW, r.RO, r.RWUsers, r.ROUsers, r.Full, r.FullUsers)
}

// smbList builds an smb.conf list: groups with @ and users without, always quoted.
func smbList(groups, users []string) string {
	q := make([]string, 0, len(groups)+len(users))
	for _, g := range groups {
		q = append(q, `@"`+g+`"`)
	}
	for _, u := range users {
		q = append(q, `"`+u+`"`)
	}
	return strings.Join(q, " ")
}

// CreateShare publishes (or republishes) the share in the Samba registry.
func (m *Manager) CreateShare(ctx context.Context, r ShareReq) error {
	if err := m.ValidateShare(&r); err != nil {
		return err
	}
	if r.CreatePath {
		owner := ""
		if len(r.RW) > 0 {
			owner = r.RW[0]
		}
		if err := m.CreateFolder(ctx, FolderReq{Path: r.Path, Owner: owner, RW: r.RW, RO: r.RO, RWUsers: r.RWUsers, ROUsers: r.ROUsers,
			Full: r.Full, FullUsers: r.FullUsers}); err != nil {
			return err
		}
	} else if fi, err := os.Stat(r.Path); err != nil || !fi.IsDir() {
		return fmt.Errorf("%w: the folder %s does not exist (check \"create folder\")", ErrInvalid, r.Path)
	}
	// The whole definition goes in at once (one registry transaction): the share
	// is never published without valid users. Recreating means replacing.
	admins, err := m.adminsName(ctx)
	if err != nil { // never published without them because winbind blinked: the next save would keep it so
		return err
	}
	params, vfs := shareParams(r, admins)
	if m.Conf != nil {
		_, err := m.Conf.SetShare(ctx, r.Name, params, vfs)
		if errors.Is(err, sambabridge.ErrInvalid) {
			return fmt.Errorf("%w: %v", ErrInvalid, err)
		}
		return err
	}
	conf := "[" + r.Name + "]\n"
	for _, kv := range params {
		conf += "\t" + kv[0] + " = " + kv[1] + "\n"
	}
	f, err := os.CreateTemp("", "ad-manager-share-*.conf")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(conf); err != nil {
		f.Close()
		return err
	}
	f.Close()
	_, err = m.Run(ctx, "net", "conf", "import", f.Name(), r.Name)
	return err
}

// The domain's Domain Admins always get into every share, as Windows lets Administrators in: the folders' owners
// manage, back up and migrate them over SMB (the import's smbcacls, a sync with the administrator's account); the
// folder's Windows ACL still decides what they reach. The group is not shown among the share's groups.
const adminsGroup = "Domain Admins"

// withAdmins adds admins (this domain's Domain Admins as winbind names it; "" on a standalone server) to groups
// unless it is among them already (the same principal; a Domain Admins of another domain is another group).
func withAdmins(groups []string, admins string) []string {
	if admins == "" || slices.ContainsFunc(groups, func(g string) bool { return strings.EqualFold(g, admins) }) {
		return groups
	}
	return append(slices.Clone(groups), admins)
}

// shareParams is the share's definition in Samba parameters, and the VFS modules it adds (recycle bin, audit).
func shareParams(r ShareReq, admins string) (params [][2]string, vfs []string) {
	yesNo := map[bool]string{true: "yes", false: "no"}
	params = [][2]string{{"path", r.Path}, {"comment", r.Comment}, {"guest ok", "no"}, {"read only", "yes"},
		{"browseable", yesNo[!(r.Hidden || strings.HasSuffix(r.Name, "$"))]},
		{"valid users", smbList(withAdmins(slices.Concat(r.Full, r.RW, r.RO), admins), slices.Concat(r.FullUsers, r.RWUsers, r.ROUsers))},
		{"write list", smbList(withAdmins(slices.Concat(r.Full, r.RW), admins), slices.Concat(r.FullUsers, r.RWUsers))}}
	if r.ABE { // the share and, inside it, files and folders the user cannot read are not listed
		params = append(params, [2]string{"access based share enum", "yes"}, [2]string{"hide unreadable", "yes"})
	}
	// file renames and deletions and folder creations to syslog (local5): user|ip|share|operation|path (a file's
	// creation is not logged: full_audit only sees it as an open, like every read)
	if r.Audit {
		vfs = append(vfs, "full_audit")
		params = append(params, [2]string{"full_audit:prefix", "%u|%I|%S"},
			[2]string{"full_audit:success", "mkdirat renameat unlinkat"}, [2]string{"full_audit:failure", "none"},
			[2]string{"full_audit:facility", "local5"}, [2]string{"full_audit:priority", "notice"})
	}
	// deleted files go to .recycle/<user>, with their folders and versions. The bin's folders are group-writable
	// (0770): the first user creates .recycle itself, and with 0700 the others could not create theirs — and Samba
	// then deletes their files for good instead of failing
	if r.Recycle {
		vfs = append(vfs, "recycle")
		params = append(params, [2]string{"recycle:repository", ".recycle/%U"}, [2]string{"recycle:keeptree", "yes"},
			[2]string{"recycle:versions", "yes"}, [2]string{"recycle:touch", "yes"},
			[2]string{"recycle:exclude", "*.tmp,*.temp,~$*"}, [2]string{"recycle:exclude_dir", ".recycle"},
			[2]string{"recycle:directory_mode", "0770"}, [2]string{"recycle:subdir_mode", "0770"})
	}
	return params, vfs
}

// DeleteShare unpublishes the share; the folder and its files are kept.
func (m *Manager) DeleteShare(ctx context.Context, name string) error {
	if m.reserved(name) {
		return fmt.Errorf("%w: %q is a share of the domain controller itself", ErrInvalid, name)
	}
	if err := ValidName(name); err != nil {
		return err
	}
	if m.Conf != nil {
		return m.Conf.DeleteShare(ctx, name)
	}
	// already unpublished (queue redelivery after a success) counts as success
	if _, err := m.Run(ctx, "net", "conf", "showshare", name); err != nil {
		return nil
	}
	_, err := m.Run(ctx, "net", "conf", "delshare", name)
	return err
}

// List reads the shares from the registry configuration.
func (m *Manager) List(ctx context.Context) ([]Share, error) {
	if m.Conf != nil {
		list, err := m.Conf.ListShares(ctx)
		if err != nil {
			return nil, err
		}
		var conf strings.Builder // the same text `net conf list` prints
		for _, sc := range list {
			conf.WriteString("[" + sc.Name + "]\n")
			for _, kv := range sc.Params {
				conf.WriteString("\t" + kv[0] + " = " + kv[1] + "\n")
			}
		}
		return parseConf(conf.String(), m.adminsBestEffort(ctx)), nil
	}
	out, err := m.Run(ctx, "net", "conf", "list")
	if err != nil {
		return nil, err
	}
	return parseConf(out, m.adminsBestEffort(ctx)), nil
}

// adminsBestEffort is adminsName for reading: unknown, the group simply shows among the share's groups.
func (m *Manager) adminsBestEffort(ctx context.Context) string {
	a, _ := m.adminsName(ctx)
	return a
}

// parseConf parses `net conf list` output, skipping [global]; admins (the implicit Domain Admins) is left out of the
// groups unless the share has nobody else (it is shown then, so the share can be saved again as it is).
func parseConf(out, admins string) []Share {
	list := []Share{} // no share: [] in JSON (the panel maps the list), never null
	var cur *Share
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			name := line[1 : len(line)-1]
			cur = nil
			if !strings.EqualFold(name, "global") {
				list = append(list, Share{Name: name, Browse: true})
				cur = &list[len(list)-1]
			}
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if cur == nil || !ok {
			continue
		}
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
		switch k {
		case "path":
			cur.Path = v
		case "comment":
			cur.Comment = v
		case "browseable", "browsable":
			cur.Browse = v != "no" && v != "false"
		case "read only":
			cur.ReadOnly = v == "yes" || v == "true"
		case "write list":
			cur.RW, cur.RWUsers = splitGroups(v)
		case "valid users":
			cur.RO, cur.ROUsers = splitGroups(v)
		case "vfs objects":
			mods := strings.Fields(v)
			cur.Recycle, cur.Audit = slices.Contains(mods, "recycle"), slices.Contains(mods, "full_audit")
		case "access based share enum":
			cur.ABE = v == "yes" || v == "true"
		}
	}
	isAdmins := func(g string) bool { return admins != "" && strings.EqualFold(g, admins) }
	for i := range list { // valid users includes the writers: RO = valid − write; Domain Admins are implicit
		s := &list[i]
		others := slices.ContainsFunc(slices.Concat(s.RW, s.RO), func(g string) bool { return !isAdmins(g) }) ||
			len(s.RWUsers)+len(s.ROUsers) > 0
		if others {
			s.RW = slices.DeleteFunc(s.RW, isAdmins)
			s.RO = slices.DeleteFunc(s.RO, isAdmins)
		}
		list[i].RO = slices.DeleteFunc(list[i].RO, func(g string) bool { return slices.Contains(list[i].RW, g) })
		list[i].ROUsers = slices.DeleteFunc(list[i].ROUsers, func(u string) bool { return slices.Contains(list[i].RWUsers, u) })
	}
	return list
}

// splitGroups parses Samba lists and separates groups (@, +, &) from users:
// @"DOM\g 1" @DOM\g2 "DOM\ana", +group; quotes allow spaces in names.
func splitGroups(v string) (groups, users []string) {
	var cur strings.Builder
	quoted := false
	flush := func() {
		t := cur.String()
		cur.Reset()
		name := strings.TrimLeft(t, "@+&")
		switch {
		case name == "":
		case name != t:
			groups = append(groups, name)
		default:
			users = append(users, name)
		}
	}
	for _, r := range v {
		switch {
		case r == '"':
			quoted = !quoted
		case !quoted && (r == ' ' || r == ',' || r == '\t'):
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return groups, users
}

// Dirs lists the immediate subfolders of a path inside the roots (or the roots
// themselves when p is empty).
func (m *Manager) Dirs(p string) ([]string, error) {
	if p == "" {
		return m.Roots, nil
	}
	var err error
	if !slices.Contains(m.Roots, filepath.Clean(p)) {
		if p, err = m.SafePath(p); err != nil {
			return nil, err
		}
	}
	es, err := os.ReadDir(p)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, e := range es {
		if e.IsDir() {
			out = append(out, filepath.Join(p, e.Name()))
		}
	}
	return out, nil
}

// CSVHeader is the header of the folders-and-permissions file (panel
// export/import and `migrate shares --csv`). Lists are separated by "|"; it
// carries structure only, no files.
// The older Portuguese column names still import (ad.ReadCSV reads them as these).
var CSVHeader = []string{"share", "path", "comment", "hidden", "write_groups", "read_groups", "write_users", "read_users",
	"full_control_groups", "full_control_users", "recycle_bin", "abe", "audit"}

// CSVRow converts a share into a CSV row.
func CSVRow(s Share) []string {
	hidden := "no"
	if !s.Browse || strings.HasSuffix(s.Name, "$") {
		hidden = "yes"
	}
	j := func(l []string) string { return strings.Join(l, "|") }
	yn := map[bool]string{true: "yes", false: "no"}
	return []string{s.Name, s.Path, s.Comment, hidden, j(Without(s.RW, s.Full)), j(s.RO), j(Without(s.RWUsers, s.FullUsers)), j(s.ROUsers),
		j(s.Full), j(s.FullUsers), yn[s.Recycle], yn[s.ABE], yn[s.Audit]}
}

// Without returns list minus the names in drop (case-insensitive).
func Without(list, drop []string) []string {
	return slices.DeleteFunc(slices.Clone(list), func(n string) bool {
		return slices.ContainsFunc(drop, func(d string) bool { return strings.EqualFold(d, n) })
	})
}

// FromCSV builds a creation request from a parsed row (column → value).
func FromCSV(m map[string]string) ShareReq {
	l := func(k string) []string {
		var out []string
		for _, v := range strings.Split(m[k], "|") {
			if v = strings.TrimSpace(v); v != "" {
				out = append(out, v)
			}
		}
		return out
	}
	yes := func(k string) bool {
		switch strings.ToLower(strings.TrimSpace(m[k])) {
		case "yes", "y", "true", "1", "sim", "s": // sim, s: older Portuguese files
			return true
		}
		return false
	}
	return ShareReq{Name: m["share"], Path: m["path"], Comment: m["comment"],
		Hidden:  yes("hidden") || strings.HasSuffix(m["share"], "$"),
		Recycle: yes("recycle_bin"), ABE: yes("abe"), Audit: yes("audit"),
		RW: l("write_groups"), RO: l("read_groups"), RWUsers: l("write_users"), ROUsers: l("read_users"),
		Full: l("full_control_groups"), FullUsers: l("full_control_users"), CreatePath: true}
}

// Sizes returns the total size (bytes) of each share's folder, via `du -sb`.
// Shares whose folder fails (gone, no access) are left out.
func (m *Manager) Sizes(ctx context.Context) (map[string]int64, error) {
	list, err := m.List(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, s := range list {
		if s.Path == "" {
			continue
		}
		o, err := m.Run(ctx, "du", "-sb", "--", s.Path)
		if err != nil {
			continue
		}
		if f := strings.Fields(o); len(f) > 0 {
			if n, err := strconv.ParseInt(f[0], 10, 64); err == nil {
				out[s.Name] = n
			}
		}
	}
	return out, nil
}
