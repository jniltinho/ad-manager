//go:build !windows

package agent

import (
	"fmt"
	"os"
	"syscall"
)

// ownedByMe tells whether fi belongs to this process's user (root, for the agent).
func ownedByMe(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Uid == uint32(os.Geteuid())
}

// sameDevice tells whether two open folders are on the same file system.
func sameDevice(a, b *os.File) bool {
	var sa, sb syscall.Stat_t
	return syscall.Fstat(int(a.Fd()), &sa) == nil && syscall.Fstat(int(b.Fd()), &sb) == nil && sa.Dev == sb.Dev
}

// fileID is an open folder's "dev:ino".
func fileID(f *os.File) string {
	var st syscall.Stat_t
	if syscall.Fstat(int(f.Fd()), &st) != nil {
		return ""
	}
	return fmt.Sprintf("%d:%d", st.Dev, st.Ino)
}
