//go:build linux

package shares

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"ad-manager/internal/sambabridge"
)

var sidRe = regexp.MustCompile(`^S-1-(5-21-\d+-\d+-\d+-\d+|22-[12]-\d+|5-32-\d+|5-18|3-0|1-0|5-11)$`)

// applyNTACL gives the opened folder a Windows ACL through Samba (the helper
// writes it via smbd, which also derives the POSIX ACL): owner Domain Admins,
// group = the owner group, write groups/users with Modify, read ones with Read
// & execute, inheritance to subfolders and files. Administrators and SYSTEM
// always keep full control.
func (m *Manager) applyNTACL(ctx context.Context, fd int, r FolderReq) error {
	owner, err := m.domainAdmins(ctx)
	if err != nil {
		return err
	}
	group := owner
	if owner == rootSID {
		group = "S-1-22-2-0" // a group SID (root's group): a user SID cannot be the descriptor's group
	}
	if r.Owner != "" {
		if group, err = m.SID(ctx, r.Owner, false); err != nil {
			return err
		}
	}
	var entries []sambabridge.Entry
	add := func(names []string, user bool, access string) error {
		for _, n := range names {
			sid, err := m.SID(ctx, n, user)
			if err != nil {
				return err
			}
			entries = append(entries, sambabridge.Entry{SID: sid, Access: access})
		}
		return nil
	}
	for _, step := range []struct {
		names  []string
		user   bool
		access string
	}{{r.Full, false, "full"}, {r.FullUsers, true, "full"}, {r.RW, false, "rw"}, {r.RO, false, "ro"}, {r.RWUsers, true, "rw"}, {r.ROUsers, true, "ro"}} {
		if err := add(step.names, step.user, step.access); err != nil {
			return err
		}
	}
	dir, err := dupFile(fd, r.Path)
	if err != nil {
		return err
	}
	defer dir.Close()
	// replacing an existing Windows ACL (share edit): keep the previous one in
	// the history so it can be put back
	var before *sambabridge.ACL
	if hasNTACL(fmt.Sprintf("/proc/%d/fd/%d", os.Getpid(), fd)) {
		if before, err = m.ACL.GetACL(ctx, dir, r.Path, m.Roots, ""); err != nil {
			return fmt.Errorf("reading the current ACL of %s: %w", r.Path, err)
		}
		// persisted before the change: if writing it fails, nothing changes
		if err := m.recordACL(r.Path, before, nil); err != nil {
			return err
		}
	}
	// an existing folder passes the new ACL on to its contents (a new one is empty)
	after, err := m.ACL.SetFolderACL(ctx, dir, sambabridge.FolderACL{Path: r.Path, Roots: m.Roots, Owner: owner, Group: group,
		Entries: entries, Propagate: before != nil})
	if errors.Is(err, sambabridge.ErrInvalid) {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err != nil {
		return err
	}
	return m.recordACL(r.Path, nil, after)
}

// recordACL appends a folder's SDDL to the history file (JSON lines, 0600):
// one line with "before" ahead of a change, one with "after" once it is done —
// what is needed to put a folder's permissions back.
func (m *Manager) recordACL(path string, before, after *sambabridge.ACL) error {
	if m.History == "" {
		return nil
	}
	sddl := func(a *sambabridge.ACL) string {
		if a == nil {
			return ""
		}
		return a.SDDL
	}
	line, _ := json.Marshal(map[string]string{"time": time.Now().UTC().Format(time.RFC3339Nano), "path": path,
		"before": sddl(before), "after": sddl(after)})
	f, err := os.OpenFile(m.History, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("ACL history: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return fmt.Errorf("ACL history: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("ACL history: %w", err)
	}
	return f.Close()
}

// ReadACL returns the Windows ACL of a folder inside the roots, with the SIDs
// resolved to names (winbind) for display.
func (m *Manager) ReadACL(ctx context.Context, path string) (*sambabridge.ACL, error) {
	if m.ACL == nil || !m.NTACL {
		return nil, sambabridge.ErrUnavailable
	}
	p, err := m.SafePath(path)
	if err != nil {
		return nil, err
	}
	fd, err := syscall.Open(p, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: %s is not a regular folder: %v", ErrInvalid, p, err)
	}
	defer syscall.Close(fd)
	dir, err := dupFile(fd, p)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	acl, err := m.ACL.GetACL(ctx, dir, p, m.Roots, "")
	if err != nil {
		if errors.Is(err, sambabridge.ErrInvalid) {
			return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
		return nil, err
	}
	names := map[string]string{}
	name := func(sid string) string {
		if n, ok := names[sid]; ok {
			return n
		}
		n := m.sidName(ctx, sid)
		names[sid] = n
		return n
	}
	acl.OwnerName, acl.GroupName = name(acl.Owner), name(acl.Group)
	for i := range acl.ACEs {
		acl.ACEs[i].Name = name(acl.ACEs[i].SID)
	}
	return acl, nil
}

// FolderACLs reads the Windows ACLs of the given folders with SIDs only (no names): the share list marks Full control
// from them. With the batch of the Samba helper they come from one process (a Python start per folder took the
// most); a folder that cannot be read has no entry.
func (m *Manager) FolderACLs(ctx context.Context, paths []string) map[string]*sambabridge.ACL {
	out := map[string]*sambabridge.ACL{}
	if m.ACL == nil || !m.NTACL {
		return out
	}
	batch, ok := m.ACL.(interface {
		GetACLs(context.Context, []*os.File, []sambabridge.FolderRef, []string) ([]*sambabridge.ACL, []error, error)
	})
	// 32 folders per helper: the answer stays well under the helper's output cap even with long ACLs
	const per = 32
	for start := 0; start < len(paths); start += per {
		var dirs []*os.File
		var refs []sambabridge.FolderRef
		var asked []string
		for _, path := range paths[start:min(len(paths), start+per)] {
			p, err := m.SafePath(path)
			if err != nil {
				continue
			}
			fd, err := syscall.Open(p, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
			if err != nil {
				continue
			}
			dir, err := dupFile(fd, p)
			syscall.Close(fd)
			if err != nil {
				continue
			}
			dirs, refs, asked = append(dirs, dir), append(refs, sambabridge.FolderRef{Path: p}), append(asked, path)
		}
		if len(dirs) > 0 {
			var acls []*sambabridge.ACL
			var err error = sambabridge.ErrUnavailable
			if ok {
				acls, _, err = batch.GetACLs(ctx, dirs, refs, m.Roots)
			}
			if err == nil {
				for i, a := range acls {
					if a != nil {
						out[asked[i]] = a
					}
				}
			} else { // no batch, or the whole batch failed (e.g. an answer too long): one folder at a time
				for i, dir := range dirs {
					if a, err := m.ACL.GetACL(ctx, dir, refs[i].Path, m.Roots, ""); err == nil {
						out[asked[i]] = a
					}
				}
			}
		}
		for _, d := range dirs {
			d.Close()
		}
	}
	return out
}

// SID resolves a group or user name to a SID: winbind first (domain
// accounts, full SIDs), else the Unix SID of a local group/user
// (S-1-22-2-gid / S-1-22-1-uid, standalone servers).
func (m *Manager) SID(ctx context.Context, name string, user bool) (string, error) {
	if out, err := m.Run(ctx, "wbinfo", "--name-to-sid", name); err == nil {
		if f := strings.Fields(out); len(f) > 0 && sidRe.MatchString(f[0]) {
			return f[0], nil
		}
	}
	// a domain account (DOMAIN\name) must come from winbind: a Unix SID made
	// from its uid/gid would be a different principal for Windows
	if strings.Contains(name, `\`) {
		return "", fmt.Errorf("%w: %q not resolved by winbind", ErrInvalid, name)
	}
	db, prefix := "group", "S-1-22-2-"
	if user {
		db, prefix = "passwd", "S-1-22-1-"
	}
	out, err := m.Run(ctx, "getent", db, "--", name)
	if err != nil {
		return "", fmt.Errorf("%w: %q not found (winbind/getent)", ErrInvalid, name)
	}
	f := strings.Split(strings.TrimSpace(out), ":")
	if len(f) < 3 || f[2] == "" {
		return "", fmt.Errorf("getent %s %s: unexpected output %q", db, name, out)
	}
	return prefix + f[2], nil
}

// domainAdmins is the owner SID of the folders: DOMAIN\Domain Admins on a
// member, root (S-1-22-1-0) on a standalone server.
func (m *Manager) domainAdmins(ctx context.Context) (string, error) {
	if out, err := m.Run(ctx, "wbinfo", "--own-domain"); err == nil && strings.TrimSpace(out) != "" {
		dom := strings.TrimSpace(out)
		if sid, err := m.SID(ctx, dom+`\Domain Admins`, false); err == nil && !strings.HasPrefix(sid, "S-1-22-") {
			return sid, nil
		}
		// the group renamed (or localized): the domain's SID with RID 512
		if info, err := m.Run(ctx, "wbinfo", "-D", dom); err == nil {
			for _, l := range strings.Split(info, "\n") {
				if k, v, ok := strings.Cut(l, ":"); ok && strings.TrimSpace(k) == "SID" && sidRe.MatchString(strings.TrimSpace(v)+"-512") {
					return strings.TrimSpace(v) + "-512", nil // wbinfo -D gives the domain SID, without a RID
				}
			}
		}
	}
	if m.DC { // a DC never gives folders to root by default
		return "", fmt.Errorf("%w: Domain Admins of this domain did not resolve", ErrInvalid)
	}
	return rootSID, nil
}

// adminsName is this domain's Domain Admins as winbind names it (DOMAIN\Domain Admins, or the name it was renamed
// to: found by its RID 512); "" only on a standalone server. On a member or a DC a group that does not resolve is an
// error, never a share without its administrators.
func (m *Manager) adminsName(ctx context.Context) (string, error) {
	role, err := m.Run(ctx, "testparm", "-s", "--parameter-name=server role")
	if err != nil {
		return "", fmt.Errorf("server role (testparm): %w", err)
	}
	if strings.Contains(strings.ToLower(role), "standalone") {
		return "", nil
	}
	sid, err := m.domainAdmins(ctx)
	if err == nil && sid == rootSID {
		err = fmt.Errorf("%w: Domain Admins of this domain did not resolve (winbind)", ErrInvalid)
	}
	if err != nil {
		return "", err
	}
	n := m.sidName(ctx, sid)
	if n == sid {
		return "", fmt.Errorf("%w: Domain Admins (%s) has no name (winbind)", ErrInvalid, sid)
	}
	return n, nil
}

// rootSID is root's Unix SID, the owner on a standalone server.
const rootSID = "S-1-22-1-0"

// sidName resolves a SID to "DOMAIN\name" for display (the SID when unknown).
func (m *Manager) sidName(ctx context.Context, sid string) string {
	if !sidRe.MatchString(sid) {
		return sid
	}
	out, err := m.Run(ctx, "wbinfo", "--sid-to-name", sid)
	if err != nil {
		return sid
	}
	n := strings.TrimSpace(out)
	if i := strings.LastIndex(n, " "); i > 0 { // "DOMAIN\name 2" (type suffix)
		n = n[:i]
	}
	return n
}

// hasNTACL reports whether the folder already holds a Windows ACL (xattr
// security.NTACL): setfacl on it would make Samba silently drop that ACL.
func hasNTACL(path string) bool {
	_, err := syscall.Getxattr(path, "security.NTACL", nil)
	return err == nil
}

// dupFile hands an independent copy of the descriptor to the helper (fd 3).
func dupFile(fd int, path string) (*os.File, error) {
	d, err := syscall.Dup(fd)
	if err != nil {
		return nil, err
	}
	syscall.CloseOnExec(d) // exec.Cmd clears it for ExtraFiles
	return os.NewFile(uintptr(d), path), nil
}

// maxHistory is how many changes of a folder ACLHistory returns (newest first).
const maxHistory = 50

// ACLHistory lists the recorded ACL changes of a folder inside the roots, newest first (at most maxHistory: only
// that many are kept while reading).
func (m *Manager) ACLHistory(path string) ([]ACLChange, error) {
	p, err := m.SafePath(path)
	if err != nil {
		return nil, err
	}
	ring := make([]ACLChange, 0, maxHistory)
	n := 0
	err = m.eachChange(func(c ACLChange) bool {
		if c.Path == p {
			if len(ring) < maxHistory {
				ring = append(ring, c)
			} else {
				ring[n%maxHistory] = c
			}
			n++
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	// oldest first from the ring's start, then reversed
	k := 0
	if n > maxHistory {
		k = n % maxHistory
	}
	list := append(ring[k:len(ring):len(ring)], ring[:k]...)
	slices.Reverse(list)
	return list, nil
}

// eachChange reads the history file line by line until fn returns false (no file: no changes).
func (m *Manager) eachChange(fn func(ACLChange) bool) error {
	if m.History == "" {
		return nil
	}
	f, err := os.Open(m.History)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("ACL history: %w", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var c ACLChange
		if json.Unmarshal(sc.Bytes(), &c) == nil && !fn(c) {
			return nil
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("ACL history: %w", err)
	}
	return nil
}

// restoreSDDL finds the SDDL to put back in the whole history: the "before" of the line with that path and time
// (a line older than what ACLHistory shows still counts).
func (m *Manager) restoreSDDL(r *RestoreReq) (string, error) {
	if !m.NTACL || m.ACL == nil {
		return "", sambabridge.ErrUnavailable
	}
	p, err := m.SafePath(r.Path)
	if err != nil {
		return "", err
	}
	sddl := ""
	err = m.eachChange(func(c ACLChange) bool {
		if c.Path == p && c.Time == r.Time && c.Before != "" {
			sddl = c.Before
			return false
		}
		return true
	})
	if err != nil {
		return "", err
	}
	if sddl == "" {
		return "", fmt.Errorf("%w: no ACL recorded for %s at %q", ErrInvalid, r.Path, r.Time)
	}
	r.Path = p
	return sddl, nil
}

// ValidateRestore checks that the history has what RestoreACL would put back.
func (m *Manager) ValidateRestore(r *RestoreReq) error {
	_, err := m.restoreSDDL(r)
	return err
}

// RestoreACL puts back the Windows ACL a folder had before a recorded change and passes it on to the folder's
// contents. The current ACL goes to the history first, so the restore can be undone too.
func (m *Manager) RestoreACL(ctx context.Context, r RestoreReq) (*sambabridge.ACL, error) {
	sddl, err := m.restoreSDDL(&r)
	if err != nil {
		return nil, err
	}
	fd, err := syscall.Open(r.Path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: %s is not a regular folder: %v", ErrInvalid, r.Path, err)
	}
	defer syscall.Close(fd)
	real, err := os.Readlink(fmt.Sprintf("/proc/%d/fd/%d", os.Getpid(), fd))
	if err != nil {
		return nil, err
	}
	if _, err := m.SafePath(real); err != nil || real != r.Path {
		return nil, fmt.Errorf("%w: %s changed (now %s)", ErrInvalid, r.Path, real)
	}
	dir, err := dupFile(fd, r.Path)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	cur, err := m.ACL.GetACL(ctx, dir, r.Path, m.Roots, "")
	if err != nil {
		return nil, fmt.Errorf("reading the current ACL of %s: %w", r.Path, err)
	}
	if err := m.recordACL(r.Path, cur, nil); err != nil {
		return nil, err
	}
	after, err := m.ACL.SetSDDL(ctx, dir, r.Path, m.Roots, sddl, true)
	if errors.Is(err, sambabridge.ErrInvalid) {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err != nil {
		return nil, err
	}
	return after, m.recordACL(r.Path, nil, after)
}

// Reinherit applies the Windows ACL of folder path (inside the roots, no link) again to everything under it, so
// files written there without Samba (a sync) take the inheritance a Windows client would see.
func (m *Manager) Reinherit(ctx context.Context, path string) (*sambabridge.ACL, error) {
	if !m.NTACL {
		return nil, sambabridge.ErrUnavailable
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: %s is not a regular folder: %v", ErrInvalid, path, err)
	}
	defer syscall.Close(fd)
	real, err := os.Readlink(fmt.Sprintf("/proc/%d/fd/%d", os.Getpid(), fd))
	if err != nil {
		return nil, err
	}
	if _, err := m.SafePath(real); err != nil || real != path {
		return nil, fmt.Errorf("%w: %s changed (now %s)", ErrInvalid, path, real)
	}
	dir, err := dupFile(fd, path)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	cur, err := m.ACL.GetACL(ctx, dir, path, m.Roots, "")
	if err != nil {
		return nil, fmt.Errorf("reading the ACL of %s: %w", path, err)
	}
	return m.ACL.Reapply(ctx, dir, path, m.Roots, cur.SDDL)
}
