package agent

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/unix"

	"ad-manager/internal/backup"
)

const dirFlags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC

// openFolder opens an absolute folder from / one component at a time, never through a link.
func openFolder(p string) (*os.File, error) {
	_, f, err := backup.OpenDir(p, false)
	return f, err
}

// openParent opens the folder that holds rel inside base, one component at a time without following links,
// creating the missing ones (0770) when mk is set; it returns the folder's descriptor and rel's last name.
func openParent(base *os.File, rel string, mk bool) (int, string, error) {
	fd, err := unix.Openat(int(base.Fd()), ".", dirFlags, 0)
	if err != nil {
		return -1, "", err
	}
	parts := strings.Split(rel, "/")
	for _, c := range parts[:len(parts)-1] {
		if mk {
			if err := unix.Mkdirat(fd, c, 0o770); err != nil && !errors.Is(err, unix.EEXIST) {
				unix.Close(fd)
				return -1, "", err
			}
		}
		next, err := unix.Openat(fd, c, dirFlags, 0)
		unix.Close(fd)
		if err != nil {
			return -1, "", errors.New(c + ": not a plain folder")
		}
		fd = next
	}
	return fd, parts[len(parts)-1], nil
}

// restoreFile moves the file rel of the quarantine qf to the same place in the destination df (a rename: both are
// on one disk). A file there already gives errInDest, or with replace swaps places with it (the destination's goes
// into the quarantine), so nothing is lost.
func restoreFile(qf, df *os.File, rel string, replace bool) (swapped bool, err error) {
	qfd, name, err := openParent(qf, rel, false)
	if err != nil {
		return false, err
	}
	defer unix.Close(qfd)
	var st unix.Stat_t
	if err := unix.Fstatat(qfd, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return false, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return false, errors.New("not a file in the quarantine")
	}
	dfd, _, err := openParent(df, rel, true)
	if err != nil {
		return false, err
	}
	defer unix.Close(dfd)
	var dt unix.Stat_t
	switch err := unix.Fstatat(dfd, name, &dt, unix.AT_SYMLINK_NOFOLLOW); {
	case errors.Is(err, unix.ENOENT):
		return false, unix.Renameat2(qfd, name, dfd, name, unix.RENAME_NOREPLACE)
	case err != nil:
		return false, err
	case dt.Mode&unix.S_IFMT != unix.S_IFREG:
		return false, errors.New("not a file in the destination")
	case !replace:
		return false, errInDest
	}
	return true, unix.Renameat2(qfd, name, dfd, name, unix.RENAME_EXCHANGE)
}

// openQuarantineFile opens a file of a quarantine for reading, never through a link.
func openQuarantineFile(q Quarantine, rel string) (*os.File, error) {
	qf, err := openFolder(q.Path)
	if err != nil {
		return nil, err
	}
	defer qf.Close()
	fd, name, err := openParent(qf, rel, false)
	if err != nil {
		return nil, err
	}
	defer unix.Close(fd)
	ffd, err := unix.Openat(fd, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(ffd), name)
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("not a file in the quarantine")
	}
	return f, nil
}

// diskSpace is the free and total space of the disk that holds p.
func diskSpace(p string) (Space, bool) {
	var s unix.Statfs_t
	if unix.Statfs(p, &s) != nil {
		return Space{}, false
	}
	return Space{Free: int64(s.Bavail) * s.Bsize, Total: int64(s.Blocks) * s.Bsize}, true
}
