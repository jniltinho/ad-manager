package audit_test

import (
	"os"
	"path/filepath"
	"testing"

	"ad-manager/internal/audit"
)

func TestReadAudit(t *testing.T) {
	p := filepath.Join(t.TempDir(), "audit.log")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	audit.SetWriter(f)
	audit.Log("ana", "10.0.0.1", "user.disable", "bruno", nil)
	audit.Log("ana", "10.0.0.1", "password.reset", "carla", nil)
	audit.Log("joao", "cli", "user.delete", "bruno", os.ErrPermission)
	f.WriteString("garbage that is not JSON\n")
	f.Close()

	tests := []struct {
		name, q string
		n       int
		first   string
		total   int
	}{
		{"newest first", "", 10, "user.delete", 3},
		{"limit", "", 1, "user.delete", 1},
		{"filter", "bruno", 10, "user.delete", 2},
		{"filter without match", "zzz", 10, "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, err := audit.Read(p, tt.n, tt.q)
			if err != nil {
				t.Fatal(err)
			}
			if len(ev) != tt.total || (tt.total > 0 && ev[0].Action != tt.first) {
				t.Errorf("got %+v", ev)
			}
		})
	}
}
