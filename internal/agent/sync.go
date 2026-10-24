package agent

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ad-manager/internal/backup"
	"ad-manager/internal/sambabridge"
	"ad-manager/internal/secretbox"
	"ad-manager/internal/shares"
	"ad-manager/internal/syncer"

	"github.com/hibiken/asynq"
	"github.com/labstack/echo/v5"
	"github.com/robfig/cron/v3"
)

// TypeSync is a sync preview or run, on the agent's sync queue (its own worker: a long sync does not hold the
// backups and folder tasks back).
const TypeSync = "sync:run"

// SyncDir is the folder, inside a share root, of the quarantines (deleted and replaced files of each run).
const SyncDir = "_sync"

// LegacySyncDir is SyncDir's old name: quarantines already kept there are still listed, given back and protected.
const LegacySyncDir = "_sincronizacao"

// SyncJob is a folder brought from an SMB share into this server, kept by the agent.
type SyncJob struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Kind         string        `json:"kind,omitempty"` // "" (a folder) or "server": every share of the source server
	Gen          string        `json:"gen"`            // set when the job is created: a job deleted and made again is another one
	Rev          int           `json:"rev"`            // grows on every save: a queued run of an older revision is dropped
	Source       syncer.Source `json:"source"`
	PasswordFile string        `json:"password_file,omitempty"` // ntlm: a 0600 file of root with the password
	Dest         string        `json:"dest"`
	Mode         string        `json:"mode"`
	Exclude      []string      `json:"exclude,omitempty"`
	BwLimit      string        `json:"bwlimit,omitempty"`
	Transfers    int           `json:"transfers,omitempty"`
	MaxDelete    int           `json:"max_delete"`
	MaxDeletePct int           `json:"max_delete_pct"`
	Verify       bool          `json:"verify,omitempty"`
	Reinherit    bool          `json:"reinherit"`           // apply the destination's Windows ACL to what came in
	KeepDays     int           `json:"keep_days,omitempty"` // quarantine retention in days (0: 30)
	KeepGiB      int           `json:"keep_gib,omitempty"`  // and in GiB, the newest kept (0: no size limit)
	Cron         string        `json:"cron,omitempty"`
	Enabled      bool          `json:"enabled"`
	Approved     int           `json:"approved_rev,omitempty"` // mirror: the revision a clean preview approved
	PasswordSet  bool          `json:"password_set,omitempty"` // listing only: a sealed password is kept for it
}

// SyncRun is one preview or run.
type SyncRun struct {
	ID         string         `json:"id"`
	Job        string         `json:"job"`
	Rev        int            `json:"rev"`
	Preview    bool           `json:"preview"`
	Origin     string         `json:"origin"` // manual or schedule
	By         string         `json:"by,omitempty"`
	Started    time.Time      `json:"started"`
	Finished   time.Time      `json:"finished,omitzero"`
	Status     string         `json:"status"` // queued, running, ok, error, cancelled
	Error      string         `json:"error,omitempty"`
	Result     *syncer.Result `json:"result,omitempty"`
	Progress   map[string]any `json:"progress,omitempty"`
	Confined   bool           `json:"confined"` // ran in a systemd unit limited to the destination and quarantine
	Notes      []string       `json:"notes,omitempty"`
	Quarantine string         `json:"quarantine,omitempty"`
}

type syncTask struct {
	Job     string `json:"job"`
	Gen     string `json:"gen"`
	Rev     int    `json:"rev"`
	Run     string `json:"run"`
	Preview bool   `json:"preview"`
	Origin  string `json:"origin"`
	By      string `json:"by,omitempty"`
}

var syncIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// syncState holds what the agent keeps for the syncs: the jobs file, the runs and the running ones.
type syncState struct {
	mu      sync.Mutex
	running map[string]context.CancelFunc // run id → cancel
	trees   map[string]string             // destination being written → run id
	sched   []string                      // scheduler entries of the sync jobs
	active  map[string]string             // job → its run going on (preview or run)
	qmu     sync.Mutex                    // quarantines: made, pruned, given back and emptied one at a time
}

func (a *Agent) syncDir() string { return filepath.Join(a.backups.Dir, "sync") }

func (a *Agent) syncJobs() ([]SyncJob, error) {
	b, err := os.ReadFile(filepath.Join(a.syncDir(), "jobs.json"))
	if errors.Is(err, os.ErrNotExist) {
		return []SyncJob{}, nil
	}
	if err != nil {
		return nil, err
	}
	list := []SyncJob{}
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, err
	}
	if list == nil { // a saved "null"
		list = []SyncJob{}
	}
	return list, nil
}

func (a *Agent) saveSyncJobs(list []SyncJob) error {
	if err := os.MkdirAll(a.syncDir(), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(list, "", "  ")
	return writeFile0600(filepath.Join(a.syncDir(), "jobs.json"), b)
}

func (a *Agent) syncJob(id string) (SyncJob, error) {
	list, err := a.syncJobs()
	if err != nil {
		return SyncJob{}, err
	}
	for _, j := range list {
		if j.ID == id {
			return j, nil
		}
	}
	return SyncJob{}, errors.New("sync job not found")
}

func (a *Agent) runFile(id string) string { return filepath.Join(a.syncDir(), "runs", id+".json") }

func (a *Agent) writeRun(r SyncRun) {
	_ = os.MkdirAll(filepath.Dir(a.runFile(r.ID)), 0o700)
	b, _ := json.MarshalIndent(r, "", "  ")
	_ = writeFile0600(a.runFile(r.ID), b)
}

// syncRuns are the runs kept (newest first, at most 200 listed; older ones are removed).
func (a *Agent) syncRuns() []SyncRun {
	es, _ := os.ReadDir(filepath.Join(a.syncDir(), "runs"))
	out := []SyncRun{} // never null in the JSON: the screens read .length
	for _, e := range es {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(a.syncDir(), "runs", e.Name()))
		var r SyncRun
		if err == nil && json.Unmarshal(b, &r) == nil {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started.After(out[j].Started) })
	for _, old := range out[min(len(out), 200):] {
		_ = os.Remove(a.runFile(old.ID))
		_ = os.Remove(strings.TrimSuffix(a.runFile(old.ID), ".json") + ".log")
	}
	return out[:min(len(out), 200)]
}

// validateSync checks a job against this server: a file server, the destination a real folder strictly inside a
// share root (not the restorations' or quarantines' folder, no link on the way, no file system mounted inside), a
// valid source, an NTLM password file of root that nobody else reads, and sane limits.
func (a *Agent) validateSync(j *SyncJob) error {
	if a.readOnly() {
		return errors.New("folders are synced into a file server")
	}
	if !syncIDRe.MatchString(j.ID) {
		return fmt.Errorf("sync id %q: lowercase letters, digits and -", j.ID)
	}
	if len(j.Name) > 80 {
		return errors.New("name too long")
	}
	if j.Mode == "" {
		j.Mode = syncer.ModeCopy
	}
	switch j.Kind {
	case "", "folder":
		j.Kind = ""
		if err := syncer.CheckSource(j.Source); err != nil {
			return err
		}
		if j.Mode != syncer.ModeCopy && j.Mode != syncer.ModeMirror {
			return fmt.Errorf("unknown mode %q", j.Mode)
		}
		j.Dest = filepath.Clean(j.Dest)
		if err := a.validateDest(j.Dest); err != nil {
			return err
		}
	case "server": // every share of the source, each into the folder of the share of the same name here
		if j.Source.Share != "" || j.Source.Path != "" {
			return errors.New("a server job names the server alone (no share, no path)")
		}
		if err := syncer.CheckBrowse(j.Source); err != nil {
			return err
		}
		if j.Mode != syncer.ModeCopy {
			return errors.New("a server job copies only (it never deletes)")
		}
		j.Dest = ""
	default:
		return fmt.Errorf("unknown kind %q", j.Kind)
	}
	if j.Source.Auth == "ntlm" {
		if j.PasswordFile != "" { // else the password typed in the wizard, kept sealed here
			if err := checkSecretFile(j.PasswordFile); err != nil {
				return err
			}
		}
	} else {
		j.PasswordFile = ""
	}
	if len(j.Exclude) > 100 || j.Transfers < 0 || j.Transfers > 16 || j.MaxDelete < 0 || j.MaxDeletePct < 0 || j.MaxDeletePct > 100 {
		return errors.New("invalid limits")
	}
	for _, x := range j.Exclude {
		if x == "" || len(x) > 200 || strings.ContainsAny(x, "\n\r") {
			return fmt.Errorf("invalid exclude %q", x)
		}
	}
	if len(j.BwLimit) > 100 {
		return errors.New("invalid bandwidth limit")
	}
	if j.KeepDays < 0 || j.KeepDays > 3650 || j.KeepGiB < 0 || j.KeepGiB > 1<<20 {
		return errors.New("invalid quarantine retention")
	}
	return nil
}

// validateDest checks a sync destination: a real folder strictly inside a share root (not the root, not the
// restorations' or quarantines' folder), no link on the way, no file system mounted inside.
func (a *Agent) validateDest(dest string) error {
	if !a.restorable(dest) || slices.ContainsFunc(a.cfg.ShareRoots, func(r string) bool {
		r = filepath.Clean(r)
		if dest == r {
			return true
		}
		for _, d := range []string{SyncDir, LegacySyncDir} {
			if d = filepath.Join(r, d); dest == d || strings.HasPrefix(dest, d+"/") {
				return true
			}
		}
		return false
	}) {
		return fmt.Errorf("%q must be a folder inside a share root (not the root itself)", dest)
	}
	if mp := backup.MountsUnder([]string{dest}); mp != "" {
		return fmt.Errorf("%s is a mount point inside the destination", mp)
	}
	return nil
}

// checkSecretFile accepts an absolute path to a regular file of root, readable by root only.
func checkSecretFile(p string) error {
	if !filepath.IsAbs(p) {
		return errors.New("ntlm: the password file must be an absolute path on the server")
	}
	fi, err := os.Lstat(p)
	if err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("ntlm: %s is not a file on the server", p)
	}
	if fi.Mode().Perm()&0o077 != 0 || !ownedByMe(fi) {
		return fmt.Errorf("ntlm: %s must belong to root with mode 0600", p)
	}
	return nil
}

// lockTree marks dest as being written by run; a destination inside or around one being written is refused.
func (a *Agent) lockTree(dest, run string) error {
	a.sync.mu.Lock()
	defer a.sync.mu.Unlock()
	for d, r := range a.sync.trees {
		if d == dest || strings.HasPrefix(d, dest+"/") || strings.HasPrefix(dest, d+"/") {
			return fmt.Errorf("%s is being written by the run %s", d, r)
		}
	}
	a.sync.trees[dest] = run
	return nil
}

func (a *Agent) unlockTree(dest string) {
	a.sync.mu.Lock()
	delete(a.sync.trees, dest)
	a.sync.mu.Unlock()
}

func newRunID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b)
}

var runIDRe = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{8}$`)

// enqueueSync queues a preview or run of job j.
func (a *Agent) enqueueSync(ctx context.Context, j SyncJob, preview bool, origin, by string) (SyncRun, error) {
	r := SyncRun{ID: newRunID(), Job: j.ID, Rev: j.Rev, Preview: preview, Origin: origin, By: by, Started: time.Now().UTC(), Status: "queued"}
	payload, _ := json.Marshal(syncTask{Job: j.ID, Gen: j.Gen, Rev: j.Rev, Run: r.ID, Preview: preview, Origin: origin, By: by})
	_, err := a.client.EnqueueContext(ctx, asynq.NewTask(TypeSync, payload), asynq.Queue(a.syncQueue()),
		asynq.MaxRetry(0), asynq.Timeout(24*time.Hour), asynq.Retention(7*24*time.Hour))
	if err != nil {
		return r, fmt.Errorf("queue unavailable: %w", err)
	}
	a.writeRun(r)
	return r, nil
}

func (a *Agent) syncQueue() string { return a.cfg.Queue + "-sync" }

// registerSyncs replaces the scheduler's sync entries with the enabled jobs that have a cron.
func (a *Agent) registerSyncs(list []SyncJob) error {
	a.muSched.Lock()
	defer a.muSched.Unlock()
	var ids []string
	for _, j := range list {
		if !j.Enabled || j.Cron == "" {
			continue
		}
		payload, _ := json.Marshal(syncTask{Job: j.ID, Gen: j.Gen, Rev: j.Rev, Origin: "schedule"})
		id, err := a.scheduler.Register(j.Cron, asynq.NewTask(TypeSync, payload), asynq.Queue(a.syncQueue()),
			asynq.MaxRetry(0), asynq.Timeout(24*time.Hour), asynq.Retention(7*24*time.Hour))
		if err != nil {
			for _, x := range ids {
				_ = a.scheduler.Unregister(x)
			}
			return fmt.Errorf("sync %s: %w", j.ID, err)
		}
		ids = append(ids, id)
	}
	for _, x := range a.sync.sched {
		_ = a.scheduler.Unregister(x)
	}
	a.sync.sched = ids
	return nil
}

// handleSync runs one preview or run (TypeSync).
func (a *Agent) handleSync(ctx context.Context, t *asynq.Task) error {
	var tk syncTask
	if err := json.Unmarshal(t.Payload(), &tk); err != nil {
		return fmt.Errorf("%w: %w", err, asynq.SkipRetry)
	}
	j, err := a.syncJob(tk.Job)
	if err == nil && (tk.Rev != j.Rev || tk.Gen != j.Gen) {
		err = fmt.Errorf("the job changed since it was queued (revision %d, now %d)", tk.Rev, j.Rev)
	}
	if err == nil {
		err = a.validateSync(&j)
	}
	if tk.Run == "" { // a schedule: a run of its own
		tk.Run, tk.Preview = newRunID(), false
	}
	r := SyncRun{ID: tk.Run, Job: tk.Job, Rev: tk.Rev, Preview: tk.Preview, Origin: tk.Origin, By: tk.By, Started: time.Now().UTC(), Status: "running"}
	if prev, rerr := os.ReadFile(a.runFile(r.ID)); rerr == nil {
		var old SyncRun
		if json.Unmarshal(prev, &old) == nil && old.Status != "queued" {
			return nil // delivered again: it ran already
		}
	}
	if err == nil && !tk.Preview && j.Mode == syncer.ModeMirror && tk.Origin != "schedule" && j.Approved != j.Rev {
		err = errors.New("preview this mirror (its current revision) before running it")
	}
	if err != nil {
		r.Status, r.Error, r.Finished = "error", err.Error(), time.Now().UTC()
		a.writeRun(r)
		slog.Info("task", "type", TypeSync, "job", tk.Job, "run", r.ID, "err", err)
		return fmt.Errorf("%w: %w", err, asynq.SkipRetry)
	}
	r.Progress = map[string]any{"phase": "preparing"} // the panel shows the phase while the worker starts up
	a.writeRun(r)
	rctx, cancel := context.WithCancel(ctx)
	a.sync.mu.Lock()
	a.sync.running[r.ID] = cancel
	a.sync.active[j.ID] = r.ID
	a.sync.mu.Unlock()
	defer func() {
		a.sync.mu.Lock()
		delete(a.sync.running, r.ID)
		delete(a.sync.active, j.ID)
		a.sync.mu.Unlock()
		cancel()
	}()
	var res *syncer.Result
	if j.Kind == "server" {
		res, err = a.syncServer(rctx, j, &r)
	} else {
		res, err = a.syncTree(rctx, j, &r, "")
	}
	r.Result = res
	if !tk.Preview && j.Kind != "server" { // a server job closed each share's quarantine as it went
		r.Progress = map[string]any{"phase": "finalizing"} // retention cleanup of the job's old quarantines
		a.writeRun(r)
		a.closeQuarantine(j, &r)
	}
	r.Progress = nil // a finished run shows its summary, not a phase
	r.Finished = time.Now().UTC()
	switch {
	case err != nil && rctx.Err() != nil:
		r.Status, r.Error = "cancelled", "cancelled"
	case err != nil:
		r.Status, r.Error = "error", syncer.WithHint(err).Error()
	default:
		r.Status = "ok"
	}
	if r.Status == "ok" && tk.Preview && j.Mode == syncer.ModeMirror {
		a.approve(j.ID, j.Gen, j.Rev)
	}
	a.writeRun(r)
	slog.Info("task", "type", TypeSync, "job", j.ID, "run", r.ID, "preview", tk.Preview, "status", r.Status, "err", r.Error)
	return nil // the run file records the outcome; a sync is never retried by the queue
}

// syncTree brings j's source into j.Dest for run r (part, for a server job's share, suffixes its worker unit, run
// folder and quarantine): the destination locked, Windows inheritance marked pending, the worker, the inheritance.
func (a *Agent) syncTree(rctx context.Context, j SyncJob, r *SyncRun, part string) (*syncer.Result, error) {
	if err := a.lockTree(j.Dest, r.ID); err != nil {
		return nil, err
	}
	defer a.unlockTree(j.Dest)
	// Windows inheritance is pending from before it starts until it is applied: a run that fails, is cancelled or
	// dies after rclone wrote files, or whose ACL pass fails, leaves the mark, and the next run redoes the whole tree
	aclPending := false
	if !r.Preview && j.Reinherit {
		_, perr := os.Stat(a.aclPendingFile(j.ID, j.Dest))
		// a mark that cannot be looked up counts as pending: rewriting it below must not forget an earlier one
		aclPending = !errors.Is(perr, os.ErrNotExist) || a.legacyACLPending(j.ID, r.ID)
		if err := a.markACLPending(j.ID, j.Dest); err != nil {
			return nil, errors.New("could not mark the Windows inheritance pending: " + err.Error())
		}
	}
	res, notes, err := a.runSyncWorker(rctx, j, r, part)
	for _, n := range notes { // once per run, not once per share of a server job
		if !slices.Contains(r.Notes, n) {
			r.Notes = append(r.Notes, n)
		}
	}
	// the transferred paths are only for the reinherit decision below; take them out of the result so they are never
	// kept in the run record (200 runs of thousands of paths would blow past the /syncs size limit)
	var changed []string
	changedCapped := false
	if res != nil {
		changed, changedCapped = res.Changed, res.ChangedCapped
		res.Changed, res.ChangedCapped = nil, false
	}
	// reapply Windows inheritance while the run is still "running" and not finished, so the panel keeps polling and
	// its Cancel stays live: only to what rclone transferred (the whole tree only on a first or capped run, its list
	// empty or partial), under rctx so Stop interrupts the helper walk. A reinherit failure is best-effort (a note),
	// not a failed run; a cancellation marks the run cancelled.
	aclDone := false
	doACL, full := aclPlan(aclPending, res, changed, changedCapped)
	if err == nil && !r.Preview && j.Reinherit && doACL {
		if aclPending {
			r.Notes = append(r.Notes, "an earlier run left the Windows inheritance pending: reapplying it to the whole folder")
		}
		r.Progress = map[string]any{"phase": "acl", "acl_items": len(changed), "acl_full": full}
		if part != "" {
			r.Progress["share"] = j.Source.Share
		}
		a.writeRun(*r)
		var acl *sambabridge.ACL
		var aerr error
		if full {
			acl, aerr = a.mgr.Reinherit(rctx, j.Dest)
		} else if acl, aerr = a.mgr.ReinheritPaths(rctx, j.Dest, changed); aerr != nil && rctx.Err() == nil {
			// the incremental pass failed for a non-cancel reason (e.g. the request was too large): the whole tree is
			// always correct, so fall back to it rather than leave the new files without inheritance
			r.Notes = append(r.Notes, "incremental Windows inheritance failed ("+aerr.Error()+"), reapplying to the whole folder")
			acl, aerr = a.mgr.Reinherit(rctx, j.Dest)
		}
		r.Progress = nil
		switch {
		case aerr != nil && rctx.Err() != nil:
			err = aerr // falls through to the cancelled branch below
		case aerr != nil:
			r.Notes = append(r.Notes, "Windows inheritance not applied: "+aerr.Error())
		default:
			aclDone = true
			r.Notes = append(r.Notes, fmt.Sprintf("Windows inheritance of %s applied to %d items", j.Dest, acl.Propagated))
		}
	}
	if !r.Preview && j.Reinherit && err == nil && (aclDone || (res != nil && !doACL)) {
		_ = os.Remove(a.aclPendingFile(j.ID, j.Dest)) // applied, or nothing came in and nothing was pending
	}
	return res, err
}

// aclPlan says whether a finished copy needs Windows inheritance work and whether over the whole tree: only what rclone
// transferred, the whole tree on a first or capped run (its list empty or partial) or while an earlier run left it
// pending (then even when nothing came in).
func aclPlan(pending bool, res *syncer.Result, changed []string, capped bool) (do, full bool) {
	if res == nil {
		return false, false
	}
	return pending || res.Transfers > 0, pending || capped || len(changed) == 0
}

// aclPendingFile marks job's Windows inheritance pending on dest (one per destination: a run on an old destination
// never clears the mark of the new one).
func (a *Agent) aclPendingFile(job, dest string) string {
	sum := sha256.Sum256([]byte(dest))
	return filepath.Join(a.syncDir(), "acl-pending", job+"."+hex.EncodeToString(sum[:8]))
}

func (a *Agent) markACLPending(job, dest string) error {
	if err := os.MkdirAll(filepath.Join(a.syncDir(), "acl-pending"), 0o700); err != nil {
		return err
	}
	return writeFile0600(a.aclPendingFile(job, dest), []byte(dest+"\n"))
}

// legacyACLPending tells from the job's history whether a run made before the pending mark existed left the Windows
// inheritance unapplied: the newest run that noted its inheritance, or copied files, decides.
func (a *Agent) legacyACLPending(job, current string) bool {
	for _, r := range a.syncRuns() {
		if r.Job != job || r.ID == current || r.Preview || r.Finished.IsZero() {
			continue
		}
		for _, n := range r.Notes {
			switch {
			case strings.HasPrefix(n, "Windows inheritance not applied"):
				return true
			case strings.HasPrefix(n, "Windows inheritance of "):
				return false
			}
		}
		switch {
		case r.Status != "ok" && (r.Result == nil || r.Result.Transfers > 0):
			return true // copied (or may have: a worker killed before its result), then failed or was cancelled
		case r.Result != nil && r.Result.Transfers > 0:
			return false
		}
	}
	return false
}

// approve records that a clean preview approved revision rev of a mirror.
func (a *Agent) approve(id, gen string, rev int) {
	a.muPut.Lock()
	defer a.muPut.Unlock()
	list, err := a.syncJobs()
	if err != nil {
		return
	}
	for i := range list {
		if list[i].ID == id && list[i].Gen == gen && list[i].Rev == rev {
			list[i].Approved = rev
		}
	}
	_ = a.saveSyncJobs(list)
}

// workerMsg is one line the sync worker writes.
type workerMsg struct {
	Progress map[string]any `json:"progress,omitempty"`
	Result   *syncer.Result `json:"result,omitempty"`
	Error    string         `json:"error,omitempty"`
}

// runSyncWorker prepares the credentials and the quarantine, then runs `ad-manager agent sync-worker` with the
// spec on its standard input: as a transient systemd unit that can write only the destination and the
// quarantine when the agent runs under systemd, as a plain child otherwise.
func (a *Agent) runSyncWorker(ctx context.Context, j SyncJob, r *SyncRun, part string) (*syncer.Result, []string, error) {
	var notes []string
	sp := syncer.Spec{Source: j.Source, Dest: j.Dest, Mode: j.Mode, Exclude: j.Exclude, MaxDelete: j.MaxDelete,
		MaxDelPct: j.MaxDeletePct, BwLimit: j.BwLimit, Transfers: j.Transfers, Verify: j.Verify, Preview: r.Preview}
	id := r.ID + part // a server job's share: its own run folder, unit and quarantine
	runDir := filepath.Join("/run/ad-manager-sync", id)
	env := []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=" + runDir, "LANG=C.UTF-8"}
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return nil, notes, err
	}
	defer os.RemoveAll(runDir)
	pw, err := a.syncPassword(j)
	if err != nil {
		return nil, notes, err
	}
	if sp.Password, sp.CCache, err = a.credentials(ctx, j.Source, pw, runDir, &env); err != nil {
		return nil, notes, err
	}
	// destination and quarantine: opened folder by folder without following links; systemd gets their pinned /proc
	// paths, so a folder renamed or swapped for a link meanwhile cannot widen what the unit may write (the worker
	// itself uses the plain paths: one that no longer leads there is read-only to it)
	destPin, destF, err := backup.OpenDir(j.Dest, true)
	if err != nil {
		return nil, notes, err
	}
	defer destF.Close()
	root := ""
	for _, rr := range a.cfg.ShareRoots {
		if rr = filepath.Clean(rr); strings.HasPrefix(j.Dest, rr+"/") {
			root = rr
		}
	}
	rw := []string{destPin}
	sp.DestID = fileID(destF)
	if !r.Preview {
		qName := j.ID + "-" + id
		a.sync.qmu.Lock() // not while the job's quarantine is being emptied
		qPin, qf, err := backup.MakeOwnDir(root, SyncDir, qName)
		if err == nil { // the manifest at once: a quarantine nobody records would never leave
			if err = a.writeQuarantine(Quarantine{Name: qName, Job: j.ID, Gen: j.Gen, Run: r.ID, Part: part, Dest: j.Dest,
				Path: filepath.Join(root, SyncDir, qName), Created: time.Now().UTC()}); err != nil {
				qf.Close()
			}
		}
		a.sync.qmu.Unlock()
		if err != nil {
			return nil, notes, err
		}
		defer qf.Close()
		if !sameDevice(destF, qf) { // a quarantine move must be a rename, never a copy onto another disk
			return nil, notes, fmt.Errorf("%s is on another file system than %s: the quarantine would fill it", j.Dest, filepath.Join(root, SyncDir))
		}
		sp.Quarantine, sp.QuarantineID = filepath.Join(root, SyncDir, qName), fileID(qf)
		r.Quarantine = sp.Quarantine
		rw = append(rw, qPin)
	}
	confined := os.Getenv("INVOCATION_ID") != ""
	sp.Confined = confined
	in, _ := json.Marshal(sp)
	exe, err := os.Executable()
	if err != nil {
		return nil, notes, err
	}
	var cmd *exec.Cmd
	unit := "ad-manager-sync-" + id
	switch {
	case confined:
		args := []string{"--wait", "--collect", "--quiet", "--pipe", "--unit=" + unit,
			"-p", "ProtectSystem=strict", "-p", "ProtectHome=yes", "-p", "PrivateTmp=yes", "-p", "PrivateDevices=yes",
			"-p", "NoNewPrivileges=yes", "-p", "ProtectKernelTunables=yes", "-p", "ProtectKernelModules=yes",
			"-p", "ProtectControlGroups=yes", "-p", "RestrictSUIDSGID=yes", "-p", "UMask=0077",
			"-p", "ProtectKernelLogs=yes", "-p", "ProtectClock=yes", "-p", "ProtectHostname=yes", "-p", "ReadOnlyPaths=/run",
			// fewer capabilities than any other root process: /proc/<pid>/root of the others (and so their view of
			// the file system) is closed to it; ProtectProc hides what it may not trace
			"-p", "CapabilityBoundingSet=CAP_DAC_OVERRIDE CAP_DAC_READ_SEARCH CAP_FOWNER CAP_CHOWN",
			"-p", "ProtectProc=invisible", "-p", "ProcSubset=pid",
			"-p", "InaccessiblePaths=-/var/lib/samba/private", "-p", "MemoryMax=2G", "-p", "RuntimeMaxSec=23h"}
		if a.privateDir != "" && a.privateDir != "/var/lib/samba/private" { // a DC's private dir where it really is
			args = append(args, "-p", "InaccessiblePaths=-"+a.privateDir)
		}
		for _, p := range rw {
			args = append(args, "-p", "ReadWritePaths="+p)
		}
		for _, e := range env { // the unit does not inherit this process's environment
			args = append(args, "--setenv="+e)
		}
		args = append(args, "--", exe, "agent", "sync-worker")
		cmd = exec.Command("systemd-run", args...)
		r.Confined = true
	case a.cfg.SyncUnconfined:
		cmd = exec.Command(exe, "agent", "sync-worker")
		notes = append(notes, "not confined: the agent does not run under systemd (sync_unconfined)")
	default:
		return nil, notes, errors.New("the agent does not run under systemd: a sync runs only confined (sync_unconfined = true allows it, e.g. in a container)")
	}
	cmd.Env = env
	cmd.Stdin = strings.NewReader(string(in))
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if part != "" { // the shares of a server job, one after the other in the run's log
		flags = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	}
	logf, err := os.OpenFile(strings.TrimSuffix(a.runFile(r.ID), ".json")+".log", flags, 0o600)
	if err != nil {
		return nil, notes, err
	}
	defer logf.Close()
	if part != "" {
		_, _ = fmt.Fprintf(logf, "==== %s -> %s\n", j.Source.Share, j.Dest)
	}
	cmd.Stderr = &limitWriter{w: logf, n: 3 << 20} // under what the panel reads back (4 MiB)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, notes, err
	}
	if err := cmd.Start(); err != nil {
		return nil, notes, err
	}
	stop := context.AfterFunc(ctx, func() { // cancelled: the unit (or the child) and all it started go
		if r.Confined {
			_ = exec.Command("systemctl", "stop", unit).Run()
		}
		_ = cmd.Process.Kill()
	})
	defer stop()
	var res *syncer.Result
	var werr string
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 64<<20) // the result line is bounded well below (samples, 200 quarantined names)
	last := time.Time{}
	for sc.Scan() {
		var m workerMsg
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		if m.Progress != nil && time.Since(last) > 3*time.Second {
			if part != "" {
				m.Progress["share"] = j.Source.Share
			}
			r.Progress, last = m.Progress, time.Now()
			a.writeRun(*r)
		}
		if m.Result != nil || m.Error != "" {
			res, werr = m.Result, m.Error
		}
	}
	_, _ = io.Copy(io.Discard, out) // a line too long for the scanner: the worker must not block on a full pipe
	err = cmd.Wait()
	if werr != "" {
		return res, notes, errors.New(werr)
	}
	if err != nil {
		return res, notes, fmt.Errorf("sync worker: %w", err)
	}
	if res == nil {
		return nil, notes, errors.New("the sync worker gave no result")
	}
	return res, notes, nil
}

// credentials prepares what the worker signs in with: the NTLM password, or a Kerberos ticket of the machine
// account in runDir with a krb5.conf that finds the domain's KDCs by DNS (the system one may name none).
func (a *Agent) credentials(ctx context.Context, src syncer.Source, pw, runDir string, env *[]string) (password, ccache string, err error) {
	if src.Auth == "ntlm" {
		if pw == "" {
			return "", "", errors.New("no password for the source user (type it in the job)")
		}
		return pw, "", nil
	}
	cc := filepath.Join(runDir, "ccache")
	if err := machineTicket(ctx, cc); err != nil {
		return "", "", err
	}
	krb := fmt.Sprintf("[libdefaults]\n\tdefault_realm = %s\n\tdns_lookup_kdc = true\n\tdns_lookup_realm = false\n\trdns = false\n", strings.ToUpper(a.realm))
	if err := os.WriteFile(filepath.Join(runDir, "krb5.conf"), []byte(krb), 0o600); err != nil {
		return "", "", err
	}
	*env = append(*env, "KRB5_CONFIG="+filepath.Join(runDir, "krb5.conf"))
	return "", "FILE:" + cc, nil
}

// syncBox seals the passwords typed in the wizard (the panel keeps its own sealed copy): key and data under the
// agent's sync folder, 0600 root.
func (a *Agent) syncBox() (*secretbox.Box, error) {
	return secretbox.Load(filepath.Join(a.syncDir(), "secret.key"))
}

func (a *Agent) secretFile(id string) string { return filepath.Join(a.syncDir(), "secrets", id) }

func (a *Agent) storeSyncPassword(id, pw string) error {
	box, err := a.syncBox()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.secretFile(id)), 0o700); err != nil {
		return err
	}
	return writeFile0600(a.secretFile(id), box.Seal([]byte(pw), "sync:"+id))
}

// syncPassword is a job's NTLM password: from its password file, or the sealed one kept here.
func (a *Agent) syncPassword(j SyncJob) (string, error) {
	if j.Source.Auth != "ntlm" {
		return "", nil
	}
	if j.PasswordFile != "" {
		if err := checkSecretFile(j.PasswordFile); err != nil {
			return "", err
		}
		b, err := os.ReadFile(j.PasswordFile)
		return strings.TrimRight(string(b), "\r\n"), err
	}
	sealed, err := os.ReadFile(a.secretFile(j.ID))
	if errors.Is(err, os.ErrNotExist) {
		return "", errors.New("no password for the source user (type it in the job)")
	}
	if err != nil {
		return "", err
	}
	box, err := a.syncBox()
	if err != nil {
		return "", err
	}
	b, err := box.Open(sealed, "sync:"+j.ID)
	return string(b), err
}

// browseResult is what a listing worker answers.
type browseResult struct {
	Steps   []syncer.Step `json:"steps,omitempty"` // a connection check
	Dirs    []string      `json:"dirs,omitempty"`
	Targets []syncTarget  `json:"targets,omitempty"` // a server job: where each share of the source goes
	Files   int64         `json:"files"`
	Bytes   int64         `json:"bytes"`
	Error   string        `json:"error,omitempty"`
}

// browse lists, through a worker child (it writes nothing), the shares of a server or the folders of a share path;
// size counts the files and bytes under the path instead.
func (a *Agent) browse(ctx context.Context, src syncer.Source, pw string, size, check bool) (*browseResult, error) {
	if err := syncer.CheckBrowse(src); err != nil {
		if check {
			return &browseResult{Steps: []syncer.Step{{Name: "resolve", Detail: err.Error()}}}, nil
		}
		return nil, err
	}
	runDir := filepath.Join("/run/ad-manager-sync", "browse-"+newRunID())
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return nil, err
	}
	defer os.RemoveAll(runDir)
	env := []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=" + runDir, "LANG=C.UTF-8"}
	sp := syncer.Spec{Source: src, Browse: true, Size: size, Check: check}
	var err error
	if sp.Password, sp.CCache, err = a.credentials(ctx, src, pw, runDir, &env); err != nil {
		if check { // the sign-in step fails here (no password, no machine ticket)
			return &browseResult{Steps: []syncer.Step{{Name: "credentials", Detail: err.Error()}}}, nil
		}
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	in, _ := json.Marshal(sp)
	cmd := exec.CommandContext(ctx, exe, "agent", "sync-worker")
	cmd.Env, cmd.Stdin = env, strings.NewReader(string(in))
	out, err := cmd.Output()
	res := browseResult{}
	if check { // the credentials were ready (the failure is reported above)
		what := "password"
		if src.Auth != "ntlm" {
			what = "Kerberos ticket of the machine account"
		}
		defer func() { res.Steps = append([]syncer.Step{{Name: "credentials", OK: true, Detail: what}}, res.Steps...) }()
	}
	if jerr := json.Unmarshal(out, &res); jerr != nil {
		if err == nil {
			err = jerr
		}
		return nil, fmt.Errorf("sync worker: %w", err)
	}
	if res.Error != "" {
		return nil, errors.New(res.Error)
	}
	if res.Dirs == nil && !size && !check {
		res.Dirs = []string{}
	}
	return &res, nil
}

// machineTicket gets a Kerberos ticket of this server's machine account into ccache (its password from Samba's
// secrets: no user password involved).
func machineTicket(ctx context.Context, ccache string) error {
	cmd := exec.CommandContext(ctx, "net", "ads", "kerberos", "kinit", "-P")
	cmd.Env = append(os.Environ(), "KRB5CCNAME=FILE:"+ccache)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("Kerberos ticket of this server's machine account: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return os.Chmod(ccache, 0o600)
}

type limitWriter struct {
	w io.Writer
	n int64
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.n <= 0 {
		return len(p), nil
	}
	q := p
	if int64(len(q)) > l.n {
		q = q[:l.n]
	}
	n, err := l.w.Write(q)
	l.n -= int64(n)
	if err != nil {
		return n, err
	}
	return len(p), nil
}

// syncRoutes: jobs, preview, run, cancel, runs and their logs.
func (a *Agent) syncRoutes(api *echo.Group) {
	bad := func(c *echo.Context, code int, err error) error {
		return c.JSON(code, map[string]string{"error": err.Error()})
	}
	api.GET("/syncs", func(c *echo.Context) error {
		list, err := a.syncJobs()
		if err != nil {
			return bad(c, http.StatusInternalServerError, err)
		}
		for i := range list {
			_, err := os.Stat(a.secretFile(list[i].ID))
			list[i].PasswordSet = err == nil
		}
		_, netErr := exec.LookPath("net") // the machine account's ticket comes from net ads kerberos kinit
		machine := ""
		if n := testparm(c.Request().Context(), "netbios name"); n != "" {
			machine = strings.ToUpper(n) + "$"
		}
		return c.JSON(http.StatusOK, map[string]any{"jobs": list, "runs": a.syncRuns(), "roots": a.cfg.ShareRoots,
			"file_server": !a.readOnly(), "kerberos": netErr == nil && a.realm != "", "machine": machine,
			"realm": strings.ToUpper(a.realm)})
	})
	api.PUT("/syncs/:id", func(c *echo.Context) error {
		var in struct {
			SyncJob
			Confirm  string `json:"confirm"`
			Password string `json:"password"` // ntlm without a password file: sealed here, never kept in the job
		}
		if err := c.Bind(&in); err != nil {
			return bad(c, http.StatusBadRequest, errors.New("invalid JSON"))
		}
		j := in.SyncJob
		j.ID = c.Param("id")
		if err := a.validateSync(&j); err != nil {
			return bad(c, http.StatusBadRequest, err)
		}
		if j.Mode == syncer.ModeMirror && in.Confirm != j.ID {
			return bad(c, http.StatusBadRequest, errors.New("confirm the mirror: it deletes in the destination what the source no longer has"))
		}
		j.PasswordSet = false
		if j.Source.Auth == "ntlm" && j.PasswordFile == "" && in.Password == "" {
			if _, err := os.Stat(a.secretFile(j.ID)); err != nil {
				return bad(c, http.StatusBadRequest, errors.New("type the password of the source user"))
			}
		}
		a.muPut.Lock()
		defer a.muPut.Unlock()
		list, err := a.syncJobs()
		if err != nil {
			return bad(c, http.StatusInternalServerError, err)
		}
		j.Approved = 0
		replaced := false
		for i := range list {
			if list[i].ID == j.ID {
				j.Gen, j.Rev = list[i].Gen, list[i].Rev+1
				list[i], replaced = j, true
			}
		}
		if !replaced {
			if len(list) >= 100 {
				return bad(c, http.StatusBadRequest, errors.New("too many sync jobs"))
			}
			j.Gen, j.Rev = newRunID(), 1
			list = append(list, j)
		}
		if err := a.registerSyncs(list); err != nil {
			return bad(c, http.StatusBadRequest, err)
		}
		if err := a.saveSyncJobs(list); err != nil {
			return bad(c, http.StatusInternalServerError, err)
		}
		// the password changes only with the job saved (a refused edit keeps the one that works)
		switch {
		case j.Source.Auth == "ntlm" && j.PasswordFile == "" && in.Password != "":
			if err := a.storeSyncPassword(j.ID, in.Password); err != nil {
				return bad(c, http.StatusInternalServerError, err)
			}
		case j.Source.Auth != "ntlm" || j.PasswordFile != "":
			_ = os.Remove(a.secretFile(j.ID))
		}
		return c.JSON(http.StatusOK, j)
	})
	api.DELETE("/syncs/:id", func(c *echo.Context) error {
		a.muPut.Lock()
		defer a.muPut.Unlock()
		list, err := a.syncJobs()
		if err != nil {
			return bad(c, http.StatusInternalServerError, err)
		}
		n := len(list)
		list = slices.DeleteFunc(list, func(j SyncJob) bool { return j.ID == c.Param("id") })
		if len(list) == n {
			return bad(c, http.StatusNotFound, errors.New("sync job not found"))
		}
		if err := a.registerSyncs(list); err != nil {
			return bad(c, http.StatusInternalServerError, err)
		}
		if err := a.saveSyncJobs(list); err != nil {
			return bad(c, http.StatusInternalServerError, err)
		}
		_ = os.Remove(a.secretFile(c.Param("id")))
		if marks, err := filepath.Glob(filepath.Join(a.syncDir(), "acl-pending", c.Param("id")+".*")); err == nil {
			for _, m := range marks {
				_ = os.Remove(m)
			}
		}
		return c.JSON(http.StatusOK, map[string]string{"deleted": c.Param("id")})
	})
	start := func(preview bool) echo.HandlerFunc {
		return func(c *echo.Context) error {
			j, err := a.syncJob(c.Param("id"))
			if err != nil {
				return bad(c, http.StatusNotFound, err)
			}
			if err := a.validateSync(&j); err != nil {
				return bad(c, http.StatusBadRequest, err)
			}
			if !preview && j.Mode == syncer.ModeMirror && j.Approved != j.Rev {
				return bad(c, http.StatusBadRequest, errors.New("preview this mirror (its current revision) before running it"))
			}
			r, err := a.enqueueSync(c.Request().Context(), j, preview, "manual", c.QueryParam("by"))
			if err != nil {
				return bad(c, http.StatusServiceUnavailable, err)
			}
			return c.JSON(http.StatusAccepted, r)
		}
	}
	api.POST("/syncs/browse", func(c *echo.Context) error {
		var in struct {
			Source       syncer.Source `json:"source"`
			Password     string        `json:"password"`
			PasswordFile string        `json:"password_file"`
			Job          string        `json:"job"`
			Size         bool          `json:"size"`    // count the files and bytes under the path instead
			Check        bool          `json:"check"`   // test the way to the source, step by step
			Targets      bool          `json:"targets"` // a server job: the source's shares and where each goes
		}
		if err := c.Bind(&in); err != nil {
			return bad(c, http.StatusBadRequest, errors.New("invalid JSON"))
		}
		pw := in.Password
		if in.Source.Auth == "ntlm" && pw == "" {
			j := SyncJob{ID: in.Job, Source: in.Source, PasswordFile: in.PasswordFile}
			if in.Job != "" && in.PasswordFile == "" && !syncIDRe.MatchString(in.Job) {
				return bad(c, http.StatusBadRequest, errors.New("invalid job"))
			}
			var err error
			if pw, err = a.syncPassword(j); err != nil && !in.Check {
				return bad(c, http.StatusBadRequest, err)
			}
		}
		limit := 60 * time.Second
		if in.Size {
			limit = 5 * time.Minute // a whole tree
		}
		ctx, cancel := context.WithTimeout(c.Request().Context(), limit)
		defer cancel()
		if in.Targets {
			t, err := a.serverTargets(ctx, in.Source, pw)
			if err != nil {
				return bad(c, http.StatusBadRequest, syncer.WithHint(err))
			}
			dirs := make([]string, 0, len(t))
			for _, x := range t {
				dirs = append(dirs, x.Share)
			}
			return c.JSON(http.StatusOK, browseResult{Dirs: dirs, Targets: t})
		}
		res, err := a.browse(ctx, in.Source, pw, in.Size, in.Check)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return bad(c, http.StatusGatewayTimeout, errors.New("the folder is too large to count in 5 minutes"))
		}
		if err != nil {
			return bad(c, http.StatusBadRequest, syncer.WithHint(err))
		}
		return c.JSON(http.StatusOK, res)
	})
	// validate: a destination (can it be one; is it there, how full, which share) and a schedule (next runs), for
	// the wizard before anything is saved
	api.POST("/syncs/validate", func(c *echo.Context) error {
		var in struct {
			Dest string `json:"dest"`
			Cron string `json:"cron"`
		}
		if err := c.Bind(&in); err != nil {
			return bad(c, http.StatusBadRequest, errors.New("invalid JSON"))
		}
		out := map[string]any{}
		if in.Dest != "" {
			d := filepath.Clean(in.Dest)
			r := map[string]any{"ok": true}
			if err := a.validateDest(d); err != nil { // nothing about a folder that cannot be a destination
				out["dest"] = map[string]any{"ok": false, "error": err.Error()}
				d = ""
			}
			if d == "" {
			} else if f, err := os.Open(d); err == nil {
				names, _ := f.Readdirnames(10001)
				f.Close()
				r["exists"], r["items"] = true, len(names)
			}
			for p := d; d != "" && p != "/" && p != "."; p = filepath.Dir(p) {
				if free, err := backup.FreeBytes(p); err == nil {
					r["free_bytes"] = free
					break
				}
			}
			if list, err := a.mgr.List(c.Request().Context()); err == nil && d != "" {
				best := ""
				for _, sh := range list {
					sp := filepath.Clean(sh.Path)
					if (d == sp || strings.HasPrefix(d, sp+"/")) && len(sp) > len(best) {
						best, r["share"] = sp, sh.Name
					}
				}
			}
			if d != "" {
				out["dest"] = r
			}
		}
		if in.Cron != "" {
			r := map[string]any{"ok": true}
			sched, err := cron.ParseStandard(in.Cron)
			if err != nil {
				r["ok"], r["error"] = false, err.Error()
			} else {
				var next []time.Time
				t := time.Now()
				for range 3 {
					t = sched.Next(t)
					next = append(next, t)
				}
				r["next"] = next
			}
			out["cron"] = r
		}
		return c.JSON(http.StatusOK, out)
	})
	api.POST("/syncs/:id/preview", start(true))
	api.POST("/syncs/:id/run", start(false))
	api.POST("/sync-runs/:run/cancel", func(c *echo.Context) error {
		a.sync.mu.Lock()
		cancel := a.sync.running[c.Param("run")]
		a.sync.mu.Unlock()
		if cancel == nil {
			return bad(c, http.StatusNotFound, errors.New("this run is not running"))
		}
		cancel()
		return c.JSON(http.StatusOK, map[string]string{"cancelled": c.Param("run")})
	})
	api.GET("/sync-runs/:run/log", func(c *echo.Context) error {
		id := c.Param("run")
		if !runIDRe.MatchString(id) {
			return bad(c, http.StatusNotFound, errors.New("run not found"))
		}
		b, err := os.ReadFile(strings.TrimSuffix(a.runFile(id), ".json") + ".log")
		if err != nil {
			return bad(c, http.StatusNotFound, errors.New("run not found"))
		}
		return c.Blob(http.StatusOK, "text/plain; charset=utf-8", b)
	})
	a.quarantineRoutes(api, bad)
}

// quarantineRoutes: the quarantines of the current jobs and the free space, their files, giving files back,
// downloading one and emptying a job's.
func (a *Agent) quarantineRoutes(api *echo.Group, bad func(*echo.Context, int, error) error) {
	// get reads the quarantine named in the path, of a current job (one deleted or made again is not listed); ok
	// false: the 404 is written already
	get := func(c *echo.Context) (Quarantine, bool) {
		q, err := a.quarantine(c.Param("name"))
		if err == nil {
			if j, jerr := a.syncJob(q.Job); jerr != nil || j.Gen != q.Gen {
				err = errQuarantineGone
			}
		}
		if err != nil {
			_ = bad(c, http.StatusNotFound, err)
			return q, false
		}
		return q, true
	}
	api.GET("/syncs/quarantines", func(c *echo.Context) error {
		list := a.quarantines("", "")
		if list == nil {
			list = []Quarantine{}
		}
		sp := a.spaces()
		if sp == nil {
			sp = []Space{}
		}
		return c.JSON(http.StatusOK, map[string]any{"quarantines": list, "space": sp})
	})
	api.GET("/syncs/quarantines/:name/files", func(c *echo.Context) error {
		q, ok := get(c)
		if !ok {
			return nil
		}
		files, truncated := quarantineFiles(q, 2000)
		if files == nil {
			files = []QFile{}
		}
		return c.JSON(http.StatusOK, map[string]any{"quarantine": q, "files": files, "truncated": truncated})
	})
	api.GET("/syncs/quarantines/:name/file", func(c *echo.Context) error {
		q, ok := get(c)
		if !ok {
			return nil
		}
		rel, err := cleanRel(c.QueryParam("path"))
		if err != nil {
			return bad(c, http.StatusBadRequest, err)
		}
		f, err := openQuarantineFile(q, rel)
		if err != nil {
			return bad(c, http.StatusNotFound, errors.New("file not found in the quarantine"))
		}
		defer f.Close()
		fi, _ := f.Stat()
		h := c.Response().Header()
		h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(rel)}))
		h.Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
		return c.Stream(http.StatusOK, "application/octet-stream", f)
	})
	api.POST("/syncs/quarantines/:name/restore", func(c *echo.Context) error {
		if a.readOnly() {
			return bad(c, http.StatusForbidden, errors.New("folders are synced into a file server"))
		}
		q, ok := get(c)
		if !ok {
			return nil
		}
		var in struct {
			Paths   []string `json:"paths"` // none: all the files
			Replace bool     `json:"replace"`
		}
		if err := c.Bind(&in); err != nil {
			return bad(c, http.StatusBadRequest, errors.New("invalid JSON"))
		}
		res, err := a.giveBack(q, in.Paths, in.Replace)
		if err != nil {
			return bad(c, http.StatusConflict, err)
		}
		if res.Restored+res.Swapped > 0 {
			if j, jerr := a.syncJob(q.Job); jerr == nil && restoreReinherits(j, q) {
				if _, err := a.mgr.Reinherit(context.WithoutCancel(c.Request().Context()), q.Dest); err != nil {
					res.Errors, res.Failed = append(res.Errors, "Windows inheritance not applied: "+err.Error()), res.Failed+1
				}
			}
		}
		return c.JSON(http.StatusOK, res)
	})
	api.POST("/syncs/:id/quarantine/empty", func(c *echo.Context) error {
		if a.readOnly() {
			return bad(c, http.StatusForbidden, errors.New("folders are synced into a file server"))
		}
		j, err := a.syncJob(c.Param("id"))
		if err != nil {
			return bad(c, http.StatusNotFound, err)
		}
		var in struct {
			Confirm string `json:"confirm"`
		}
		if err := c.Bind(&in); err != nil || in.Confirm != j.ID {
			return bad(c, http.StatusBadRequest, errors.New("confirm with the job's id: the quarantine cannot be brought back"))
		}
		a.sync.qmu.Lock() // a run makes its quarantine under qmu too: none appears meanwhile
		defer a.sync.qmu.Unlock()
		if run := a.activeRun(j.ID); run != "" { // whatever destination that run writes
			return bad(c, http.StatusConflict, fmt.Errorf("the run %s of this job is running", run))
		}
		n, freed := 0, int64(0)
		for _, q := range a.quarantines(j.ID, j.Gen) {
			if err := a.dropQuarantine(q); err != nil {
				return bad(c, http.StatusInternalServerError, err)
			}
			n, freed = n+1, freed+q.Size
		}
		slog.Info("sync.quarantine.empty", "job", j.ID, "removed", n, "size", freed)
		return c.JSON(http.StatusOK, map[string]any{"removed": n, "freed": freed})
	})
}

// syncTarget is where a server job brings one share of the source: the folder of the share of the same name
// published here, or why it is skipped.
type syncTarget struct {
	Share string `json:"share"`
	Dest  string `json:"dest,omitempty"`
	Skip  string `json:"skip,omitempty"`
}

// serverTargets lists the source server's shares (its administrative ones, sysvol and netlogon left out) and maps
// each to the folder of the share of the same name published here; a share not published here, reserved here,
// whose folder cannot be synced into, or inside (or the same as) another share's folder is skipped with the reason.
func (a *Agent) serverTargets(ctx context.Context, src syncer.Source, pw string) ([]syncTarget, error) {
	src.Share, src.Path = "", ""
	res, err := a.browse(ctx, src, pw, false, false)
	if err != nil {
		return nil, err
	}
	local, err := a.mgr.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("the shares of this server: %w", err)
	}
	paths := map[string]string{}
	for _, s := range local {
		paths[strings.ToLower(s.Name)] = s.Path
	}
	return mapTargets(res.Dirs, paths, a.mgr.Reserved, a.validateDest), nil
}

// mapTargets is serverTargets' mapping: source share names, this server's share paths by lowercase name, the shares
// reserved here, and the destination check.
func mapTargets(names []string, paths map[string]string, reserved []string, valid func(string) error) []syncTarget {
	names = slices.Clone(names)
	slices.SortFunc(names, func(x, y string) int { return strings.Compare(strings.ToLower(x), strings.ToLower(y)) })
	var out []syncTarget
	for _, n := range names {
		if shares.IsSystemShare(n) {
			continue
		}
		t := syncTarget{Share: n}
		p := paths[strings.ToLower(n)]
		switch {
		case slices.ContainsFunc(reserved, func(r string) bool { return strings.EqualFold(r, n) }):
			t.Skip = "reserved on this server"
		case p == "":
			t.Skip = "not published on this server"
		default:
			t.Dest = filepath.Clean(p)
			if err := valid(t.Dest); err != nil {
				t.Skip = err.Error()
			}
		}
		out = append(out, t)
	}
	// a share inside another one's folder (the source's nesting) comes with it; the same folder twice, once
	kept := make([]bool, len(out))
	for i := range out {
		kept[i] = out[i].Skip == ""
	}
	for i := range out {
		if !kept[i] {
			continue
		}
		for k := range out {
			if k == i || !kept[k] {
				continue
			}
			if out[k].Dest == out[i].Dest && k < i {
				out[i].Skip = "same folder as " + out[k].Share
				break
			}
			if strings.HasPrefix(out[i].Dest, out[k].Dest+"/") {
				out[i].Skip = "inside " + out[k].Share + ": copied with it"
				break
			}
		}
	}
	return out
}

// syncServer brings every share of a server job's source, one at a time, each as a folder run of its own worker
// into the folder of the share of the same name here; the run records the sum and a note per share.
func (a *Agent) syncServer(rctx context.Context, j SyncJob, r *SyncRun) (*syncer.Result, error) {
	pw, err := a.syncPassword(j)
	if err != nil {
		return nil, err
	}
	lctx, cancel := context.WithTimeout(rctx, 2*time.Minute)
	targets, err := a.serverTargets(lctx, j.Source, pw)
	cancel()
	if err != nil {
		return nil, err
	}
	var todo []syncTarget
	for _, t := range targets {
		if t.Skip != "" {
			r.Notes = append(r.Notes, fmt.Sprintf("%s: skipped (%s)", t.Share, t.Skip))
		} else {
			todo = append(todo, t)
		}
	}
	if len(todo) == 0 {
		return nil, errors.New("no share of the source is published on this server (import the shares first)")
	}
	sum := &syncer.Result{Plan: syncer.Plan{Sample: map[string][]string{}}, Checked: true, ContentVerified: j.Verify}
	var failed []string
	for i, t := range todo {
		if rctx.Err() != nil {
			return sum, rctx.Err()
		}
		jj := j
		jj.Kind, jj.Source.Share, jj.Dest = "", t.Share, t.Dest
		r.Progress = map[string]any{"phase": "preparing", "share": t.Share, "share_index": i + 1, "share_count": len(todo)}
		a.writeRun(*r)
		part := fmt.Sprintf("-s%d", i+1)
		res, err := a.syncTree(rctx, jj, r, part)
		addResult(sum, res, t.Share)
		switch {
		case err != nil && rctx.Err() != nil:
			return sum, err
		case err != nil:
			failed = append(failed, t.Share)
			r.Notes = append(r.Notes, fmt.Sprintf("%s -> %s: %s", t.Share, t.Dest, syncer.WithHint(err)))
		case res != nil:
			r.Notes = append(r.Notes, fmt.Sprintf("%s -> %s: %d files copied (%s)", t.Share, t.Dest, res.Transfers, humanBytes(res.Bytes)))
		}
		if !r.Preview { // the share's quarantine, kept only when something went into it
			if q, qerr := a.quarantine(j.ID + "-" + r.ID + part); qerr == nil {
				if n, _ := dirUsage(q.Path); n == 0 {
					a.sync.qmu.Lock()
					_ = a.dropQuarantine(q)
					a.sync.qmu.Unlock()
				}
			}
		}
	}
	r.Quarantine = ""
	if !r.Preview {
		if n, freed := a.pruneQuarantine(j, r.ID); n > 0 {
			r.Notes = append(r.Notes, fmt.Sprintf("quarantine retention: %d old quarantines removed (%s)", n, humanBytes(freed)))
		}
	}
	if len(failed) > 0 {
		return sum, fmt.Errorf("%d of %d shares failed: %s", len(failed), len(todo), strings.Join(failed, ", "))
	}
	return sum, nil
}

// addResult adds one share's result to a server run's sum (its sample names under the share's name).
func addResult(sum, x *syncer.Result, share string) {
	if x == nil {
		sum.Checked = false
		return
	}
	p, q := &sum.Plan, x.Plan
	p.New, p.Changed, p.Delete, p.Same, p.Errors = p.New+q.New, p.Changed+q.Changed, p.Delete+q.Delete, p.Same+q.Same, p.Errors+q.Errors
	p.NewBytes, p.ChangedBytes, p.DeleteBytes = p.NewBytes+q.NewBytes, p.ChangedBytes+q.ChangedBytes, p.DeleteBytes+q.DeleteBytes
	for g, names := range q.Sample {
		for _, n := range names {
			if len(p.Sample[g]) < 50 {
				p.Sample[g] = append(p.Sample[g], share+"/"+n)
			}
		}
	}
	sum.Transfers, sum.Bytes, sum.Deleted, sum.Errors = sum.Transfers+x.Transfers, sum.Bytes+x.Bytes, sum.Deleted+x.Deleted, sum.Errors+x.Errors
	sum.Checked = sum.Checked && x.Checked
	sum.ContentVerified = sum.ContentVerified && x.ContentVerified
	sum.Seconds += x.Seconds
	for _, n := range x.Quarantined {
		if len(sum.Quarantined) < 200 {
			sum.Quarantined = append(sum.Quarantined, share+"/"+n)
		}
	}
}

// restoreReinherits tells whether files given back from quarantine q get job j's Windows inheritance again: a
// folder job's current destination, or the share folder a server job's part recorded (checked by giveBack).
func restoreReinherits(j SyncJob, q Quarantine) bool {
	if !j.Reinherit {
		return false
	}
	if j.Kind == "server" {
		return q.Part != "" && q.Dest != ""
	}
	return j.Dest == q.Dest
}
