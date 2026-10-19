//go:build !linux

package backup

import (
	"os"
	"path/filepath"
)

// pinDir: without /proc the path itself (restorations run on Linux servers).
func pinDir(dir string) (string, *os.File, error) {
	f, err := os.Open(dir)
	return dir, f, err
}

func makeRestoreDir(root, base, name string) (string, *os.File, error) {
	dir := filepath.Join(root, base, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, err
	}
	return pinDir(dir)
}

func MakeOwnDir(root, base, name string) (string, *os.File, error) {
	return makeRestoreDir(root, base, name)
}

func OpenDir(p string, create bool) (string, *os.File, error) {
	if create {
		if err := os.MkdirAll(p, 0o770); err != nil {
			return "", nil, err
		}
	}
	return pinDir(p)
}
