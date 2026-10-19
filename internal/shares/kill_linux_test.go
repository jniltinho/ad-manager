//go:build linux

package shares

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// startAs runs sleep under another name (its comm).
func startAs(t *testing.T, name string) *exec.Cmd {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep")
	}
	b, err := os.ReadFile(sleep)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(bin, b, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return cmd
}

func TestKillPinned(t *testing.T) {
	other := startAs(t, "notsamba")
	if err := killPinned(other.Process.Pid, func() error { return nil }); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a process that is not smbd: got %v", err)
	}
	smbd := startAs(t, "smbd")
	if err := killPinned(smbd.Process.Pid, func() error { return ErrInvalid }); !errors.Is(err, ErrInvalid) {
		t.Fatalf("failed check: got %v", err)
	}
	if err := killPinned(smbd.Process.Pid, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- smbd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("smbd still running after SIGTERM")
	}
}
