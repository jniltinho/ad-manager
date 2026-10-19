package secretbox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSealOpen(t *testing.T) {
	p := filepath.Join(t.TempDir(), "k", "secret.key")
	b, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Fatalf("key mode %v", fi.Mode())
	}
	s := b.Seal([]byte("senha"), "sync:1:fin")
	if got, err := b.Open(s, "sync:1:fin"); err != nil || string(got) != "senha" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := b.Open(s, "sync:1:other"); err == nil {
		t.Fatal("a sealed value opened for another record")
	}
	b2, err := Load(p) // the same key again
	if err != nil {
		t.Fatal(err)
	}
	if got, err := b2.Open(s, "sync:1:fin"); err != nil || string(got) != "senha" {
		t.Fatalf("reloaded key: %q %v", got, err)
	}
}
