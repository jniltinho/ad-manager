// Package backup keeps the backups an agent makes of its server: the domain controller (samba-tool domain backup
// offline), the file server's configuration and permissions, and chosen data folders (full plus incremental
// archives forming a chain), each with a manifest, a SHA-256 and a retention that deletes only whole chains.
package backup

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Kinds of backup.
const (
	KindDC         = "dc"                // samba-tool domain backup offline (base, secrets, SYSVOL, smb.conf)
	KindFileConfig = "fileserver-config" // smb.conf, registry shares, NT ACLs of the share roots, ACL history
	KindFileData   = "fileserver-data"   // chosen folders: a full archive, then incrementals (a chain)
	KindPanel      = "panel"             // the panel's database, configuration without secrets and audit log
	KindSambaFiles = "samba-files"       // /etc/samba, /var/lib/samba and /etc/krb5.conf as they are (before the setup touches them)
	// KindAll is not a backup of its own: an agent turns it into a fileserver-config and a full fileserver-data
	// backup of every share root
	KindAll = "all"
)

// Manifest describes one backup: what, when, how big, its hash and how it went.
type Manifest struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	Origin   string    `json:"origin"`            // "manual" or "schedule:<id>"
	By       string    `json:"by,omitempty"`      // who asked (manual)
	Chain    string    `json:"chain"`             // id of the full backup this one depends on (itself for a full one)
	Full     bool      `json:"full"`              // a full backup (the head of a chain)
	Folders  []string  `json:"folders,omitempty"` // fileserver-data
	File     string    `json:"file,omitempty"`    // archive name in the backup folder
	Size     int64     `json:"size"`
	SHA256   string    `json:"sha256,omitempty"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished,omitzero"`
	Status   string    `json:"status"` // running, ok, error
	Error    string    `json:"error,omitempty"`
	Log      []string  `json:"log,omitempty"` // what was done (never secrets)
	Note     string    `json:"note,omitempty"`
}

// idRe is a backup id: <UTC time>-<kind>-<random>, never taken from a request without this check.
var idRe = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-(dc|fileserver-config|fileserver-data|panel|samba-files)-[0-9a-f]{8}$`)

// ValidID tells whether id has the form of a backup id.
func ValidID(id string) bool { return idRe.MatchString(id) }

// KindOf is the kind written in a backup id ("" for something that is not an id).
func KindOf(id string) string {
	if m := idRe.FindStringSubmatch(id); m != nil {
		return m[1]
	}
	return ""
}

// ConfirmCode is what an operator types to confirm a destructive action on a backup: the end of its id.
func ConfirmCode(id string) string { return id[max(0, len(id)-8):] }

// ErrNotFound reports an id that is not a backup here (or not an id at all).
var ErrNotFound = errors.New("backup not found")

// ErrBusy reports a backup already running on this server.
var ErrBusy = errors.New("a backup is already running on this server")

// Store is the backup folder of a server (0700 root): <id>.json manifests, the archives and the chains' tar
// snapshot files.
type Store struct {
	Dir string
	mu  sync.Mutex // one backup at a time
	run sync.Mutex // guards the manifests
}

func newID(kind string, now time.Time) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return now.UTC().Format("20060102T150405Z") + "-" + kind + "-" + hex.EncodeToString(b)
}

// List returns the manifests, newest first.
func (s *Store) List() ([]Manifest, error) {
	s.run.Lock()
	defer s.run.Unlock()
	es, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Manifest{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Manifest{}
	for _, e := range es {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || !idRe.MatchString(id) {
			continue
		}
		m, err := s.read(id)
		if err == nil {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started.After(out[j].Started) })
	return out, nil
}

// Get returns one manifest.
func (s *Store) Get(id string) (Manifest, error) {
	if !idRe.MatchString(id) {
		return Manifest{}, ErrNotFound
	}
	s.run.Lock()
	defer s.run.Unlock()
	return s.read(id)
}

func (s *Store) read(id string) (Manifest, error) {
	var m Manifest
	b, err := os.ReadFile(filepath.Join(s.Dir, id+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return m, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(b, &m)
	}
	return m, err
}

func (s *Store) write(m Manifest) error {
	s.run.Lock()
	defer s.run.Unlock()
	b, _ := json.MarshalIndent(m, "", "  ")
	tmp := filepath.Join(s.Dir, m.ID+".json.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.Dir, m.ID+".json"))
}

// Open returns the archive of id for reading (download), with its manifest.
func (s *Store) Open(id string) (*os.File, Manifest, error) {
	m, err := s.Get(id)
	if err != nil {
		return nil, m, err
	}
	if m.Status != "ok" || m.File == "" {
		return nil, m, ErrNotFound
	}
	f, err := os.Open(filepath.Join(s.Dir, filepath.Base(m.File)))
	return f, m, err
}

// Dependents are the backups that need id (the incrementals of the chain it heads), oldest first.
func (s *Store) Dependents(id string) ([]Manifest, error) {
	list, err := s.List()
	if err != nil {
		return nil, err
	}
	var out []Manifest
	for _, m := range list {
		if m.Chain == id && m.ID != id {
			out = append(out, m)
		}
	}
	slices.Reverse(out)
	return out, nil
}

// Delete removes a backup; a full one takes its whole chain with it (the incrementals cannot be restored
// without it). It returns the ids removed.
func (s *Store) Delete(id string) ([]string, error) {
	if !s.mu.TryLock() { // a running backup may depend on what would go
		return nil, ErrBusy
	}
	defer s.mu.Unlock()
	return s.delete(id)
}

// delete is Delete under the run lock (Prune holds it already).
func (s *Store) delete(id string) ([]string, error) {
	m, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	ids := []string{id}
	if m.Full {
		deps, err := s.Dependents(id)
		if err != nil {
			return nil, err
		}
		for _, d := range deps {
			ids = append(ids, d.ID)
		}
	} else if m.Kind == KindFileData {
		// an incremental in the middle would break the ones after it: they go too
		deps, err := s.Dependents(m.Chain)
		if err != nil {
			return nil, err
		}
		after := false
		for _, d := range deps {
			if d.ID == id {
				after = true
				continue
			}
			if after {
				ids = append(ids, d.ID)
			}
		}
	}
	s.run.Lock()
	defer s.run.Unlock()
	for _, x := range ids {
		mm, err := s.read(x)
		if err != nil {
			continue
		}
		if mm.File != "" {
			_ = os.Remove(filepath.Join(s.Dir, filepath.Base(mm.File)))
		}
		_ = os.Remove(filepath.Join(s.Dir, x+".json"))
		if mm.Full {
			_ = os.Remove(filepath.Join(s.Dir, x+".snar"))
		}
	}
	if !m.Full && m.Kind == KindFileData {
		// the chain's snapshot already counts what the deleted incrementals held: the next backup of this chain
		// would leave those changes out, so the chain is closed (the next one starts a new full backup)
		_ = os.Remove(filepath.Join(s.Dir, m.Chain+".snar"))
	}
	return ids, nil
}

// Retention keeps, for one schedule, the newest Chains chains (0: no count limit) and the chains whose last
// backup is at most Days days old (0: no age limit); keep is never deleted (the backup just made).
type Retention struct {
	Chains int `json:"chains"`
	Days   int `json:"days"`
}

// Prune applies r to the backups of origin (a schedule) and returns the ids deleted.
func (s *Store) Prune(origin string, r Retention, keep string, now time.Time) ([]string, error) {
	if r.Chains <= 0 && r.Days <= 0 {
		return nil, nil
	}
	list, err := s.List() // newest first
	if err != nil {
		return nil, err
	}
	type chain struct {
		head string
		last time.Time
		keep bool
	}
	var chains []*chain
	byHead := map[string]*chain{}
	for _, m := range list {
		if m.Origin != origin || m.Kind != KindOf(keep) { // a schedule of "all" keeps each kind's chains apart
			continue
		}
		c := byHead[m.Chain]
		if c == nil {
			c = &chain{head: m.Chain, last: m.Started}
			byHead[m.Chain] = c
			chains = append(chains, c) // newest chain first (by its newest backup)
		}
		if m.ID == keep || m.Status == "running" {
			c.keep = true
		}
	}
	var gone []string
	for i, c := range chains {
		old := r.Days > 0 && now.Sub(c.last) > time.Duration(r.Days)*24*time.Hour
		over := r.Chains > 0 && i >= r.Chains
		if c.keep || !(old || over) {
			continue
		}
		if _, err := s.Get(c.head); err != nil {
			continue // the head was deleted by hand: its incrementals stay listed until deleted
		}
		ids, err := s.delete(c.head)
		if err != nil {
			return gone, err
		}
		gone = append(gone, ids...)
	}
	return gone, nil
}

// sha256File hashes a file.
func sha256File(p string) (string, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}

// freeBytes is the free space for root in dir (statfs; see free_*.go).
var freeBytes = func(dir string) (uint64, error) { return diskFree(dir) }

// checkSpace refuses to start with less free space than twice the last backup of kind.
func (s *Store) checkSpace(kind string) error {
	list, err := s.List()
	if err != nil {
		return err
	}
	for _, m := range list {
		if m.Kind == kind && m.Status == "ok" && m.Full {
			free, err := freeBytes(s.Dir)
			if err == nil && free < uint64(2*m.Size) {
				return fmt.Errorf("not enough free space in %s: %d MiB free, the last %s backup took %d MiB", s.Dir, free>>20, kind, m.Size>>20)
			}
			return nil
		}
	}
	return nil
}
