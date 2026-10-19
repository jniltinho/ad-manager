//go:build !linux

package shares

import "errors"

// killPinned is only implemented on Linux (the agent runs on the Samba file server).
func killPinned(int, func() error) error { return errors.New("the shares agent only works on Linux") }
