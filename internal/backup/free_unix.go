//go:build !windows

package backup

import "syscall"

func diskFree(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

// FreeBytes is the space free for root in dir's file system.
func FreeBytes(dir string) (uint64, error) { return diskFree(dir) }
