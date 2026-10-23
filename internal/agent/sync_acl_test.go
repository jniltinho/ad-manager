package agent

import (
	"fmt"
	"os"
	"testing"
	"time"

	"ad-manager/internal/backup"
	"ad-manager/internal/syncer"
)

// TestACLPlan: the ACL pass follows the transfers, the whole tree on a capped or listless run, and a pending
// inheritance forces the whole tree even when nothing came in.
func TestACLPlan(t *testing.T) {
	some := &syncer.Result{Transfers: 3}
	none := &syncer.Result{}
	for _, c := range []struct {
		name          string
		pending       bool
		res           *syncer.Result
		changed       []string
		capped        bool
		wantDo, wantF bool
	}{
		{"incremental", false, some, []string{"a", "b", "c"}, false, true, false},
		{"capped", false, some, []string{"a"}, true, true, true},
		{"no list", false, some, nil, false, true, true},
		{"up to date", false, none, nil, false, false, true},
		{"pending, nothing came in", true, none, nil, false, true, true},
		{"pending, incremental list", true, some, []string{"a"}, false, true, true},
		{"worker gave no result", true, nil, nil, false, false, false},
	} {
		do, full := aclPlan(c.pending, c.res, c.changed, c.capped)
		if do != c.wantDo || (do && full != c.wantF) {
			t.Errorf("%s: do %v full %v, want %v %v", c.name, do, full, c.wantDo, c.wantF)
		}
	}
}

// TestACLPendingMark: one mark per destination, so a run on another destination never clears it.
func TestACLPendingMark(t *testing.T) {
	a := &Agent{backups: &backup.Store{Dir: t.TempDir()}}
	if err := a.markACLPending("job1", "/srv/shares/A"); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(a.aclPendingFile("job1", "/srv/shares/A"))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("mark: %v %v", st, err)
	}
	if a.aclPendingFile("job1", "/srv/shares/A") == a.aclPendingFile("job1", "/srv/shares/B") {
		t.Fatal("two destinations share a mark")
	}
	if err := a.markACLPending("job1", "/srv/shares/A"); err != nil { // marking again is fine
		t.Fatal(err)
	}
}

// TestLegacyACLPending: a run made before the mark existed that failed its inheritance (or copied and then failed)
// leaves the job pending; later runs that copied nothing do not hide it, and one that applied it clears it.
func TestLegacyACLPending(t *testing.T) {
	a := &Agent{backups: &backup.Store{Dir: t.TempDir()}}
	at := time.Date(2026, 10, 23, 3, 0, 0, 0, time.UTC)
	n := 0
	run := func(status string, transfers int64, notes ...string) {
		n++
		at = at.Add(time.Hour)
		a.writeRun(SyncRun{ID: fmt.Sprintf("r%02d", n), Job: "j", Started: at, Finished: at.Add(time.Minute),
			Status: status, Result: &syncer.Result{Transfers: transfers}, Notes: notes})
	}
	if a.legacyACLPending("j", "now") {
		t.Fatal("no history: not pending")
	}
	run("ok", 15828, "Windows inheritance not applied: samba bridge: no answer (read unix @->@: i/o timeout)")
	run("ok", 0)
	if !a.legacyACLPending("j", "now") {
		t.Fatal("a timed-out inheritance behind an up-to-date run must stay pending")
	}
	if a.legacyACLPending("other", "now") {
		t.Fatal("another job's history counts")
	}
	run("ok", 0, "an earlier run left the Windows inheritance pending: reapplying it to the whole folder",
		"Windows inheritance of /srv/shares/F applied to 16641 items")
	if a.legacyACLPending("j", "now") {
		t.Fatal("applied since: not pending")
	}
	run("cancelled", 40)
	if !a.legacyACLPending("j", "now") {
		t.Fatal("cancelled after copying: pending")
	}
	run("ok", 0, "Windows inheritance of /srv/shares/F applied to 16641 items")
	n++
	at = at.Add(time.Hour)
	a.writeRun(SyncRun{ID: fmt.Sprintf("r%02d", n), Job: "j", Started: at, Finished: at.Add(time.Minute), Status: "cancelled"})
	if !a.legacyACLPending("j", "now") {
		t.Fatal("cancelled with no result (the worker killed before it): pending")
	}
}
