//go:build linux

package sambabridge

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func helperPid(t *testing.T) int {
	t.Helper()
	h := &pool("/usr/bin/python3").quick
	h.turn <- struct{}{}
	defer func() { <-h.turn }()
	if h.cmd == nil {
		return 0
	}
	return h.cmd.Process.Pid
}

// TestHelper runs the embedded helper for real: it stays up between requests, comes back after it dies or times out,
// and receives the folder descriptors.
func TestHelper(t *testing.T) {
	if _, err := os.Stat("/usr/bin/python3"); err != nil {
		t.Skip("no /usr/bin/python3")
	}
	ctx := context.Background()
	b := &Bridge{}
	_ = b.Capabilities(ctx)
	pid := helperPid(t)
	_ = b.Capabilities(ctx)
	if pid == 0 || helperPid(t) != pid {
		t.Fatalf("the helper must stay up: %d then %d", pid, helperPid(t))
	}

	t.Run("one request at a time", func(t *testing.T) {
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				if err := b.call(ctx, "rm", map[string]any{}, nil, &struct{}{}); err == nil || !strings.Contains(err.Error(), "unknown operation") {
					t.Errorf("got %v", err)
				}
			})
		}
		wg.Wait()
		if helperPid(t) != pid {
			t.Fatal("answers out of step restarted the helper")
		}
	})

	t.Run("descriptor reaches the helper", func(t *testing.T) {
		dir := t.TempDir()
		f, err := os.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		_, err = b.GetACL(ctx, f, dir, []string{os.TempDir()}, "")
		if err != nil && strings.Contains(err.Error(), "descriptor missing") {
			t.Fatalf("descriptor lost: %v", err)
		}
	})

	t.Run("restart after a crash", func(t *testing.T) {
		_ = syscall.Kill(helperPid(t), syscall.SIGKILL)
		time.Sleep(100 * time.Millisecond)
		if c := b.Capabilities(ctx); c.Error != "" && strings.Contains(c.Error, "samba bridge:") {
			t.Fatalf("no restart: %+v", c)
		}
		if p := helperPid(t); p == 0 || p == pid {
			t.Fatalf("pid %d after a crash (was %d)", p, pid)
		}
	})

	t.Run("works after a timeout", func(t *testing.T) {
		short := &Bridge{Timeout: time.Nanosecond}
		if err := short.call(ctx, "capabilities", map[string]any{}, nil, &struct{}{}); err == nil {
			t.Fatal("a past deadline must fail")
		}
		if err := b.call(ctx, "rm", map[string]any{}, nil, &struct{}{}); err == nil || !strings.Contains(err.Error(), "unknown operation") {
			t.Fatalf("after a timeout: %v", err)
		}
	})

	t.Run("cancelled before sending", func(t *testing.T) {
		old := helperPid(t)
		gone, cancel := context.WithCancel(ctx)
		cancel()
		if err := b.call(gone, "capabilities", map[string]any{}, nil, &struct{}{}); err == nil {
			t.Fatal("a cancelled request must fail")
		}
		if p := helperPid(t); p != old {
			t.Fatalf("nothing was sent, the helper must stay: %d then %d", old, p)
		}
	})
}

// TestScript runs the embedded helper for real: it must parse and reject bad
// requests before touching Samba (python3-samba is not needed for that).
func TestScript(t *testing.T) {
	if _, err := os.Stat("/usr/bin/python3"); err != nil {
		t.Skip("no /usr/bin/python3")
	}
	b := &Bridge{}
	for _, tt := range []struct {
		name string
		op   string
		args any
		msg  string
	}{
		{name: "unknown operation", op: "rm", args: map[string]any{}, msg: "unknown operation"},
		{name: "relative path", op: "get_acl", args: map[string]any{"path": "srv/x", "roots": []string{"/srv"}}, msg: "absolute"},
		{name: "system path", op: "get_acl", args: map[string]any{"path": "/etc/samba", "roots": []string{"/etc"}}, msg: "system path"},
		{name: "outside roots", op: "get_acl", args: map[string]any{"path": "/srv/x", "roots": []string{"/data"}}, msg: "outside"},
		{name: "no descriptor", op: "get_acl", args: map[string]any{"path": "/srv/x", "roots": []string{"/srv"}}, msg: "descriptor"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := b.call(context.Background(), tt.op, tt.args, nil, &struct{}{})
			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), tt.msg) {
				t.Fatalf("got %v, want ErrInvalid with %q", err, tt.msg)
			}
		})
	}
}
