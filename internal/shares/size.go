package shares

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SizeResult is the measure of one share's folder.
type SizeResult struct {
	RealPath string // the folder after resolving symlinks (what Samba serves)
	Bytes    int64  // apparent size; a lower bound when Partial
	Partial  bool   // du failed on some entries but still printed a total
}

// SizeRunner runs du (with its idle-priority prefix) and returns its stdout and stderr, each bounded.
type SizeRunner func(ctx context.Context, name string, args ...string) (stdout, stderr string, err error)

// sizeOutMax bounds what is kept of du's stdout and stderr (an error flood is drained, not kept).
const sizeOutMax = 4 << 10

// capWriter keeps the first max bytes written and discards the rest.
type capWriter struct {
	b   bytes.Buffer
	max int
}

func (w *capWriter) Write(p []byte) (int, error) {
	if room := w.max - w.b.Len(); room > 0 {
		w.b.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

// ExecSize is the real SizeRunner: output bounded, killed with its context, pipes given up 5 s after.
func ExecSize(ctx context.Context, name string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, errOut := &capWriter{max: sizeOutMax}, &capWriter{max: sizeOutMax}
	cmd.Stdout, cmd.Stderr, cmd.WaitDelay = out, errOut, 5*time.Second
	err := cmd.Run()
	return out.b.String(), errOut.b.String(), err
}

var (
	idleOnce   sync.Once
	idlePrefix []string
)

// idle is the prefix that runs du at idle CPU and I/O priority, or nothing (logged once) where nice/ionice are missing.
func idle() []string {
	idleOnce.Do(func() {
		_, e1 := exec.LookPath("nice")
		_, e2 := exec.LookPath("ionice")
		if e1 != nil || e2 != nil {
			slog.Warn("folder sizes: nice or ionice not installed, du runs at normal priority")
			return
		}
		idlePrefix = []string{"nice", "-n", "19", "ionice", "-t", "-c", "3"}
	})
	return idlePrefix
}

// Size measures one share folder with `du -s -b -P` on its symlink-resolved path. A du that exits non-zero but printed
// a total gives a Partial result together with its first error line.
func (m *Manager) Size(ctx context.Context, path string) (SizeResult, error) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return SizeResult{}, err
	}
	run := m.SizeRun
	if run == nil {
		run = ExecSize
	}
	args := append(append([]string{}, idle()...), "du", "-s", "-b", "-P", "--", real)
	out, errOut, runErr := run(ctx, args[0], args[1:]...)
	n, parsed := int64(0), false
	if f := strings.Fields(out); len(f) > 0 {
		n, err = strconv.ParseInt(f[0], 10, 64)
		parsed = err == nil
	}
	r := SizeResult{RealPath: real}
	switch {
	case runErr == nil && parsed:
		r.Bytes = n
		return r, nil
	case ctx.Err() != nil:
		return r, ctx.Err()
	}
	msg, _, _ := strings.Cut(strings.TrimSpace(errOut), "\n")
	if msg == "" && runErr != nil {
		msg = "du: " + runErr.Error()
	} else if msg == "" {
		msg = "du: no total in its output"
	}
	if parsed {
		r.Bytes, r.Partial = n, true
	}
	return r, errors.New(msg)
}
