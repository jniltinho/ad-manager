package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"ad-manager/internal/shares"
)

// The share folder sizes: measured by a background job of the agent (never inside a request), one share at a time at
// idle priority, every 4 hours with jitter, cached in memory and in <state dir>/share-sizes.json.
const (
	sizeInterval     = 4 * time.Hour
	sizeShareTimeout = 30 * time.Minute
	sizeListTimeout  = 2 * time.Minute
	sizeListRetry    = 30 * time.Minute // a scan that could not list the shares tries again sooner
)

// sizeEntry is one share's measure: the last complete value kept apart from a later partial or failed attempt.
type sizeEntry struct {
	Path         string     `json:"path"`
	RealPath     string     `json:"real_path"`
	Bytes        *int64     `json:"bytes,omitempty"`
	MeasuredAt   *time.Time `json:"measured_at,omitempty"`
	PartialBytes *int64     `json:"partial_bytes,omitempty"`
	AttemptedAt  *time.Time `json:"attempted_at,omitempty"`
	Error        string     `json:"error"`
}

// sizeFile is the cache, as kept on disk.
type sizeFile struct {
	Version         int                   `json:"version"`
	FullScanEndedAt time.Time             `json:"full_scan_ended_at,omitzero"`
	Shares          map[string]*sizeEntry `json:"shares"`
}

// sizeScan is the scan running: kind full, short (after a share task) or manual (Refresh).
type sizeScan struct {
	Kind      string    `json:"kind"`
	StartedAt time.Time `json:"started_at"`
	Done      int       `json:"done"`
	Count     int       `json:"count"`
	Current   string    `json:"current"`
}

// sizeJob owns the cache and the single goroutine that runs every scan (so at most one du exists at any time).
type sizeJob struct {
	file    string
	list    func(ctx context.Context) ([]shares.Share, error)
	measure func(ctx context.Context, path string) (shares.SizeResult, error)
	resolve func(path string) (real string, link bool, err error)
	now     func() time.Time
	jitter  func() float64 // uniform in [0, 1)

	mu      sync.Mutex
	data    sizeFile
	scan    *sizeScan
	listErr string
	next    time.Time
	manual  bool
	short   []string
	wake    chan struct{}
}

func newSizeJob(file string, mgr *shares.Manager) *sizeJob {
	j := &sizeJob{file: file, list: mgr.List, measure: mgr.Size, resolve: resolvePath, now: time.Now, jitter: rand.Float64,
		wake: make(chan struct{}, 1)}
	j.load()
	return j
}

// load reads the disk cache (missing, unreadable or of another version: an empty cache) and sets the first full scan.
func (j *sizeJob) load() {
	j.data = sizeFile{Version: 1, Shares: map[string]*sizeEntry{}}
	if b, err := os.ReadFile(j.file); err == nil {
		var f sizeFile
		if json.Unmarshal(b, &f) == nil && f.Version == 1 && f.Shares != nil {
			j.data = f
		} else {
			slog.Warn("folder sizes: cache ignored", "file", j.file)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		slog.Warn("folder sizes: cache", "err", err)
	}
	j.next = firstDue(j.now(), j.data.FullScanEndedAt, j.jitter())
}

// resolvePath is path with its symlinks resolved, and whether path itself is a symlink.
func resolvePath(path string) (string, bool, error) {
	link := false
	if fi, err := os.Lstat(path); err == nil {
		link = fi.Mode()&os.ModeSymlink != 0
	}
	real, err := filepath.EvalSymlinks(path)
	return real, link, err
}

// firstDue: 1–5 minutes after the start when the last full scan is older than the interval (or none), else its end
// plus the interval with jitter.
func firstDue(now, ended time.Time, r float64) time.Time {
	if ended.IsZero() || now.Sub(ended) > sizeInterval {
		return now.Add(time.Minute + time.Duration(r*float64(4*time.Minute)))
	}
	return nextDue(ended, r)
}

// nextDue is end + 4 h ± 10%.
func nextDue(end time.Time, r float64) time.Time {
	return end.Add(time.Duration(float64(sizeInterval) * (0.9 + 0.2*r)))
}

// save writes the cache atomically (0600), from a copy made under the lock.
func (j *sizeJob) save(b []byte) {
	tmp, err := os.CreateTemp(filepath.Dir(j.file), ".share-sizes-*")
	if err == nil {
		_, err = tmp.Write(b)
		if err == nil {
			err = tmp.Sync()
		}
		if cerr := tmp.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Rename(tmp.Name(), j.file)
		}
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}
	if err != nil {
		slog.Warn("folder sizes: saving the cache", "err", err)
	}
}

// marshal is the cache as JSON (call with mu held).
func (j *sizeJob) marshal() []byte {
	b, _ := json.Marshal(j.data)
	return b
}

// refresh starts a full scan unless one is running (the running scan is the answer); it never waits for du.
func (j *sizeJob) refresh() {
	j.mu.Lock()
	if j.scan == nil && !j.manual {
		j.manual = true
		j.scan = &sizeScan{Kind: "manual", StartedAt: j.now()}
	}
	j.mu.Unlock()
	j.poke()
}

// measureShare queues a short scan of one share (after a share task), behind the running scan.
func (j *sizeJob) measureShare(name string) {
	j.mu.Lock()
	if !slices.Contains(j.short, name) {
		j.short = append(j.short, name)
	}
	j.mu.Unlock()
	j.poke()
}

func (j *sizeJob) poke() {
	select {
	case j.wake <- struct{}{}:
	default:
	}
}

// run is the job goroutine: until ctx ends, the due full scan, a manual one or the queued short ones.
func (j *sizeJob) run(ctx context.Context) {
	for {
		j.mu.Lock()
		kind, names, wait := "", []string(nil), j.next.Sub(j.now())
		switch {
		case j.manual:
			kind, j.manual = "manual", false // its scan was announced by refresh
		case len(j.short) > 0:
			kind, names, j.short = "short", j.short, nil
			j.scan = &sizeScan{Kind: kind, StartedAt: j.now()}
		case wait <= 0:
			kind = "full"
			j.scan = &sizeScan{Kind: kind, StartedAt: j.now()}
		}
		j.mu.Unlock()
		if kind != "" {
			j.scanOnce(ctx, kind, names)
			if ctx.Err() != nil {
				return
			}
			continue
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-j.wake:
		case <-t.C:
		}
		t.Stop()
	}
}

// scanOnce measures the shares (all of them, or names for a short scan), one at a time, saving after each.
func (j *sizeJob) scanOnce(ctx context.Context, kind string, names []string) {
	full := kind != "short"
	lctx, cancel := context.WithTimeout(ctx, sizeListTimeout)
	list, err := j.list(lctx)
	cancel()
	j.mu.Lock()
	if err != nil {
		j.listErr = "listing the shares: " + err.Error()
		j.scan = nil
		if full && ctx.Err() == nil {
			j.next = j.now().Add(sizeListRetry)
		}
		j.mu.Unlock()
		slog.Warn("folder sizes", "err", err)
		return
	}
	j.listErr = ""
	var todo []shares.Share
	for _, s := range list {
		if s.Path != "" && (full || slices.Contains(names, s.Name)) {
			todo = append(todo, s)
		}
	}
	if j.scan == nil || j.scan.Kind != kind { // run picks and announces it (scanOnce alone: tests)
		j.scan = &sizeScan{Kind: kind, StartedAt: j.now()}
	}
	j.scan.Count = len(todo)
	j.mu.Unlock()

	for i, s := range todo {
		var err error
		if ctx.Err() != nil {
			break
		}
		j.mu.Lock()
		j.scan.Current = s.Name
		j.mu.Unlock()
		// the folder's identity, resolved once and outside the lock (a hung mount never blocks the API): a share
		// path that is a symlink no longer resolving may point elsewhere now, so its old numbers go too
		real, link, rerr := j.resolve(s.Path)
		j.mu.Lock()
		e := j.data.Shares[s.Name]
		if e == nil || e.Path != s.Path || (rerr == nil && e.RealPath != "" && e.RealPath != real) || (rerr != nil && link) {
			e = &sizeEntry{Path: s.Path} // another folder: its old numbers do not belong to this one
			j.data.Shares[s.Name] = e
		}
		j.mu.Unlock()

		sctx, cancel := context.WithTimeout(ctx, sizeShareTimeout)
		var r shares.SizeResult
		if rerr == nil {
			r, err = j.measure(sctx, real)
		} else {
			err = rerr
		}
		timedOut := errors.Is(sctx.Err(), context.DeadlineExceeded) && ctx.Err() == nil
		cancel()

		j.mu.Lock()
		now := j.now()
		e.AttemptedAt = &now
		if r.RealPath != "" {
			e.RealPath = r.RealPath
		}
		switch {
		case err == nil:
			n := r.Bytes
			e.Bytes, e.MeasuredAt, e.PartialBytes, e.Error = &n, &now, nil, ""
		case r.Partial:
			n := r.Bytes
			e.PartialBytes, e.Error = &n, err.Error()
		case timedOut:
			e.Error = fmt.Sprintf("timed out after %s", sizeShareTimeout)
		case ctx.Err() != nil:
			e.Error = "cancelled"
		default:
			e.Error = err.Error()
		}
		j.scan.Done = i + 1
		b := j.marshal()
		j.mu.Unlock()
		if err != nil {
			slog.Warn("folder size", "share", s.Name, "err", e.Error)
		}
		j.save(b)
	}

	j.mu.Lock()
	if full && ctx.Err() == nil { // a full scan that ended: drop removed shares, schedule the next one
		keep := map[string]bool{}
		for _, s := range list {
			keep[s.Name] = s.Path != ""
		}
		for name := range j.data.Shares {
			if !keep[name] {
				delete(j.data.Shares, name)
			}
		}
		j.data.FullScanEndedAt = j.now()
		j.next = nextDue(j.data.FullScanEndedAt, j.jitter())
	}
	j.scan = nil
	b := j.marshal()
	j.mu.Unlock()
	j.save(b)
}

// within: path p is q or inside q (component-wise, on cleaned absolute paths).
func within(p, q string) bool {
	p, q = filepath.Clean(p), filepath.Clean(q)
	return p == q || q == "/" || strings.HasPrefix(p, q+"/")
}

// snapshot is the body of GET /api/sizes, read from memory under a short lock.
func (j *sizeJob) snapshot() map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	sizes := map[string]int64{}
	entries := make(map[string]sizeEntry, len(j.data.Shares))
	type valued struct {
		name, path string
		n          int64
	}
	var vals []valued
	complete := !j.data.FullScanEndedAt.IsZero()
	var oldest *time.Time
	for name, e := range j.data.Shares {
		entries[name] = *e
		if e.Bytes != nil {
			sizes[name] = *e.Bytes
		}
		if e.Bytes == nil || e.PartialBytes != nil || e.Error != "" {
			complete = false
		}
		if e.MeasuredAt != nil && (oldest == nil || e.MeasuredAt.Before(*oldest)) {
			oldest = e.MeasuredAt
		}
		p := e.RealPath
		if p == "" {
			p = e.Path
		}
		switch {
		case e.Bytes != nil:
			vals = append(vals, valued{name, p, *e.Bytes})
		case e.PartialBytes != nil:
			vals = append(vals, valued{name, p, *e.PartialBytes})
		}
	}
	// nested and aliased shares once: a share inside (or equal to) another share with a value is left out
	var total int64
	for _, a := range vals {
		inner := false
		for _, b := range vals {
			if a.name != b.name && within(a.path, b.path) && (filepath.Clean(a.path) != filepath.Clean(b.path) || b.name < a.name) {
				inner = true
				break
			}
		}
		if !inner {
			total += a.n
		}
	}
	body := map[string]any{"sizes": sizes, "at": j.data.FullScanEndedAt, "shares": entries, "total": total,
		"total_complete": complete, "oldest_at": oldest, "next_at": j.next}
	if j.scan != nil {
		sc := *j.scan
		body["scanning"] = &sc
	} else {
		body["scanning"] = nil
	}
	if j.listErr != "" {
		body["error"] = j.listErr
	}
	return body
}
