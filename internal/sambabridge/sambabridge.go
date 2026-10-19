// Package sambabridge runs the Python helper embedded in the binary
// (bridge.py), which uses Samba's own Python API (python3-samba) as root:
// Windows (NT) ACLs written through smbd's VFS stack, so the NTACL xattr and
// the POSIX ACL stay in step and Explorer shows real permissions.
//
// The helper runs as `python3 -I -s -B -c <code>` with a clean environment and
// stays up (helper_unix.go): requests go one at a time over a Unix socket, each
// with a timeout and a capped answer. A folder is handed over as an already
// opened and checked descriptor (SCM_RIGHTS), never only by path.
package sambabridge

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

//go:embed bridge.py
var script string

// ErrInvalid reports a request the helper rejected (bad path, SID, entry).
var ErrInvalid = errors.New("invalid input")

// ErrUnavailable reports that python3-samba is not usable on this server.
var ErrUnavailable = errors.New("Samba Python API unavailable (install python3-samba)")

const maxOutput = 4 << 20

// Bridge runs the helper. The zero value uses /usr/bin/python3 and a 60 s timeout.
type Bridge struct {
	Python  string
	Timeout time.Duration
	// GoRead reads ACLs in Go when Samba would return the stored descriptor as is (ntacl.go); the helper answers
	// the rest. Set it from Caps.GoReadable.
	GoRead bool
	// run executes the helper; tests replace it.
	run func(ctx context.Context, stdin []byte, folders []*os.File) ([]byte, error)
}

// Caps is what the helper reports about this server.
type Caps struct {
	NTACL    bool   `json:"nt_acl"`    // python3-samba importable
	ACLXattr bool   `json:"acl_xattr"` // vfs objects = acl_xattr in [global]
	Samba    string `json:"samba"`
	Python   string `json:"python"`
	Role     string `json:"role"` // server role of smb.conf ("active directory domain controller" on a DC)
	// NTACLXattr is acl_xattr:security_acl_name in [global]; nil when not set (set empty counts as set)
	NTACLXattr *string  `json:"ntacl_xattr"`
	VFSObjects []string `json:"vfs_objects,omitempty"` // vfs objects of [global]
	Error      string   `json:"error,omitempty"`
}

// goReadVFS are the modules that leave the Windows ACL where the Go reader looks (security.NTACL on the disk, checked
// against the POSIX ACL): a DC's default stack and modules that do not touch ACLs. Any other (xattr_tdb keeps the
// xattrs in a database, zfsacl and friends keep other ACLs) leaves reads to the helper.
var goReadVFS = map[string]bool{"dfs_samba4": true, "acl_xattr": true, "recycle": true, "full_audit": true}

// GoReadable reports whether the Go reader sees what Samba sees: acl_xattr in [global] with its default xattr name,
// and only modules of goReadVFS.
func (c Caps) GoReadable() bool {
	if !c.Usable() || c.NTACLXattr != nil || len(c.VFSObjects) == 0 {
		return false
	}
	for _, m := range c.VFSObjects {
		if !goReadVFS[m] {
			return false
		}
	}
	return true
}

// Usable reports whether folders can get Windows ACLs on this server.
func (c Caps) Usable() bool { return c.NTACL && c.ACLXattr }

// ACE is one access control entry.
type ACE struct {
	SID       string `json:"sid"`
	Name      string `json:"name,omitempty"` // resolved by the caller (winbind)
	Type      string `json:"type"`           // allow | deny
	Mask      uint32 `json:"mask"`
	Flags     int    `json:"flags"`
	Inherited bool   `json:"inherited"`
	Access    string `json:"access"` // full | rw | ro | custom
}

// ACL is a folder's Windows security descriptor.
type ACL struct {
	Owner     string `json:"owner"`
	OwnerName string `json:"owner_name,omitempty"`
	Group     string `json:"group"`
	GroupName string `json:"group_name,omitempty"`
	SDDL      string `json:"sddl"`
	ACEs      []ACE  `json:"aces"`
	Protected bool   `json:"protected"` // does not inherit from the parent
	NullDACL  bool   `json:"null_dacl"` // no DACL at all (everyone has access)
	// with Propagate: entries under the folder updated and skipped (links, special files, other file systems)
	Propagated int `json:"propagated,omitempty"`
	Skipped    int `json:"skipped,omitempty"`
}

// Entry grants access to a SID: rw (Modify), ro (Read & execute) or full.
type Entry struct {
	SID    string `json:"sid"`
	Access string `json:"access"`
}

// FolderACL is the Windows ACL to apply to a folder. Administrators and
// SYSTEM always get full control (the helper adds them).
type FolderACL struct {
	Path    string   `json:"path"`
	Roots   []string `json:"roots"`
	Service string   `json:"service,omitempty"` // share name, when the folder is one
	Owner   string   `json:"owner_sid"`
	Group   string   `json:"group_sid"`
	Entries []Entry  `json:"entries"`
	// Propagate recomputes the inherited entries of everything under the folder (an existing folder whose ACL changes)
	Propagate bool `json:"propagate,omitempty"`
}

// propagateTimeout bounds a call that walks the folder's tree.
const propagateTimeout = 30 * time.Minute

// Capabilities asks the helper what this server supports. Errors are folded
// into Caps.Error (the agent keeps working on the POSIX path).
func (b *Bridge) Capabilities(ctx context.Context) Caps {
	var c Caps
	if err := b.call(ctx, "capabilities", map[string]any{}, nil, &c); err != nil {
		c.Error = err.Error()
	}
	return c
}

// GetACL reads the Windows ACL of an opened folder.
func (b *Bridge) GetACL(ctx context.Context, dir *os.File, path string, roots []string, service string) (*ACL, error) {
	if b.GoRead && service == "" { // a share's own options could differ from [global]: the helper reads those
		if a, err := readNTACL(dir, path, roots); err == nil {
			return a, nil
		}
	}
	var a ACL
	args := map[string]any{"path": path, "roots": roots, "service": service}
	if err := b.call(ctx, "get_acl", args, dir, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// MaxBatch is the most folders GetACLs reads in one request (their descriptors go in one SCM_RIGHTS message,
// and Linux takes 253).
const MaxBatch = 250

// FolderRef names an opened folder of GetACLs: its path and, when it is one, its share.
type FolderRef struct {
	Path    string `json:"path"`
	Service string `json:"service,omitempty"`
}

// GetACLs reads the Windows ACLs of several opened folders (dirs[i] is refs[i]) in one helper process. The result
// has one ACL per folder, nil where that folder failed (its error in errs[i]).
func (b *Bridge) GetACLs(ctx context.Context, dirs []*os.File, refs []FolderRef, roots []string) (acls []*ACL, errs []error, err error) {
	if len(dirs) != len(refs) || len(refs) == 0 || len(refs) > MaxBatch {
		return nil, nil, fmt.Errorf("%w: 1 to %d folders, one descriptor each", ErrInvalid, MaxBatch)
	}
	acls, errs = make([]*ACL, len(refs)), make([]error, len(refs))
	var left []int // the folders the helper reads
	for i, r := range refs {
		if b.GoRead && r.Service == "" {
			if a, err := readNTACL(dirs[i], r.Path, roots); err == nil {
				acls[i] = a
				continue
			}
		}
		left = append(left, i)
	}
	if len(left) == 0 {
		return acls, errs, nil
	}
	askDirs, askRefs := make([]*os.File, len(left)), make([]FolderRef, len(left))
	for j, i := range left {
		askDirs[j], askRefs[j] = dirs[i], refs[i]
	}
	var res struct {
		Items []struct {
			ACL   *ACL   `json:"acl"`
			Error string `json:"error"`
		} `json:"items"`
	}
	if err := b.callFolders(ctx, "get_acls", map[string]any{"items": askRefs, "roots": roots}, askDirs, &res); err != nil {
		return nil, nil, err
	}
	if len(res.Items) != len(left) {
		return nil, nil, fmt.Errorf("samba bridge: %d answers for %d folders", len(res.Items), len(left))
	}
	for j, it := range res.Items {
		i := left[j]
		if it.ACL == nil {
			errs[i] = fmt.Errorf("samba bridge: %s: %s", refs[i].Path, it.Error)
			continue
		}
		acls[i] = it.ACL
	}
	return acls, errs, nil
}

// SetFolderACL replaces the Windows ACL of an opened folder and returns the
// result as Samba stores it.
func (b *Bridge) SetFolderACL(ctx context.Context, dir *os.File, req FolderACL) (*ACL, error) {
	var a ACL
	if err := b.walking(req.Propagate).call(ctx, "set_folder_acl", req, dir, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// SetSDDL puts back a security descriptor (SDDL from the ACL history) on an opened folder, optionally passing it on
// to the contents, and returns the result as Samba stores it.
func (b *Bridge) SetSDDL(ctx context.Context, dir *os.File, path string, roots []string, sddl string, propagate bool) (*ACL, error) {
	var a ACL
	args := map[string]any{"path": path, "roots": roots, "sddl": sddl, "propagate": propagate}
	if err := b.walking(propagate).call(ctx, "set_acl_sddl", args, dir, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// Reapply writes sddl on the folder again and passes it on to everything under it; an entry with no stored
// Windows ACL of its own (written without Samba, by a sync) keeps only the inherited permissions, never the ones
// Samba would synthesize from its mode bits.
func (b *Bridge) Reapply(ctx context.Context, dir *os.File, path string, roots []string, sddl string) (*ACL, error) {
	var a ACL
	args := map[string]any{"path": path, "roots": roots, "sddl": sddl, "propagate": true, "reset_unstored": true}
	if err := b.walking(true).call(ctx, "set_acl_sddl", args, dir, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// walking is b with room for a tree walk when propagate is set.
func (b *Bridge) walking(propagate bool) *Bridge {
	if !propagate || b.Timeout >= propagateTimeout {
		return b
	}
	w := *b
	w.Timeout = propagateTimeout
	return &w
}

// call runs one request and decodes the result into out.
func (b *Bridge) call(ctx context.Context, op string, args any, folder *os.File, out any) error {
	var folders []*os.File
	if folder != nil {
		folders = []*os.File{folder}
	}
	return b.callFolders(ctx, op, args, folders, out)
}

// callFolders is call with the folders as descriptors 3, 4, … in the helper.
func (b *Bridge) callFolders(ctx context.Context, op string, args any, folders []*os.File, out any) error {
	in, err := json.Marshal(map[string]any{"op": op, "args": args})
	if err != nil {
		return err
	}
	timeout := b.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	run := b.run
	if run == nil {
		run = b.exec
	}
	raw, err := run(ctx, in, folders)
	if err != nil {
		return err
	}
	var resp struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
		Code   string          `json:"code"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("samba bridge: invalid response: %w", err)
	}
	if !resp.OK {
		if resp.Code == "invalid" {
			return fmt.Errorf("%w: %s", ErrInvalid, resp.Error)
		}
		return fmt.Errorf("samba bridge: %s", resp.Error)
	}
	return json.Unmarshal(resp.Result, out)
}

// trusted refuses an interpreter that is missing or that someone other than
// root could replace.
func trusted(path string) error {
	real, err := filepathEval(path)
	if err != nil {
		return ErrUnavailable
	}
	fi, err := os.Stat(real)
	if err != nil {
		return ErrUnavailable
	}
	if fi.Mode().Perm()&0o022 != 0 || !ownedByRoot(fi) {
		return fmt.Errorf("samba bridge: %s is writable by non-root users", real)
	}
	return nil
}

// capped is an io.Writer that keeps at most max bytes. The buffer is a named field: an embedded one would lend
// capped its ReadFrom, and the copy from the helper's pipe would bypass Write (and the cap).
type capped struct {
	buf bytes.Buffer
	max int
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); room > 0 {
		if len(p) > room {
			c.buf.Write(p[:room])
		} else {
			c.buf.Write(p)
		}
	}
	return len(p), nil
}

// Bytes is what was kept.
func (c *capped) Bytes() []byte { return c.buf.Bytes() }

var _ io.Writer = (*capped)(nil)

// ShareConf is a share in Samba's registry configuration: its name and parameters in order.
type ShareConf struct {
	Name   string      `json:"name"`
	Params [][2]string `json:"params"`
}

// ListShares reads the shares of the registry configuration (what `net conf list` shows, without [global]).
func (b *Bridge) ListShares(ctx context.Context) ([]ShareConf, error) {
	var list []ShareConf
	if err := b.call(ctx, "list_shares", map[string]any{}, nil, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// SetShare creates or replaces a share in one registry transaction. vfs lists modules (full_audit, recycle) the
// share adds in front of the global ones; the helper refuses parameters outside its allowed list.
func (b *Bridge) SetShare(ctx context.Context, name string, params [][2]string, vfs []string) (*ShareConf, error) {
	var sc ShareConf
	if err := b.call(ctx, "set_share", map[string]any{"name": name, "params": params, "vfs": vfs}, nil, &sc); err != nil {
		return nil, err
	}
	return &sc, nil
}

// DeleteShare removes a share from the registry configuration (no error when it is not there).
func (b *Bridge) DeleteShare(ctx context.Context, name string) error {
	return b.call(ctx, "delete_share", map[string]any{"name": name}, nil, &struct{}{})
}
