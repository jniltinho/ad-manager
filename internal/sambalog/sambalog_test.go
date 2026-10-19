package sambalog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const sample = `[2026/10/17 19:04:58.123456,  0] ../../source4/samba/server.c:620(binary_smbd_main)
  samba version 4.23.6 started.
  Copyright Andrew Tridgell and the Samba Team 1992-2025
[2026/10/17 19:05:07.925294,  3, pid=812, effective(0, 0), real(0, 0)] source4/dsdb/dns/dns_update.c:85(dnsupdate_nameupdate_done)
  dnsupdate_nameupdate_done: Failed DNS update with exit code 110
`

func TestRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "log.samba"), []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Read(dir, "log.samba", 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Level != 3 || got[0].Message != "dnsupdate_nameupdate_done: Failed DNS update with exit code 110" {
		t.Fatalf("newest first = %+v", got)
	}
	if got[1].Time != "2026/10/17 19:04:58" || got[1].Message != "samba version 4.23.6 started.\nCopyright Andrew Tridgell and the Samba Team 1992-2025" {
		t.Errorf("multi-line entry = %+v", got[1])
	}
	if got, _ := Read(dir, "log.samba", 10, "DNS UPDATE"); len(got) != 1 {
		t.Errorf("filter: %d entries", len(got))
	}
	if got, _ := Read(dir, "log.samba", 1, ""); len(got) != 1 {
		t.Errorf("limit: %d entries", len(got))
	}
}

func TestReadRefuses(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	secret := filepath.Join(t.TempDir(), "shadow")
	os.WriteFile(secret, []byte("root:x"), 0o600)
	os.Symlink(secret, filepath.Join(dir, "log.link"))
	os.Mkdir(filepath.Join(dir, "log.dir"), 0o755)
	for _, name := range []string{"../shadow", "log.link", "log.dir", "smb.conf", "log.x/../../etc/passwd"} {
		if _, err := Read(dir, name, 10, ""); !errors.Is(err, ErrInvalidName) {
			t.Errorf("%q: err = %v, want ErrInvalidName", name, err)
		}
	}
	files, _ := List(dir)
	if len(files) != 0 {
		t.Errorf("List must skip symlinks and directories: %+v", files)
	}
}
