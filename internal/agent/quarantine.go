package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"ad-manager/internal/backup"
)

// Quarantine is what one run deleted or replaced in its destination, kept in <share root>/_sync/<name>
// (the same relative paths as in the destination) until the job's retention removes it. Its manifest, in the
// agent's own folder, records the job's generation and the destination of that run: files go back there, and a job
// made again with the same id never sees (nor removes) the quarantines of the one before.
type Quarantine struct {
	Name    string    `json:"name"` // <job>-<run>
	Job     string    `json:"job"`
	Gen     string    `json:"gen"`
	Run     string    `json:"run"`
	Part    string    `json:"part,omitempty"` // a server job's share: "-s<n>" after the run in the name
	Dest    string    `json:"dest"`
	Path    string    `json:"path"`
	Created time.Time `json:"created"`
	Files   int       `json:"files"` // listing only
	Size    int64     `json:"size"`  // listing only
}

var (
	quarantineRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}-\d{8}T\d{6}Z-[0-9a-f]{8}(-s\d{1,4})?$`)
	partRe       = regexp.MustCompile(`^(-s\d{1,4})?$`)
)

// defaultKeepDays is the retention of a job that sets none.
const defaultKeepDays = 30

var errQuarantineGone = errors.New("quarantine not found")

func (a *Agent) quarantineManifest(name string) string {
	return filepath.Join(a.syncDir(), "quarantine", name+".json")
}

func (a *Agent) writeQuarantine(q Quarantine) error {
	if err := os.MkdirAll(filepath.Dir(a.quarantineManifest(q.Name)), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(q, "", "  ")
	return writeFile0600(a.quarantineManifest(q.Name), b)
}

// quarantineOK tells whether a manifest names a quarantine folder of this agent: <share root>/_sync/<name> (or
// <share root>/_sincronizacao/<name>, the folder's old name).
func (a *Agent) quarantineOK(q Quarantine) bool {
	if !quarantineRe.MatchString(q.Name) || !partRe.MatchString(q.Part) || q.Name != q.Job+"-"+q.Run+q.Part {
		return false
	}
	for _, r := range a.cfg.ShareRoots {
		for _, d := range []string{SyncDir, LegacySyncDir} {
			if q.Path == filepath.Join(filepath.Clean(r), d, q.Name) {
				return true
			}
		}
	}
	return false
}

// quarantine reads one manifest.
func (a *Agent) quarantine(name string) (Quarantine, error) {
	var q Quarantine
	if !quarantineRe.MatchString(name) {
		return q, errQuarantineGone
	}
	b, err := os.ReadFile(a.quarantineManifest(name))
	if err != nil || json.Unmarshal(b, &q) != nil || q.Name != name || !a.quarantineOK(q) {
		return q, errQuarantineGone
	}
	return q, nil
}

// quarantines lists the quarantines kept (newest first) with their size: of job's generation gen, or (job "") of
// the current generation of every job.
func (a *Agent) quarantines(job, gen string) []Quarantine {
	gens := map[string]string{job: gen}
	if job == "" {
		list, _ := a.syncJobs()
		for _, j := range list {
			gens[j.ID] = j.Gen
		}
	}
	es, _ := os.ReadDir(filepath.Join(a.syncDir(), "quarantine"))
	var out []Quarantine
	for _, e := range es {
		q, err := a.quarantine(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil || gens[q.Job] != q.Gen || q.Gen == "" {
			continue
		}
		q.Files, q.Size = dirUsage(q.Path)
		out = append(out, q)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out
}

// dirUsage counts the files under dir and their size (links are not followed).
func dirUsage(dir string) (files int, size int64) {
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			files++
			size += fi.Size()
		}
		return nil
	})
	return files, size
}

// dropQuarantine removes a quarantine folder and its manifest. Its parent (_sync) is opened from / one component at a
// time without following links (a folder on the way swapped for a link fails), and the removal goes through that
// open folder: links inside are removed, never followed.
func (a *Agent) dropQuarantine(q Quarantine) error {
	if !a.quarantineOK(q) {
		return errQuarantineGone
	}
	pin, f, err := backup.OpenDir(filepath.Dir(q.Path), false)
	switch {
	case err == nil:
		defer f.Close()
		root, err := os.OpenRoot(pin)
		if err != nil {
			return err
		}
		defer root.Close()
		if err := root.RemoveAll(q.Name); err != nil {
			return err
		}
	case !errors.Is(err, fs.ErrNotExist):
		if _, serr := os.Lstat(filepath.Dir(q.Path)); !errors.Is(serr, fs.ErrNotExist) {
			return err
		}
	}
	return os.Remove(a.quarantineManifest(q.Name))
}

// activeRun is the run of job going on ("" none).
func (a *Agent) activeRun(job string) string {
	a.sync.mu.Lock()
	defer a.sync.mu.Unlock()
	return a.sync.active[job]
}

// pruneQuarantine applies the job's retention after a run: the quarantines of its generation older than keep_days,
// then the oldest beyond keep_gib, never the one of the run that just ended (current).
func (a *Agent) pruneQuarantine(j SyncJob, current string) (removed int, freed int64) {
	a.sync.qmu.Lock()
	defer a.sync.qmu.Unlock()
	days := j.KeepDays
	if days == 0 {
		days = defaultKeepDays
	}
	cut := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	limit := int64(j.KeepGiB) << 30
	var kept int64
	for _, q := range a.quarantines(j.ID, j.Gen) { // newest first: the newest stay within the size
		if q.Run == current {
			kept += q.Size
			continue
		}
		if !q.Created.Before(cut) && (limit == 0 || kept+q.Size <= limit) {
			kept += q.Size
			continue
		}
		if err := a.dropQuarantine(q); err != nil {
			slog.Warn("sync.quarantine.prune", "job", j.ID, "quarantine", q.Name, "err", err)
			continue
		}
		removed++
		freed += q.Size
		slog.Info("sync.quarantine.prune", "job", j.ID, "quarantine", q.Name, "files", q.Files, "size", q.Size)
	}
	return removed, freed
}

// closeQuarantine ends a run's quarantine: removed when the run moved nothing there, then the job's retention.
func (a *Agent) closeQuarantine(j SyncJob, r *SyncRun) {
	if r.Quarantine == "" {
		return
	}
	if q, err := a.quarantine(j.ID + "-" + r.ID); err == nil {
		if n, _ := dirUsage(q.Path); n == 0 {
			a.sync.qmu.Lock()
			_ = a.dropQuarantine(q)
			a.sync.qmu.Unlock()
			r.Quarantine = ""
		}
	}
	if n, freed := a.pruneQuarantine(j, r.ID); n > 0 {
		r.Notes = append(r.Notes, fmt.Sprintf("quarantine retention: %d old quarantines removed (%s)", n, humanBytes(freed)))
	}
	if sp, ok := diskSpace(j.Dest); ok && sp.low() {
		r.Notes = append(r.Notes, fmt.Sprintf("little free space on the destination's disk: %s of %s", humanBytes(sp.Free), humanBytes(sp.Total)))
	}
}

// Space is the free and total space of a share root's disk.
type Space struct {
	Root  string `json:"root"`
	Free  int64  `json:"free"`
	Total int64  `json:"total"`
	Low   bool   `json:"low"`
}

// low: under 10% or 5 GiB free.
func (s Space) low() bool { return s.Total > 0 && (s.Free < s.Total/10 || s.Free < 5<<30) }

func (a *Agent) spaces() []Space {
	var out []Space
	for _, r := range a.cfg.ShareRoots {
		if sp, ok := diskSpace(r); ok {
			sp.Root, sp.Low = filepath.Clean(r), sp.low()
			out = append(out, sp)
		}
	}
	return out
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	default:
		return fmt.Sprintf("%d KiB", (n+1023)/1024)
	}
}

// cleanRel accepts a relative path inside a quarantine: no .., no empty or . component, not absolute.
func cleanRel(rel string) (string, error) {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.ContainsRune(rel, 0) || filepath.Clean(rel) != rel {
		return "", fmt.Errorf("invalid path %q", rel)
	}
	for _, c := range strings.Split(rel, "/") {
		if c == "" || c == "." || c == ".." {
			return "", fmt.Errorf("invalid path %q", rel)
		}
	}
	return rel, nil
}

// QFile is one file of a quarantine.
type QFile struct {
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	InDest   bool      `json:"in_dest"` // a file with this path is in the destination now (giving back asks to replace)
}

// quarantineFiles lists the files of a quarantine (at most max; truncated says there are more).
func quarantineFiles(q Quarantine, max int) (out []QFile, truncated bool) {
	_ = filepath.WalkDir(q.Path, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		if len(out) >= max {
			truncated = true
			return filepath.SkipAll
		}
		rel, err := filepath.Rel(q.Path, p)
		fi, ierr := d.Info()
		if err != nil || ierr != nil {
			return nil
		}
		_, derr := os.Lstat(filepath.Join(q.Dest, rel))
		out = append(out, QFile{Path: rel, Size: fi.Size(), Modified: fi.ModTime().UTC(), InDest: derr == nil})
		return nil
	})
	return out, truncated
}

// RestoreResult is what giving files back did.
type RestoreResult struct {
	Restored  int      `json:"restored"`
	Swapped   int      `json:"swapped"`   // the destination's file went into the quarantine in its place
	Conflicts []string `json:"conflicts"` // in the destination already, given back only with replace (first 200)
	Conflict  int      `json:"conflict_count"`
	Errors    []string `json:"errors"` // first 200
	Failed    int      `json:"error_count"`
}

// giveBack moves files of a quarantine back to their place in the run's destination (all of them when paths is
// empty). A file there already is a conflict, or with replace swaps places with the one given back.
func (a *Agent) giveBack(q Quarantine, paths []string, replace bool) (RestoreResult, error) {
	res := RestoreResult{Conflicts: []string{}, Errors: []string{}}
	if err := a.validateDest(q.Dest); err != nil {
		return res, err
	}
	if len(paths) > 100000 {
		return res, errors.New("too many files")
	}
	a.sync.qmu.Lock()
	defer a.sync.qmu.Unlock()
	if a.activeRun(q.Job) == q.Run {
		return res, errors.New("this run is still going on")
	}
	if len(paths) == 0 { // all: every file of the tree, however many
		_ = filepath.WalkDir(q.Path, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.Type().IsRegular() {
				if rel, err := filepath.Rel(q.Path, p); err == nil {
					paths = append(paths, rel)
				}
			}
			return nil
		})
	}
	if err := a.lockTree(q.Dest, "quarantine "+q.Name); err != nil {
		return res, err
	}
	defer a.unlockTree(q.Dest)
	qf, err := openFolder(q.Path)
	if err != nil {
		return res, err
	}
	defer qf.Close()
	df, err := openFolder(q.Dest)
	if err != nil {
		return res, err
	}
	defer df.Close()
	for _, p := range paths {
		rel, err := cleanRel(filepath.ToSlash(p))
		if err == nil {
			var swapped bool
			swapped, err = restoreFile(qf, df, rel, replace)
			switch {
			case errors.Is(err, errInDest):
				if res.Conflict++; len(res.Conflicts) < 200 {
					res.Conflicts = append(res.Conflicts, rel)
				}
				continue
			case err == nil && swapped:
				res.Swapped++
			case err == nil:
				res.Restored++
			}
		}
		if err != nil {
			if res.Failed++; len(res.Errors) < 200 {
				res.Errors = append(res.Errors, p+": "+err.Error())
			}
		}
	}
	slog.Info("sync.quarantine.restore", "quarantine", q.Name, "dest", q.Dest, "restored", res.Restored,
		"swapped", res.Swapped, "conflicts", res.Conflict, "errors", res.Failed)
	return res, nil
}

var errInDest = errors.New("a file with this name is in the destination")
