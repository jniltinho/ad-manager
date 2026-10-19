//go:build linux

package shares

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

// CreateFolder creates the folder with its owner group and permissions (with
// inheritance): a Windows ACL written by Samba when the server supports it
// (applyNTACL), else 0770 + setgid and POSIX ACLs. It creates and opens the
// folder component by component from the root's descriptor, without following
// links (mkdirFromRoot), checks through the descriptor itself that the folder
// is inside a root, and only then applies owner, mode and ACL — through the
// descriptor (/proc/<pid>/fd/N), so that swapping a path component for a
// symbolic link midway cannot lead root outside the allowed roots.
func (m *Manager) CreateFolder(ctx context.Context, r FolderReq) error {
	if err := m.ValidateFolder(&r); err != nil {
		return err
	}
	fd, err := m.mkdirFromRoot(r.Path)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)
	if err := m.ensureTraverse(filepath.Dir(r.Path)); err != nil {
		return err
	}
	self := fmt.Sprintf("/proc/%d/fd/%d", os.Getpid(), fd) // setfacl is another process: it needs our PID
	real, err := os.Readlink(self)
	if err != nil {
		return err
	}
	if _, err := m.SafePath(real); err != nil || real != r.Path {
		return fmt.Errorf("%w: %s changed during creation (now %s)", ErrInvalid, r.Path, real)
	}
	if m.NTACL && m.ACL != nil {
		return m.applyNTACL(ctx, fd, r)
	}
	if hasNTACL(self) {
		return fmt.Errorf("%w: %s already has Windows permissions and this server cannot write them (install python3-samba); setfacl would discard them", ErrInvalid, r.Path)
	}
	if r.Owner != "" {
		out, err := m.Run(ctx, "getent", "group", "--", r.Owner) // NSS/winbind (a static binary cannot resolve it on its own)
		if err != nil {
			return fmt.Errorf("group %s not resolved by winbind: %w", r.Owner, err)
		}
		f := strings.Split(strings.TrimSpace(out), ":") // name:x:gid:members
		if len(f) < 3 {
			return fmt.Errorf("getent group %s: unexpected output %q", r.Owner, out)
		}
		gid, err := strconv.Atoi(f[2])
		if err != nil {
			return fmt.Errorf("getent group %s: invalid gid %q", r.Owner, f[2])
		}
		chown := m.Chown
		if chown == nil {
			chown = func(_ string, uid, gid int) error { return syscall.Fchown(fd, uid, gid) }
		}
		if err := chown(self, 0, gid); err != nil {
			return err
		}
	}
	if err := syscall.Fchmod(fd, 0o2770); err != nil {
		return err
	}
	var acl []string
	for _, g := range slices.Concat(r.Full, r.RW) { // POSIX has no "full control": same as write
		acl = append(acl, "g:"+g+":rwx", "d:g:"+g+":rwx")
	}
	for _, g := range r.RO {
		acl = append(acl, "g:"+g+":rx", "d:g:"+g+":rx")
	}
	for _, u := range slices.Concat(r.FullUsers, r.RWUsers) {
		acl = append(acl, "u:"+u+":rwx", "d:u:"+u+":rwx")
	}
	for _, u := range r.ROUsers {
		acl = append(acl, "u:"+u+":rx", "d:u:"+u+":rx")
	}
	if len(acl) == 0 {
		return nil
	}
	_, err = m.Run(ctx, "setfacl", "-m", strings.Join(acl, ","), "--", self)
	return err
}

// mkdirFromRoot creates p one component at a time from a descriptor of its
// allowed root (Mkdirat + Openat with O_NOFOLLOW), never resolving a path: a
// component swapped for a symbolic link fails instead of leading root outside
// the root, and nothing is created there. Intermediate folders (not shared,
// e.g. E:\Users) get 0771: inside a share they inherit the parent's default
// ACL (rwx mask); outside one, others can only traverse them — the Windows
// "bypass traverse checking" behavior. The folder itself gets 0770. It returns
// a descriptor of the folder, which the caller closes.
func (m *Manager) mkdirFromRoot(p string) (int, error) {
	var root string
	for _, r := range m.Roots {
		if r = filepath.Clean(r); strings.HasPrefix(p, r+"/") && len(r) > len(root) {
			root = r
		}
	}
	if root == "" {
		return -1, fmt.Errorf("%w: %q is outside the allowed roots %v", ErrInvalid, p, m.Roots)
	}
	const flags = syscall.O_RDONLY | syscall.O_DIRECTORY | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
	fd, err := syscall.Open(root, flags, 0)
	if err != nil {
		return -1, fmt.Errorf("%w: %s is not a regular folder: %v", ErrInvalid, root, err)
	}
	parts := strings.Split(strings.TrimPrefix(p, root+"/"), "/")
	for i, name := range parts {
		cur := filepath.Join(root, filepath.Join(parts[:i+1]...))
		mode := uint32(0o771)
		if i == len(parts)-1 {
			mode = 0o770
		}
		if err := syscall.Mkdirat(fd, name, mode); err != nil && !errors.Is(err, syscall.EEXIST) {
			syscall.Close(fd)
			return -1, &os.PathError{Op: "mkdir", Path: cur, Err: err}
		}
		next, err := syscall.Openat(fd, name, flags, 0)
		syscall.Close(fd)
		if err != nil {
			return -1, fmt.Errorf("%w: %s is not a regular folder: %v", ErrInvalid, cur, err)
		}
		fd = next
	}
	return fd, nil
}

// ensureTraverse grants o+x (traverse without listing) from dir up to the
// root, exclusive: without it a nested share (Users/Publico, Comercial/Contrato)
// is unreachable for anyone without permission on the parent folder. Each
// folder is opened without following links and checked through its descriptor
// before Fchmod.
func (m *Manager) ensureTraverse(dir string) error {
	var root string
	for _, r := range m.Roots {
		if strings.HasPrefix(dir+"/", strings.TrimRight(r, "/")+"/") {
			root = strings.TrimRight(r, "/")
		}
	}
	for p := dir; root != "" && p != root && strings.HasPrefix(p, root+"/"); p = filepath.Dir(p) {
		fd, err := syscall.Open(p, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if err != nil {
			return fmt.Errorf("%w: %s is not a regular folder: %v", ErrInvalid, p, err)
		}
		real, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
		var st syscall.Stat_t
		if err == nil && real == p && hasNTACL(fmt.Sprintf("/proc/self/fd/%d", fd)) {
			// a folder with a Windows ACL: changing its mode would make Samba
			// drop that ACL (acl_xattr hash); traverse comes from the ACL itself
		} else if err == nil && real == p {
			if err = syscall.Fstat(fd, &st); err == nil && st.Mode&0o001 == 0 {
				err = syscall.Fchmod(fd, st.Mode&0o7777|0o001)
			}
		} else if err == nil {
			err = fmt.Errorf("%w: %s changed during creation (now %s)", ErrInvalid, p, real)
		}
		syscall.Close(fd)
		if err != nil {
			return err
		}
	}
	return nil
}
