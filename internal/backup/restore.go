package backup

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Restore targets of a fileserver-data backup.
const (
	TargetNew      = "new"      // a new folder <root>/_restored/<time>/ (default): nothing current is touched
	TargetOriginal = "original" // the folders where they were (the current ones are backed up first)
)

// Parts of a fileserver-config backup that can be restored, each on its own.
const (
	PartSmbConf = "smbconf" // /etc/samba/smb.conf (checked with testparm; the current one kept beside it)
	PartShares  = "shares"  // the registry shares (net conf import)
	PartACLs    = "acls"    // NT ACL and POSIX ACL of the share roots that still exist
)

// RestoreDir is the folder, inside a share root, where restorations to a new folder go.
const RestoreDir = "_restored"

// LegacyRestoreDir is RestoreDir's old name: restorations already made there stay out of backups and protected.
const LegacyRestoreDir = "_restaurado"

// IsRestoreDir tells whether name is the restorations' folder, under its current or its old name.
func IsRestoreDir(name string) bool { return name == RestoreDir || name == LegacyRestoreDir }

// RestoreJob is one restoration.
type RestoreJob struct {
	ID      string   `json:"id"`              // the backup
	Target  string   `json:"target"`          // fileserver-data: new (default) or original
	Folders []string `json:"folders"`         // fileserver-data: only these of the backup's folders (empty: all)
	Parts   []string `json:"parts,omitempty"` // fileserver-config: what to restore
	By      string   `json:"by"`
	Confirm string   `json:"confirm,omitempty"` // the backup's id, when the restoration overwrites (checked by the agent)
}

// Overwrites tells whether the restoration of a backup of kind changes what is in use (and so needs a typed
// confirmation and a backup of the current state first).
func (j RestoreJob) Overwrites(kind string) bool {
	return kind != KindFileData || j.Target == TargetOriginal
}

// RestoreStatus is the last restoration of a store (restore.json).
type RestoreStatus struct {
	Backup    string    `json:"backup"`
	Kind      string    `json:"kind"`
	Target    string    `json:"target,omitempty"`
	Dest      string    `json:"dest,omitempty"`       // where the files went
	PreBackup string    `json:"pre_backup,omitempty"` // the backup of the current state taken first
	By        string    `json:"by,omitempty"`
	Started   time.Time `json:"started"`
	Finished  time.Time `json:"finished,omitzero"`
	Status    string    `json:"status"` // running, ok, error
	Error     string    `json:"error,omitempty"`
	Log       []string  `json:"log,omitempty"`
}

// LastRestore returns the last restoration (nil: none yet).
func (s *Store) LastRestore() *RestoreStatus {
	b, err := os.ReadFile(filepath.Join(s.Dir, "restore.json"))
	if err != nil {
		return nil
	}
	var st RestoreStatus
	if json.Unmarshal(b, &st) != nil {
		return nil
	}
	return &st
}

func (s *Store) writeRestore(st RestoreStatus) {
	b, _ := json.MarshalIndent(st, "", "  ")
	tmp := filepath.Join(s.Dir, "restore.json.tmp")
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, filepath.Join(s.Dir, "restore.json"))
	}
}

// Restore restores a backup of this store (one backup or restoration at a time: ErrBusy otherwise). The caller
// has checked the job against its server (roles, roots, confirmation); src are the current share roots.
func (s *Store) Restore(ctx context.Context, run Runner, j RestoreJob, src Sources) (RestoreStatus, error) {
	if !s.mu.TryLock() {
		return RestoreStatus{}, ErrBusy
	}
	defer s.mu.Unlock()
	m, err := s.Get(j.ID)
	if err == nil && (m.Status != "ok" || m.File == "") {
		err = ErrNotFound
	}
	if err != nil {
		return RestoreStatus{}, err
	}
	st := RestoreStatus{Backup: m.ID, Kind: m.Kind, By: j.By, Started: time.Now().UTC(), Status: "running"}
	s.writeRestore(st)
	work, err := os.MkdirTemp(s.Dir, ".restore-")
	if err == nil {
		defer os.RemoveAll(work)
		switch m.Kind {
		case KindFileData:
			err = s.restoreData(ctx, run, &st, m, j, src)
		case KindFileConfig:
			err = s.restoreConfig(ctx, run, &st, m, j, src, work)
		case KindSambaFiles:
			err = s.restoreSambaFiles(ctx, run, &st, m)
		case KindDC:
			err = errors.New("a domain controller backup is restored by the initial setup on a new server (role: restore a DC), never over a DC in use")
		default:
			err = fmt.Errorf("a %s backup is not restored by an agent", m.Kind)
		}
	}
	st.Finished = time.Now().UTC()
	st.Status = "ok"
	if err != nil {
		st.Status, st.Error = "error", err.Error()
	}
	s.writeRestore(st)
	return st, err
}

// verify checks a backup's archive against the SHA-256 of its manifest.
func (s *Store) verify(m Manifest) (string, error) {
	p := filepath.Join(s.Dir, filepath.Base(m.File))
	sum, _, err := sha256File(p)
	if err != nil {
		return "", err
	}
	if m.SHA256 != "" && sum != m.SHA256 {
		return "", fmt.Errorf("%s: the archive does not match its SHA-256 (damaged or changed)", m.ID)
	}
	return p, nil
}

// chainTo is what restoring m needs, oldest first: the full backup of its chain and the incrementals up to m.
func (s *Store) chainTo(m Manifest) ([]Manifest, error) {
	if m.Full {
		return []Manifest{m}, nil
	}
	head, err := s.Get(m.Chain)
	if err != nil {
		return nil, fmt.Errorf("the full backup %s of this chain is gone: it cannot be restored", m.Chain)
	}
	deps, err := s.Dependents(m.Chain)
	if err != nil {
		return nil, err
	}
	out := []Manifest{head}
	for _, d := range deps {
		if d.Status != "ok" {
			continue // a failed incremental holds nothing (the snapshot moved on only with an ok one)
		}
		out = append(out, d)
		if d.ID == m.ID {
			return out, nil
		}
	}
	return nil, fmt.Errorf("%s is not in the chain of %s", m.ID, m.Chain)
}

// entryName checks one archive entry name: relative, clean, no "..", inside one of prefixes (none: anywhere).
func entryName(name string, prefixes []string) (string, error) {
	n := strings.TrimSuffix(strings.TrimPrefix(name, "./"), "/")
	if n == "" || path.IsAbs(n) || path.Clean(n) != n || n == ".." || strings.HasPrefix(n, "../") || strings.Contains(n, "/../") {
		return "", fmt.Errorf("unsafe name in the archive: %q", name)
	}
	if len(prefixes) == 0 {
		return n, nil
	}
	for _, p := range prefixes {
		if n == p || strings.HasPrefix(n, p+"/") {
			return n, nil
		}
	}
	return "", fmt.Errorf("%q in the archive is outside what this backup holds", name)
}

// scanTarGz reads every entry of a .tar.gz, refusing unsafe names, links out of prefixes, special files and GNU
// incremental directory lists that name anything out of prefixes (they rename and delete when extracted); each
// entry goes to fn (with its content reader) when fn is not nil.
func scanTarGz(file string, prefixes []string, fn func(h *tar.Header, name string, r io.Reader) error) (info scanInfo, err error) {
	f, err := os.Open(file)
	if err != nil {
		return info, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return info, fmt.Errorf("not a .tar.gz: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return info, nil
		}
		if err != nil {
			return info, fmt.Errorf("damaged archive: %w", err)
		}
		name, err := entryName(h.Name, prefixes)
		if err != nil {
			return info, err
		}
		var r io.Reader = tr
		switch h.Typeflag {
		case tar.TypeReg, tar.TypeDir, tar.TypeSymlink, tar.TypeGNUSparse:
		case 'D': // GNU tar's incremental directory list: its content is the list, checked whole
			if h.Size > 256<<20 {
				return info, fmt.Errorf("%s: directory list too large", name)
			}
			var b bytes.Buffer
			d, err := checkDumpdir(name, io.TeeReader(tr, &b), prefixes)
			if err != nil {
				return info, err
			}
			info.add(d)
			r = &b
		case tar.TypeLink: // a hard link must point inside too
			if _, err := entryName(h.Linkname, prefixes); err != nil {
				return info, fmt.Errorf("hard link %q: %w", h.Name, err)
			}
		default:
			return info, fmt.Errorf("%q: special files are not restored", h.Name)
		}
		if d, ok := h.PAXRecords["GNU.dumpdir"]; ok { // the same list in the POSIX format
			di, err := checkDumpdir(name, strings.NewReader(d), prefixes)
			if err != nil {
				return info, err
			}
			info.add(di)
		}
		if fn != nil {
			if err := fn(h, name, r); err != nil {
				return info, err
			}
		}
	}
}

// scanInfo is what the directory lists of an archive tell: an incremental one (files marked as not dumped) and one
// that renames folders.
type scanInfo struct {
	incremental, renames bool
}

func (i *scanInfo) add(o scanInfo) {
	i.incremental = i.incremental || o.incremental
	i.renames = i.renames || o.renames
}

// checkDumpdir checks a GNU incremental directory list (NUL-separated, a code letter then a name): Y, N and D name
// entries of the directory itself (a base name); R, T and X name what extraction renames, by their full name in the
// archive (inside prefixes). It tells whether some file is marked as not dumped (N: an incremental archive).
func checkDumpdir(dir string, r io.Reader, prefixes []string) (info scanInfo, err error) {
	br := bufio.NewReader(r)
	for {
		e, err := br.ReadString(0)
		if errors.Is(err, io.EOF) {
			if e != "" {
				return info, fmt.Errorf("%s: damaged directory list", dir)
			}
			return info, nil
		}
		if err != nil {
			return info, err
		}
		e = strings.TrimSuffix(e, "\x00")
		if e == "" {
			continue
		}
		code, n := e[0], e[1:]
		switch code {
		case 'Y', 'N', 'D':
			if n == "" || n == "." || n == ".." || strings.Contains(n, "/") {
				return info, fmt.Errorf("%s: unsafe name in the directory list: %q", dir, n)
			}
			// N: not dumped (an incremental), except what the backups leave out on purpose (restored copies)
			info.incremental = info.incremental || (code == 'N' && !IsRestoreDir(n))
		case 'R', 'T', 'X':
			if n == "" {
				continue
			}
			if _, err := entryName(strings.TrimPrefix(n, "/"), prefixes); err != nil {
				return info, fmt.Errorf("%s: rename in the directory list: %w", dir, err)
			}
			info.renames = true
		default:
			return info, fmt.Errorf("%s: unknown entry in the directory list", dir)
		}
	}
}

// rel turns absolute folders into the names tar keeps (without the leading /).
func rel(folders []string) []string {
	out := make([]string, len(folders))
	for i, f := range folders {
		out[i] = strings.TrimPrefix(filepath.Clean(f), "/")
	}
	return out
}

// rootOf is the share root (real path) that holds p, or "".
func rootOf(p string, roots []string) string {
	for _, r := range roots {
		if rr, err := filepath.EvalSymlinks(r); err == nil {
			r = rr
		}
		r = filepath.Clean(r)
		if p == r || strings.HasPrefix(p, r+"/") {
			return r
		}
	}
	return ""
}

// restoreData extracts the chain up to m (full, then each incremental in order, as GNU tar replays them) into a
// new folder inside the share root, or over the original folders after backing them up.
func (s *Store) restoreData(ctx context.Context, run Runner, st *RestoreStatus, m Manifest, j RestoreJob, src Sources) error {
	// the backup's folders, or folders inside them (one folder out of a complete backup)
	folders := m.Folders
	if len(j.Folders) > 0 {
		folders = nil
		for _, f := range j.Folders {
			f = filepath.Clean(f)
			if !filepath.IsAbs(f) || rootOf(f, m.Folders) == "" {
				return fmt.Errorf("%q is not a folder of this backup", f)
			}
			folders = append(folders, f)
		}
	}
	if len(folders) == 0 {
		return errors.New("this backup has no folders")
	}
	chain, err := s.chainTo(m)
	if err != nil {
		return err
	}
	var files []string
	var members [][]string // per archive, the chosen folders it has (a folder made later is not in the older ones)
	renames := false
	for _, c := range chain {
		p, err := s.verify(c)
		dirs := map[string]bool{}
		if err == nil {
			var info scanInfo
			info, err = scanTarGz(p, rel(m.Folders), func(h *tar.Header, name string, _ io.Reader) error {
				if h.Typeflag == tar.TypeDir || h.Typeflag == 'D' {
					dirs[name] = true
				}
				return nil
			})
			renames = renames || info.renames
		}
		if err != nil {
			return fmt.Errorf("%s: %w", c.ID, err)
		}
		var has []string
		for _, f := range rel(folders) {
			if dirs[f] {
				has = append(has, f)
			}
		}
		files, members = append(files, p), append(members, has)
	}
	for _, f := range rel(folders) {
		if !slices.Contains(members[len(members)-1], f) {
			return fmt.Errorf("%q is not a folder of this backup", "/"+f)
		}
		// a folder inside the backup's, in a chain where folders were renamed: its unchanged files may live in an
		// older archive under the old name, which extracting only it would leave out
		if renames && !slices.Contains(rel(m.Folders), f) {
			return fmt.Errorf("folders were renamed in this chain: restore %s whole (to a new folder) and take %q from it", "/"+rel(m.Folders)[0], "/"+f)
		}
	}
	st.Target = j.Target
	dest := "/"
	if j.Target == TargetOriginal {
		// GNU tar replays deletions: a file system mounted inside a folder (left out of the backup by
		// --one-file-system) would come back empty
		if mp := MountsUnder(folders); mp != "" {
			return fmt.Errorf("%s is a mount point inside the folders: restore to a new folder", mp)
		}
		var now []string // the current folders (a deleted one has nothing to keep)
		for _, f := range folders {
			if _, err := os.Lstat(f); err == nil {
				now = append(now, f)
			}
		}
		if len(now) > 0 {
			pre, err := s.runLocked(ctx, run, Job{Kind: KindFileData, Origin: "pre-restore", By: j.By, Folders: now, Full: true, Note: "before restoring " + m.ID}, src)
			if err != nil {
				return fmt.Errorf("backup of the current folders: %w", err)
			}
			st.PreBackup = pre.ID
			st.Log = append(st.Log, "current folders backed up: "+pre.ID)
		}
	} else {
		st.Target = TargetNew
		root := rootOf(filepath.Clean(folders[0]), src.Roots)
		if root == "" {
			root = rootOf(filepath.Clean(src.firstRoot()), src.Roots)
		}
		if root == "" {
			return errors.New("no share root on this server to restore into")
		}
		// the restorations' folder is root's alone (the share's users cannot reach into it), and tar gets the new
		// folder by an open handle: renaming folders in the share meanwhile cannot send it elsewhere
		dest = filepath.Join(root, RestoreDir, time.Now().Format("2006-01-02_150405.000"))
	}
	st.Dest = dest
	s.writeRestore(*st)
	var dir string
	var pin *os.File
	if j.Target == TargetOriginal {
		dir, pin, err = pinDir(dest)
	} else {
		dir, pin, err = makeRestoreDir(filepath.Dir(filepath.Dir(dest)), RestoreDir, filepath.Base(dest))
	}
	if err != nil {
		return err
	}
	defer pin.Close()
	for i, f := range files {
		if len(members[i]) == 0 {
			continue // none of the chosen folders existed yet
		}
		args := []string{"--extract", "--gzip", "--file=" + f, "--listed-incremental=/dev/null", "--acls", "--xattrs",
			"--xattrs-include=security.NTACL", "--xattrs-include=user.*", "--directory=" + dir, "--"}
		args = append(args, members[i]...)
		if _, err := run(ctx, "tar", args...); err != nil {
			return fmt.Errorf("%s: %w", chain[i].ID, err)
		}
		st.Log = append(st.Log, "extracted "+chain[i].ID)
		s.writeRestore(*st)
	}
	return nil
}

// mountInfo lists the mounts (tests replace it).
var mountInfo = "/proc/self/mountinfo"

// MountsUnder returns a mount point strictly inside one of folders, or "".
func MountsUnder(folders []string) string {
	b, err := os.ReadFile(mountInfo)
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) < 5 {
			continue
		}
		mp := unescapeMount(f[4])
		for _, d := range folders {
			if strings.HasPrefix(mp, filepath.Clean(d)+"/") {
				return mp
			}
		}
	}
	return ""
}

// unescapeMount undoes mountinfo's octal escapes (\040 for a space).
func unescapeMount(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func (src Sources) firstRoot() string {
	if len(src.Roots) == 0 {
		return ""
	}
	return src.Roots[0]
}

// restoreConfig restores the chosen parts of a fileserver-config backup after backing up the current
// configuration.
func (s *Store) restoreConfig(ctx context.Context, run Runner, st *RestoreStatus, m Manifest, j RestoreJob, src Sources, work string) error {
	if len(j.Parts) == 0 {
		return errors.New("choose what to restore: smbconf, shares, acls")
	}
	for _, p := range j.Parts {
		if p != PartSmbConf && p != PartShares && p != PartACLs {
			return fmt.Errorf("unknown part %q", p)
		}
	}
	file, err := s.verify(m)
	if err != nil {
		return err
	}
	files := map[string][]byte{}
	var total int64
	_, err = scanTarGz(file, nil, func(h *tar.Header, name string, r io.Reader) error {
		keep := name == "smb.conf" || name == "net-conf-list.txt" || strings.HasPrefix(name, "acl/")
		if h.Typeflag != tar.TypeReg || !keep {
			return nil // the member databases and the rest are not restored here
		}
		if total += h.Size; h.Size > 16<<20 || total > 64<<20 {
			return fmt.Errorf("%s: too large for a configuration backup", name)
		}
		b, err := io.ReadAll(io.LimitReader(r, h.Size))
		files[name] = b
		return err
	})
	if err != nil {
		return err
	}
	pre, err := s.runLocked(ctx, run, Job{Kind: KindFileConfig, Origin: "pre-restore", By: j.By, Note: "before restoring " + m.ID}, src)
	if err != nil {
		return fmt.Errorf("backup of the current configuration: %w", err)
	}
	st.PreBackup = pre.ID
	st.Log = append(st.Log, "current configuration backed up: "+pre.ID)
	s.writeRestore(*st)
	put := func(name string) (string, error) {
		b, ok := files[name]
		if !ok {
			return "", fmt.Errorf("%s is not in this backup", name)
		}
		p := filepath.Join(work, strings.ReplaceAll(name, "/", "_"))
		return p, os.WriteFile(p, b, 0o600)
	}
	if slices.Contains(j.Parts, PartSmbConf) {
		p, err := put("smb.conf")
		if err == nil {
			_, err = run(ctx, "testparm", "-s", "--suppress-prompt", p)
		}
		conf := src.SmbConf
		if conf == "" {
			conf = "/etc/samba/smb.conf"
		}
		// through run: the agent's own sandbox keeps /etc read-only
		if err == nil {
			_, err = run(ctx, "cp", "--preserve=mode,ownership", "--", conf, conf+".before-restore")
		}
		if err == nil {
			_, err = run(ctx, "install", "-m", "0644", "--", p, conf+".tmp")
		}
		if err == nil {
			_, err = run(ctx, "mv", "-f", "--", conf+".tmp", conf)
		}
		if err != nil {
			return fmt.Errorf("smb.conf: %w", err)
		}
		if _, err := run(ctx, "smbcontrol", "all", "reload-config"); err != nil {
			st.Log = append(st.Log, "warning: reload-config: "+err.Error())
		}
		st.Log = append(st.Log, "smb.conf restored (the previous one is "+conf+".before-restore)")
	}
	if slices.Contains(j.Parts, PartShares) {
		p, err := put("net-conf-list.txt")
		if err == nil {
			_, err = run(ctx, "net", "conf", "import", "-T", p) // test mode first: a file that does not parse changes nothing
		}
		if err == nil {
			_, err = run(ctx, "net", "conf", "import", p)
		}
		if err != nil {
			return fmt.Errorf("shares: %w", err)
		}
		st.Log = append(st.Log, "registry shares restored (net conf import)")
	}
	if slices.Contains(j.Parts, PartACLs) {
		n := 0
		for name, b := range files {
			if !strings.HasPrefix(name, "acl/") {
				continue
			}
			p, sddl, facl := parseACLFile(string(b))
			if !slices.Contains(cleanAll(src.Roots), p) {
				st.Log = append(st.Log, "skipped: "+p+" is not a share root here")
				continue
			}
			if sddl != "" {
				if _, err := run(ctx, "samba-tool", "ntacl", "set", sddl, p); err != nil {
					return fmt.Errorf("NT ACL of %s: %w", p, err)
				}
			}
			if entries, ok := aclEntries(facl); ok {
				fp := filepath.Join(work, fmt.Sprintf("facl-%d", n))
				if err := os.WriteFile(fp, []byte("# file: "+p+"\n"+entries), 0o600); err != nil {
					return err
				}
				if _, err := run(ctx, "setfacl", "--restore="+fp); err != nil {
					return fmt.Errorf("POSIX ACL of %s: %w", p, err)
				}
			}
			n++
		}
		st.Log = append(st.Log, fmt.Sprintf("ACLs of %d share roots restored", n))
	}
	return nil
}

// parseACLFile reads an acl/NN.txt of a fileserver-config backup: "path: …", "sddl: …", a blank line, then
// getfacl -p's output.
func parseACLFile(s string) (p, sddl, facl string) {
	head, facl, _ := strings.Cut(s, "\n\n")
	for _, l := range strings.Split(head, "\n") {
		if v, ok := strings.CutPrefix(l, "path: "); ok {
			p = filepath.Clean(v)
		}
		if v, ok := strings.CutPrefix(l, "sddl: "); ok {
			sddl = strings.TrimSpace(v)
		}
	}
	return p, sddl, facl
}

// aclRe is one ACL entry as getfacl writes it (after the "#effective" note is cut).
var aclRe = regexp.MustCompile(`^(default:)?(user|group|mask|other):[^:\s]*:[rwx-]{3}$`)

// aclEntries takes the entries of the first section of getfacl output (its comments dropped): the file setfacl
// --restore gets is rebuilt from them for the one root, so nothing else in the backup reaches setfacl. ok is false
// when a line is not an ACL entry.
func aclEntries(facl string) (string, bool) {
	var b strings.Builder
	for _, l := range strings.Split(facl, "\n") {
		l, _, _ = strings.Cut(l, "#effective")
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "#") {
			continue
		}
		if l == "" {
			if b.Len() > 0 {
				break // end of the first section
			}
			continue
		}
		if !aclRe.MatchString(l) {
			return "", false
		}
		b.WriteString(l + "\n")
	}
	return b.String(), b.Len() > 0
}

func cleanAll(ps []string) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = filepath.Clean(p)
	}
	return out
}

// sambaUnits are stopped while Samba's own files are replaced.
var sambaUnits = []string{"samba-ad-dc", "smbd", "nmbd", "winbind"}

// restoreSambaFiles puts /etc/samba, /var/lib/samba and krb5.conf back as the backup has them, with Samba stopped:
// the current ones are moved aside (…before-restore-<time>), not deleted.
func (s *Store) restoreSambaFiles(ctx context.Context, run Runner, st *RestoreStatus, m Manifest) error {
	file, err := s.verify(m)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	_, err = scanTarGz(file, rel(sambaPaths), func(_ *tar.Header, name string, _ io.Reader) error {
		for _, p := range rel(sambaPaths) {
			if name == p || strings.HasPrefix(name, p+"/") {
				have[p] = true
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	var stopped []string
	// the undo and the restarts run even when ctx ended (the task's deadline), each with its own time: Samba never
	// stays down
	after := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
	}
	defer func() { // every unit stopped here comes back, whatever happened
		for _, u := range stopped {
			actx, cancel := after()
			_, err := run(actx, "systemctl", "start", u)
			cancel()
			if err != nil {
				st.Log = append(st.Log, "warning: "+u+" did not start: "+err.Error())
			}
		}
	}()
	for _, u := range sambaUnits {
		if _, err := run(ctx, "systemctl", "is-active", "--quiet", u); err == nil {
			if _, err := run(ctx, "systemctl", "stop", u); err != nil {
				return fmt.Errorf("stopping %s: %w", u, err)
			}
			stopped = append(stopped, u)
		}
	}
	stamp := time.Now().Format("20060102-150405")
	var moved [][2]string
	// moves go through run (the agent's sandbox keeps /etc read-only)
	undo := func() {
		for _, mv := range slices.Backward(moved) {
			actx, cancel := after()
			_, _ = run(actx, "rm", "-rf", "--", mv[0])
			_, _ = run(actx, "mv", "-T", "--", mv[1], mv[0])
			cancel()
		}
	}
	var members []string
	for _, p := range sambaPaths {
		r := strings.TrimPrefix(p, "/")
		if !have[r] {
			continue // the backup does not have it: what is here stays
		}
		members = append(members, r)
		if _, err := os.Lstat(p); err == nil {
			aside := p + ".before-restore-" + stamp
			if _, err := run(ctx, "mv", "-T", "--", p, aside); err != nil {
				undo()
				return err
			}
			moved = append(moved, [2]string{p, aside})
			st.Log = append(st.Log, p+" moved to "+aside)
		}
	}
	args := append([]string{"--extract", "--gzip", "--file=" + file, "--acls", "--xattrs", "--xattrs-include=*", "--directory=/", "--"}, members...)
	if _, err := run(ctx, "tar", args...); err != nil {
		undo()
		return err
	}
	st.Dest = "/"
	st.Log = append(st.Log, "restored: "+strings.Join(members, ", "))
	return nil
}

// ExtractFile copies one regular file of a .tar.gz backup (checked against its SHA-256) to dst (0600), up to max
// bytes.
func (s *Store) ExtractFile(id, name, dst string, max int64) error {
	m, err := s.Get(id)
	if err == nil && (m.Status != "ok" || m.File == "") {
		err = ErrNotFound
	}
	if err != nil {
		return err
	}
	file, err := s.verify(m)
	if err != nil {
		return err
	}
	found := false
	_, err = scanTarGz(file, nil, func(h *tar.Header, n string, r io.Reader) error {
		if n != name || h.Typeflag != tar.TypeReg {
			return nil
		}
		if h.Size > max {
			return fmt.Errorf("%s: larger than %d MiB", name, max>>20)
		}
		found = true
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, io.LimitReader(r, h.Size)); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
	if err == nil && !found {
		err = fmt.Errorf("%s is not in this backup", name)
	}
	return err
}

// UploadKinds are the kinds a backup may be sent as (a DC backup goes to the initial setup instead).
var UploadKinds = []string{KindFileConfig, KindFileData, KindSambaFiles, KindPanel}

// Import keeps a backup sent from elsewhere (a .tar.gz of kind, at most max bytes): its content is checked for the
// kind, then it is listed with origin "upload" like the backups made here (a data backup as a chain of its own).
func (s *Store) Import(r io.Reader, kind, by string, max int64) (Manifest, error) {
	if !slices.Contains(UploadKinds, kind) {
		return Manifest{}, fmt.Errorf("a %q backup cannot be sent", kind)
	}
	if !s.mu.TryLock() {
		return Manifest{}, ErrBusy
	}
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return Manifest{}, err
	}
	now := time.Now()
	m := Manifest{ID: newID(kind, now), Kind: kind, Origin: "upload", By: by, Full: true, Started: now.UTC()}
	m.Chain, m.File = m.ID, m.ID+".tar.gz"
	dst := filepath.Join(s.Dir, m.File)
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return m, err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(r, max+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > max {
		err = fmt.Errorf("the backup is larger than the limit (%d MiB)", max>>20)
	}
	if err == nil && n == 0 {
		err = errors.New("empty file")
	}
	if err == nil {
		m.Folders, err = checkUpload(dst, kind)
	}
	if err != nil {
		os.Remove(dst)
		return m, err
	}
	m.Size, m.SHA256 = n, hex.EncodeToString(h.Sum(nil))
	m.Finished, m.Status = time.Now().UTC(), "ok"
	m.Log = []string{"sent by " + by}
	return m, s.write(m)
}

// checkUpload checks that a sent archive is what its kind holds; a data backup returns its folders (the
// directories with no parent in the archive).
func checkUpload(file, kind string) ([]string, error) {
	var names []string
	dirs := map[string]bool{}
	var db []byte
	var prefixes []string
	if kind == KindSambaFiles {
		prefixes = rel(sambaPaths)
	}
	info, err := scanTarGz(file, prefixes, func(h *tar.Header, name string, r io.Reader) error {
		names = append(names, name)
		if h.Typeflag == tar.TypeDir || h.Typeflag == 'D' {
			dirs[name] = true
		}
		if kind == KindPanel && name == "ad-manager.db" {
			db = make([]byte, 16)
			_, err := io.ReadFull(r, db)
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if info.incremental {
		return nil, errors.New("an incremental backup cannot be sent alone: send the full backup of its chain")
	}
	switch kind {
	case KindFileConfig:
		if !slices.Contains(names, "smb.conf") {
			return nil, errors.New("not a file server configuration backup (no smb.conf)")
		}
	case KindPanel:
		if !bytes.Equal(db, []byte("SQLite format 3\x00")) {
			return nil, errors.New("not a panel backup (no ad-manager.db)")
		}
	case KindSambaFiles:
		if len(names) == 0 {
			return nil, errors.New("empty archive")
		}
	case KindFileData:
		var folders []string
		for d := range dirs {
			top := true
			for p := path.Dir(d); p != "." && p != "/"; p = path.Dir(p) {
				if dirs[p] {
					top = false
					break
				}
			}
			if top {
				folders = append(folders, "/"+d)
			}
		}
		if len(folders) == 0 {
			return nil, errors.New("not a data backup (no folders)")
		}
		slices.Sort(folders)
		for _, n := range names { // everything must be inside one of the folders
			if _, err := entryName(n, rel(folders)); err != nil {
				return nil, err
			}
		}
		return folders, nil
	}
	return nil, nil
}
