package agent

import (
	"os"
	"path/filepath"
	"testing"

	"ad-manager/internal/config"
)

// The import write modes: switched on by import_modes (no file), or the former account and 0600 file; the GPO
// account and its PASSWD_FILE come from the panel's credential (a 0600 file removed by done), else the former file.
func TestImportModes(t *testing.T) {
	off := &Agent{}
	if off.gpoRestoreReady() == nil || off.passwordMigrationReady() == nil {
		t.Fatal("on without any setting")
	}
	on := &Agent{cfg: config.Agent{ImportModes: true}}
	if on.gpoRestoreReady() != nil || on.passwordMigrationReady() != nil {
		t.Fatal("import_modes did not turn the modes on")
	}

	user, f, done, err := on.gpoAdmin("Administrator", "S3cret pw")
	if err != nil || user != "Administrator" {
		t.Fatalf("gpoAdmin: %q %v", user, err)
	}
	fi, err := os.Stat(f)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("PASSWD_FILE %s: %v %v", f, fi, err)
	}
	if b, _ := os.ReadFile(f); string(b) != "S3cret pw" {
		t.Fatalf("PASSWD_FILE holds %q", b)
	}
	done()
	if _, err := os.Stat(filepath.Dir(f)); !os.IsNotExist(err) {
		t.Fatal("the credential's folder survived the task")
	}
	if _, _, _, err := on.gpoAdmin("", ""); err == nil {
		t.Fatal("no credential and no former file accepted")
	}
	if _, _, _, err := on.gpoAdmin("Admin", "a\nb"); err == nil {
		t.Fatal("a password with a newline accepted")
	}

	legacy := filepath.Join(t.TempDir(), "import.pass")
	_ = os.WriteFile(legacy, []byte("x\n"), 0o600)
	old := &Agent{cfg: config.Agent{GPORestoreUser: "Administrator", GPORestorePasswordFile: legacy}}
	if old.gpoRestoreReady() != nil {
		t.Fatal("the former keys no longer work")
	}
	if u, f, done, err := old.gpoAdmin("", ""); err != nil || u != "Administrator" || f != legacy {
		t.Fatalf("former file: %q %q %v", u, f, err)
	} else {
		done()
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatal("done removed the operator's own file")
	}
}
