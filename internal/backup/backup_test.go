package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestDCBackup(t *testing.T) {
	defer func(f func(string) (string, error)) { lookPath = f }(lookPath)
	lookPath = func(string) (string, error) { return "/usr/bin/tdbbackup", nil }
	s := &Store{Dir: t.TempDir()}
	run := func(_ context.Context, name string, args ...string) (string, error) {
		if name != "samba-tool" || !slices.Equal(args[:3], []string{"domain", "backup", "offline"}) {
			t.Fatalf("%s %v", name, args)
		}
		dir := strings.TrimPrefix(args[3], "--targetdir=")
		return "", os.WriteFile(filepath.Join(dir, "samba-backup-empresa.test-2026.tar.bz2"), []byte("BZh9"), 0o644)
	}
	m, err := s.Run(context.Background(), run, Job{Kind: KindDC, By: "ana"}, Sources{})
	if err != nil || m.Status != "ok" || m.Size != 4 || m.SHA256 == "" || !m.Full || m.Chain != m.ID || m.Origin != "manual" {
		t.Fatalf("%+v %v", m, err)
	}
	fi, _ := os.Stat(filepath.Join(s.Dir, m.File))
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("archive mode %v", fi.Mode())
	}
	if _, err := s.Get("../../etc/passwd"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a path as id: %v", err)
	}
	failing := func(context.Context, string, ...string) (string, error) { return "", errors.New("boom") }
	m2, err := s.Run(context.Background(), failing, Job{Kind: KindDC}, Sources{})
	if err == nil || m2.Status != "error" || m2.File != "" {
		t.Fatalf("failed run %+v", m2)
	}
	if list, _ := s.List(); len(list) != 2 {
		t.Fatalf("list %d", len(list))
	}
}

// TestDataChain uses the real GNU tar: a full backup, then an incremental of the same chain, then a new chain when
// the full is old; deleting the full takes its incrementals.
func TestDataChain(t *testing.T) {
	if _, err := Exec(context.Background(), "tar", "--version"); err != nil {
		t.Skip("no tar")
	}
	data := t.TempDir()
	os.WriteFile(filepath.Join(data, "a.txt"), []byte("a"), 0o644)
	s := &Store{Dir: t.TempDir()}
	j := Job{Kind: KindFileData, Origin: "schedule:x", Folders: []string{data}, FullEveryDays: 7}
	full, err := s.Run(context.Background(), Exec, j, Sources{})
	if err != nil || !full.Full {
		t.Fatalf("full %+v %v", full, err)
	}
	os.WriteFile(filepath.Join(data, "b.txt"), []byte("b"), 0o644)
	inc, err := s.Run(context.Background(), Exec, j, Sources{})
	if err != nil || inc.Full || inc.Chain != full.ID {
		t.Fatalf("incremental %+v %v", inc, err)
	}
	out, _ := Exec(context.Background(), "tar", "--list", "--gzip", "--file="+filepath.Join(s.Dir, inc.File))
	if !strings.Contains(out, "b.txt") || strings.Contains(out, "a.txt") {
		t.Fatalf("incremental holds %q", out)
	}
	// the full is now 8 days old: a new chain
	old := full
	old.Started = time.Now().Add(-8 * 24 * time.Hour)
	if err := s.write(old); err != nil {
		t.Fatal(err)
	}
	next, err := s.Run(context.Background(), Exec, j, Sources{})
	if err != nil || !next.Full {
		t.Fatalf("new chain %+v %v", next, err)
	}
	ids, err := s.Delete(full.ID)
	if err != nil || !slices.Equal(ids, []string{full.ID, inc.ID}) {
		t.Fatalf("delete chain %v %v", ids, err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, full.ID+".snar")); !os.IsNotExist(err) {
		t.Error("the chain's snapshot stays")
	}
}

func TestPrune(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	os.MkdirAll(s.Dir, 0o700)
	now := time.Now()
	mk := func(id, chain string, full bool, age time.Duration) {
		_ = s.write(Manifest{ID: id, Kind: KindFileData, Origin: "schedule:x", Chain: chain, Full: full, Started: now.Add(-age), Status: "ok"})
	}
	// three chains, newest last: C1 (full + 1 inc), C2 (full + 1 inc), C3 (full)
	c1 := "20260101T000000Z-fileserver-data-00000001"
	c2 := "20260108T000000Z-fileserver-data-00000002"
	c3 := "20260115T000000Z-fileserver-data-00000003"
	mk(c1, c1, true, 20*24*time.Hour)
	mk("20260102T000000Z-fileserver-data-00000011", c1, false, 19*24*time.Hour)
	mk(c2, c2, true, 13*24*time.Hour)
	mk("20260109T000000Z-fileserver-data-00000021", c2, false, 12*24*time.Hour)
	mk(c3, c3, true, time.Hour)
	gone, err := s.Prune("schedule:x", Retention{Chains: 2}, c3, now)
	if err != nil || !slices.Equal(gone, []string{c1, "20260102T000000Z-fileserver-data-00000011"}) {
		t.Fatalf("by count: %v %v", gone, err)
	}
	gone, _ = s.Prune("schedule:x", Retention{Days: 10}, c3, now)
	if !slices.Equal(gone, []string{c2, "20260109T000000Z-fileserver-data-00000021"}) {
		t.Fatalf("by age: %v", gone)
	}
	if gone, _ = s.Prune("schedule:x", Retention{Days: 0, Chains: 1}, c3, now); len(gone) != 0 {
		t.Fatalf("the backup just made stays: %v", gone)
	}
}

func TestBusyAndSecrets(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	s.mu.Lock()
	if _, err := s.Run(context.Background(), Exec, Job{Kind: KindDC}, Sources{}); !errors.Is(err, ErrBusy) {
		t.Fatalf("busy: %v", err)
	}
	s.mu.Unlock()
	checkStrip(t)
}

// TestChainClosedAfterDelete: deleting an incremental closes the chain (its snapshot already counts the deleted
// changes), so the next backup is a full one; Delete is refused while a backup runs.
func TestChainClosedAfterDelete(t *testing.T) {
	if _, err := Exec(context.Background(), "tar", "--version"); err != nil {
		t.Skip("no tar")
	}
	data := t.TempDir()
	os.WriteFile(filepath.Join(data, "a.txt"), []byte("a"), 0o644)
	s := &Store{Dir: t.TempDir()}
	j := Job{Kind: KindFileData, Origin: "manual", Folders: []string{data}}
	full, _ := s.Run(context.Background(), Exec, j, Sources{})
	os.WriteFile(filepath.Join(data, "b.txt"), []byte("b"), 0o644)
	inc, _ := s.Run(context.Background(), Exec, j, Sources{})
	if inc.Full {
		t.Fatal("second backup should be incremental")
	}
	if _, err := s.Delete(inc.ID); err != nil {
		t.Fatal(err)
	}
	next, err := s.Run(context.Background(), Exec, j, Sources{})
	if err != nil || !next.Full {
		t.Fatalf("after deleting an incremental the next backup is full: %+v %v", next, err)
	}
	s.mu.Lock()
	if _, err := s.Delete(full.ID); !errors.Is(err, ErrBusy) {
		t.Fatalf("delete while running: %v", err)
	}
	s.mu.Unlock()
	fi, _ := os.Stat(s.Dir)
	if fi.Mode().Perm() != 0o700 {
		t.Fatalf("backup folder mode %v", fi.Mode())
	}
}

func checkStrip(t *testing.T) {
	t.Helper()
	in := "[redis]\naddr = \"x\"\npassword = \"s3cret\"\n[agent]\ntoken_file = \"/a\"\ngpo_restore_user = \"svc\"\n" +
		"inline = { password = \"inl1ne\", host = \"h\" }\nnote = \"\"\"\nline\n\"\"\"\napi_key = '''k3y'''\n"
	got := string(stripSecrets([]byte(in)))
	for _, secret := range []string{"s3cret", "inl1ne", "k3y"} {
		if strings.Contains(got, secret) {
			t.Fatalf("%s kept: %q", secret, got)
		}
	}
	for _, kept := range []string{"/a", "svc", "addr"} {
		if !strings.Contains(got, kept) {
			t.Fatalf("%s lost: %q", kept, got)
		}
	}
	if strings.Contains(string(stripSecrets([]byte("not = = toml"))), "toml\n[") {
		t.Fatal("a config that does not parse is not kept")
	}
}
