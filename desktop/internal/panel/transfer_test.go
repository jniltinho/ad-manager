package panel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// bigPanel serves /api/backup (size bytes of a pattern) and stores /api/upload's body hash, after a sign-in.
func bigPanel(t *testing.T, size int64, gotHash *string) *Panel {
	t.Helper()
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/login":
			http.SetCookie(w, &http.Cookie{Name: "adm_session", Value: "x", Path: "/"})
		case "/api/backup":
			if _, err := r.Cookie("adm_session"); err != nil {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"error":"sign in"}`)
				return
			}
			_, _ = io.CopyN(w, pattern{}, size)
		case "/api/upload":
			h := sha256.New()
			_, _ = io.Copy(h, r.Body)
			*gotHash = hex.EncodeToString(h.Sum(nil))
			_, _ = io.WriteString(w, `{"ok":true}`)
		}
	}))
	t.Cleanup(s.Close)
	p, _ := New(s.URL, CertPin(s.Certificate().Raw), false)
	do(t, p, http.MethodPost, "/api/login", "")
	return p
}

type pattern struct{}

func (pattern) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = byte(i * 7)
	}
	return len(b), nil
}

func TestDownloadStreamsPrivately(t *testing.T) {
	const size = 200 << 20
	p := bigPanel(t, size, new(string))
	dst := filepath.Join(t.TempDir(), "backup.tar.gz")
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, sum, err := p.Download(context.Background(), p.Generation(), "/api/backup", dst)
	if err != nil || n != size {
		t.Fatalf("download: %d %v", n, err)
	}
	h := sha256.New()
	_, _ = io.CopyN(h, pattern{}, size)
	if sum != hex.EncodeToString(h.Sum(nil)) {
		t.Fatal("hash differs")
	}
	if st, _ := os.Stat(dst); (runtime.GOOS != "windows" && st.Mode().Perm() != 0o600) || st.Size() != size {
		t.Fatalf("file: %v %d", st.Mode(), st.Size())
	}
}

func TestDownloadErrorLeavesNothing(t *testing.T) {
	p := bigPanel(t, 10, new(string))
	p.reset() // signed out
	dir := t.TempDir()
	_, _, err := p.Download(context.Background(), p.Generation(), "/api/backup", filepath.Join(dir, "b.tar.gz"))
	if err != ErrSessionEnded {
		t.Fatalf("error: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("left behind: %v", entries)
	}
	if _, _, err := p.Download(context.Background(), p.Generation(), "https://elsewhere/api/backup", filepath.Join(dir, "x")); err == nil {
		t.Fatal("a full URL was accepted")
	}
}

func TestUploadStreamsWithProgress(t *testing.T) {
	var got string
	p := bigPanel(t, 0, &got)
	src := filepath.Join(t.TempDir(), "in.tar.gz")
	f, _ := os.Create(src)
	h := sha256.New()
	_, _ = io.CopyN(io.MultiWriter(f, h), pattern{}, 50<<20)
	f.Close()
	var last int64
	body, err := p.Upload(context.Background(), p.Generation(), "/api/upload?kind=panel", src, func(sent, total int64) { last = sent })
	if err != nil || !strings.Contains(string(body), "ok") || last != 50<<20 || got != hex.EncodeToString(h.Sum(nil)) {
		t.Fatalf("upload: %v %s sent=%d", err, body, last)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Upload(ctx, p.Generation(), "/api/upload", src, nil); err == nil {
		t.Fatal("a cancelled upload succeeded")
	}
}

func TestSaveTextPrivate(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "users.csv")
	if err := SaveText(dst, "a;b\r\n"); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(dst); runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
}

func TestTransferOfEndedSessionRefused(t *testing.T) {
	p := bigPanel(t, 10, new(string))
	old := p.Generation()
	p.reset()
	if _, _, err := p.Download(context.Background(), old, "/api/backup", filepath.Join(t.TempDir(), "x")); err != ErrStale {
		t.Fatalf("download of an ended session: %v", err)
	}
}
