package sambadc

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestBackupGPO(t *testing.T) {
	guid := "{31B2F340-016D-11D2-945F-00C04FB984F9}"
	// a fake samba-tool that writes what gpo backup writes into --tmpdir
	run := func(_ context.Context, name string, args ...string) (string, error) {
		if name != "samba-tool" || args[0] != "gpo" || args[1] != "backup" || args[2] != guid || args[6] != "ldap://dc1.empresa.net" || args[7] != "--machine-pass" {
			t.Fatalf("command %s %v", name, args)
		}
		dir := filepath.Join(args[4], "policy", guid)
		if err := os.MkdirAll(filepath.Join(dir, "MACHINE"), 0o755); err != nil {
			t.Fatal(err)
		}
		_ = os.Symlink("/etc/shadow", filepath.Join(dir, "link"))
		return "", os.WriteFile(filepath.Join(dir, "GPT.INI.xml"), []byte("<xml/>"), 0o644)
	}
	sysvol := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sysvol, guid), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := BackupGPO(context.Background(), run, "dc1.empresa.net", sysvol, guid)
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err != nil {
			break
		}
		names = append(names, h.Name)
	}
	want := []string{"policy", "policy/" + guid, "policy/" + guid + "/GPT.INI.xml", "policy/" + guid + "/MACHINE"}
	if !slices.Equal(names, want) {
		t.Fatalf("archive: %v, want %v (no link)", names, want)
	}
	if _, err := BackupGPO(context.Background(), run, "dc1.empresa.net", "", "../../etc"); !errors.Is(err, ErrInvalidGUID) {
		t.Fatalf("bad GUID: %v", err)
	}
	if _, err := BackupGPO(context.Background(), run, "dc1.", sysvol, guid); err == nil {
		t.Fatal("a DC name without realm is refused")
	}
	// a file larger than the limits is refused
	big := func(_ context.Context, _ string, args ...string) (string, error) {
		dir := filepath.Join(args[4], "policy", guid)
		_ = os.MkdirAll(dir, 0o755)
		f, _ := os.Create(filepath.Join(dir, "big"))
		defer f.Close()
		return "", f.Truncate(maxBackupFiles + 1)
	}
	if _, err := BackupGPO(context.Background(), big, "dc1.empresa.net", sysvol, guid); !errors.Is(err, errTooLarge) {
		t.Fatalf("too large: %v", err)
	}
	// a GPO already too large in SYSVOL is refused before samba-tool runs
	pol := t.TempDir()
	_ = os.MkdirAll(filepath.Join(pol, strings.ToLower(guid)), 0o755)
	f, _ := os.Create(filepath.Join(pol, strings.ToLower(guid), "Registry.pol"))
	_ = f.Truncate(maxBackupFiles + 1)
	f.Close()
	never := func(context.Context, string, ...string) (string, error) { t.Fatal("samba-tool ran"); return "", nil }
	if _, err := BackupGPO(context.Background(), never, "dc1.empresa.net", pol, guid); !errors.Is(err, errTooLarge) {
		t.Fatalf("too large in SYSVOL: %v", err)
	}
	// a size it cannot check stops the backup
	for _, p := range []string{"", filepath.Join(pol, "missing"), t.TempDir()} {
		if _, err := BackupGPO(context.Background(), never, "dc1.empresa.net", p, guid); err == nil {
			t.Fatalf("policies %q: backup ran unchecked", p)
		}
	}
}
