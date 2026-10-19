//go:build !windows

package sambabridge

import (
	"os"
	"path/filepath"
	"syscall"
)

var filepathEval = filepath.EvalSymlinks

// ownedByRoot reports whether the file belongs to uid 0.
func ownedByRoot(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Uid == 0
}
