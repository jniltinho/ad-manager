package agent

import (
	"os"
	"path/filepath"
	"testing"

	"ad-manager/internal/config"
)

// TestPasswordMigrationReady checks the dedicated gate: both fields set and a 0600 password file. It mirrors the
// GPO-restore gate but is independent — one being on must not satisfy the other.
func TestPasswordMigrationReady(t *testing.T) {
	dir := t.TempDir()
	pass := filepath.Join(dir, "pm.pass")
	if err := os.WriteFile(pass, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	// off by default
	a := &Agent{cfg: config.Agent{}}
	if a.passwordMigrationReady() == nil {
		t.Error("gate must be off when the fields are empty")
	}

	// GPO restore on does not enable password migration
	a = &Agent{cfg: config.Agent{GPORestoreUser: "svc", GPORestorePasswordFile: pass}}
	if a.passwordMigrationReady() == nil {
		t.Error("the GPO-restore gate must not satisfy the password-migration gate")
	}

	// fully set up
	a = &Agent{cfg: config.Agent{PasswordMigrationUser: "administrator@example.local", PasswordMigrationPasswordFile: pass}}
	if err := a.passwordMigrationReady(); err != nil {
		t.Errorf("gate should be on: %v", err)
	}

	// password file readable by group/others is refused
	if err := os.Chmod(pass, 0o644); err != nil {
		t.Fatal(err)
	}
	if a.passwordMigrationReady() == nil {
		t.Error("a 0644 password file must be refused")
	}

	// missing file is refused
	a = &Agent{cfg: config.Agent{PasswordMigrationUser: "svc", PasswordMigrationPasswordFile: filepath.Join(dir, "nope")}}
	if a.passwordMigrationReady() == nil {
		t.Error("a missing password file must be refused")
	}
}
