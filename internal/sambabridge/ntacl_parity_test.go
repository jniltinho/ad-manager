//go:build linux && parity

package sambabridge

// Parity of the Go reader with Samba, on a real server (as root, python3-samba installed):
//
//	go test -c -tags parity -o parity.test ./internal/sambabridge
//	sudo NTACL_PARITY_ROOT=/srv/shares ./parity.test -test.run TestNTACLParity -test.v
//
// Every folder under the root (up to 3 levels) is read by the helper and by the Go reader; where Go answers, the
// answer must be the helper's, byte for byte in JSON. The Go path must have answered at least once.

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestNTACLParity(t *testing.T) {
	root := os.Getenv("NTACL_PARITY_ROOT")
	if root == "" {
		t.Skip("NTACL_PARITY_ROOT not set")
	}
	ctx := context.Background()
	b := &Bridge{}
	if c := b.Capabilities(ctx); !c.GoReadable() {
		t.Fatalf("Go reader not usable here: %+v", c)
	}
	roots := []string{root}
	var dirs []*os.File
	var refs []FolderRef
	same, fallback, missed := 0, 0, 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() || p == root {
			return nil
		}
		if strings.Count(strings.TrimPrefix(p, root), "/") > 3 {
			return fs.SkipDir
		}
		fd, err := syscall.Open(p, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if err != nil {
			return nil
		}
		dir := os.NewFile(uintptr(fd), p)
		py, perr := b.GetACL(ctx, dir, p, roots, "")
		goACL, gerr := readNTACL(dir, p, roots)
		switch {
		case gerr != nil:
			fallback++
			// left to the helper although Samba answered with the stored descriptor: a v4 whose POSIX hash
			// differs while its descriptor hash still matches (Samba's second check), or a bug here
			if blob, err := xattr(int(dir.Fd()), ntACLXattr, maxNTACLBlob); err == nil && perr == nil {
				if sd, _, err := parseNTACL(blob); err == nil {
					if a, err := describe(sd); err == nil {
						x, _ := json.Marshal(py)
						y, _ := json.Marshal(a)
						if string(x) == string(y) {
							missed++
							t.Logf("left to the helper, Samba gave the stored descriptor: %s", p)
						}
					}
				}
			}
		case perr != nil:
			t.Errorf("%s: Go answered, the helper failed: %v", p, perr)
		default:
			x, _ := json.Marshal(py)
			y, _ := json.Marshal(goACL)
			if string(x) != string(y) {
				t.Errorf("%s:\n helper %s\n     go %s", p, x, y)
			}
			same++
		}
		if len(dirs) < MaxBatch {
			dirs, refs = append(dirs, dir), append(refs, FolderRef{Path: p})
		} else {
			dir.Close()
		}
		return nil
	})
	defer func() {
		for _, d := range dirs {
			d.Close()
		}
	}()
	t.Logf("%d folders read in Go equal to Samba, %d left to the helper (%d of them with the stored descriptor)", same, fallback, missed)
	if same == 0 {
		t.Fatal("the Go path never answered")
	}
	// the batch with the Go reader on gives what the helper alone gives
	pyAll, _, err := b.GetACLs(ctx, dirs, refs, roots)
	if err != nil {
		t.Fatal(err)
	}
	g := &Bridge{GoRead: true}
	before := goReads.Load()
	goAll, _, err := g.GetACLs(ctx, dirs, refs, roots)
	if err != nil {
		t.Fatal(err)
	}
	x, _ := json.Marshal(pyAll)
	y, _ := json.Marshal(goAll)
	if string(x) != string(y) || goReads.Load() == before {
		t.Fatalf("batch differs (Go reads %d):\n helper %s\n  mixed %s", goReads.Load()-before, x, y)
	}
}
