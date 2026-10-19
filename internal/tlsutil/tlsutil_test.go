package tlsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSelfSigned(t *testing.T) {
	dir := t.TempDir()
	c1, fp1, err := Config("", "", true, dir)
	if err != nil || c1 == nil || len(fp1) != 64 {
		t.Fatalf("generate: %v %q", err, fp1)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "selfsigned.key")); fi.Mode().Perm() != 0o600 {
		t.Errorf("key has mode %v, want 0600", fi.Mode().Perm())
	}
	_, fp2, _ := Config("", "", true, dir)
	if fp1 != fp2 {
		t.Error("the second start should reuse the same certificate")
	}
	if c, _, _ := Config("", "", false, dir); c != nil {
		t.Error("without self_signed and without files there must be no TLS")
	}
}
