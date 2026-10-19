package sambadc

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
)

// ErrInvalidGPOName reports a display name that cannot be a new GPO's.
var ErrInvalidGPOName = errors.New("GPO name: 1 to 128 characters, no control characters, leading/trailing spaces or leading -")

// ErrInvalidBackup reports an archive that is not a GPO backup of this panel.
var ErrInvalidBackup = errors.New("not a GPO backup (.tar.gz from the panel with one policy/{GUID} folder)")

// maxBackupEntries caps the files and folders of an archive.
const maxBackupEntries = 4000

// createdAs is samba-tool's answer to gpo restore: GPO '<name>' created as {GUID}, at the end of its line (the
// name itself may contain "created as {…}": the last match on the line is samba-tool's).
var createdAs = regexp.MustCompile(`(?m)created as (\{[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}\})\s*$`)

// maxUnpacked caps everything gzip yields, tar headers included (PAX/GNU records are read inside tar.Reader and
// would escape the per-file counters).
const maxUnpacked = maxBackupFiles + 4<<20

// limited fails past n bytes instead of ending quietly (a truncated archive must not pass as a smaller one).
type limited struct {
	r io.Reader
	n int64
}

func (l *limited) Read(p []byte) (int, error) {
	if l.n <= 0 {
		return 0, errTooLarge
	}
	if int64(len(p)) > l.n {
		p = p[:l.n]
	}
	k, err := l.r.Read(p)
	l.n -= int64(k)
	return k, err
}

// ValidGPOName checks the display name of a new GPO.
func ValidGPOName(name string) error {
	if name == "" || len(name) > 128 || strings.TrimSpace(name) != name || strings.HasPrefix(name, "-") ||
		strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return ErrInvalidGPOName
	}
	return nil
}

// WithEnv is a Runner that adds env to the command's environment (the password file of samba-tool: PASSWD_FILE,
// never an argument, which other local users could read in the process list).
func WithEnv(env ...string) Runner {
	return func(ctx context.Context, name string, args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return string(out), fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(out)))
		}
		return string(out), nil
	}
}

// extractBackup unpacks a backup archive (as BackupGPO writes it) into dir and returns the folder of its one GPO:
// only folders and regular files, relative names under policy/{GUID}/, at most maxBackupFiles bytes and
// maxBackupEntries entries.
func extractBackup(ctx context.Context, archive []byte, dir string) (string, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return "", ErrInvalidBackup
	}
	tr := tar.NewReader(&limited{r: gz, n: maxUnpacked})
	guid := ""
	total, n := int64(0), 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if errors.Is(err, errTooLarge) {
			return "", errTooLarge
		}
		if err != nil {
			return "", ErrInvalidBackup
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if n++; n > maxBackupEntries {
			return "", errTooLarge
		}
		name := path.Clean(h.Name)
		parts := strings.Split(name, "/")
		if h.Name == "" || path.IsAbs(h.Name) || strings.Contains(h.Name, `\`) || parts[0] != "policy" ||
			slices.Contains(parts, "..") || (len(parts) > 1 && !gpoGUID.MatchString(parts[1])) {
			return "", ErrInvalidBackup
		}
		if len(parts) > 1 {
			if guid != "" && parts[1] != guid { // one GPO per backup
				return "", ErrInvalidBackup
			}
			guid = parts[1]
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return "", err
			}
		case tar.TypeReg:
			if total += h.Size; h.Size < 0 || total > maxBackupFiles {
				return "", errTooLarge
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return "", err
			}
			f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return "", ErrInvalidBackup // a name twice
			}
			_, err = io.Copy(f, io.LimitReader(tr, h.Size))
			if err != nil && !errors.Is(err, errTooLarge) {
				err = ErrInvalidBackup
			}
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return "", err
			}
		default: // links, devices
			return "", ErrInvalidBackup
		}
	}
	if guid == "" {
		return "", ErrInvalidBackup
	}
	return filepath.Join(dir, "policy", guid), nil
}

// RestoreGPO creates a new GPO named name from a backup archive (BackupGPO's .tar.gz) with `samba-tool gpo restore`
// on the DC self: a new GUID, never over an existing GPO (samba-tool also refuses a name in use), unlinked. run
// must authenticate samba-tool as the account of the agent's explicit write mode (WithEnv with PASSWD_FILE; user
// is passed with -U). It returns the new GPO's GUID.
func RestoreGPO(ctx context.Context, run Runner, self, user, name string, archive []byte) (string, error) {
	if err := ValidGPOName(name); err != nil {
		return "", err
	}
	if !hostName.MatchString(self) {
		return "", fmt.Errorf("GPO backup: this DC's name %q (netbios name and realm of smb.conf)", self)
	}
	if len(archive) > MaxBackup {
		return "", errTooLarge
	}
	tmp, err := os.MkdirTemp("", "ad-manager-restore-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	src, err := extractBackup(ctx, archive, filepath.Join(tmp, "backup"))
	if err != nil {
		return "", err
	}
	return restoreDir(ctx, run, self, user, name, src, tmp)
}

// restoreDir runs samba-tool gpo restore of the backup folder src as the new GPO name on self (work files under
// tmp) and returns the new GUID.
func restoreDir(ctx context.Context, run Runner, self, user, name, src, tmp string) (string, error) {
	work := filepath.Join(tmp, "work") // samba-tool's copy of the files before they go to SYSVOL
	if err := os.Mkdir(work, 0o700); err != nil {
		return "", err
	}
	// options first, then -- and the positionals: a name can never be read as an option
	out, err := run(ctx, "samba-tool", "gpo", "restore", "-H", "ldap://"+self, "-U", user, "--tmpdir", work, "--", name, src)
	if err != nil {
		return "", err
	}
	all := createdAs.FindAllStringSubmatch(out, -1)
	if all == nil {
		return "", fmt.Errorf("GPO restore: unexpected answer from samba-tool")
	}
	return strings.ToUpper(all[len(all)-1][1]), nil
}

// watchDir stops (stop with errTooLarge) when the files under dir pass maxBackupFiles bytes or maxBackupEntries
// entries, checking twice a second until ctx ends.
func watchDir(ctx context.Context, dir string, stop context.CancelCauseFunc) {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		var total int64
		n := 0
		_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // a file samba-tool is still moving
			}
			n++
			if fi, err := d.Info(); err == nil && d.Type().IsRegular() {
				total += fi.Size()
			}
			if total > maxBackupFiles || n > maxBackupEntries {
				return errTooLarge
			}
			return nil
		})
		if total > maxBackupFiles || n > maxBackupEntries {
			stop(errTooLarge)
			return
		}
	}
}

// ImportGPO copies the GPO guid of another domain's DC (srcHost: its DNS name or address) into this DC self as
// the new GPO name: `samba-tool gpo backup` from the source with runSrc (authenticated as srcUser, password by
// PASSWD_FILE), then `samba-tool gpo restore` here with runDst as dstUser (the explicit write mode). A new GUID,
// unlinked; the backup is refused past the size limit. It returns the new GUID.
func ImportGPO(ctx context.Context, runSrc, runDst Runner, self, srcHost, srcUser, dstUser, guid, name string) (string, error) {
	if !gpoGUID.MatchString(guid) {
		return "", fmt.Errorf("%w: %q", ErrInvalidGUID, guid)
	}
	if err := ValidGPOName(name); err != nil {
		return "", err
	}
	if !hostName.MatchString(self) {
		return "", fmt.Errorf("GPO backup: this DC's name %q (netbios name and realm of smb.conf)", self)
	}
	if !hostName.MatchString(strings.ToLower(srcHost)) || strings.ContainsAny(srcUser, "\r\n") || srcUser == "" || strings.HasPrefix(srcUser, "-") {
		return "", fmt.Errorf("GPO import: invalid source %q", srcHost)
	}
	tmp, err := os.MkdirTemp("", "ad-manager-import-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	got := filepath.Join(tmp, "backup")
	if err := os.Mkdir(got, 0o700); err != nil {
		return "", err
	}
	// the source decides how big its GPO is: a watcher stops samba-tool as soon as what it wrote passes the
	// limits, instead of checking after the whole download
	bctx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	go watchDir(bctx, got, stop)
	// and the process itself is capped (samba-tool reads each file whole into memory before writing it): 1 GiB of
	// address space, no file over the backup limit
	_, err = runSrc(bctx, "prlimit", "--as=1073741824", fmt.Sprintf("--fsize=%d", maxBackupFiles+1), "--",
		"samba-tool", "gpo", "backup", "-H", "ldap://"+strings.ToLower(srcHost), "-U", srcUser, "--tmpdir", got, "--", strings.ToUpper(guid))
	if cause := context.Cause(bctx); cause != nil && !errors.Is(cause, context.Canceled) {
		return "", cause
	}
	if err != nil {
		return "", err
	}
	src := filepath.Join(got, "policy", strings.ToUpper(guid))
	if fi, err := os.Lstat(src); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("GPO import: samba-tool wrote no backup of %s", guid)
	}
	var total int64
	n := 0
	err = filepath.WalkDir(got, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return ErrInvalidBackup // the backup is samba-tool's own output: a link means something is wrong
		}
		if n++; n > maxBackupEntries {
			return errTooLarge
		}
		if fi, err := d.Info(); err == nil && d.Type().IsRegular() {
			if total += fi.Size(); total > maxBackupFiles {
				return errTooLarge
			}
		}
		return ctx.Err()
	})
	if err != nil {
		return "", err
	}
	return restoreDir(ctx, runDst, self, dstUser, name, src, tmp)
}
