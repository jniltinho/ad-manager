package sambadc

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// showreplJSON is `samba-tool drs showrepl --json` of a DC with one healthy partner and one failing.
const showreplJSON = `{"dsa": {"options": 1},
 "repsFrom": [
  {"NC dn": "DC=empresa,DC=net", "DSA objectGUID": "a", "DSA": "Default-First-Site-Name\\DC2",
   "last attempt time": "Sun Oct 18 12:00:00 2026 -03", "last attempt message": "was successful",
   "consecutive failures": 0, "last success": "Sun Oct 18 12:00:00 2026 -03", "is deleted": false},
  {"NC dn": "CN=Configuration,DC=empresa,DC=net", "DSA objectGUID": "b",
   "last attempt time": "Sun Oct 18 12:00:00 2026 -03", "last attempt message": "WERR_BADFILE",
   "consecutive failures": 3, "last success": "NTTIME(0)", "is deleted": false}],
 "repsTo": [], "NTDSConnections": []}`

func TestParseReplication(t *testing.T) {
	r, err := ParseReplication([]byte(showreplJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Links) != 2 || r.OK {
		t.Fatalf("got %+v", r)
	}
	if !r.Links[0].OK || r.Links[0].DSA != `Default-First-Site-Name\DC2` || r.Links[0].Direction != "inbound" {
		t.Fatalf("healthy link: %+v", r.Links[0])
	}
	if r.Links[1].OK || r.Links[1].DSA != "b" || r.Links[1].Failures != 3 {
		t.Fatalf("failing link: %+v", r.Links[1])
	}
	single, err := ParseReplication([]byte(`{"repsFrom": [], "repsTo": []}`))
	if err != nil || !single.OK || single.Links == nil {
		t.Fatalf("a single DC is healthy: %+v %v", single, err)
	}
	if _, err := ParseReplication([]byte("Failed to connect")); err == nil {
		t.Fatal("invalid output accepted")
	}
}

func TestRunChecks(t *testing.T) {
	ok := func(_ context.Context, name string, args ...string) (string, error) {
		if args[0] != "dbcheck" || slices.Contains(args, "--fix") {
			t.Fatalf("unexpected command %s %v", name, args)
		}
		return "Checking 3847 objects\nChecked 3847 objects (0 errors)\n", nil
	}
	c := RunChecks(context.Background(), ok)
	if !c.OK || len(c.Checks) != 1 || c.Checks[0].Objects != 3847 || c.Checks[0].Errors != 0 {
		t.Fatalf("healthy: %+v", c)
	}
	bad := func(_ context.Context, name string, args ...string) (string, error) {
		if args[0] == "dbcheck" {
			return "ERROR: missing backlink\nChecked 10 objects (2 errors)\n", errors.New("exit status 1")
		}
		t.Fatalf("unexpected command %s %v", name, args)
		return "", nil
	}
	c = RunChecks(context.Background(), bad)
	if c.OK || c.Checks[0].Errors != 2 || !strings.Contains(c.Checks[0].Output, "missing backlink") {
		t.Fatalf("problems: %+v", c)
	}
}
