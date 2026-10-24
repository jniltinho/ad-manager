package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"ad-manager/internal/shares"
)

// testSizes is a job on fakes: the listed shares, and a measure per path.
func testSizes(t *testing.T, list []shares.Share, measure func(path string) (shares.SizeResult, error)) (*sizeJob, *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	j := &sizeJob{file: filepath.Join(t.TempDir(), "share-sizes.json"), now: func() time.Time { return now },
		jitter: func() float64 { return 0.5 }, wake: make(chan struct{}, 1),
		list:    func(context.Context) ([]shares.Share, error) { return list, nil },
		resolve: func(p string) (string, bool, error) { return p, false, nil },
		measure: func(_ context.Context, p string) (shares.SizeResult, error) { return measure(p) }}
	j.load()
	return j, &now
}

func ok(n int64) (shares.SizeResult, error) { return shares.SizeResult{Bytes: n}, nil }

func TestSizeSchedule(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	if d := firstDue(now, time.Time{}, 0).Sub(now); d != time.Minute {
		t.Errorf("never scanned: %s", d)
	}
	if d := firstDue(now, now.Add(-5*time.Hour), 1).Sub(now); d != 5*time.Minute {
		t.Errorf("old scan: %s", d)
	}
	if got := firstDue(now, now.Add(-time.Hour), 0.5); !got.Equal(now.Add(3 * time.Hour)) {
		t.Errorf("recent scan: %s", got)
	}
	if lo, hi := nextDue(now, 0).Sub(now), nextDue(now, 1).Sub(now); lo != 216*time.Minute || hi != 264*time.Minute {
		t.Errorf("jitter: %s %s", lo, hi)
	}
}

// A full scan keeps the last complete value apart from a partial or failed attempt, prunes removed shares and
// schedules the next scan from its end; a short scan moves neither.
func TestSizeScan(t *testing.T) {
	list := []shares.Share{{Name: "Finance", Path: "/srv/shares/Finance"}, {Name: "HR", Path: "/srv/shares/HR"},
		{Name: "Gone", Path: "/srv/shares/Gone"}, {Name: "Printers"}}
	var fail atomic.Bool
	j, now := testSizes(t, list, func(p string) (shares.SizeResult, error) {
		switch {
		case !fail.Load():
			return ok(int64(len(p)))
		case p == "/srv/shares/Finance":
			return shares.SizeResult{Bytes: 7, Partial: true}, errors.New("du: cannot read directory 'x'")
		default:
			return shares.SizeResult{}, errors.New("du: cannot access")
		}
	})
	j.scanOnce(context.Background(), "full", nil)
	s := j.snapshot()
	if s["total_complete"] != true || len(s["sizes"].(map[string]int64)) != 3 || s["scanning"] != nil {
		t.Fatalf("first scan: %v", s)
	}
	if !j.data.FullScanEndedAt.Equal(*now) || !j.next.Equal(now.Add(4*time.Hour)) {
		t.Fatalf("schedule: %s %s", j.data.FullScanEndedAt, j.next)
	}

	fail.Store(true)
	*now = now.Add(time.Hour)
	j.scanOnce(context.Background(), "short", []string{"Finance"})
	f := j.data.Shares["Finance"]
	if *f.Bytes != int64(len("/srv/shares/Finance")) || *f.PartialBytes != 7 || f.Error == "" || f.MeasuredAt.Equal(*now) {
		t.Fatalf("partial: %+v", f)
	}
	if !j.data.FullScanEndedAt.Equal(now.Add(-time.Hour)) {
		t.Fatal("a short scan moved the full scan's end")
	}
	if j.data.Shares["HR"].Error != "" {
		t.Fatal("a short scan measured another share")
	}

	list = list[:2]
	j.list = func(context.Context) ([]shares.Share, error) { return list, nil }
	j.scanOnce(context.Background(), "full", nil)
	if _, ok := j.data.Shares["Gone"]; ok {
		t.Fatal("a removed share kept")
	}
	hr := j.data.Shares["HR"]
	if hr.Bytes == nil || hr.Error != "du: cannot access" {
		t.Fatalf("failed: %+v", hr)
	}
	if s := j.snapshot(); s["total_complete"] != false {
		t.Fatal("complete with errors")
	}

	// on disk, and read back by a new agent; a corrupt file is an empty cache
	b, _ := os.ReadFile(j.file)
	var disk sizeFile
	if err := json.Unmarshal(b, &disk); err != nil || disk.Version != 1 || *disk.Shares["HR"].Bytes != *hr.Bytes {
		t.Fatalf("disk: %v %s", err, b)
	}
	if fi, _ := os.Stat(j.file); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode())
	}
	k := &sizeJob{file: j.file, now: j.now, jitter: j.jitter}
	k.load()
	if *k.data.Shares["HR"].Bytes != *hr.Bytes || !k.next.Equal(nextDue(k.data.FullScanEndedAt, 0.5)) {
		t.Fatalf("reload: %+v %s", k.data, k.next)
	}
	_ = os.WriteFile(j.file, []byte("{"), 0o600)
	k.load()
	if len(k.data.Shares) != 0 || !k.next.Equal(now.Add(3*time.Minute)) {
		t.Fatalf("corrupt: %+v %s", k.data, k.next)
	}
}

// A share moved to another folder, or whose symlink now points elsewhere, loses the old folder's numbers.
func TestSizePathChange(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	_ = os.Mkdir(a, 0o755)
	_ = os.Mkdir(b, 0o755)
	link := filepath.Join(dir, "link")
	_ = os.Symlink(a, link)
	list := []shares.Share{{Name: "S", Path: link}}
	failing := false
	j, _ := testSizes(t, list, func(p string) (shares.SizeResult, error) {
		r, _ := filepath.EvalSymlinks(p)
		if failing {
			return shares.SizeResult{RealPath: r}, errors.New("du: failed")
		}
		return shares.SizeResult{RealPath: r, Bytes: 10}, nil
	})
	j.resolve = resolvePath
	j.scanOnce(context.Background(), "full", nil)
	_ = os.Remove(link)
	_ = os.Symlink(b, link)
	failing = true
	j.scanOnce(context.Background(), "full", nil)
	if e := j.data.Shares["S"]; e.Bytes != nil || e.RealPath != b {
		t.Fatalf("retargeted: %+v", e)
	}
	// retargeted to a folder that does not exist: nothing of the old one is kept either
	failing = false
	j.scanOnce(context.Background(), "full", nil)
	_ = os.Remove(link)
	_ = os.Symlink(filepath.Join(dir, "missing"), link)
	j.scanOnce(context.Background(), "full", nil)
	if e := j.data.Shares["S"]; e.Bytes != nil || e.Error == "" {
		t.Fatalf("retargeted to nothing: %+v", e)
	}
	j.data.Shares["S"].Bytes = new(int64(5))
	failing = true
	j.list = func(context.Context) ([]shares.Share, error) { return []shares.Share{{Name: "S", Path: a}}, nil }
	j.scanOnce(context.Background(), "full", nil)
	if e := j.data.Shares["S"]; e.Bytes != nil || e.Path != a {
		t.Fatalf("moved: %+v", e)
	}
}

// Nested and aliased shares count once; an unmeasured outer share hides nothing; oldest_at covers every entry.
func TestSizeTotal(t *testing.T) {
	j, now := testSizes(t, nil, nil)
	old := now.Add(-10 * time.Hour)
	n := func(v int64) *int64 { return &v }
	j.data.FullScanEndedAt = *now
	j.data.Shares = map[string]*sizeEntry{
		"Comercial":  {RealPath: "/srv/shares/Comercial", Bytes: n(100), MeasuredAt: now},
		"Contrato":   {RealPath: "/srv/shares/Comercial/Contrato", Bytes: n(40), MeasuredAt: &old},
		"Alias":      {RealPath: "/srv/shares/Comercial", Bytes: n(100), MeasuredAt: now},
		"Users":      {RealPath: "/srv/shares/Users"},
		"Ana":        {RealPath: "/srv/shares/Users/Ana", Bytes: n(7), MeasuredAt: now},
		"Remote":     {RealPath: "/mnt/remote", PartialBytes: n(3), Error: "du: I/O error"},
		"Comerciais": {RealPath: "/srv/shares/Comerciais", Bytes: n(1), MeasuredAt: now},
	}
	s := j.snapshot()
	if s["total"] != int64(100+7+3+1) || s["total_complete"] != false || !s["oldest_at"].(*time.Time).Equal(old) {
		t.Fatalf("snapshot: total %v complete %v oldest %v", s["total"], s["total_complete"], s["oldest_at"])
	}
	if _, ok := s["sizes"].(map[string]int64)["Remote"]; ok {
		t.Fatal("a partial value in sizes")
	}
}

// Reading never scans; refresh starts one at once and is a no-op while it runs; the job stops with its context.
func TestSizeRefresh(t *testing.T) {
	var lists atomic.Int32
	release := make(chan struct{})
	j, _ := testSizes(t, []shares.Share{{Name: "A", Path: "/a"}}, func(string) (shares.SizeResult, error) {
		<-release
		return ok(1)
	})
	j.next = j.now().Add(time.Hour)
	inner := j.list
	j.list = func(ctx context.Context) ([]shares.Share, error) { lists.Add(1); return inner(ctx) }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { j.run(ctx); close(done) }()
	j.snapshot()
	time.Sleep(20 * time.Millisecond)
	if lists.Load() != 0 {
		t.Fatal("a read started a scan")
	}
	j.refresh()
	if sc := j.snapshot()["scanning"].(*sizeScan); sc.Kind != "manual" {
		t.Fatalf("scanning = %+v", sc)
	}
	j.refresh()
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for j.snapshot()["scanning"] != nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if lists.Load() != 1 || len(j.snapshot()["sizes"].(map[string]int64)) != 1 {
		t.Fatalf("lists %d, %v", lists.Load(), j.snapshot())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the job outlived its context")
	}
}

// A scan shows as running from the moment it is picked, also while the shares are being listed.
func TestSizeScanVisibleWhileListing(t *testing.T) {
	listing, release := make(chan struct{}), make(chan struct{})
	j, _ := testSizes(t, nil, func(string) (shares.SizeResult, error) { return ok(1) })
	j.list = func(context.Context) ([]shares.Share, error) {
		close(listing)
		<-release
		return []shares.Share{{Name: "A", Path: "/a"}}, nil
	}
	j.next = j.now() // due
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { j.run(ctx); close(done) }()
	<-listing
	if sc, _ := j.snapshot()["scanning"].(*sizeScan); sc == nil || sc.Kind != "full" {
		t.Fatalf("while listing: %v", j.snapshot()["scanning"])
	}
	close(release)
	// the scan ends (and writes its cache) before the test's folder goes
	for deadline := time.Now().Add(5 * time.Second); j.snapshot()["scanning"] != nil && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
}
