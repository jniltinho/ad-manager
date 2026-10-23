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

// TestReinheritPathsParity: a partial reinherit over a set of paths leaves each directory in that set with the same
// Windows ACL a full reinherit of the whole tree gives it. Two identical trees are built and seeded without Samba;
// one gets the full Reapply, the other ReinheritPaths over all its items. Needs a real Samba root (as the parity run):
//
//	sudo NTACL_REINHERIT_ROOT=/srv/shares/Test ./parity.test -test.run TestReinheritPathsParity -test.v
func TestReinheritPathsParity(t *testing.T) {
	root := os.Getenv("NTACL_REINHERIT_ROOT")
	if root == "" {
		t.Skip("NTACL_REINHERIT_ROOT not set")
	}
	ctx := context.Background()
	b := &Bridge{}
	if c := b.Capabilities(ctx); !c.Usable() {
		t.Skipf("Samba NT-ACL helper not usable here (needs acl_xattr and root): %+v", c)
	}
	roots := []string{root}
	// seed one tree (dirs and files written by this process, i.e. without Samba) under base
	seed := func(base string) {
		for _, d := range []string{"", "sub", "sub/deep", "other"} {
			if err := os.MkdirAll(filepath.Join(base, d), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		for _, f := range []string{"top.txt", "sub/a.txt", "sub/deep/b.txt", "other/c.txt"} {
			if err := os.WriteFile(filepath.Join(base, f), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	open := func(p string) *os.File {
		fd, err := syscall.Open(p, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if err != nil {
			t.Fatalf("open %s: %v", p, err)
		}
		return os.NewFile(uintptr(fd), p)
	}

	full := filepath.Join(root, "reinherit-full")
	inc := filepath.Join(root, "reinherit-inc")
	os.RemoveAll(full)
	os.RemoveAll(inc)
	t.Cleanup(func() { os.RemoveAll(full); os.RemoveAll(inc) })
	seed(full)
	seed(inc)

	fullTop, incTop := open(full), open(inc)
	defer fullTop.Close()
	defer incTop.Close()
	sddl, err := b.GetACL(ctx, fullTop, full, roots, "")
	if err != nil {
		t.Fatalf("read top ACL: %v", err)
	}
	if _, err := b.Reapply(ctx, fullTop, full, roots, sddl.SDDL); err != nil { // full tree
		t.Fatalf("full reapply: %v", err)
	}
	rels := []string{"sub", "sub/deep", "other", "top.txt", "sub/a.txt", "sub/deep/b.txt", "other/c.txt"}
	if _, err := b.ReinheritPaths(ctx, incTop, inc, roots, rels); err != nil { // only these paths (+ ancestors)
		t.Fatalf("reinherit paths: %v", err)
	}
	// every directory in the set ends identical to the full reinherit (files follow the same per-item code path)
	for _, d := range []string{"sub", "sub/deep", "other"} {
		fa, ia := open(filepath.Join(full, d)), open(filepath.Join(inc, d))
		af, aerr := b.GetACL(ctx, fa, filepath.Join(full, d), roots, "")
		bf, berr := b.GetACL(ctx, ia, filepath.Join(inc, d), roots, "")
		fa.Close()
		ia.Close()
		if aerr != nil || berr != nil {
			t.Fatalf("%s: read %v / %v", d, aerr, berr)
		}
		if af.SDDL != bf.SDDL {
			t.Fatalf("%s differs:\n full %s\n  inc %s", d, af.SDDL, bf.SDDL)
		}
	}
}
