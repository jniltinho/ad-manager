package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ad-manager/internal/backup"
	"ad-manager/internal/config"
)

// a share root with a destination and one run's quarantine holding Financeiro/a.txt.
func quarantineLab(t *testing.T) (*Agent, Quarantine) {
	t.Helper()
	root, state := t.TempDir(), t.TempDir()
	a := &Agent{cfg: config.Agent{ShareRoots: []string{root}}, backups: &backup.Store{Dir: state},
		sync: &syncState{running: map[string]context.CancelFunc{}, trees: map[string]string{}, active: map[string]string{}}}
	j := SyncJob{ID: "fin", Gen: "g1", Dest: filepath.Join(root, "Dados")}
	if err := a.saveSyncJobs([]SyncJob{j}); err != nil {
		t.Fatal(err)
	}
	q := Quarantine{Name: "fin-20261018T120000Z-0000000a", Job: "fin", Gen: "g1", Run: "20261018T120000Z-0000000a",
		Dest: j.Dest, Created: time.Now().UTC()}
	q.Path = filepath.Join(root, SyncDir, q.Name)
	must(t, os.MkdirAll(filepath.Join(q.Path, "Financeiro"), 0o700))
	must(t, os.MkdirAll(j.Dest, 0o770))
	must(t, os.WriteFile(filepath.Join(q.Path, "Financeiro", "a.txt"), []byte("antes"), 0o600))
	must(t, a.writeQuarantine(q))
	return a, q
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// The file goes back to where it was; a file there already is a conflict, and with replace the two swap places.
func TestGiveBack(t *testing.T) {
	a, q := quarantineLab(t)
	res, err := a.giveBack(q, nil, false)
	if err != nil || res.Restored != 1 {
		t.Fatalf("give back: %+v %v", res, err)
	}
	if b, _ := os.ReadFile(filepath.Join(q.Dest, "Financeiro", "a.txt")); string(b) != "antes" {
		t.Fatalf("destination has %q", b)
	}
	// the run's quarantine gets a file again, and the destination has a newer one
	must(t, os.WriteFile(filepath.Join(q.Path, "Financeiro", "a.txt"), []byte("velho"), 0o600))
	res, _ = a.giveBack(q, []string{"Financeiro/a.txt"}, false)
	if len(res.Conflicts) != 1 || res.Restored != 0 {
		t.Fatalf("conflict expected: %+v", res)
	}
	res, _ = a.giveBack(q, []string{"Financeiro/a.txt"}, true)
	if res.Swapped != 1 {
		t.Fatalf("swap expected: %+v", res)
	}
	d, _ := os.ReadFile(filepath.Join(q.Dest, "Financeiro", "a.txt"))
	qq, _ := os.ReadFile(filepath.Join(q.Path, "Financeiro", "a.txt"))
	if string(d) != "velho" || string(qq) != "antes" {
		t.Fatalf("after the swap: destination %q, quarantine %q", d, qq)
	}
}

// A link on the way in the destination is never followed, and paths with .. are refused.
func TestGiveBackNoLinks(t *testing.T) {
	a, q := quarantineLab(t)
	out := t.TempDir()
	must(t, os.Symlink(out, filepath.Join(q.Dest, "Financeiro")))
	res, err := a.giveBack(q, nil, true)
	if err != nil || res.Restored+res.Swapped != 0 || len(res.Errors) != 1 {
		t.Fatalf("a link in the destination: %+v %v", res, err)
	}
	if es, _ := os.ReadDir(out); len(es) != 0 {
		t.Fatal("wrote through the link")
	}
	for _, p := range []string{"../x", "/etc/passwd", "a/../../b", "a//b", "."} {
		if _, err := cleanRel(p); err == nil {
			t.Errorf("cleanRel(%q) accepted", p)
		}
	}
}

// Retention: older than keep_days goes, the current run's never; another generation is not touched.
func TestPruneQuarantine(t *testing.T) {
	a, q := quarantineLab(t)
	old := q
	old.Run, old.Created = "20260901T120000Z-0000000b", time.Now().Add(-40*24*time.Hour)
	old.Name = "fin-" + old.Run
	old.Path = filepath.Join(filepath.Dir(q.Path), old.Name)
	must(t, os.MkdirAll(old.Path, 0o700))
	must(t, os.WriteFile(filepath.Join(old.Path, "x"), []byte("x"), 0o600))
	must(t, a.writeQuarantine(old))
	other := old
	other.Gen, other.Run, other.Name = "g0", "20260801T120000Z-0000000c", "fin-20260801T120000Z-0000000c"
	other.Path = filepath.Join(filepath.Dir(q.Path), other.Name)
	must(t, os.MkdirAll(other.Path, 0o700))
	must(t, a.writeQuarantine(other))
	j, _ := a.syncJob("fin")
	if n, _ := a.pruneQuarantine(j, old.Run); n != 0 {
		t.Fatalf("pruned the current run's quarantine (%d)", n)
	}
	if n, _ := a.pruneQuarantine(j, "none"); n != 1 {
		t.Fatalf("pruned %d, want 1", n)
	}
	if _, err := os.Stat(old.Path); !os.IsNotExist(err) {
		t.Fatal("the old quarantine stayed")
	}
	for _, p := range []string{q.Path, other.Path} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s removed", p)
		}
	}
}

// _sync swapped for a link: removing a quarantine never reaches the folder it points to.
func TestDropQuarantineNoLinks(t *testing.T) {
	a, q := quarantineLab(t)
	elsewhere := t.TempDir()
	victim := filepath.Join(elsewhere, q.Name)
	must(t, os.MkdirAll(victim, 0o700))
	sd := filepath.Dir(q.Path)
	must(t, os.Rename(sd, sd+".old"))
	must(t, os.Symlink(elsewhere, sd))
	if err := a.dropQuarantine(q); err == nil {
		t.Fatal("removed through a link")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatal("the folder behind the link is gone")
	}
}

// A quarantine of a run still going on is not given back.
func TestGiveBackActiveRun(t *testing.T) {
	a, q := quarantineLab(t)
	a.sync.active[q.Job] = q.Run
	if _, err := a.giveBack(q, nil, false); err == nil {
		t.Fatal("gave back from a running run's quarantine")
	}
}
