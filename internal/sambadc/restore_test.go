package sambadc

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// tgz builds a .tar.gz with the given headers (a body of Size bytes for regular files).
func tgz(t *testing.T, hs ...tar.Header) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for _, h := range hs {
		if h.Typeflag == tar.TypeReg {
			h.Mode = 0o644
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			_, _ = tw.Write(bytes.Repeat([]byte("x"), int(h.Size)))
		}
	}
	_ = tw.Close()
	_ = gz.Close()
	return b.Bytes()
}

func TestRestoreGPO(t *testing.T) {
	guid := "{31B2F340-016D-11D2-945F-00C04FB984F9}"
	dir := func(n string) tar.Header { return tar.Header{Name: n, Typeflag: tar.TypeDir, Mode: 0o755} }
	file := func(n string, size int64) tar.Header { return tar.Header{Name: n, Typeflag: tar.TypeReg, Size: size} }
	good := tgz(t, dir("policy"), dir("policy/"+guid), file("policy/"+guid+"/GPT.INI.xml", 6), dir("policy/"+guid+"/MACHINE"))
	var got []string
	run := func(_ context.Context, name string, args ...string) (string, error) {
		got = append([]string{name}, args...)
		if _, err := os.Stat(filepath.Join(args[len(args)-1], "GPT.INI.xml")); err != nil {
			t.Fatalf("backup not extracted: %v", err)
		}
		return "GPO 'Copy of Default' created as {91048d73-a843-4ff2-b88f-9bcc2fb86614}\n", nil
	}
	// a name that mimics samba-tool's answer: the GUID is the last one on the line
	tricky := "x created as {00000000-0000-0000-0000-000000000000}"
	run2 := func(ctx context.Context, name string, args ...string) (string, error) {
		return "GPO '" + tricky + "' created as {91048d73-a843-4ff2-b88f-9bcc2fb86614}\n", nil
	}
	if g, err := RestoreGPO(context.Background(), run2, "dc1.empresa.net", "svc-gpo", tricky, good); err != nil || g != "{91048D73-A843-4FF2-B88F-9BCC2FB86614}" {
		t.Fatalf("tricky name: %q %v", g, err)
	}
	g, err := RestoreGPO(context.Background(), run, "dc1.empresa.net", "svc-gpo", "Copy of Default", good)
	if err != nil || g != "{91048D73-A843-4FF2-B88F-9BCC2FB86614}" {
		t.Fatalf("restore: %q %v", g, err)
	}
	if got[1] != "gpo" || got[2] != "restore" || got[4] != "ldap://dc1.empresa.net" || got[6] != "svc-gpo" ||
		got[9] != "--" || got[10] != "Copy of Default" || filepath.Base(got[11]) != guid {
		t.Fatalf("command %v", got)
	}

	for _, tt := range []struct {
		name    string
		archive []byte
		gpo     string
		err     error
	}{
		{"bad name", good, " x", ErrInvalidGPOName},
		{"name as an option", good, "--restore-metadata", ErrInvalidGPOName},
		{"control character", good, "a\nb", ErrInvalidGPOName},
		{"not gzip", []byte("nope"), "x", ErrInvalidBackup},
		{"path escape", tgz(t, file("policy/"+guid+"/../../../etc/x", 1)), "x", ErrInvalidBackup},
		{"absolute", tgz(t, file("/policy/"+guid+"/x", 1)), "x", ErrInvalidBackup},
		{"outside policy", tgz(t, file("other/x", 1)), "x", ErrInvalidBackup},
		{"not a GUID", tgz(t, file("policy/abc/x", 1)), "x", ErrInvalidBackup},
		{"two GPOs", tgz(t, file("policy/"+guid+"/x", 1), file("policy/{31B2F340-016D-11D2-945F-00C04FB984F8}/x", 1)), "x", ErrInvalidBackup},
		{"symlink", tgz(t, dir("policy/"+guid), tar.Header{Name: "policy/" + guid + "/l", Typeflag: tar.TypeSymlink, Linkname: "/etc/shadow"}), "x", ErrInvalidBackup},
		{"same file twice", tgz(t, file("policy/"+guid+"/x", 1), file("policy/"+guid+"/x", 1)), "x", ErrInvalidBackup},
		{"empty", tgz(t, dir("policy")), "x", ErrInvalidBackup},
		{"too large", tgz(t, file("policy/"+guid+"/big", maxBackupFiles+1)), "x", errTooLarge},
		{"PAX headers past the limit", paxBomb(t, guid), "x", errTooLarge},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got = nil
			if _, err := RestoreGPO(context.Background(), run, "dc1.empresa.net", "svc-gpo", tt.gpo, tt.archive); !errors.Is(err, tt.err) {
				t.Fatalf("err %v, want %v", err, tt.err)
			}
			if got != nil {
				t.Fatal("samba-tool ran")
			}
		})
	}
}

// paxBomb is a .tar.gz of many ~1 MiB PAX extended headers before one small file: tar.Reader consumes them
// inside Next, so only the limit on everything gzip yields stops it.
func paxBomb(t *testing.T, guid string) []byte {
	t.Helper()
	header := func(name string, flag byte, size int) []byte {
		var tw bytes.Buffer
		w := tar.NewWriter(&tw)
		_ = w.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Size: int64(size), Mode: 0o644, Format: tar.FormatUSTAR})
		h := tw.Bytes()[:512]
		h[156] = flag
		copy(h[148:156], "        ")
		sum := 0
		for _, c := range h {
			sum += int(c)
		}
		copy(h[148:156], fmt.Sprintf("%06o\x00 ", sum))
		return h
	}
	pad := func(b []byte) []byte { return append(b, make([]byte, (512-len(b)%512)%512)...) }
	value := strings.Repeat("a", 1000*1000)
	rec := fmt.Sprintf(" comment=%s\n", value)
	rec = fmt.Sprintf("%d%s", len(rec)+7, rec) // the length counts its own 7 digits
	var raw []byte
	for range 24 {
		raw = append(raw, header("pax", 'x', len(rec))...)
		raw = append(raw, pad([]byte(rec))...)
	}
	raw = append(raw, header("policy/"+guid+"/x", '0', 1)...)
	raw = append(raw, pad([]byte("x"))...)
	raw = append(raw, make([]byte, 1024)...)
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	_, _ = gz.Write(raw)
	_ = gz.Close()
	return b.Bytes()
}

func TestImportGPO(t *testing.T) {
	guid := "{6AC1786C-016F-11D2-945F-00C04FB984F9}"
	var srcArgs, dstArgs []string
	runSrc := func(_ context.Context, name string, args ...string) (string, error) {
		srcArgs = append([]string{name}, args...)
		dir := filepath.Join(args[11], "policy", guid) // prlimit … -- samba-tool gpo backup … --tmpdir <dir>
		if err := os.MkdirAll(filepath.Join(dir, "MACHINE"), 0o700); err != nil {
			t.Fatal(err)
		}
		return "", os.WriteFile(filepath.Join(dir, "GPT.INI.xml"), []byte("<xml/>"), 0o600)
	}
	runDst := func(_ context.Context, name string, args ...string) (string, error) {
		dstArgs = append([]string{name}, args...)
		if _, err := os.Stat(filepath.Join(args[len(args)-1], "GPT.INI.xml")); err != nil {
			t.Fatalf("restore without the backup: %v", err)
		}
		return "GPO 'Mapeamento' created as {11111111-2222-3333-4444-555555555555}\n", nil
	}
	g, err := ImportGPO(context.Background(), runSrc, runDst, "dc1.destino.lab", "192.168.57.19", `ORIGEM\admin`, "svc-gpo", guid, "Mapeamento")
	if err != nil || g != "{11111111-2222-3333-4444-555555555555}" {
		t.Fatalf("import: %q %v", g, err)
	}
	if srcArgs[0] != "prlimit" || srcArgs[1] != "--as=1073741824" || srcArgs[3] != "--" || srcArgs[4] != "samba-tool" ||
		srcArgs[6] != "backup" || srcArgs[8] != "ldap://192.168.57.19" || srcArgs[10] != `ORIGEM\admin` || srcArgs[13] != "--" || srcArgs[14] != guid {
		t.Fatalf("backup command %v", srcArgs)
	}
	if dstArgs[2] != "restore" || dstArgs[4] != "ldap://dc1.destino.lab" || dstArgs[6] != "svc-gpo" || dstArgs[10] != "Mapeamento" {
		t.Fatalf("restore command %v", dstArgs)
	}
	for _, tt := range []struct{ name, host, user, guid, gpo string }{
		{"option as host", "-c", "a", guid, "x"},
		{"path as host", "a/b", "a", guid, "x"},
		{"option as user", "dc.origem.lab", "-k", guid, "x"},
		{"bad GUID", "dc.origem.lab", "a", "../x", "x"},
		{"bad name", "dc.origem.lab", "a", guid, " x"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srcArgs, dstArgs = nil, nil
			if _, err := ImportGPO(context.Background(), runSrc, runDst, "dc1.destino.lab", tt.host, tt.user, "svc", tt.guid, tt.gpo); err == nil || srcArgs != nil {
				t.Fatalf("accepted (%v, ran %v)", err, srcArgs)
			}
		})
	}
}

// TestImportGPOTooLarge: a source that keeps writing past the limit is stopped while samba-tool runs, not after.
func TestImportGPOTooLarge(t *testing.T) {
	guid := "{6AC1786C-016F-11D2-945F-00C04FB984F9}"
	runSrc := func(ctx context.Context, _ string, args ...string) (string, error) {
		dir := filepath.Join(args[11], "policy", guid)
		_ = os.MkdirAll(dir, 0o700)
		f, _ := os.Create(filepath.Join(dir, "big"))
		defer f.Close()
		chunk := make([]byte, 1<<20)
		for {
			select {
			case <-ctx.Done(): // killed by the watcher
				return "", ctx.Err()
			default:
			}
			if _, err := f.Write(chunk); err != nil {
				return "", err
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	never := func(context.Context, string, ...string) (string, error) { t.Fatal("restore ran"); return "", nil }
	if _, err := ImportGPO(context.Background(), runSrc, never, "dc1.destino.lab", "dc.origem.lab", "a", "b", guid, "x"); !errors.Is(err, errTooLarge) {
		t.Fatalf("err %v, want too large", err)
	}
}
