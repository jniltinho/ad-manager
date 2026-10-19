//go:build windows

package backup

import "errors"

func diskFree(string) (uint64, error) { return 0, errors.New("not on Windows") }

// FreeBytes is the space free for root in dir's file system.
func FreeBytes(dir string) (uint64, error) { return diskFree(dir) }
