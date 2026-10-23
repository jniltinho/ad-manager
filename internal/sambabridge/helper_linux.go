//go:build linux

package sambabridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// maxAge restarts a helper this old before its next request: it picks up an updated Samba and does not grow forever.
const maxAge = time.Hour

// idleTimeout stops a helper no request used for this long: it is started again on the next one.
var idleTimeout = 5 * time.Minute

// helper is one long-lived python3 serving requests one at a time on a Unix socket (fd 3 on its side). Each request
// carries its folders as SCM_RIGHTS descriptors.
type helper struct {
	turn    chan struct{} // one request at a time; waiting for the turn respects the request's context
	cmd     *exec.Cmd
	conn    *net.UnixConn
	exited  chan struct{}
	started time.Time
	stderr  capped
	idle    *time.Timer // stops the helper once it has been free for idleTimeout
	last    time.Time   // when the last request ended
}

// helpers are the helpers of one interpreter: quick requests, and the long ones (a propagation can take 30 minutes)
// apart, so a tree walk never makes a listing wait.
type helpers struct{ quick, long helper }

var pools sync.Map // python path → *helpers

func pool(py string) *helpers {
	if p, ok := pools.Load(py); ok {
		return p.(*helpers)
	}
	p, _ := pools.LoadOrStore(py, &helpers{
		quick: helper{turn: make(chan struct{}, 1)},
		long:  helper{turn: make(chan struct{}, 1)},
	})
	return p.(*helpers)
}

// exec sends one request to the interpreter's helper (started when needed) and returns its answer.
func (b *Bridge) exec(ctx context.Context, req []byte, folders []*os.File) ([]byte, error) {
	py := b.Python
	if py == "" {
		py = "/usr/bin/python3"
	}
	if err := trusted(py); err != nil {
		return nil, err
	}
	p := pool(py)
	h := &p.quick
	if b.Timeout >= propagateTimeout {
		h = &p.long
	}
	select {
	case h.turn <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("samba bridge: waiting for the helper: %w", ctx.Err())
	}
	defer func() { h.armIdle(); <-h.turn }()
	if err := ctx.Err(); err != nil { // cancelled while waiting: nothing was sent
		return nil, fmt.Errorf("samba bridge: %w", err)
	}
	return h.do(ctx, py, req, folders)
}

func (h *helper) do(ctx context.Context, py string, req []byte, folders []*os.File) ([]byte, error) {
	if h.cmd != nil && (time.Since(h.started) > maxAge || h.dead()) {
		h.stop()
	}
	if h.cmd == nil {
		if err := h.start(py); err != nil {
			return nil, err
		}
	}
	if d, ok := ctx.Deadline(); ok {
		_ = h.conn.SetDeadline(d)
	} else {
		_ = h.conn.SetDeadline(time.Time{})
	}
	// a cancelled request stops waiting at once (the helper is then dropped, like on a timeout)
	conn := h.conn
	interrupted := make(chan struct{})
	stopAfter := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Unix(1, 0)); close(interrupted) })
	defer func() {
		if !stopAfter() {
			<-interrupted // never let it touch the next request's deadline
		}
	}()
	fds := make([]int, len(folders))
	for i, f := range folders {
		fds[i] = int(f.Fd())
	}
	msg := binary.LittleEndian.AppendUint32(nil, uint32(len(req)))
	msg = append(msg, req...)
	var oob []byte
	if len(fds) > 0 {
		oob = syscall.UnixRights(fds...) // with the first bytes of the request
	}
	sent := false
	n, _, err := h.conn.WriteMsgUnix(msg, oob, nil)
	if err == nil && n < len(msg) {
		_, err = h.conn.Write(msg[n:])
	}
	if err == nil {
		sent = true
		var head [4]byte
		if _, err = io.ReadFull(h.conn, head[:]); err == nil {
			size := binary.LittleEndian.Uint32(head[:])
			if size > maxOutput {
				err = fmt.Errorf("answer of %d bytes, more than %d", size, maxOutput)
			} else {
				out := make([]byte, size)
				if _, err = io.ReadFull(h.conn, out); err == nil {
					return out, nil
				}
			}
		}
	}
	// no answer: the helper is not trusted any more (the next request starts a new one)
	stderr := h.stop()
	if sent {
		// the request reached the helper: a change may have been applied (do not repeat it blindly; read it again)
		return nil, fmt.Errorf("samba bridge: no answer (%v); the change may have been applied: %s", err, stderr)
	}
	return nil, fmt.Errorf("samba bridge: %w: %s", err, stderr)
}

func (h *helper) start(py string) error {
	pair, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("samba bridge: %w", err)
	}
	ours, theirs := os.NewFile(uintptr(pair[0]), "bridge"), os.NewFile(uintptr(pair[1]), "bridge-helper")
	defer theirs.Close()
	defer ours.Close()
	cmd := exec.Command(py, "-I", "-s", "-B", "-c", script)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8"}
	cmd.Dir = "/"
	h.stderr = capped{max: 64 << 10}
	cmd.Stderr = &h.stderr
	cmd.ExtraFiles = []*os.File{theirs} // fd 3 in the helper
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("samba bridge: %w", err)
	}
	c, err := net.FileConn(ours)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("samba bridge: %w", err)
	}
	h.cmd, h.conn, h.started, h.exited = cmd, c.(*net.UnixConn), time.Now(), make(chan struct{})
	go func(exited chan struct{}) { _ = cmd.Wait(); close(exited) }(h.exited)
	return nil
}

// dead reports a helper that exited by itself (killed, or Samba's code crashed it).
func (h *helper) dead() bool {
	select {
	case <-h.exited:
		return true
	default:
		return false
	}
}

// armIdle restarts the idle countdown; it runs with the turn held, as does the stop it schedules, so a helper is never
// stopped under a request (a request already waiting for the turn simply starts a new helper).
func (h *helper) armIdle() {
	h.last = time.Now()
	if h.idle != nil {
		h.idle.Stop()
	}
	d := idleTimeout
	h.idle = time.AfterFunc(d, func() {
		h.turn <- struct{}{}
		defer func() { <-h.turn }()
		if time.Since(h.last) >= d { // not used again since this countdown began
			h.stop()
		}
	})
}

// stop ends the helper and returns what it wrote on stderr.
func (h *helper) stop() string {
	if h.cmd == nil {
		return ""
	}
	_ = h.conn.Close() // the helper reads an end and leaves
	select {
	case <-h.exited:
	case <-time.After(2 * time.Second):
		_ = h.cmd.Process.Kill()
		<-h.exited
	}
	h.cmd, h.conn = nil, nil
	return string(bytes.TrimSpace(h.stderr.Bytes()))
}
