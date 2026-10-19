//go:build linux

package shares

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestEnsureTraverse(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	parent := filepath.Join(root, "Comercial")
	middle := filepath.Join(parent, "Clientes")
	if err := os.MkdirAll(middle, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Chmod(parent, 0o2770); err != nil { // os.Chmod does not understand 0o2000
		t.Fatal(err)
	}
	before, _ := os.Stat(root)
	m := &Manager{Roots: []string{root}}
	if err := m.ensureTraverse(middle); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]os.FileMode{parent: 0o771, middle: 0o751, root: before.Mode().Perm()} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != want {
			t.Errorf("%s: %v, want %v", p, fi.Mode().Perm(), want)
		}
	}
	if fi, _ := os.Stat(parent); fi.Mode()&os.ModeSetgid == 0 {
		t.Error("setgid lost on the parent")
	}
	// symbolic link in the path: rejected
	if err := os.Symlink(middle, filepath.Join(root, "shortcut")); err != nil {
		t.Fatal(err)
	}
	if err := m.ensureTraverse(filepath.Join(root, "shortcut")); err == nil {
		t.Error("should reject a symbolic link")
	}
}

func TestMkdirFromRoot(t *testing.T) {
	t.Parallel()
	root, outside := t.TempDir(), t.TempDir()
	m := &Manager{Roots: []string{root}}
	fd, err := m.mkdirFromRoot(filepath.Join(root, "Users", "Ana"))
	if err != nil {
		t.Fatal(err)
	}
	syscall.Close(fd)
	if fi, err := os.Stat(filepath.Join(root, "Users", "Ana")); err != nil || !fi.IsDir() {
		t.Fatalf("folder not created: %v", err)
	}
	// an intermediate component swapped for a link to outside the root (after
	// ValidateFolder): rejected, and nothing created outside
	if err := os.Symlink(outside, filepath.Join(root, "Link")); err != nil {
		t.Fatal(err)
	}
	if fd, err := m.mkdirFromRoot(filepath.Join(root, "Link", "Sub", "Leaf")); err == nil {
		syscall.Close(fd)
		t.Fatal("should reject a symbolic link in the path")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("created outside the root: %v", entries)
	}
	if _, err := m.mkdirFromRoot(filepath.Join(outside, "X")); err == nil {
		t.Error("should reject a path outside the roots")
	}
}
