package backup

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
)

// pinDir opens the directory dir (never through a link) and returns a path to it that a rename of dir (or of a
// folder above it) cannot redirect: /proc/<agent>/fd/<n>, valid while the returned file stays open.
func pinDir(dir string) (string, *os.File, error) {
	f, err := os.OpenFile(dir, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("/proc/%d/fd/%d", os.Getpid(), f.Fd()), f, nil
}

// makeRestoreDir creates root/base/name through open directories, never a path again: base (made 0700 if missing)
// must be a real folder of this process's user that nobody else writes, and name is new. A user of the share who
// renames folders meanwhile cannot send it anywhere else. It returns the pinned path of root/base/name.
func makeRestoreDir(root, base, name string) (string, *os.File, error) {
	rf, err := openNoFollow(root, false)
	if err != nil {
		return "", nil, fmt.Errorf("%s: %w", root, err)
	}
	defer rf.Close()
	rfd := int(rf.Fd())
	if err := syscall.Mkdirat(rfd, base, 0o700); err != nil && !errors.Is(err, syscall.EEXIST) {
		return "", nil, fmt.Errorf("%s/%s: %w", root, base, err)
	}
	const dirFlags = syscall.O_RDONLY | syscall.O_DIRECTORY | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
	bfd, err := syscall.Openat(rfd, base, dirFlags, 0)
	if err != nil {
		return "", nil, fmt.Errorf("%s/%s must be a folder of root that only root writes", root, base)
	}
	defer syscall.Close(bfd)
	var st syscall.Stat_t
	if err := syscall.Fstat(bfd, &st); err != nil || st.Uid != uint32(os.Geteuid()) || st.Mode&0o022 != 0 {
		return "", nil, fmt.Errorf("%s/%s must be a folder of root that only root writes", root, base)
	}
	if err := syscall.Mkdirat(bfd, name, 0o700); err != nil {
		return "", nil, fmt.Errorf("%s/%s/%s: %w", root, base, name, err)
	}
	dfd, err := syscall.Openat(bfd, name, dirFlags, 0)
	if err != nil {
		return "", nil, err
	}
	f := os.NewFile(uintptr(dfd), root+"/"+base+"/"+name)
	return fmt.Sprintf("/proc/%d/fd/%d", os.Getpid(), f.Fd()), f, nil
}

// MakeOwnDir is makeRestoreDir for the other callers (a sync's quarantine): root/base/name made through open
// directories, base root's own, name new; the pinned path stays valid while the file is open.
func MakeOwnDir(root, base, name string) (string, *os.File, error) {
	return makeRestoreDir(root, base, name)
}

// OpenDir opens the absolute folder p walking it from / one component at a time, never through a link (a rename
// of a folder on the way cannot send it elsewhere), creating its last component (0770) when create is set; it
// returns the pinned /proc path of the open folder.
func OpenDir(p string, create bool) (string, *os.File, error) {
	f, err := openNoFollow(p, create)
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("/proc/%d/fd/%d", os.Getpid(), f.Fd()), f, nil
}

func openNoFollow(p string, create bool) (*os.File, error) {
	const dirFlags = syscall.O_RDONLY | syscall.O_DIRECTORY | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
	if !strings.HasPrefix(p, "/") {
		return nil, fmt.Errorf("%s is not absolute", p)
	}
	fd, err := syscall.Open("/", dirFlags, 0)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	for i, c := range parts {
		if c == "" || c == "." || c == ".." {
			syscall.Close(fd)
			return nil, fmt.Errorf("%s: invalid path", p)
		}
		if create && i == len(parts)-1 {
			if err := syscall.Mkdirat(fd, c, 0o770); err != nil && !errors.Is(err, syscall.EEXIST) {
				syscall.Close(fd)
				return nil, fmt.Errorf("%s: %w", p, err)
			}
		}
		next, err := syscall.Openat(fd, c, dirFlags, 0)
		syscall.Close(fd)
		if err != nil {
			return nil, fmt.Errorf("%s: not a plain folder (%w)", p, err)
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), p), nil
}
