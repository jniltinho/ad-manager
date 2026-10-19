package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// lookPath finds a program (tests replace it).
var lookPath = exec.LookPath

// Runner runs a command and returns its combined output.
type Runner func(ctx context.Context, name string, args ...string) (string, error)

// Exec is the real Runner.
func Exec(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(lastLine(string(out))))
	}
	return string(out), nil
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// Job is one backup to make.
type Job struct {
	Kind    string   `json:"kind"`
	Origin  string   `json:"origin"`  // "manual" or "schedule:<id>"
	By      string   `json:"by"`      // who asked
	Folders []string `json:"folders"` // fileserver-data: absolute folders (the caller checks they are allowed)
	// fileserver-data: start a new chain (full backup) when the current one's full is this many days old (0: 7)
	FullEveryDays int       `json:"full_every_days"`
	Full          bool      `json:"full,omitempty"` // fileserver-data: a full backup now (a new chain)
	Retention     Retention `json:"retention"`
	Note          string    `json:"note,omitempty"`
}

// Sources are what the file-server kinds read: the share roots (their NT ACLs) and the ACL history file.
type Sources struct {
	Roots      []string
	ACLHistory string
	SmbConf    string // default /etc/samba/smb.conf
	// panel
	Database, Config, AuditLog string
}

// Run makes one backup (one at a time per store: ErrBusy otherwise), records it in a manifest, then applies the
// job's retention to its origin. The returned manifest is the final one (status ok or error).
func (s *Store) Run(ctx context.Context, run Runner, j Job, src Sources) (Manifest, error) {
	if !s.mu.TryLock() {
		return Manifest{}, ErrBusy
	}
	defer s.mu.Unlock()
	return s.runLocked(ctx, run, j, src)
}

// runLocked is Run under the store's lock (a restoration backs up the current state with it held).
func (s *Store) runLocked(ctx context.Context, run Runner, j Job, src Sources) (Manifest, error) {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return Manifest{}, err
	}
	if err := os.Chmod(s.Dir, 0o700); err != nil { // an existing folder too: the archives hold secrets
		return Manifest{}, err
	}
	now := time.Now()
	m := Manifest{ID: newID(j.Kind, now), Kind: j.Kind, Origin: j.Origin, By: j.By, Started: now.UTC(), Status: "running", Note: j.Note}
	if m.Origin == "" {
		m.Origin = "manual"
	}
	m.Chain, m.Full = m.ID, true
	if err := s.checkSpace(j.Kind); err != nil {
		return m, err
	}
	if err := s.write(m); err != nil {
		return m, err
	}
	work, err := os.MkdirTemp(s.Dir, ".work-")
	if err != nil {
		return m, err
	}
	defer os.RemoveAll(work)
	var file, nextSnar string
	switch j.Kind {
	case KindDC:
		file, err = s.runDC(ctx, run, &m, work)
	case KindFileConfig:
		file, err = s.runConfig(ctx, run, &m, src)
	case KindFileData:
		file, err = s.runData(ctx, run, &m, j, work)
		nextSnar = filepath.Join(work, "snapshot.snar")
	case KindPanel:
		file, err = s.runPanel(ctx, run, &m, src, work)
	case KindSambaFiles:
		file, err = s.runSambaFiles(ctx, run, &m)
	default:
		err = fmt.Errorf("unknown backup kind %q", j.Kind)
	}
	m.Finished = time.Now().UTC()
	if err == nil {
		m.File = filepath.Base(file)
		m.SHA256, m.Size, err = sha256File(file)
	}
	if err != nil {
		m.Status, m.Error = "error", err.Error()
		if file != "" {
			os.Remove(file)
		}
		m.File = ""
		_ = s.write(m)
		return m, err
	}
	m.Status = "ok"
	if err := s.write(m); err != nil {
		return m, err
	}
	if nextSnar != "" { // the chain's snapshot moves on only once this backup is published (ok), atomically
		dst := filepath.Join(s.Dir, m.Chain+".snar")
		err := copyFile(nextSnar, dst+".tmp")
		if err == nil {
			err = os.Rename(dst+".tmp", dst)
		}
		if err != nil {
			m.Status, m.Error = "error", "chain snapshot: "+err.Error()
			_ = s.write(m)
			return m, err
		}
	}
	if gone, err := s.Prune(m.Origin, j.Retention, m.ID, time.Now()); err != nil {
		m.Log = append(m.Log, "retention: "+err.Error())
		_ = s.write(m)
	} else if len(gone) > 0 {
		m.Log = append(m.Log, "retention removed: "+strings.Join(gone, ", "))
		_ = s.write(m)
	}
	return m, nil
}

// runDC runs samba-tool domain backup offline into work and moves its archive into the store.
func (s *Store) runDC(ctx context.Context, run Runner, m *Manifest, work string) (string, error) {
	if _, err := lookPath("tdbbackup"); err != nil { // samba-tool needs it for the secrets and fails half-way without
		return "", errors.New("tdbbackup not found: install the tdb-tools package")
	}
	if _, err := run(ctx, "samba-tool", "domain", "backup", "offline", "--targetdir="+work); err != nil {
		return "", err
	}
	got, _ := filepath.Glob(filepath.Join(work, "samba-backup-*.tar.bz2"))
	if len(got) != 1 {
		return "", errors.New("samba-tool wrote no backup archive")
	}
	dst := filepath.Join(s.Dir, m.ID+".tar.bz2")
	if err := os.Rename(got[0], dst); err != nil {
		return "", err
	}
	m.Log = append(m.Log, "samba-tool domain backup offline: "+filepath.Base(got[0]))
	return dst, os.Chmod(dst, 0o600)
}

// runConfig archives smb.conf, the effective configuration, the registry shares, the NT ACL (SDDL) and POSIX ACL of
// each share root, and the ACL history.
func (s *Store) runConfig(ctx context.Context, run Runner, m *Manifest, src Sources) (string, error) {
	dst := filepath.Join(s.Dir, m.ID+".tar.gz")
	w, err := newTarGz(dst)
	if err != nil {
		return "", err
	}
	conf := src.SmbConf
	if conf == "" {
		conf = "/etc/samba/smb.conf"
	}
	add := func(name, text string) error { return w.addBytes(name, []byte(text)) }
	if err = w.addFile("smb.conf", conf); err == nil {
		var out string
		if out, err = run(ctx, "testparm", "-s"); err == nil || out != "" {
			err = add("testparm.txt", out)
		}
	}
	if err == nil {
		out, _ := run(ctx, "net", "conf", "list") // empty without registry shares
		err = add("net-conf-list.txt", out)
	}
	for i, root := range src.Roots {
		if err != nil {
			break
		}
		sddl, serr := run(ctx, "samba-tool", "ntacl", "get", "--as-sddl", root)
		facl, ferr := run(ctx, "getfacl", "-p", root)
		for _, e := range []error{serr, ferr} {
			if e != nil { // kept as a warning: the rest of the configuration is still worth having
				m.Log = append(m.Log, "warning: "+root+": "+e.Error())
			}
		}
		err = add(fmt.Sprintf("acl/%02d.txt", i), "path: "+root+"\nsddl: "+strings.TrimSpace(sddl)+"\n\n"+facl)
	}
	if err == nil && src.ACLHistory != "" {
		if _, serr := os.Stat(src.ACLHistory); serr == nil {
			err = w.addFile("acl-history.jsonl", src.ACLHistory)
		}
	}
	var dbs []string
	if err == nil {
		dbs, err = s.addSambaDBs(ctx, run, w)
	}
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	m.Log = append(m.Log, fmt.Sprintf("smb.conf, testparm, net conf, ACL of %d share roots, ACL history", len(src.Roots)))
	if len(dbs) > 0 {
		m.Log = append(m.Log, "Samba databases (tdbbackup): "+strings.Join(dbs, ", "))
	}
	return dst, err
}

// memberDBs are the Samba databases of a file server worth keeping: the machine account's password (restore
// without joining again), the registry shares, the id mapping and winbind's state.
var memberDBs = []string{"/var/lib/samba/private/secrets.tdb", "/var/lib/samba/private/passdb.tdb", "/var/lib/samba/registry.tdb",
	"/var/lib/samba/winbindd_idmap.tdb", "/var/lib/samba/account_policy.tdb", "/var/lib/samba/share_info.tdb"}

// addSambaDBs adds a consistent copy of each member database that exists (tdbbackup locks the database while it
// copies; the live file could be caught half-written) under samba/; without tdbbackup they are skipped.
func (s *Store) addSambaDBs(ctx context.Context, run Runner, w *tarGz) ([]string, error) {
	var done []string
	if _, err := lookPath("tdbbackup"); err != nil {
		return []string{"none: tdbbackup not found (install tdb-tools)"}, nil
	}
	for _, db := range memberDBs {
		if _, err := os.Stat(db); err != nil {
			continue
		}
		snap := db + ".adm-bak"
		os.Remove(snap)
		if _, err := run(ctx, "tdbbackup", "-s", ".adm-bak", db); err != nil {
			return done, fmt.Errorf("tdbbackup %s: %w", db, err)
		}
		err := w.addFile("samba/"+strings.TrimPrefix(db, "/var/lib/samba/"), snap)
		os.Remove(snap)
		if err != nil {
			return done, err
		}
		done = append(done, filepath.Base(db))
	}
	return done, nil
}

// runData archives the folders with GNU tar's incremental snapshot: a full archive starts a chain (its snapshot
// kept as <chain>.snar); the next ones only what changed, until the full is FullEveryDays old.
func (s *Store) runData(ctx context.Context, run Runner, m *Manifest, j Job, work string) (string, error) {
	if len(j.Folders) == 0 {
		return "", errors.New("no folders to back up")
	}
	m.Folders = j.Folders
	every := j.FullEveryDays
	if every <= 0 {
		every = 7
	}
	// the current chain of this origin: its full one, if young enough and its snapshot still here
	list, err := s.List()
	if err != nil {
		return "", err
	}
	var head *Manifest
	for i := range list {
		x := &list[i]
		if x.Origin == m.Origin && x.Kind == KindFileData && x.Status == "ok" && x.Full && sameFolders(x.Folders, j.Folders) {
			head = x
			break // newest full
		}
	}
	snar := filepath.Join(work, "snapshot.snar")
	if head != nil && !j.Full && time.Since(head.Started) < time.Duration(every)*24*time.Hour {
		if err := copyFile(filepath.Join(s.Dir, head.ID+".snar"), snar); err == nil {
			m.Chain, m.Full = head.ID, false
		}
	}
	dst := filepath.Join(s.Dir, m.ID+".tar.gz")
	if err := precreate(dst); err != nil {
		return "", err
	}
	args := []string{"--create", "--gzip", "--file=" + dst, "--listed-incremental=" + snar, "--one-file-system", "--acls", "--xattrs",
		"--xattrs-include=security.NTACL", "--xattrs-include=user.*", "--exclude=" + RestoreDir, "--exclude=" + LegacyRestoreDir, "--"} // restored copies stay out
	args = append(args, j.Folders...)
	if out, err := run(ctx, "tar", args...); err != nil {
		// GNU tar exits 1 when a file changed while it was read: the archive is still good, with a warning
		var ee *exec.ExitError
		if !(errors.As(err, &ee) && ee.ExitCode() == 1) {
			return dst, err
		}
		m.Log = append(m.Log, "tar: some files changed during the backup: "+lastLine(out))
	}
	// a folder (or one above it) swapped for a link while tar ran would have led out of the roots: the real paths
	// must still be the ones checked before (ponytail: a swap and back inside the window is not seen; openat2
	// with RESOLVE_NO_SYMLINKS would close it)
	for _, f := range j.Folders {
		if real, err := filepath.EvalSymlinks(f); err != nil || real != f {
			return dst, fmt.Errorf("%s changed during the backup", f)
		}
	}
	if m.Full {
		m.Log = append(m.Log, "full backup (new chain)")
	} else {
		m.Log = append(m.Log, "incremental of the chain "+m.Chain)
	}
	return dst, nil
}

// sambaPaths are what a samba-files backup keeps (the ones that exist).
var sambaPaths = []string{"/etc/samba", "/var/lib/samba", "/etc/krb5.conf"}

// runSambaFiles archives Samba's configuration and state with their ACLs and extended attributes (the NT ACLs of
// SYSVOL live there): the copy the setup takes before it changes anything.
func (s *Store) runSambaFiles(ctx context.Context, run Runner, m *Manifest) (string, error) {
	var paths []string
	for _, p := range sambaPaths {
		if _, err := os.Lstat(p); err == nil {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return "", errors.New("no Samba files on this server")
	}
	dst := filepath.Join(s.Dir, m.ID+".tar.gz")
	if err := precreate(dst); err != nil {
		return "", err
	}
	args := append([]string{"--create", "--gzip", "--file=" + dst, "--acls", "--xattrs", "--xattrs-include=*", "--"}, paths...)
	if _, err := run(ctx, "tar", args...); err != nil {
		return dst, err // a file that changed during the copy is a failure here: this is the copy to come back to
	}
	m.Log = append(m.Log, "files: "+strings.Join(paths, ", "))
	return dst, os.Chmod(dst, 0o600)
}

// runPanel archives the panel's database (a consistent copy with VACUUM INTO, by the caller's sqlite helper through
// run "sqlite-vacuum"), its configuration without secrets and the audit log.
func (s *Store) runPanel(ctx context.Context, run Runner, m *Manifest, src Sources, work string) (string, error) {
	db := filepath.Join(work, "ad-manager.db")
	if _, err := run(ctx, "sqlite-vacuum", src.Database, db); err != nil {
		return "", err
	}
	dst := filepath.Join(s.Dir, m.ID+".tar.gz")
	w, err := newTarGz(dst)
	if err != nil {
		return "", err
	}
	err = w.addFile("ad-manager.db", db)
	if err == nil && src.Config != "" {
		var conf []byte
		if conf, err = os.ReadFile(src.Config); err == nil {
			err = w.addBytes("config.toml", stripSecrets(conf))
		}
	}
	if err == nil && src.AuditLog != "" {
		if _, serr := os.Stat(src.AuditLog); serr == nil {
			err = w.addFile("audit.log", src.AuditLog)
		}
	}
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	m.Log = append(m.Log, "database (VACUUM INTO), config.toml without secrets, audit log")
	return dst, err
}

// stripSecrets parses a config.toml and blanks every value whose key may hold a secret (password, secret, token,
// key — but not the *_file paths), inline tables and multi-line strings included; the comments are lost. A file
// that does not parse is not kept at all.
func stripSecrets(b []byte) []byte {
	var doc map[string]any
	if err := toml.Unmarshal(b, &doc); err != nil {
		return []byte("# config.toml did not parse: not kept in the backup\n")
	}
	var walk func(m map[string]any)
	walk = func(m map[string]any) {
		for k, v := range m {
			key := strings.ToLower(k)
			if strings.HasSuffix(key, "_file") {
				continue
			}
			if strings.Contains(key, "password") || strings.Contains(key, "secret") || strings.Contains(key, "token") || key == "key" || strings.HasSuffix(key, "_key") {
				m[k] = "removed-by-backup"
				continue
			}
			switch x := v.(type) {
			case map[string]any:
				walk(x)
			case []any:
				for _, e := range x {
					if mm, ok := e.(map[string]any); ok {
						walk(mm)
					}
				}
			}
		}
	}
	walk(doc)
	out, err := toml.Marshal(doc)
	if err != nil {
		return []byte("# config.toml did not encode: not kept in the backup\n")
	}
	return append([]byte("# config.toml of the panel, secrets removed by the backup (comments are not kept)\n"), out...)
}

// precreate makes the archive file 0600 before the tool writes into it (never readable while it is written).
func precreate(p string) error {
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}

func sameFolders(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if filepath.Clean(a[i]) != filepath.Clean(b[i]) {
			return false
		}
	}
	return true
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// tarGz writes a .tar.gz (0600).
type tarGz struct {
	f  *os.File
	gz *gzip.Writer
	tw *tar.Writer
}

func newTarGz(path string) (*tarGz, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	gz := gzip.NewWriter(f)
	return &tarGz{f: f, gz: gz, tw: tar.NewWriter(gz)}, nil
}

func (w *tarGz) addBytes(name string, b []byte) error {
	if err := w.tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(b)), ModTime: time.Now()}); err != nil {
		return err
	}
	_, err := w.tw.Write(b)
	return err
}

func (w *tarGz) addFile(name, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	if err := w.tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: fi.Size(), ModTime: fi.ModTime()}); err != nil {
		return err
	}
	_, err = io.Copy(w.tw, io.LimitReader(f, fi.Size()))
	return err
}

func (w *tarGz) Close() error {
	err := w.tw.Close()
	if gerr := w.gz.Close(); err == nil {
		err = gerr
	}
	if ferr := w.f.Close(); err == nil {
		err = ferr
	}
	return err
}
