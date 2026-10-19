//go:build linux

package shares

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// killPinned opens a pidfd for pid first, checks that it is an smbd process and that check passes, and only then
// sends SIGTERM through the pidfd: the signal reaches the process that was checked, even if pid is reused meanwhile.
func killPinned(pid int, check func() error) error {
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return fmt.Errorf("%w: process %d: %v", ErrInvalid, pid, err)
	}
	defer unix.Close(fd)
	comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil || !strings.HasPrefix(string(comm), "smbd") {
		return fmt.Errorf("%w: process %d is not smbd", ErrInvalid, pid)
	}
	if err := check(); err != nil {
		return err
	}
	if err := unix.PidfdSendSignal(fd, unix.SIGTERM, nil, 0); err != nil {
		return fmt.Errorf("ending process %d: %w", pid, err)
	}
	return nil
}
