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
	"fmt"
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

// TestReinheritMemoParity: the memoized walk (one Samba write per distinct key, the state copied to the rest) leaves
// every item exactly as a pass through Samba item by item does: the same owner, group, mode and ACL xattrs, byte for
// byte. Tree A gets one full Reapply; tree B gets one ReinheritPaths per item, so each walk meets every item first
// (no memo hit). Folders with an explicit ACE and files with another mode make more than one key per folder.
//
//	sudo NTACL_REINHERIT_ROOT=/srv/shares/Test ./parity.test -test.run TestReinheritMemoParity -test.v
func TestReinheritMemoParity(t *testing.T) {
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
	dirs := []string{"d1", "d1/d2", "d3", "d3/d4"}
	var rels []string
	seed := func(base string) {
		rels = rels[:0]
		if err := os.MkdirAll(base, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, d := range dirs {
			if err := os.MkdirAll(filepath.Join(base, d), 0o755); err != nil {
				t.Fatal(err)
			}
			rels = append(rels, d)
		}
		for _, d := range append([]string{""}, dirs...) {
			for i := range 25 {
				f := filepath.Join(d, fmt.Sprintf("f%02d.txt", i))
				mode := os.FileMode(0o644)
				if i%5 == 0 {
					mode = 0o600
				}
				if err := os.WriteFile(filepath.Join(base, f), []byte("x"), mode); err != nil {
					t.Fatal(err)
				}
				rels = append(rels, f)
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
	// the template on the top and, on d3, an explicit ACE of its own that its contents inherit too
	prepare := func(base string) string {
		top := open(base)
		defer top.Close()
		cur, err := b.GetACL(ctx, top, base, roots, "")
		if err != nil {
			t.Fatalf("read top ACL: %v", err)
		}
		og := cur.SDDL[:strings.Index(cur.SDDL, "D:")]
		sddl := og + "D:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;0x1301bf;;;BU)(A;OICI;FA;;;CO)"
		if _, err := b.SetSDDL(ctx, top, base, roots, sddl, false); err != nil {
			t.Fatalf("set top ACL: %v", err)
		}
		d3 := open(filepath.Join(base, "d3"))
		defer d3.Close()
		if _, err := b.SetSDDL(ctx, d3, filepath.Join(base, "d3"), roots, og+"D:AI(A;OICI;0x1200a9;;;BG)", false); err != nil {
			t.Fatalf("set d3 ACL: %v", err)
		}
		return sddl
	}

	ta, tb := filepath.Join(root, "memo-a"), filepath.Join(root, "memo-b")
	os.RemoveAll(ta)
	os.RemoveAll(tb)
	t.Cleanup(func() { os.RemoveAll(ta); os.RemoveAll(tb) })
	seed(ta)
	seed(tb)
	sddl := prepare(ta)
	prepare(tb)

	topA := open(ta)
	defer topA.Close()
	if _, err := b.Reapply(ctx, topA, ta, roots, sddl); err != nil { // memoized
		t.Fatalf("reapply: %v", err)
	}
	topB := open(tb)
	defer topB.Close()
	for _, r := range rels { // parents first: dirs come before their files in rels
		if _, err := b.ReinheritPaths(ctx, topB, tb, roots, []string{r}); err != nil {
			t.Fatalf("reinherit %s: %v", r, err)
		}
	}
	state := func(p string) string {
		var st syscall.Stat_t
		if err := syscall.Lstat(p, &st); err != nil {
			t.Fatal(err)
		}
		s := fmt.Sprintf("%d:%d:%o", st.Uid, st.Gid, st.Mode)
		for _, x := range []string{"system.posix_acl_access", "system.posix_acl_default", "user.SAMBA_PAI", ntACLXattr} {
			buf := make([]byte, 64<<10)
			n, err := syscall.Getxattr(p, x, buf)
			if err == syscall.ENODATA {
				s += " " + x + "=-"
				continue
			}
			if err != nil {
				t.Fatalf("%s: read %s: %v", p, x, err)
			}
			s += fmt.Sprintf(" %s=%x", x, buf[:n])
		}
		return s
	}
	for _, r := range rels {
		a, bb := state(filepath.Join(ta, r)), state(filepath.Join(tb, r))
		if !strings.Contains(a, ntACLXattr+"=") || strings.Contains(a, ntACLXattr+"=-") {
			t.Errorf("%s: no Windows ACL stored", r)
		}
		if a != bb {
			t.Errorf("%s differs:\n memo %s\n item %s", r, a, bb)
		}
	}
}
