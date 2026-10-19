package sambabridge

import (
	"os"
	"path/filepath"
)

var filepathEval = filepath.EvalSymlinks

// ownedByRoot is never true on Windows: the helper only runs on Samba servers.
func ownedByRoot(os.FileInfo) bool { return false }
