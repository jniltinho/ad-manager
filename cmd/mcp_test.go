package cmd

import (
	"context"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPTools checks which tools exist with and without --allow-write.
func TestMCPTools(t *testing.T) {
	list := func(write bool) []string {
		t.Helper()
		ctx := context.Background()
		s := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "1"}, nil)
		registerTools(s, write)
		ct, st := mcp.NewInMemoryTransports()
		if _, err := s.Connect(ctx, st, nil); err != nil {
			t.Fatal(err)
		}
		cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "1"}, nil).Connect(ctx, ct, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer cs.Close()
		r, err := cs.ListTools(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, tl := range r.Tools {
			names = append(names, tl.Name)
		}
		return names
	}
	ro, rw := list(false), list(true)
	for _, name := range []string{"ad_users_list", "ad_audit_log", "ad_samba_logs", "ad_policy_show", "ad_import_plan"} {
		if !slices.Contains(ro, name) {
			t.Errorf("read-only tool %s missing", name)
		}
	}
	for _, name := range []string{"ad_user_create", "ad_object_delete", "ad_ou_create", "ad_policy_set", "ad_import_apply"} {
		if slices.Contains(ro, name) {
			t.Errorf("%s must need --allow-write", name)
		}
		if !slices.Contains(rw, name) {
			t.Errorf("write tool %s missing", name)
		}
	}
	if len(ro) != 13 || len(rw) != 32 {
		t.Errorf("tools: %d read-only, %d with write; want 13 and 32", len(ro), len(rw))
	}
}
