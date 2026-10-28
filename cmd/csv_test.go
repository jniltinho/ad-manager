package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWritePasswordsCreatesDir: the users already exist when the passwords are written, so a --dir that does not
// exist yet is created (0700) instead of losing the only copy of the generated passwords.
func TestWritePasswordsCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new", "imp")
	path, err := writePasswords(dir, map[string]string{"plain": "x"})
	if err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]os.FileMode{dir: 0o700, path: 0o600} {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != want {
			t.Errorf("%s: mode %v, want %v", p, st.Mode().Perm(), want)
		}
	}
}
