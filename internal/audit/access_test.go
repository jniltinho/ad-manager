package audit_test

import (
	"path/filepath"
	"testing"

	"ad-manager/internal/audit"
)

func TestAccessLog(t *testing.T) {
	p := filepath.Join(t.TempDir(), "access.log")
	f, err := audit.OpenAccess(p)
	if err != nil {
		t.Fatal(err)
	}
	audit.Access(audit.Request{IP: "10.0.0.1", User: "ana", Method: "GET", Path: "/api/users", Status: 200, Ms: 1.5, Bytes: 120})
	audit.Access(audit.Request{IP: "10.0.0.2", Method: "POST", Path: "/api/login", Status: 401})
	audit.Access(audit.Request{IP: "10.0.0.1", User: "ana", Method: "GET", Path: "/api/policy", Status: 502, Error: "ldap down"})
	f.Close()
	defer audit.OpenAccess("") //nolint:errcheck // back to discarding

	tests := []struct {
		name, q string
		n       int
		first   string
		total   int
	}{
		{"newest first", "", 10, "/api/policy", 3},
		{"limit", "", 1, "/api/policy", 1},
		{"filter by user", "ANA", 10, "/api/policy", 2},
		{"filter by path", "login", 10, "/api/login", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs, err := audit.ReadAccess(p, tt.n, tt.q)
			if err != nil {
				t.Fatal(err)
			}
			if len(rs) != tt.total || rs[0].Path != tt.first {
				t.Fatalf("got %d entries, first %+v", len(rs), rs[0])
			}
		})
	}
	rs, _ := audit.ReadAccess(p, 10, "")
	if rs[0].Status != 502 || rs[0].Error != "ldap down" || rs[2].Ms != 1.5 || rs[2].Bytes != 120 {
		t.Fatalf("fields not kept: %+v", rs)
	}
}
