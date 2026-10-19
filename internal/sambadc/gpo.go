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
	"path/filepath"
	"regexp"
	"strings"
)

// gpoGUID is a GPO's name in the directory: {xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx}.
var gpoGUID = regexp.MustCompile(`^\{[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}\}$`)

// ErrInvalidGUID reports a GPO name that is not a GUID.
var ErrInvalidGUID = errors.New("invalid GPO GUID")

// MaxBackup caps a GPO backup (the panel relays at most 4 MiB).
const MaxBackup = 4 << 20

// maxBackupFiles caps the files of a backup before compression (16 MiB).
const maxBackupFiles = 16 << 20

// hostName is a DNS name (the DC's own, for samba-tool's -H).
var hostName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

// policySize sums the files of the GPO's folder under policies (SYSVOL's Policies, names compared ignoring case),
// without following links. A size it cannot read stops the backup (it must not run unchecked).
func policySize(policies, guid string) (int64, error) {
	if policies == "" {
		return 0, errors.New("GPO backup: this DC's SYSVOL is unknown (path of [sysvol] in smb.conf)")
	}
	es, err := os.ReadDir(policies)
	if err != nil {
		return 0, fmt.Errorf("GPO backup: %w", err)
	}
	var total int64
	found := false
	for _, e := range es {
		if !e.IsDir() || !strings.EqualFold(e.Name(), guid) {
			continue
		}
		found = true
		err = filepath.WalkDir(filepath.Join(policies, e.Name()), func(_ string, d fs.DirEntry, err error) error {
			if err != nil || !d.Type().IsRegular() {
				return err
			}
			fi, err := d.Info()
			if err == nil {
				total += fi.Size()
			}
			if total > maxBackupFiles {
				return errTooLarge
			}
			return err
		})
	}
	if err == nil && !found {
		err = fmt.Errorf("GPO backup: %s is not in this DC's SYSVOL", guid)
	}
	return total, err
}

// BackupGPO exports one GPO with `samba-tool gpo backup` (its container and SYSVOL files as readable XML, read only)
// from the DC self (its own DNS name, with its machine account) and returns them as a .tar.gz. policies is the local
// SYSVOL Policies folder: a GPO larger than the limit is refused before samba-tool reads it all into memory and /tmp.
func BackupGPO(ctx context.Context, run Runner, self, policies, guid string) ([]byte, error) {
	if !gpoGUID.MatchString(guid) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidGUID, guid)
	}
	if !hostName.MatchString(self) {
		return nil, fmt.Errorf("GPO backup: this DC's name %q (netbios name and realm of smb.conf)", self)
	}
	if _, err := policySize(policies, guid); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "ad-manager-gpo-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if _, err := run(ctx, "samba-tool", "gpo", "backup", strings.ToUpper(guid), "--tmpdir", tmp,
		"-H", "ldap://"+self, "--machine-pass"); err != nil {
		return nil, err
	}
	buf := &capped{max: MaxBackup}
	gz := gzip.NewWriter(buf)
	tw := tar.NewWriter(gz)
	total := int64(0)
	err = filepath.WalkDir(tmp, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == tmp {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil || !(fi.Mode().IsRegular() || fi.IsDir()) { // no links or special files
			return err
		}
		rel, _ := filepath.Rel(tmp, p)
		h, err := tar.FileInfoHeader(fi, "")
		if err != nil {
			return err
		}
		h.Name, h.Uid, h.Gid, h.Uname, h.Gname = filepath.ToSlash(rel), 0, 0, "", ""
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		if total += fi.Size(); total > maxBackupFiles {
			return errTooLarge
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, io.LimitReader(f, fi.Size()))
		return err
	})
	if err == nil {
		err = tw.Close()
	}
	if err == nil {
		err = gz.Close()
	}
	if err != nil {
		return nil, err
	}
	return buf.buf.Bytes(), nil
}

// errTooLarge reports a backup over the limits.
var errTooLarge = fmt.Errorf("GPO backup larger than %d MiB", MaxBackup>>20)

// capped is a buffer that fails past max bytes (the compressed archive never grows beyond it in memory).
type capped struct {
	buf bytes.Buffer // named, not embedded: an embedded buffer's ReadFrom would bypass Write
	max int
}

func (c *capped) Write(p []byte) (int, error) {
	if c.buf.Len()+len(p) > c.max {
		return 0, errTooLarge
	}
	return c.buf.Write(p)
}
