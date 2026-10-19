package backup

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

func needTar(t *testing.T) {
	t.Helper()
	if _, err := Exec(context.Background(), "tar", "--version"); err != nil {
		t.Skip("no tar")
	}
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

// TestRestoreData replays a chain with the real GNU tar: into a new folder (a file deleted before the incremental
// stays deleted), then over the original after backing it up.
func TestRestoreData(t *testing.T) {
	needTar(t)
	root := t.TempDir()
	data := filepath.Join(root, "Fin")
	os.Mkdir(data, 0o755)
	os.WriteFile(filepath.Join(data, "a.txt"), []byte("a"), 0o644)
	s := &Store{Dir: t.TempDir()}
	src := Sources{Roots: []string{root}}
	j := Job{Kind: KindFileData, Origin: "schedule:x", Folders: []string{data}}
	full, err := s.Run(context.Background(), Exec, j, src)
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(data, "a.txt"))
	os.WriteFile(filepath.Join(data, "b.txt"), []byte("b"), 0o644)
	os.Mkdir(filepath.Join(data, "sub"), 0o755)
	os.WriteFile(filepath.Join(data, "sub", "s.txt"), []byte("s"), 0o644)
	inc, err := s.Run(context.Background(), Exec, j, src)
	if err != nil || inc.Full {
		t.Fatalf("incremental %+v %v", inc, err)
	}

	st, err := s.Restore(context.Background(), Exec, RestoreJob{ID: inc.ID, By: "ana"}, src)
	if err != nil || st.Status != "ok" || st.Target != TargetNew || !strings.HasPrefix(st.Dest, filepath.Join(root, RestoreDir)+"/") {
		t.Fatalf("restore %+v %v", st, err)
	}
	got := filepath.Join(st.Dest, strings.TrimPrefix(data, "/"))
	if !exists(filepath.Join(got, "b.txt")) || exists(filepath.Join(got, "a.txt")) {
		t.Fatalf("restored chain: b.txt %v, a.txt %v", exists(filepath.Join(got, "b.txt")), exists(filepath.Join(got, "a.txt")))
	}
	st, err = s.Restore(context.Background(), Exec, RestoreJob{ID: full.ID}, src)
	got = filepath.Join(st.Dest, strings.TrimPrefix(data, "/"))
	if err != nil || !exists(filepath.Join(got, "a.txt")) || exists(filepath.Join(got, "b.txt")) {
		t.Fatalf("restored full: %+v %v", st, err)
	}
	// one folder inside the backup's, made after the full one: only the incremental has it
	st, err = s.Restore(context.Background(), Exec, RestoreJob{ID: inc.ID, Folders: []string{filepath.Join(data, "sub")}}, src)
	got = filepath.Join(st.Dest, strings.TrimPrefix(data, "/"))
	if err != nil || !exists(filepath.Join(got, "sub", "s.txt")) || exists(filepath.Join(got, "b.txt")) {
		t.Fatalf("subfolder: %+v %v", st, err)
	}
	if _, err := s.Restore(context.Background(), Exec, RestoreJob{ID: inc.ID, Folders: []string{filepath.Join(data, "nope")}}, src); err == nil {
		t.Fatal("a folder the backup does not have was accepted")
	}
	if _, err := s.Restore(context.Background(), Exec, RestoreJob{ID: full.ID}, src); err != nil {
		t.Fatal(err)
	}
	if last := s.LastRestore(); last == nil || last.Backup != full.ID {
		t.Fatalf("last restore %+v", last)
	}

	// over the original: what is there now is backed up first, then the folder is as the backup had it
	os.WriteFile(filepath.Join(data, "c.txt"), []byte("c"), 0o644)
	st, err = s.Restore(context.Background(), Exec, RestoreJob{ID: inc.ID, Target: TargetOriginal}, src)
	if err != nil || st.PreBackup == "" || st.Dest != "/" {
		t.Fatalf("original %+v %v", st, err)
	}
	if !exists(filepath.Join(data, "b.txt")) || exists(filepath.Join(data, "c.txt")) {
		t.Fatal("the original folder is not as the backup had it")
	}
	pre, err := s.Get(st.PreBackup)
	if err != nil || pre.Origin != "pre-restore" {
		t.Fatalf("pre-restore backup %+v %v", pre, err)
	}
	out, _ := Exec(context.Background(), "tar", "--list", "--gzip", "--file="+filepath.Join(s.Dir, pre.File))
	if !strings.Contains(out, "c.txt") {
		t.Fatalf("the pre-restore backup lacks c.txt: %q", out)
	}

	// a damaged archive is refused before anything is extracted
	os.WriteFile(filepath.Join(s.Dir, full.File), []byte("changed"), 0o600)
	if _, err := s.Restore(context.Background(), Exec, RestoreJob{ID: inc.ID}, src); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("damaged chain: %v", err)
	}
	// a folder that is not the backup's
	if _, err := s.Restore(context.Background(), Exec, RestoreJob{ID: inc.ID, Folders: []string{"/etc"}}, src); err == nil {
		t.Fatal("a folder outside the backup was accepted")
	}
}

func TestRestoreDCRefused(t *testing.T) {
	defer func(f func(string) (string, error)) { lookPath = f }(lookPath)
	lookPath = func(string) (string, error) { return "/usr/bin/tdbbackup", nil }
	s := &Store{Dir: t.TempDir()}
	run := func(_ context.Context, _ string, args ...string) (string, error) {
		return "", os.WriteFile(filepath.Join(strings.TrimPrefix(args[3], "--targetdir="), "samba-backup-x.tar.bz2"), []byte("BZh9"), 0o644)
	}
	m, err := s.Run(context.Background(), run, Job{Kind: KindDC}, Sources{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Restore(context.Background(), run, RestoreJob{ID: m.ID}, Sources{}); err == nil || !strings.Contains(err.Error(), "initial setup") {
		t.Fatalf("dc restore: %v", err)
	}
}

// TestRestoreConfig restores the three parts of a configuration backup through the commands Samba has for them.
func TestRestoreConfig(t *testing.T) {
	defer func(f func(string) (string, error)) { lookPath = f }(lookPath)
	lookPath = func(string) (string, error) { return "", errors.New("none") }
	dir := t.TempDir()
	root := filepath.Join(dir, "shares")
	os.Mkdir(root, 0o755)
	conf := filepath.Join(dir, "smb.conf")
	os.WriteFile(conf, []byte("[global]\nold\n"), 0o644)
	var calls []string
	run := func(ctx context.Context, name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		switch name {
		case "cp", "install", "mv": // the files really change
			return Exec(ctx, name, args...)
		case "samba-tool":
			return "O:BAG:BAD:(A;;FA;;;BA)\n", nil
		case "getfacl":
			return "# file: " + root + "\nuser::rwx\n", nil
		case "net":
			return "[dados]\n\tpath = /srv/dados\n", nil
		}
		return "", nil
	}
	s := &Store{Dir: filepath.Join(dir, "bk")}
	src := Sources{Roots: []string{root}, SmbConf: conf}
	m, err := s.Run(context.Background(), run, Job{Kind: KindFileConfig}, src)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(conf, []byte("[global]\nchanged\n"), 0o644)
	calls = nil
	st, err := s.Restore(context.Background(), run, RestoreJob{ID: m.ID, Parts: []string{PartSmbConf, PartShares, PartACLs}}, src)
	if err != nil || st.PreBackup == "" {
		t.Fatalf("%+v %v", st, err)
	}
	if b, _ := os.ReadFile(conf); string(b) != "[global]\nold\n" {
		t.Fatalf("smb.conf %q", b)
	}
	if b, _ := os.ReadFile(conf + ".before-restore"); string(b) != "[global]\nchanged\n" {
		t.Fatalf("previous smb.conf %q", b)
	}
	want := []string{"testparm -s --suppress-prompt", "net conf import -T", "net conf import " + s.Dir, "samba-tool ntacl set O:BAG:BAD:(A;;FA;;;BA) " + root, "setfacl --restore="}
	for _, w := range want {
		if !slices.ContainsFunc(calls, func(c string) bool { return strings.HasPrefix(c, w) }) {
			t.Errorf("no %q in %q", w, calls)
		}
	}
	if _, err := s.Restore(context.Background(), run, RestoreJob{ID: m.ID}, src); err == nil {
		t.Error("a configuration restore with no parts was accepted")
	}
}

// TestRestoreSambaFiles puts the files back with the current ones moved aside.
func TestRestoreSambaFiles(t *testing.T) {
	needTar(t)
	dir := t.TempDir()
	defer func(p []string) { sambaPaths = p }(sambaPaths)
	etc, krb := filepath.Join(dir, "etc-samba"), filepath.Join(dir, "krb5.conf")
	sambaPaths = []string{etc, krb}
	os.Mkdir(etc, 0o755)
	os.WriteFile(filepath.Join(etc, "smb.conf"), []byte("before"), 0o644)
	os.WriteFile(krb, []byte("krb-before"), 0o644)
	var systemctl []string
	run := func(ctx context.Context, name string, args ...string) (string, error) {
		if name == "systemctl" {
			systemctl = append(systemctl, strings.Join(args, " "))
			if args[0] == "is-active" && args[2] != "smbd" {
				return "", errors.New("inactive")
			}
			return "", nil
		}
		return Exec(ctx, name, args...)
	}
	s := &Store{Dir: filepath.Join(dir, "bk")}
	m, err := s.Run(context.Background(), run, Job{Kind: KindSambaFiles}, Sources{})
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(etc, "smb.conf"), []byte("after"), 0o644)
	os.WriteFile(filepath.Join(etc, "new.conf"), []byte("new"), 0o644)
	st, err := s.Restore(context.Background(), run, RestoreJob{ID: m.ID}, Sources{})
	if err != nil {
		t.Fatalf("%+v %v", st, err)
	}
	if b, _ := os.ReadFile(filepath.Join(etc, "smb.conf")); string(b) != "before" || exists(filepath.Join(etc, "new.conf")) {
		t.Fatalf("smb.conf %q, new.conf left: %v", b, exists(filepath.Join(etc, "new.conf")))
	}
	aside, _ := filepath.Glob(etc + ".before-restore-*")
	if len(aside) != 1 || !exists(filepath.Join(aside[0], "new.conf")) {
		t.Fatalf("current files not kept aside: %v", aside)
	}
	if !slices.Contains(systemctl, "stop smbd") || !slices.Contains(systemctl, "start smbd") {
		t.Fatalf("smbd not stopped and started: %q", systemctl)
	}
}

func TestEntryName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"inside", "srv/shares/Fin/a.txt", true},
		{"the folder", "srv/shares/Fin/", true},
		{"absolute", "/etc/passwd", false},
		{"dot dot", "srv/shares/Fin/../../etc/passwd", false},
		{"up", "../x", false},
		{"other folder", "srv/shares/Other/a", false},
		{"prefix only", "srv/shares/Finance/a", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := entryName(tt.in, []string{"srv/shares/Fin"}); (err == nil) != tt.ok {
				t.Fatalf("entryName(%q) err = %v", tt.in, err)
			}
		})
	}
}

func tarGzOf(t *testing.T, entries ...tar.Header) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for _, h := range entries {
		body := []byte(h.Linkname)
		if h.Typeflag == tar.TypeReg || h.Typeflag == 'D' {
			h.Linkname, h.Size = "", int64(len(body))
		} else {
			body = nil
		}
		if h.Mode == 0 {
			h.Mode = 0o644
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		tw.Write(body)
	}
	tw.Close()
	gz.Close()
	return b.Bytes()
}

// TestImport checks a sent backup by its kind and lists it as an upload.
func TestImport(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	reg := func(name, body string) tar.Header {
		return tar.Header{Name: name, Typeflag: tar.TypeReg, Linkname: body}
	}
	dir := func(name string) tar.Header { return tar.Header{Name: name, Typeflag: tar.TypeDir} }
	data := tarGzOf(t, dir("srv/shares/Fin/"), reg("srv/shares/Fin/a.txt", "a"), dir("srv/shares/Fin/sub/"), dir("srv/shares/RH/"))
	m, err := s.Import(bytes.NewReader(data), KindFileData, "ana", 1<<20)
	if err != nil || m.Origin != "upload" || !m.Full || m.SHA256 == "" || !slices.Equal(m.Folders, []string{"/srv/shares/Fin", "/srv/shares/RH"}) {
		t.Fatalf("%+v %v", m, err)
	}
	if got, _ := s.Get(m.ID); got.Status != "ok" {
		t.Fatalf("not listed: %+v", got)
	}
	bad := []struct {
		name, kind string
		body       []byte
	}{
		{"escape", KindFileData, tarGzOf(t, dir("srv/x/"), reg("srv/x/../../etc/passwd", "x"))},
		{"no smb.conf", KindFileConfig, tarGzOf(t, reg("testparm.txt", "x"))},
		{"not sqlite", KindPanel, tarGzOf(t, reg("ad-manager.db", "not a database at all"))},
		{"outside samba", KindSambaFiles, tarGzOf(t, reg("etc/shadow", "x"))},
		{"dc", KindDC, tarGzOf(t, reg("backup.txt", "x"))},
		{"not gzip", KindFileConfig, []byte("plain text")},
		{"too large", KindFileConfig, bytes.Repeat([]byte("x"), 2<<20)},
	}
	for _, b := range bad {
		if _, err := s.Import(bytes.NewReader(b.body), b.kind, "ana", 1<<20); err == nil {
			t.Errorf("%s: accepted", b.name)
		}
	}
	if list, _ := s.List(); len(list) != 1 {
		t.Fatalf("a refused upload was kept: %d", len(list))
	}
	if left, _ := filepath.Glob(filepath.Join(s.Dir, "*.tar.gz")); len(left) != 1 {
		t.Fatalf("refused archives left: %v", left)
	}
}

func TestCheckDumpdir(t *testing.T) {
	t.Parallel()
	pre := []string{"srv/shares/Fin"}
	tests := []struct {
		name string
		list string
		ok   bool
		inc  bool
	}{
		{"full", "Ya.txt\x00Dsub\x00\x00", true, false},
		{"incremental", "Ya.txt\x00Nb.txt\x00\x00", true, true},
		{"rename inside", "Rsrv/shares/Fin/old\x00Tsrv/shares/Fin/new\x00\x00", true, false},
		{"rename out", "Rsrv/shares/Fin/x\x00Tvar/lib/samba/private/secrets.tdb\x00\x00", false, false},
		{"rename absolute out", "R/etc/passwd\x00T\x00\x00", false, false},
		{"slash in a name", "Y../../etc/passwd\x00", false, false},
		{"unknown code", "Zx\x00", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			info, err := checkDumpdir("srv/shares/Fin", strings.NewReader(tt.list), pre)
			if (err == nil) != tt.ok || (err == nil && info.incremental != tt.inc) {
				t.Fatalf("info %+v err %v", info, err)
			}
		})
	}
}

func TestACLEntries(t *testing.T) {
	t.Parallel()
	got, ok := aclEntries("# file: /srv/shares\n# owner: root\nuser::rwx\ngroup:dom\\040users:r-x\t#effective:r-x\nmask::r-x\nother::---\n\n# file:\t/etc/shadow\nother::rwx\n")
	if !ok || got != "user::rwx\ngroup:dom\\040users:r-x\nmask::r-x\nother::---\n" {
		t.Fatalf("entries %q %v", got, ok)
	}
	if _, ok := aclEntries("# file: /srv/shares\nuser::rwx\n# file:\t/etc/shadow\nother::rwx\n"); !ok {
		// comments inside the section are dropped; the second path never reaches setfacl (the header is rebuilt)
		t.Fatal("a section with comments was refused")
	}
	if _, ok := aclEntries("user::rwx\nsomething else\n"); ok {
		t.Fatal("a line that is not an ACL entry was accepted")
	}
}

// The restorations' folder must be root's own folder, never a link a share user left there.
func TestRestoreDirLinkRefused(t *testing.T) {
	root := t.TempDir()
	os.Symlink(t.TempDir(), filepath.Join(root, RestoreDir))
	if _, _, err := makeRestoreDir(root, RestoreDir, "x"); err == nil {
		t.Fatal("a link as the restorations' folder was accepted")
	}
	os.Remove(filepath.Join(root, RestoreDir))
	os.Mkdir(filepath.Join(root, RestoreDir), 0o777)
	os.Chmod(filepath.Join(root, RestoreDir), 0o777)
	if _, _, err := makeRestoreDir(root, RestoreDir, "x"); err == nil {
		t.Fatal("a folder others write was accepted")
	}
}

func TestMountsUnder(t *testing.T) {
	f := filepath.Join(t.TempDir(), "mountinfo")
	os.WriteFile(f, []byte("36 35 98:0 / / rw - ext4 /dev/root rw\n40 36 8:1 / /srv/shares/Fin/Arquivo\\040Morto rw - ext4 /dev/sdb1 rw\n"), 0o600)
	defer func(p string) { mountInfo = p }(mountInfo)
	mountInfo = f
	if got := MountsUnder([]string{"/srv/shares/Fin"}); got != "/srv/shares/Fin/Arquivo Morto" {
		t.Fatalf("got %q", got)
	}
	if got := MountsUnder([]string{"/srv/shares/RH", "/srv/shares/Fin/Arquivo Morto"}); got != "" {
		t.Fatalf("the mount point itself counted: %q", got)
	}
}

// An incremental archive sent alone would restore as if it were whole: refused.
func TestImportIncrementalRefused(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	inc := tarGzOf(t, tar.Header{Name: "srv/x/", Typeflag: 'D', Linkname: "Na.txt\x00Yb.txt\x00\x00"}, tar.Header{Name: "srv/x/b.txt", Typeflag: tar.TypeReg, Linkname: "b"})
	if _, err := s.Import(bytes.NewReader(inc), KindFileData, "ana", 1<<20); err == nil || !strings.Contains(err.Error(), "incremental") {
		t.Fatalf("incremental upload: %v", err)
	}
}

// A folder renamed between the full and the incremental keeps its unchanged files only in the full, under the old
// name: restoring just the new name is refused (the whole folder is restored instead).
func TestRestoreSubfolderAfterRename(t *testing.T) {
	needTar(t)
	root := t.TempDir()
	data := filepath.Join(root, "Dados")
	os.MkdirAll(filepath.Join(data, "Old"), 0o755)
	os.WriteFile(filepath.Join(data, "Old", "a.txt"), []byte("a"), 0o644)
	s := &Store{Dir: t.TempDir()}
	src := Sources{Roots: []string{root}}
	j := Job{Kind: KindFileData, Origin: "manual", Folders: []string{data}}
	if _, err := s.Run(context.Background(), Exec, j, src); err != nil {
		t.Fatal(err)
	}
	os.Rename(filepath.Join(data, "Old"), filepath.Join(data, "Financeiro"))
	inc, err := s.Run(context.Background(), Exec, j, src)
	if err != nil || inc.Full {
		t.Fatalf("incremental %+v %v", inc, err)
	}
	_, err = s.Restore(context.Background(), Exec, RestoreJob{ID: inc.ID, Folders: []string{filepath.Join(data, "Financeiro")}}, src)
	if err == nil || !strings.Contains(err.Error(), "renamed") {
		t.Fatalf("subfolder after a rename: %v", err)
	}
	st, err := s.Restore(context.Background(), Exec, RestoreJob{ID: inc.ID}, src)
	if err != nil || !exists(filepath.Join(st.Dest, strings.TrimPrefix(data, "/"), "Financeiro", "a.txt")) {
		t.Fatalf("whole folder after a rename: %+v %v", st, err)
	}
}
