// Package syncer brings a folder of an SMB share into a local folder with rclone's sync engine, embedded as a
// library (no rclone binary, no mount): copy (add and update) or mirror (also delete what the source no longer
// has, only after a complete plan within the limits, with deleted and replaced files moved to a quarantine).
// It runs inside a short-lived worker process (one per preview or run): rclone keeps process-wide state.
package syncer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/rclone/rclone/backend/local" // the destination
	_ "github.com/rclone/rclone/backend/smb"   // the source (patched: signing required, third_party/rclone)
	rfs "github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/accounting"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/fs/config/obscure"
	"github.com/rclone/rclone/fs/filter"
	"github.com/rclone/rclone/fs/operations"
	rsync "github.com/rclone/rclone/fs/sync"
	"github.com/rclone/rclone/lib/encoder"
)

// Modes.
const (
	ModeCopy   = "copy"   // add and update, never delete
	ModeMirror = "mirror" // also delete in the destination what the source no longer has
)

// Source is a folder of an SMB share. Kerberos (a credential cache) unless Auth is "ntlm".
type Source struct {
	Host   string `json:"host"`
	Port   int    `json:"port,omitempty"`
	Share  string `json:"share"`
	Path   string `json:"path,omitempty"` // inside the share, with / separators
	Auth   string `json:"auth"`           // kerberos (default) or ntlm
	User   string `json:"user,omitempty"`
	Domain string `json:"domain,omitempty"`
}

// Spec is one preview or run, as the worker gets it.
type Spec struct {
	Source     Source   `json:"source"`
	Password   string   `json:"password,omitempty"` // ntlm
	CCache     string   `json:"ccache,omitempty"`   // kerberos: FILE:/run/…
	Dest       string   `json:"dest"`               // local folder (checked by the agent)
	Mode       string   `json:"mode"`
	Exclude    []string `json:"exclude,omitempty"` // rclone filter patterns, each excluded
	Quarantine string   `json:"quarantine"`        // local folder for deleted and replaced files (outside dest)
	MaxDelete  int      `json:"max_delete"`        // mirror: most deletions allowed
	MaxDelPct  int      `json:"max_delete_pct"`    // mirror: most deletions as % of the destination's files
	BwLimit    string   `json:"bwlimit,omitempty"` // e.g. 10M, or "08:00,512k 19:00,off"
	Transfers  int      `json:"transfers,omitempty"`
	Verify     bool     `json:"verify,omitempty"` // compare the content (downloads the source again)
	Preview    bool     `json:"preview"`
	Browse     bool     `json:"browse,omitempty"` // only list: the shares (no share) or the folders of Source
	Size       bool     `json:"size,omitempty"`   // with Browse: count the files and bytes under Source instead
	Check      bool     `json:"check,omitempty"`  // with Browse: test the way to the source, step by step
	// set by the agent for a confined run: the worker checks that only these folders are writable to it and that
	// they are the very folders the agent opened ("dev:ino")
	Confined     bool   `json:"confined,omitempty"`
	DestID       string `json:"dest_id,omitempty"`
	QuarantineID string `json:"quarantine_id,omitempty"`
}

// Step is one check of the way to a source.
type Step struct {
	Name   string   `json:"name"` // credentials, resolve, port, signin, share, folder
	OK     bool     `json:"ok"`
	Detail string   `json:"detail,omitempty"`
	Hint   string   `json:"hint,omitempty"`  // what it most likely means, in a fixed sentence (translated by the catalog)
	Items  []string `json:"items,omitempty"` // signin: the shares the account sees
}

// hints map what SMB, Kerberos and the network say to a sentence an administrator acts on.
var hints = []struct {
	match []string
	hint  string
}{
	{[]string{"no such host", "server misbehaving"}, "the server name does not resolve (check this server's DNS, or use the IP with user and password)"},
	{[]string{"connection refused"}, "the server refused port 445 (SMB off, or a firewall)"},
	{[]string{"i/o timeout", "no route to host", "network is unreachable", "deadline exceeded"}, "the server does not answer on port 445 (a firewall, or a wrong address)"},
	{[]string{"doesn't support signing", "signature"}, "the server does not sign SMB (turn signing on in it)"},
	{[]string{"no kdcs", "communicate with kdc"}, "the domain's Kerberos server (KDC) was not found (check this server's DNS and realm)"},
	{[]string{"principal_unknown", "server not found in kerberos", "principal unknown"}, "Kerberos does not know this server name (use its full name, not the IP)"},
	{[]string{"logon is invalid", "logon_failure", "logon failure", "bad username"}, "user or password refused"},
	{[]string{"permission denied", "access denied", "access_denied", "access is denied"}, "access denied (give the account read permission on the share and the folder)"},
	{[]string{"bad_network_name", "network name cannot be found", "bad network name"}, "the share does not exist on the server"},
	{[]string{"object name not found", "object_name_not_found", "no such file", "directory not found", "object_path_not_found"}, "the folder does not exist in the share"},
}

// Hint is the sentence for err ("" when nothing known matches).
func Hint(err error) string {
	if err == nil {
		return ""
	}
	e := strings.ToLower(err.Error())
	for _, h := range hints {
		for _, m := range h.match {
			if strings.Contains(e, m) {
				return h.hint
			}
		}
	}
	return ""
}

// WithHint puts the hint in front of err ("<hint>: <what was said>"), for the screens.
func WithHint(err error) error {
	if h := Hint(err); h != "" && !strings.HasPrefix(err.Error(), h) {
		return fmt.Errorf("%s: %w", h, err)
	}
	return err
}

// Check tests the way to the source, stopping at the first step that fails: the name resolves, port 445 answers,
// a signed SMB session signs in (and lists the shares), the share opens, the folder exists.
func Check(ctx context.Context, sp Spec) []Step {
	s := sp.Source
	var steps []Step
	add := func(name string, err error, detail string) bool {
		st := Step{Name: name, OK: err == nil, Detail: detail}
		if err != nil {
			st.Detail, st.Hint = err.Error(), Hint(err)
		}
		steps = append(steps, st)
		return err == nil
	}
	if err := CheckBrowse(s); err != nil {
		add("resolve", err, "")
		return steps
	}
	addrs, err := net.DefaultResolver.LookupHost(ctx, s.Host)
	if !add("resolve", err, strings.Join(addrs, ", ")) {
		return steps
	}
	port := s.Port
	if port == 0 {
		port = 445
	}
	d := net.Dialer{Timeout: 5 * time.Second}
	c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(s.Host, strconv.Itoa(port)))
	if err == nil {
		c.Close()
	}
	if !add("port", err, strconv.Itoa(port)) {
		return steps
	}
	root := sp
	root.Source.Share, root.Source.Path = "", ""
	shares, err := Browse(ctx, root)
	ok := add("signin", err, "")
	steps[len(steps)-1].Items = shares
	if !ok || s.Share == "" {
		return steps
	}
	share := sp
	share.Source.Path = ""
	if _, err := Browse(ctx, share); !add("share", err, s.Share) || strings.Trim(s.Path, "/") == "" {
		return steps
	}
	_, err = Browse(ctx, sp)
	add("folder", err, s.Path)
	return steps
}

// Size counts the files and bytes under the source folder (a walk of the whole tree: on request only).
func Size(ctx context.Context, sp Spec) (files, bytes int64, err error) {
	if err := CheckSource(sp.Source); err != nil {
		return 0, 0, err
	}
	f, err := smbFs(ctx, sp)
	if err != nil {
		return 0, 0, err
	}
	files, bytes, _, err = operations.Count(ctx, f)
	return files, bytes, err
}

// Browse lists the shares of the source server (no share given) or the folders of the source path.
func Browse(ctx context.Context, sp Spec) ([]string, error) {
	if err := CheckBrowse(sp.Source); err != nil {
		return nil, err
	}
	f, err := smbFs(ctx, sp)
	if err != nil {
		return nil, err
	}
	es, err := f.List(ctx, "")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range es {
		if d, ok := e.(rfs.Directory); ok {
			out = append(out, path.Base(d.Remote()))
		}
	}
	sort.Strings(out)
	return out, nil
}

// Plan is what a run would do: complete counts and a sample of names per group.
type Plan struct {
	New, Changed, Delete, Same, Errors  int
	NewBytes, ChangedBytes, DeleteBytes int64
	Sample                              map[string][]string `json:"sample"`
	deletes                             []string            // every file to delete (a mirror deletes only these)
}

// SourceFiles counts the files the source has (as listed).
func (p Plan) SourceFiles() int { return p.New + p.Changed + p.Same }

// DestFiles counts the files the destination has.
func (p Plan) DestFiles() int { return p.Changed + p.Same + p.Delete }

// Result is a preview or a run.
type Result struct {
	Plan            Plan     `json:"plan"`
	Transfers       int64    `json:"transfers"`
	Bytes           int64    `json:"bytes"`
	Deleted         int64    `json:"deleted"`
	Errors          int64    `json:"errors"`
	Checked         bool     `json:"checked"`          // sizes and dates match after the run
	ContentVerified bool     `json:"content_verified"` // and the content, when asked
	Quarantined     []string `json:"quarantined,omitempty"`
	Seconds         float64  `json:"seconds"`
	// Changed are the destination-relative paths rclone transferred (new and updated files), so the agent reapplies
	// Windows inheritance only to those; ChangedCapped means there were more than reinheritCap and the list is partial
	// (the agent then reinherits the whole tree).
	Changed       []string `json:"changed,omitempty"`
	ChangedCapped bool     `json:"changed_capped,omitempty"`
}

// reinheritCap bounds the transferred paths kept for the incremental reinherit; past it the agent walks the whole
// tree (which a first or near-full run would do anyway). reinheritBytesCap keeps the paths well under the Samba
// helper's 1 MiB request limit (leaving room for the JSON and the roots).
const (
	reinheritCap      = 5000
	reinheritBytesCap = 512 << 10
)

// sampleMax bounds the names kept per group.
const sampleMax = 50

// shareRe is a share name as Windows allows it.
var shareRe = regexp.MustCompile(`^[^\\/:*?"<>|\x00-\x1f]{1,80}$`)

// ValidHost tells whether h looks like a host name or an IPv4 address.
func ValidHost(h string) bool {
	if h == "" || len(h) > 253 || strings.ContainsAny(h, " /\\:@,") {
		return false
	}
	for _, l := range strings.Split(h, ".") {
		if l == "" || len(l) > 63 || strings.HasPrefix(l, "-") {
			return false
		}
		for _, c := range l {
			if !(c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
				return false
			}
		}
	}
	return true
}

// adminShares are never a source: a host's disks, its IPC and a domain controller's policies.
var adminShares = []string{"admin$", "ipc$", "print$", "sysvol", "netlogon"}

// CheckSource validates a source (shared by the agent and the worker).
func CheckSource(s Source) error { return checkSource(s, true) }

// CheckBrowse validates a source to list: the share may be empty (the server's shares are listed then).
func CheckBrowse(s Source) error { return checkSource(s, s.Share != "") }

func checkSource(s Source, needShare bool) error {
	if !ValidHost(s.Host) {
		return fmt.Errorf("invalid host %q", s.Host)
	}
	sh := strings.ToLower(s.Share)
	if len(sh) == 2 && sh[1] == '$' { // C$, D$…
		return fmt.Errorf("the share %q is not a source", s.Share)
	}
	for _, a := range adminShares {
		if sh == a {
			return fmt.Errorf("the share %q is not a source", s.Share)
		}
	}
	if needShare && !shareRe.MatchString(s.Share) {
		return fmt.Errorf("invalid share %q", s.Share)
	}
	for _, part := range strings.Split(strings.Trim(s.Path, "/"), "/") {
		if part == ".." || part == "." || strings.ContainsAny(part, "\\:*?\"<>|\x00") {
			return fmt.Errorf("invalid path %q", s.Path)
		}
	}
	if s.Port < 0 || s.Port > 65535 {
		return errors.New("invalid port")
	}
	switch s.Auth {
	case "", "kerberos":
	case "ntlm":
		if s.User == "" {
			return errors.New("ntlm needs a user")
		}
	default:
		return fmt.Errorf("unknown authentication %q", s.Auth)
	}
	return nil
}

// smbFs builds the source from the backend's registered options plus ours: no rclone connection string (which
// rclone logs, and where a comma or colon in a value would become an option).
func smbFs(ctx context.Context, sp Spec) (rfs.Fs, error) {
	ri, err := rfs.Find("smb")
	if err != nil {
		return nil, err
	}
	s := sp.Source
	port := s.Port
	if port == 0 {
		port = 445
	}
	m := configmap.Simple{"host": s.Host, "port": strconv.Itoa(port)}
	if s.Auth == "ntlm" {
		m["user"], m["domain"], m["pass"] = s.User, s.Domain, obscure.MustObscure(sp.Password)
	} else {
		m["use_kerberos"], m["kerberos_ccache"] = "true", sp.CCache
	}
	root := strings.Trim(s.Share+"/"+strings.Trim(s.Path, "/"), "/")
	return ri.NewFs(ctx, "src", root, rfs.ConfigMap(ri.Prefix, ri.Options, "", m))
}

// Run previews or runs sp. The plan is always made first, complete (a dry run with no deletion limit); a mirror
// stops there when the source is empty or the deletions pass the limits, before anything is deleted.
func Run(ctx context.Context, sp Spec, progress func(map[string]any)) (Result, error) {
	start := time.Now()
	var res Result
	if err := CheckSource(sp.Source); err != nil {
		return res, err
	}
	if sp.Mode != ModeCopy && sp.Mode != ModeMirror {
		return res, fmt.Errorf("unknown mode %q", sp.Mode)
	}
	ctx, ci := rfs.AddConfig(ctx)
	if sp.Transfers > 0 {
		ci.Transfers = min(sp.Transfers, 16)
	}
	ci.Checkers = 8
	ci.LogLevel = rfs.LogLevelNotice
	if sp.BwLimit != "" {
		if err := ci.BwLimit.Set(sp.BwLimit); err != nil {
			return res, fmt.Errorf("bandwidth limit: %w", err)
		}
		accounting.TokenBucket.StartTokenBucket(ctx)
	}
	fi, err := filter.NewFilter(nil)
	if err != nil {
		return res, err
	}
	for _, x := range sp.Exclude {
		if err := fi.AddRule("- " + x); err != nil {
			return res, fmt.Errorf("exclude %q: %w", x, err)
		}
	}
	ctx = filter.ReplaceConfig(ctx, fi)
	fsrc, err := smbFs(ctx, sp)
	if err != nil {
		return res, fmt.Errorf("source: %w", err)
	}
	fdst, err := rfs.NewFs(ctx, sp.Dest)
	if err != nil {
		return res, fmt.Errorf("destination: %w", err)
	}
	transfer := rsync.CopyDir
	if sp.Mode == ModeMirror {
		transfer = rsync.Sync
	}

	// 1. the complete plan
	res.Plan, err = plan(ctx, fdst, fsrc, transfer)
	if err != nil {
		return res, fmt.Errorf("plan: %w", err)
	}
	if sp.Preview {
		res.Seconds = time.Since(start).Seconds()
		return res, nil
	}
	if res.Plan.Errors > 0 {
		return res, fmt.Errorf("the plan had %d errors (listing or reading): nothing was changed", res.Plan.Errors)
	}
	if sp.Mode == ModeMirror && res.Plan.Delete > 0 {
		if res.Plan.SourceFiles() == 0 {
			return res, errors.New("the source is empty: the mirror would delete everything, nothing was changed")
		}
		if res.Plan.Delete > sp.MaxDelete {
			return res, fmt.Errorf("the mirror would delete %d files (limit %d): nothing was changed", res.Plan.Delete, sp.MaxDelete)
		}
		if sp.MaxDelPct > 0 && res.Plan.Delete*100 > sp.MaxDelPct*res.Plan.DestFiles() {
			return res, fmt.Errorf("the mirror would delete %d of %d files (limit %d%%): nothing was changed", res.Plan.Delete, res.Plan.DestFiles(), sp.MaxDelPct)
		}
	}

	// 2. the run: copy (add and update; replaced files go to the quarantine); then, a mirror, delete exactly the
	// planned files the source still lacks (each one asked again), moving them to the quarantine — a source that
	// changed meanwhile never widens what is deleted
	rctx, rci := rfs.AddConfig(ctx)
	rci.BackupDir = sp.Quarantine
	rctx = accounting.WithStatsGroup(rctx, "run")
	stats := accounting.StatsGroup(rctx, "run")
	// collect what rclone transfers (new and updated files, +/* sigils; deletes are MissingOnSrc, not these), so the
	// agent reapplies Windows inheritance only to those; the logger runs from several copy goroutines. The path is
	// turned back from rclone's encoded namespace into the on-disk name the Samba helper will walk. Past the item or
	// byte cap the list is dropped (ChangedCapped) and the agent reinherits the whole tree instead.
	var changedMu sync.Mutex
	var changed []string
	changedBytes := 0
	capped := false
	rctx = operations.WithLogger(rctx, func(_ context.Context, sigil operations.Sigil, src, dst rfs.DirEntry, _ error) {
		if sigil != operations.MissingOnDst && sigil != operations.Differ {
			return
		}
		// rclone writes the destination object under the source's remote (operations.Copy) and the matcher normalizes
		// names to NFC, so the source spelling — not the old destination's — is what ends up on disk.
		e := src
		if e == nil {
			e = dst
		}
		if e == nil {
			return
		}
		name := encoder.OS.FromStandardPath(e.Remote()) // encoded remote → native filename the fwalk sees
		changedMu.Lock()
		if capped || len(changed) >= reinheritCap || changedBytes+len(name) > reinheritBytesCap {
			capped = true
		} else {
			changed = append(changed, name)
			changedBytes += len(name) + 2
		}
		changedMu.Unlock()
	})
	done := make(chan struct{})
	var wg sync.WaitGroup
	if progress != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t := time.NewTicker(2 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-done:
					return
				case <-t.C:
					if st, err := stats.RemoteStats(false); err == nil {
						st["phase"] = "copying" // the panel shows which phase the run is in, not an opaque "running"
						st["total"] = res.Plan.New + res.Plan.Changed
						progress(map[string]any(st))
					}
				}
			}
		}()
	}
	err = rsync.CopyDir(rctx, fdst, fsrc, false)
	close(done)
	wg.Wait()
	res.Transfers, res.Bytes, res.Errors = stats.GetTransfers(), stats.GetBytes(), stats.GetErrors()
	changedMu.Lock()
	res.Changed, res.ChangedCapped = changed, capped
	changedMu.Unlock()
	if err == nil && sp.Mode == ModeMirror {
		res.Deleted, err = deletePlanned(ctx, fsrc, fdst, sp, res.Plan.deletes)
	}
	res.Quarantined = listQuarantine(sp.Quarantine)
	if err != nil {
		res.Seconds = time.Since(start).Seconds()
		return res, err
	}

	// 3. the check: sizes and dates (SMB has no hashes); the content too when asked
	// (one way: what the source has is in the destination; a skipped link or a file that came back is not a fault)
	opt := &operations.CheckOpt{Fdst: fdst, Fsrc: fsrc, OneWay: true}
	if err := operations.Check(ctx, opt); err == nil {
		res.Checked = true
	}
	res.Seconds = time.Since(start).Seconds()
	if !res.Checked {
		return res, errors.New("the destination does not match the source after the run")
	}
	if sp.Verify {
		if err := operations.CheckDownload(ctx, opt); err != nil {
			return res, fmt.Errorf("the content differs from the source after the run: %w", err)
		}
		res.ContentVerified = true
		res.Seconds = time.Since(start).Seconds()
	}
	return res, nil
}

// deletePlanned moves each planned file into the quarantine, when the source still lacks it (a link there, even
// one skipped while listing, counts as present); a source that cannot be asked stops the deletions. The move is
// rclone's: a rename, or a copy and a delete across mounts (the destination and the quarantine are separate
// mounts in the worker's unit).
func deletePlanned(ctx context.Context, fsrc, fdst rfs.Fs, sp Spec, names []string) (int64, error) {
	exists := func(name string) (bool, error) {
		if e, ok := fsrc.(interface {
			Exists(context.Context, string) (bool, error)
		}); ok {
			return e.Exists(ctx, name)
		}
		_, err := fsrc.NewObject(ctx, name)
		if errors.Is(err, rfs.ErrorObjectNotFound) {
			return false, nil
		}
		return err == nil || errors.Is(err, rfs.ErrorIsDir) || errors.Is(err, rfs.ErrorNotAFile), err
	}
	fq, err := rfs.NewFs(ctx, sp.Quarantine)
	if err != nil {
		return 0, err
	}
	var n int64
	for _, name := range names {
		there, err := exists(name)
		if err != nil && !there {
			return n, fmt.Errorf("asking the source about %s: %w", name, err)
		}
		if there {
			continue // it is back in the source (or a link we do not follow): kept
		}
		o, err := fdst.NewObject(ctx, name)
		if errors.Is(err, rfs.ErrorObjectNotFound) {
			continue // gone already
		}
		if err != nil {
			return n, err
		}
		if _, err := operations.Move(ctx, fq, nil, name, o); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// plan makes a dry run with no deletion limit and counts what it would do, from rclone's operation logger.
func plan(ctx context.Context, fdst, fsrc rfs.Fs, transfer func(context.Context, rfs.Fs, rfs.Fs, bool) error) (Plan, error) {
	p := Plan{Sample: map[string][]string{}}
	var mu sync.Mutex
	add := func(group string, n *int, bytes *int64, e rfs.DirEntry) {
		*n++
		if o, ok := e.(rfs.Object); ok && bytes != nil {
			*bytes += o.Size()
		}
		if len(p.Sample[group]) < sampleMax {
			p.Sample[group] = append(p.Sample[group], e.Remote())
		}
	}
	ctx, ci := rfs.AddConfig(ctx)
	ci.DryRun = true
	ci.MaxDelete = -1
	ctx = accounting.WithStatsGroup(ctx, "plan")
	ctx = operations.WithLogger(ctx, func(_ context.Context, sigil operations.Sigil, src, dst rfs.DirEntry, err error) {
		mu.Lock()
		defer mu.Unlock()
		if errors.Is(err, rfs.ErrorIsDir) {
			return // directories are not counted
		}
		switch sigil {
		case operations.MissingOnDst:
			add("new", &p.New, &p.NewBytes, src)
		case operations.Differ:
			add("changed", &p.Changed, &p.ChangedBytes, src)
		case operations.MissingOnSrc:
			add("delete", &p.Delete, &p.DeleteBytes, dst)
			p.deletes = append(p.deletes, dst.Remote())
		case operations.Match:
			p.Same++
		case operations.TransferError:
			e := src
			if e == nil {
				e = dst
			}
			if e != nil {
				add("error", &p.Errors, nil, e)
			} else {
				p.Errors++
			}
		}
	})
	err := transfer(ctx, fdst, fsrc, false)
	if err != nil && p.Errors == 0 {
		p.Errors++
	}
	return p, err
}

// listQuarantine names what a run moved into the quarantine (relative paths, at most 200: the rest is in the
// folder itself).
func listQuarantine(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if len(out) >= 200 {
			return filepath.SkipAll
		}
		if r, err := filepath.Rel(dir, p); err == nil {
			out = append(out, r)
		}
		return nil
	})
	return out
}
