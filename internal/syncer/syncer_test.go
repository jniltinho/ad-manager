package syncer

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	rfs "github.com/rclone/rclone/fs"
)

func TestCheckSource(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		src  Source
		ok   bool
	}{
		{"kerberos", Source{Host: "fs01.empresa.local", Share: "Financeiro", Path: "2026/Notas"}, true},
		{"ntlm", Source{Host: "10.0.0.5", Share: "Dados", Auth: "ntlm", User: "ana"}, true},
		{"ntlm without user", Source{Host: "10.0.0.5", Share: "Dados", Auth: "ntlm"}, false},
		{"disk share", Source{Host: "fs01", Share: "C$"}, false},
		{"disk share folder without via", Source{Host: "fs01", Share: "E$", Path: "ADM"}, false},
		{"denied share through its disk", Source{Host: "fs01", Share: "E$", Path: "ADM", Via: "ADM"}, true},
		{"a whole disk through via", Source{Host: "fs01", Share: "E$", Via: "ADM"}, false},
		{"admin share", Source{Host: "fs01", Share: "ADMIN$"}, false},
		{"sysvol", Source{Host: "dc01", Share: "SYSVOL"}, false},
		{"host with option", Source{Host: "fs01,user=x", Share: "Dados"}, false},
		{"path up", Source{Host: "fs01", Share: "Dados", Path: "a/../../b"}, false},
		{"share with slash", Source{Host: "fs01", Share: "Da/dos"}, false},
		{"unknown auth", Source{Host: "fs01", Share: "Dados", Auth: "guest"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := CheckSource(tt.src); (err == nil) != tt.ok {
				t.Fatalf("CheckSource err = %v", err)
			}
		})
	}
}

// TestRunSMB runs against a real share: SYNC_TEST_HOST, SYNC_TEST_PORT, SYNC_TEST_SHARE, SYNC_TEST_USER,
// SYNC_TEST_PASSWORD and SYNC_TEST_DOCKER (a container whose SYNC_TEST_DIR is that share's folder). The lab:
// make samba-up, then SYNC_TEST_HOST=127.0.0.1 SYNC_TEST_PORT=6445 SYNC_TEST_SHARE=Financeiro SYNC_TEST_USER=ana
// SYNC_TEST_PASSWORD=Ana-123 SYNC_TEST_DOCKER=docker-fileserver-1 SYNC_TEST_DIR=/srv/shares/financeiro.
func TestRunSMB(t *testing.T) {
	host := os.Getenv("SYNC_TEST_HOST")
	if host == "" {
		t.Skip("SYNC_TEST_HOST not set")
	}
	box, dir := os.Getenv("SYNC_TEST_DOCKER"), os.Getenv("SYNC_TEST_DIR")
	sh := func(script string) {
		t.Helper()
		if out, err := exec.Command("docker", "exec", box, "sh", "-c", script).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v %s", script, err, out)
		}
	}
	sub := "synctest"
	sh("rm -rf " + dir + "/" + sub + " && mkdir -p " + dir + "/" + sub + "/d && for i in 1 2 3 4 5; do echo $i > " + dir + "/" + sub + "/f$i.txt; done && echo x > " + dir + "/" + sub + "/d/x.txt && chown -R ana " + dir + "/" + sub)
	port := 0
	if p := os.Getenv("SYNC_TEST_PORT"); p != "" {
		for _, c := range p {
			port = port*10 + int(c-'0')
		}
	}
	base := t.TempDir()
	dest, quarantine := filepath.Join(base, "dest"), filepath.Join(base, "q")
	sp := Spec{
		Source:   Source{Host: host, Port: port, Share: os.Getenv("SYNC_TEST_SHARE"), Path: sub, Auth: "ntlm", User: os.Getenv("SYNC_TEST_USER")},
		Password: os.Getenv("SYNC_TEST_PASSWORD"), Dest: dest, Mode: ModeCopy, Quarantine: quarantine, MaxDelete: 2, Verify: true,
	}
	ctx := context.Background()

	pre := sp
	pre.Preview = true
	res, err := Run(ctx, pre, nil)
	if err != nil || res.Plan.New != 6 || len(res.Plan.Sample["new"]) != 6 {
		t.Fatalf("preview %+v %v", res.Plan, err)
	}
	if _, err := os.Stat(dest); err == nil {
		t.Fatal("the preview wrote the destination")
	}
	res, err = Run(ctx, sp, nil)
	if err != nil || res.Transfers != 6 || !res.Checked || !res.ContentVerified {
		t.Fatalf("copy %+v %v", res, err)
	}

	// the source loses 3 files and changes one: a mirror over the limit (2) deletes nothing
	sh("rm " + dir + "/" + sub + "/f1.txt " + dir + "/" + sub + "/f2.txt " + dir + "/" + sub + "/f3.txt && echo changed > " + dir + "/" + sub + "/f4.txt")
	sp.Mode = ModeMirror
	_, err = Run(ctx, sp, nil)
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("mirror over the limit: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "f1.txt")); err != nil {
		t.Fatal("a mirror over the limit deleted a file")
	}
	// within the limit: deleted and replaced files go to the quarantine
	sp.MaxDelete = 3
	res, err = Run(ctx, sp, nil)
	if err != nil || res.Plan.Delete != 3 || res.Plan.Changed != 1 {
		t.Fatalf("mirror %+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(dest, "f1.txt")); err == nil {
		t.Fatal("the mirror kept a file the source no longer has")
	}
	if b, _ := os.ReadFile(filepath.Join(quarantine, "f4.txt")); string(b) != "4\n" {
		t.Fatalf("the replaced file is not in the quarantine: %q (%v)", b, res.Quarantined)
	}
	if b, _ := os.ReadFile(filepath.Join(quarantine, "f1.txt")); string(b) != "1\n" {
		t.Fatal("the deleted file is not in the quarantine")
	}
	// an empty source never empties the destination
	sh("rm -rf " + dir + "/" + sub + "/* ")
	sp.MaxDelete = 100
	if _, err := Run(ctx, sp, nil); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty source: %v", err)
	}
	sh("rm -rf " + dir + "/" + sub)
}

// A mirror deletes only planned files the source still lacks: one that came back stays.
func TestDeletePlanned(t *testing.T) {
	base := t.TempDir()
	src, dest, q := filepath.Join(base, "src"), filepath.Join(base, "dest"), filepath.Join(base, "q")
	for _, d := range []string{src, filepath.Join(dest, "sub")} {
		os.MkdirAll(d, 0o755)
	}
	for _, f := range []string{"a.txt", "b.txt", "sub/c.txt"} {
		os.WriteFile(filepath.Join(dest, f), []byte(f), 0o644)
	}
	os.WriteFile(filepath.Join(src, "b.txt"), []byte("back"), 0o644) // planned, but back in the source
	fsrc, err := rfs.NewFs(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	fdst, err := rfs.NewFs(context.Background(), dest)
	if err != nil {
		t.Fatal(err)
	}
	n, err := deletePlanned(context.Background(), fsrc, fdst, Spec{Dest: dest, Quarantine: q}, []string{"a.txt", "b.txt", "sub/c.txt"})
	if err != nil || n != 2 {
		t.Fatalf("deleted %d, %v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dest, "b.txt")); err != nil {
		t.Fatal("a file back in the source was deleted")
	}
	if b, _ := os.ReadFile(filepath.Join(q, "sub", "c.txt")); string(b) != "sub/c.txt" {
		t.Fatal("a deleted file is not in the quarantine")
	}
}

// TestCheckSMB walks the connection check against the share of TestRunSMB: all steps ok, a wrong password stops
// at the sign-in, a closed port at the port, a missing folder at the folder.
func TestCheckSMB(t *testing.T) {
	host := os.Getenv("SYNC_TEST_HOST")
	if host == "" {
		t.Skip("SYNC_TEST_HOST not set")
	}
	port := 0
	for _, c := range os.Getenv("SYNC_TEST_PORT") {
		port = port*10 + int(c-'0')
	}
	sp := Spec{Source: Source{Host: host, Port: port, Share: os.Getenv("SYNC_TEST_SHARE"), Auth: "ntlm", User: os.Getenv("SYNC_TEST_USER")},
		Password: os.Getenv("SYNC_TEST_PASSWORD")}
	last := func(st []Step) Step { return st[len(st)-1] }
	if st := Check(context.Background(), sp); len(st) != 4 || !last(st).OK || last(st).Name != "share" {
		t.Fatalf("all ok: %+v", st)
	}
	bad := sp
	bad.Password = "wrong"
	if st := Check(context.Background(), bad); last(st).Name != "signin" || last(st).OK {
		t.Fatalf("wrong password: %+v", st)
	}
	closed := sp
	closed.Source.Port = 1
	if st := Check(context.Background(), closed); last(st).Name != "port" || last(st).OK {
		t.Fatalf("closed port: %+v", st)
	}
	missing := sp
	missing.Source.Path = "no-such-folder"
	if st := Check(context.Background(), missing); last(st).Name != "folder" || last(st).OK {
		t.Fatalf("missing folder: %+v", st)
	}
}

func TestHint(t *testing.T) {
	t.Parallel()
	tests := []struct{ err, want string }{
		{"dial tcp: lookup fs-x on 127.0.0.53:53: no such host", "does not resolve"},
		{"couldn't connect SMB: response error: The attempted logon is invalid. This is either due to a bad username", "password refused"},
		{"failed to get Kerberos service ticket: Networking_Error: no KDCs defined in configuration", "KDC"},
		{"couldn't initialize SMB: permission denied", "access denied"},
		{"something else", ""},
	}
	for _, tt := range tests {
		if got := Hint(errors.New(tt.err)); !strings.Contains(got, tt.want) || (tt.want == "" && got != "") {
			t.Errorf("Hint(%q) = %q", tt.err, got)
		}
	}
}
