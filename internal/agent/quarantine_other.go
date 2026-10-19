//go:build !linux

package agent

import (
	"errors"
	"os"
)

var errLinuxOnly = errors.New("the sync quarantine works on Linux only")

func openFolder(string) (*os.File, error) { return nil, errLinuxOnly }

func restoreFile(_, _ *os.File, _ string, _ bool) (bool, error) { return false, errLinuxOnly }

func openQuarantineFile(Quarantine, string) (*os.File, error) { return nil, errLinuxOnly }

func diskSpace(string) (Space, bool) { return Space{}, false }
