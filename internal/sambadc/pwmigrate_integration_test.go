//go:build integration

package sambadc

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// The Go password migration on a real destination DC, as root (it runs Samba's ldbsearch/ldbmodify on the local
// sam.ldb). scripts/test-pwmigrate.sh builds this test, starts a source and a destination Samba DC with the same realm
// and runs it in the destination with:
//
//	PW_SRC_HOST, PW_SRC_USER, PW_SRC_PASSWORD   the source DC and its replication account
//	PW_DEST_SID                                 this domain's SID (the pin)
//	PW_APPLY                                    "1" to apply, else the plan
//
// It prints the result (outcomes and SIDs only, never material) as JSON for the script to compare.
func TestMigratePasswordsLive(t *testing.T) {
	host := os.Getenv("PW_SRC_HOST")
	if host == "" {
		t.Skip("no source DC (scripts/test-pwmigrate.sh)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	res, err := MigratePasswords(ctx, os.Getenv("PW_APPLY") == "1", PwSource{Host: host, User: os.Getenv("PW_SRC_USER")},
		os.Getenv("PW_SRC_PASSWORD"), os.Getenv("PW_DEST_SID"))
	if err != nil {
		t.Fatalf("MigratePasswords: %v", err)
	}
	b, _ := json.Marshal(res)
	t.Logf("RESULT %s", b)
}
