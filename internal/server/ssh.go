package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"ad-manager/internal/audit"

	"github.com/labstack/echo/v5"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/websocket"
)

// SSH terminal limits.
const (
	maxTerminals  = 4                // per operator, across their sessions
	termFrameMax  = 64 << 10         // largest WebSocket frame either way
	termPing      = 30 * time.Second // keeps reverse proxies from dropping an idle terminal
	termWriteWait = 30 * time.Second // a browser that stops reading
	termSizeMax   = 1000             // columns and rows
)

// SSH terminal waits (tests shorten them).
var (
	termHelloWait = 30 * time.Second // the first message must arrive within it
	termStartWait = 20 * time.Second // TCP connect, SSH handshake, authentication, PTY and shell
	termCheck     = 15 * time.Second // how often an open terminal checks its panel session
)

// Terminal error codes: the only error text that reaches the browser, the audit log and the server log (the SSH
// library's text may carry what the server sent, the password included).
const (
	codeAuth        = "auth_failed"
	codeHostChanged = "hostkey_changed"
	codeHostUnknown = "hostkey_unknown"
	codeUnreachable = "unreachable"
	codeTimeout     = "timeout"
	codeClosed      = "closed"
	codeInternal    = "internal"
	codeBadRequest  = "bad_request"
)

// terminal is one open SSH terminal, registered in Server.terms under Server.mu.
type terminal struct {
	sid, user string
	cancel    context.CancelFunc
	done      chan struct{}
}

// termHello is the WebSocket's first message. The credential fields live only until the handshake.
type termHello struct {
	User       string `json:"user"`
	Password   string `json:"password"`
	Key        string `json:"key"`
	Passphrase string `json:"passphrase"`
	Cols       int    `json:"cols"`
	Rows       int    `json:"rows"`
	Trust      string `json:"trust"` // the fingerprint the operator confirmed
}

// termFrame is a WebSocket frame with its type: text frames are control messages, binary frames terminal bytes.
type termFrame struct {
	typ  byte
	data []byte
}

// frameCodec keeps the frame type, which websocket.Message drops.
var frameCodec = websocket.Codec{
	Marshal: func(v any) ([]byte, byte, error) { f := v.(termFrame); return f.data, f.typ, nil },
	Unmarshal: func(data []byte, typ byte, v any) error {
		f := v.(*termFrame)
		f.typ, f.data = typ, data
		return nil
	},
}

// codeError is a terminal failure reduced to its code.
type codeError struct{ code, fingerprint, keyType string }

func (e *codeError) Error() string { return e.code }

// dropSession deletes session id and cancels its terminals; the caller holds s.mu.
func (s *Server) dropSession(id string) {
	delete(s.sessions, id)
	for t := range s.terms {
		if t.sid == id {
			t.cancel()
		}
	}
}

// closeTerminals cancels the terminals match selects and waits up to wait for them to end.
func (s *Server) closeTerminals(match func(*terminal) bool, wait time.Duration) {
	s.mu.Lock()
	var done []chan struct{}
	for t := range s.terms {
		if match(t) {
			t.cancel()
			done = append(done, t.done)
		}
	}
	s.mu.Unlock()
	deadline := time.After(wait)
	for _, d := range done {
		select {
		case <-d:
		case <-deadline:
			return
		}
	}
}

// liveSession reports whether session sid exists and has not expired; touch also refreshes its idle time. An
// expired session is dropped, never revived. The caller holds s.mu.
func (s *Server) liveSession(sid string, touch bool) bool {
	se, ok := s.sessions[sid]
	if ok && time.Since(se.seen) > s.ttl() {
		s.dropSession(sid)
		return false
	}
	if ok && touch {
		se.seen = time.Now()
	}
	return ok
}

// sameOrigin reports whether the request's Origin is the panel's own (host and port equal to Host).
func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" || o == "null" {
		return false
	}
	u, err := url.Parse(o)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && strings.EqualFold(u.Host, r.Host)
}

// sshTerminal handles GET /api/agents/:id/ssh: a WebSocket carrying an SSH shell to the agent's host.
func (s *Server) sshTerminal(c *echo.Context) error {
	if s.store == nil || !s.settings().SSHTerminal {
		return c.JSON(http.StatusNotFound, msg("the SSH terminal is off"))
	}
	if !sameOrigin(c.Request()) {
		return c.JSON(http.StatusForbidden, msg("cross-origin request refused"))
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	// the pinned key and its revision before the agent's URL: a URL change after this bumps the revision, so a trust
	// of the old host's key can never be saved for the new host
	pinned, rev, err := s.store.HostKey(id)
	if err != nil {
		return fail(c, err)
	}
	a, err := s.store.Agent(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, msg("server not registered"))
	}
	u, err := url.Parse(a.URL)
	if err != nil || u.Hostname() == "" {
		return c.JSON(http.StatusBadRequest, msg("the server's URL has no host"))
	}
	sid, user := c.Get("sid").(string), sess(c).user
	ctx, cancel := context.WithCancel(context.Background()) // the request's context ends with the hijack
	t := &terminal{sid: sid, user: strings.ToLower(user), cancel: cancel, done: make(chan struct{})}
	// admission: one critical section with the switch, so switching off (putSettings) never misses a terminal
	s.mu.Lock()
	n := 0
	for o := range s.terms {
		if o.user == t.user {
			n++
		}
	}
	switch {
	case !s.settings().SSHTerminal:
		s.mu.Unlock()
		cancel()
		return c.JSON(http.StatusNotFound, msg("the SSH terminal is off"))
	case n >= maxTerminals:
		s.mu.Unlock()
		cancel()
		return c.JSON(http.StatusTooManyRequests, msg("at most 4 terminals per operator"))
	}
	s.terms[t] = true
	s.mu.Unlock()
	defer func() { // also when the upgrade fails and the handler never runs
		cancel()
		s.mu.Lock()
		delete(s.terms, t)
		s.mu.Unlock()
		close(t.done)
	}()
	ip := c.RealIP()
	k := hostKeyState{agent: a.ID, pinned: pinned, rev: rev}
	ws := websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil }, // Origin checked above
		Handler: func(ws *websocket.Conn) {
			s.runTerminal(ctx, t, ws, k, a.Name, net.JoinHostPort(u.Hostname(), s.sshPort), user, ip)
		},
	}
	ws.ServeHTTP(c.Response(), c.Request()) // runs the handler to its end
	return nil
}

// hostKeyState is the agent's pinned SSH host key and its revision, read before the agent's URL.
type hostKeyState struct {
	agent, rev int64
	pinned     string
}

// runTerminal reads the first message, opens the SSH shell and pumps bytes until either side ends.
func (s *Server) runTerminal(ctx context.Context, t *terminal, ws *websocket.Conn, k hostKeyState, name, addr, user, ip string) {
	defer ws.Close()
	ws.MaxPayloadBytes = termFrameMax
	// a past deadline first: Close waits for the writer, which a browser that stopped reading would hold. dmu keeps a
	// send from pushing that deadline back after the cancel set it.
	var dmu sync.Mutex
	stop := context.AfterFunc(ctx, func() {
		dmu.Lock()
		_ = ws.SetDeadline(time.Now())
		dmu.Unlock()
		ws.Close()
	})
	defer stop()
	send := func(typ byte, data []byte) error {
		dmu.Lock()
		if err := ctx.Err(); err != nil {
			dmu.Unlock()
			return err
		}
		_ = ws.SetWriteDeadline(time.Now().Add(termWriteWait))
		dmu.Unlock()
		return frameCodec.Send(ws, termFrame{typ, data})
	}
	control := func(v map[string]any) { b, _ := json.Marshal(v); _ = send(websocket.TextFrame, b) }

	// the http.Server's read and write deadlines stay on the hijacked connection
	_ = ws.SetDeadline(time.Now().Add(termHelloWait))
	var f termFrame
	var in termHello
	if err := frameCodec.Receive(ws, &f); err != nil {
		return // silent client, oversized frame or closed: nothing dialed
	}
	if f.typ != websocket.TextFrame || json.Unmarshal(f.data, &in) != nil || strings.TrimSpace(in.User) == "" ||
		in.Cols < 1 || in.Cols > termSizeMax || in.Rows < 1 || in.Rows > termSizeMax || (in.Password == "" && in.Key == "") {
		control(map[string]any{"type": "error", "code": codeBadRequest})
		return
	}
	_ = ws.SetDeadline(time.Time{})
	target := name + ":" + in.User

	client, sh, stdin, stdout, err := s.startShell(ctx, k, addr, &in, func() {
		audit.Log(user, ip, "ssh.hostkey.trust", name, nil)
	})
	in = termHello{} // drop the credential
	if err != nil {
		var ce *codeError
		if !errors.As(err, &ce) {
			ce = &codeError{code: codeInternal}
		}
		if ce.code == codeHostUnknown {
			control(map[string]any{"type": "hostkey", "fingerprint": ce.fingerprint, "key_type": ce.keyType})
			return // nothing was sent to the server: not an attempt
		}
		audit.Log(user, ip, "ssh.open", target, errors.New(ce.code))
		control(map[string]any{"type": "error", "code": ce.code})
		return
	}
	defer client.Close()
	// logout or switch-off may have come during the handshake
	s.mu.Lock()
	live := ctx.Err() == nil && s.liveSession(t.sid, false)
	s.mu.Unlock()
	if !live {
		audit.Log(user, ip, "ssh.open", target, errors.New(codeClosed))
		control(map[string]any{"type": "error", "code": codeClosed})
		return
	}
	audit.Log(user, ip, "ssh.open", target, nil)
	start := time.Now()
	defer func() {
		audit.Log(user, ip, "ssh.close", target+" "+time.Since(start).Round(time.Second).String(), nil)
	}()
	control(map[string]any{"type": "ready"})

	ended := make(chan struct{}, 2)
	go func() { // output: binary frames
		buf := make([]byte, 32<<10)
		for {
			n, err := stdout.Read(buf)
			if n > 0 && send(websocket.BinaryFrame, buf[:n]) != nil {
				break
			}
			if err != nil {
				break
			}
		}
		ended <- struct{}{}
	}()
	go func() { // input: binary frames are keystrokes, text frames control messages
		for {
			var f termFrame
			if frameCodec.Receive(ws, &f) != nil {
				break
			}
			s.mu.Lock()
			// keystrokes are activity, resizes are not; an expired session is never revived
			live := s.liveSession(t.sid, f.typ == websocket.BinaryFrame)
			s.mu.Unlock()
			if !live {
				break
			}
			if f.typ == websocket.BinaryFrame {
				if _, err := stdin.Write(f.data); err != nil {
					break
				}
				continue
			}
			var r struct {
				Type       string `json:"type"`
				Cols, Rows int
			}
			if json.Unmarshal(f.data, &r) == nil && r.Type == "resize" && r.Cols >= 1 && r.Cols <= termSizeMax &&
				r.Rows >= 1 && r.Rows <= termSizeMax {
				_ = sh.WindowChange(r.Rows, r.Cols)
			}
		}
		ended <- struct{}{}
	}()
	// the shell's end is the output's EOF (the channel closes after the last bytes), so nothing is cut
	ping, check := time.NewTicker(termPing), time.NewTicker(termCheck)
	defer ping.Stop()
	defer check.Stop()
	for {
		select {
		case <-ended:
			control(map[string]any{"type": "error", "code": codeClosed})
			return
		case <-ctx.Done():
			return
		case <-ping.C:
			_ = send(websocket.PingFrame, nil)
		case <-check.C: // expiry with no other panel request
			s.mu.Lock()
			live := s.liveSession(t.sid, false)
			s.mu.Unlock()
			if !live {
				return
			}
		}
	}
}

// startShell dials addr, checks the pinned host key, authenticates and starts a shell on a PTY, all within
// termStartWait.
func (s *Server) startShell(ctx context.Context, k hostKeyState, addr string, in *termHello, trusted func()) (*ssh.Client, *ssh.Session, io.Writer, io.Reader, error) {
	var auth []ssh.AuthMethod
	if in.Key != "" {
		var signer ssh.Signer
		var err error
		if in.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(in.Key), []byte(in.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(in.Key))
		}
		if err != nil {
			return nil, nil, nil, nil, &codeError{code: codeAuth}
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if pw := in.Password; pw != "" {
		auth = append(auth, ssh.Password(pw), ssh.KeyboardInteractive(func(_, _ string, q []string, _ []bool) ([]string, error) {
			a := make([]string, len(q))
			for i := range a {
				a[i] = pw
			}
			return a, nil
		}))
	}
	agentID, pinned, rev, trust := k.agent, k.pinned, k.rev, in.Trust
	hostKey := func(_ string, _ net.Addr, key ssh.PublicKey) error {
		line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
		fp := ssh.FingerprintSHA256(key)
		if pinned != "" {
			if pinned == line {
				return nil
			}
			return &codeError{code: codeHostChanged}
		}
		if trust != fp {
			return &codeError{code: codeHostUnknown, fingerprint: fp, keyType: key.Type()}
		}
		ok, err := s.store.TrustHostKey(agentID, rev, line)
		if err != nil {
			return &codeError{code: codeInternal}
		}
		if !ok { // another connection trusted, or a reset came, since HostKey was read
			now, _, err := s.store.HostKey(agentID)
			switch {
			case err != nil:
				return &codeError{code: codeInternal}
			case now == line:
				return nil
			case now != "":
				return &codeError{code: codeHostChanged}
			}
			return &codeError{code: codeHostUnknown, fingerprint: fp, keyType: key.Type()}
		}
		trusted()
		return nil
	}
	cfg := &ssh.ClientConfig{User: in.User, Auth: auth, HostKeyCallback: hostKey, HostKeyAlgorithms: hostKeyAlgos(pinned),
		Timeout: termStartWait}

	ctx, cancel := context.WithTimeout(ctx, termStartWait)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, nil, nil, startError(ctx, err)
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() }) // a server that stalls after the TCP connect
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		stop()
		conn.Close()
		return nil, nil, nil, nil, startError(ctx, err)
	}
	client := ssh.NewClient(c, chans, reqs)
	sh, stdin, stdout, err := shell(client, in.Cols, in.Rows)
	if !stop() || err != nil { // the deadline fired (the connection is closed) or the shell failed
		client.Close()
		if err == nil {
			err = context.DeadlineExceeded
		}
		return nil, nil, nil, nil, startError(ctx, err)
	}
	return client, sh, stdin, stdout, nil
}

// hostKeyAlgos asks for the pinned key's own type (another type the server gained later is not a "changed" key) or,
// with none pinned, prefers Ed25519, the key operators usually check.
func hostKeyAlgos(pinned string) []string {
	if pinned == "" {
		return []string{ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521,
			ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256}
	}
	k, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pinned))
	switch {
	case err != nil:
		return nil // the library's defaults; the callback still compares the whole key
	case k.Type() == ssh.KeyAlgoRSA:
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256}
	}
	return []string{k.Type()}
}

// shell opens a session with a PTY and a shell; stderr shares the PTY's output.
func shell(client *ssh.Client, cols, rows int) (*ssh.Session, io.Writer, io.Reader, error) {
	sh, err := client.NewSession()
	if err != nil {
		return nil, nil, nil, err
	}
	stdin, err := sh.StdinPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	stdout, err := sh.StdoutPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	if err := sh.RequestPty("xterm-256color", rows, cols, ssh.TerminalModes{ssh.ECHO: 1}); err != nil {
		return nil, nil, nil, err
	}
	if err := sh.Shell(); err != nil {
		return nil, nil, nil, err
	}
	return sh, stdin, stdout, nil
}

// startError reduces a start failure to its code.
func startError(ctx context.Context, err error) error {
	var ce *codeError
	var ne net.Error
	switch {
	case errors.As(err, &ce):
		return ce
	case ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()):
		return &codeError{code: codeTimeout}
	case strings.Contains(err.Error(), "unable to authenticate"):
		return &codeError{code: codeAuth}
	case errors.As(err, new(*net.OpError)):
		return &codeError{code: codeUnreachable}
	}
	return &codeError{code: codeClosed}
}

// resetHostKey handles DELETE /api/agents/:id/ssh/hostkey.
func (s *Server) resetHostKey(c *echo.Context) error {
	if s.store == nil {
		return noStore(c)
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	a, err := s.store.Agent(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, msg("server not registered"))
	}
	err = s.store.ResetHostKey(id)
	audit.Log(sess(c).user, c.RealIP(), "ssh.hostkey.reset", a.Name, err)
	if err != nil {
		return fail(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}
