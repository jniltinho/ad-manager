package sambabridge

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	"golang.org/x/sys/unix"
)

// goReads counts the reads answered in Go (the parity test checks the Go path ran).
var goReads atomic.Int64

// readNTACL reads the Windows ACL of an opened folder without the helper, when Samba would return the stored
// descriptor as is; otherwise errNotMine (the helper answers, with its own checks and messages).
func readNTACL(dir *os.File, path string, roots []string) (*ACL, error) {
	fd := int(dir.Fd())
	if !sameFolder(fd, path, roots) {
		return nil, errNotMine
	}
	blob, err := xattr(fd, ntACLXattr, maxNTACLBlob)
	if err != nil {
		return nil, errNotMine
	}
	sd, sysHash, err := parseNTACL(blob)
	if err != nil {
		return nil, err
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR {
		return nil, errNotMine
	}
	access, err := posixACL(fd, "system.posix_acl_access")
	if errors.Is(err, unix.ENODATA) {
		access, err = modeACL(st.Mode), nil
	}
	if err != nil {
		return nil, errNotMine
	}
	def, err := posixACL(fd, "system.posix_acl_default")
	if errors.Is(err, unix.ENODATA) {
		def, err = []posixEntry{}, nil // a directory without a default ACL: an empty one, not NULL
	}
	if err != nil {
		return nil, errNotMine
	}
	if !bytes.Equal(posixACLHash(access, def, st.Uid, st.Gid, st.Mode), sysHash) {
		return nil, errNotMine // changed outside Samba (setfacl, chmod): Samba maps the POSIX ACL instead
	}
	a, err := describe(sd)
	if err == nil {
		goReads.Add(1)
	}
	return a, err
}

func xattr(fd int, name string, max int) ([]byte, error) {
	buf := make([]byte, max)
	n, err := unix.Fgetxattr(fd, name, buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func posixACL(fd int, name string) ([]posixEntry, error) {
	b, err := xattr(fd, name, 8<<10)
	if err != nil {
		return nil, err
	}
	return parsePosixXattr(b)
}

// sameFolder is the helper's checked_folder: an absolute path without .., inside an allowed root and outside the
// system directories, that the descriptor is (a directory, by /proc/self/fd).
func sameFolder(fd int, path string, roots []string) bool {
	if !strings.HasPrefix(path, "/") || strings.Contains(path, "\x00") || len(roots) == 0 {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == ".." {
			return false
		}
	}
	path = filepath.Clean(path)
	for _, sys := range []string{"/etc", "/var/lib/samba", "/proc", "/sys", "/dev", "/usr", "/boot", "/root"} {
		if path == sys || strings.HasPrefix(path, sys+"/") {
			return false
		}
	}
	inside := false
	for _, r := range roots {
		if !strings.HasPrefix(r, "/") || filepath.Clean(r) == "/" {
			return false
		}
		if strings.HasPrefix(path, filepath.Clean(r)+"/") {
			inside = true
		}
	}
	real, err := os.Readlink("/proc/self/fd/" + strconv.Itoa(fd))
	return inside && err == nil && real == path
}
