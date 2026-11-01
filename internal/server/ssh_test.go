package server

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ad-manager/internal/audit"
	"ad-manager/internal/store"

	"golang.org/x/crypto/ssh"
	"golang.org/x/net/websocket"
)

const sshPassword = "Ssh-Secret-Password-7"

// sshServer is an in-process SSH server with a tiny shell: it echoes input, answers `echo $((6*7))` with 42,
// `size` with the PTY size, and ends on `exit`.
type sshServer struct {
	port     string
	host     ssh.Signer
	attempts atomic.Int32 // authentication attempts
	accepts  atomic.Int32 // TCP connections
	userKey  ssh.PublicKey
}

func newSSHServer(t *testing.T, stall bool) *sshServer {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	host, _ := ssh.NewSignerFromKey(priv)
	srv := &sshServer{host: host}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(_ ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
			srv.attempts.Add(1)
			if string(p) == sshPassword {
				return nil, nil
			}
			return nil, errors.New("denied")
		},
		PublicKeyCallback: func(_ ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
			srv.attempts.Add(1)
			if srv.userKey != nil && bytes.Equal(k.Marshal(), srv.userKey.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("denied")
		},
	}
	cfg.AddHostKey(host)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	_, srv.port, _ = net.SplitHostPort(ln.Addr().String())
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			srv.accepts.Add(1)
			if stall {
				t.Cleanup(func() { conn.Close() })
				continue
			}
			go srv.serve(conn, cfg)
		}
	}()
	return srv
}

func (srv *sshServer) serve(conn net.Conn, cfg *ssh.ServerConfig) {
	_, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		ch, reqs, err := nc.Accept()
		if err != nil {
			return
		}
		var mu sync.Mutex
		cols, rows := 0, 0
		go func() {
			for r := range reqs {
				switch r.Type {
				case "pty-req":
					var p struct {
						Term             string
						Cols, Rows, W, H uint32
						Modes            string
					}
					_ = ssh.Unmarshal(r.Payload, &p)
					mu.Lock()
					cols, rows = int(p.Cols), int(p.Rows)
					mu.Unlock()
				case "window-change":
					var p struct{ Cols, Rows, W, H uint32 }
					_ = ssh.Unmarshal(r.Payload, &p)
					mu.Lock()
					cols, rows = int(p.Cols), int(p.Rows)
					mu.Unlock()
				}
				if r.WantReply {
					_ = r.Reply(true, nil)
				}
			}
		}()
		go func() {
			defer ch.Close()
			_, _ = ch.Write([]byte("$ "))
			var line []byte
			buf := make([]byte, 1024)
			for {
				n, err := ch.Read(buf)
				if err != nil {
					return
				}
				for _, b := range buf[:n] {
					if b != '\r' {
						line = append(line, b)
						_, _ = ch.Write([]byte{b})
						continue
					}
					cmd := string(line)
					line = nil
					switch cmd {
					case "exit":
						_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
						return
					case "echo $((6*7))":
						_, _ = ch.Write([]byte("\r\n42\r\n$ "))
					case "size":
						mu.Lock()
						_, _ = fmt.Fprintf(ch, "\r\n%dx%d\r\n$ ", cols, rows)
						mu.Unlock()
					default:
						_, _ = ch.Write([]byte("\r\n$ "))
					}
				}
			}
		}()
	}
}

// sshTest is a panel with the SSH terminal on, one agent pointing at an in-process SSH server, and a signed-in
// operator.
type sshTest struct {
	s      *Server
	h      http.Handler
	web    *httptest.Server
	ssh    *sshServer
	cookie *http.Cookie
	audit  *syncBuffer
	db     string
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *syncBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }

func newSSHTest(t *testing.T, stall bool) *sshTest {
	t.Helper()
	ab := &syncBuffer{}
	audit.SetWriter(ab)
	t.Cleanup(func() { audit.SetWriter(os.Stderr) })
	db := filepath.Join(t.TempDir(), "t.db")
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s, _ := newTest(t, true)
	s.store = st
	h := s.routes()
	b, _ := json.Marshal(Settings{Lang: "en", SessionMinutes: 30, InactiveDays: 90, PasswordLength: 14, Theme: themeDefault, SSHTerminal: true})
	if err := st.SetSetting(settingsKey, b); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveAgent(&store.Agent{Name: "fs01", URL: "https://127.0.0.1:9443", Token: "x"}); err != nil {
		t.Fatal(err)
	}
	srv := newSSHServer(t, stall)
	s.sshPort = srv.port
	web := httptest.NewServer(h)
	t.Cleanup(web.Close)
	t.Cleanup(func() { s.closeTerminals(func(*terminal) bool { return true }, 2*time.Second) })
	return &sshTest{s: s, h: h, web: web, ssh: srv, cookie: login(t, h), audit: ab, db: db}
}

// dial opens the terminal WebSocket of agent 1 with the operator's cookie.
func (st *sshTest) dial(t *testing.T) (*websocket.Conn, error) {
	t.Helper()
	cfg, _ := websocket.NewConfig(strings.Replace(st.web.URL, "http", "ws", 1)+"/api/agents/1/ssh", st.web.URL)
	cfg.Header = http.Header{"Cookie": {st.cookie.Name + "=" + st.cookie.Value}}
	return websocket.DialConfig(cfg)
}

// hello sends the first message.
func hello(t *testing.T, ws *websocket.Conn, v map[string]any) {
	t.Helper()
	b, _ := json.Marshal(v)
	if err := frameCodec.Send(ws, termFrame{websocket.TextFrame, b}); err != nil {
		t.Fatal(err)
	}
}

// control reads frames until a control message and returns it.
func control(t *testing.T, ws *websocket.Conn) map[string]string {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		var f termFrame
		if err := frameCodec.Receive(ws, &f); err != nil {
			t.Fatalf("no control message: %v", err)
		}
		if f.typ == websocket.TextFrame {
			m := map[string]string{}
			_ = json.Unmarshal(f.data, &m)
			return m
		}
	}
}

// waitFor reads terminal output until it contains want.
func waitFor(t *testing.T, ws *websocket.Conn, want string) {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	var out []byte
	for !bytes.Contains(out, []byte(want)) {
		var f termFrame
		if err := frameCodec.Receive(ws, &f); err != nil {
			t.Fatalf("waiting for %q, got %q: %v", want, out, err)
		}
		if f.typ == websocket.BinaryFrame {
			out = append(out, f.data...)
		}
	}
}

// waitForSize types `size` until the shell reports want: the resize travels as an SSH channel request, apart from
// the keystrokes, so the test server may apply it after a `size` sent right behind it.
func waitForSize(t *testing.T, ws *websocket.Conn, want string) {
	t.Helper()
	for range 50 {
		typeIn(t, ws, "size\r")
		_ = ws.SetReadDeadline(time.Now().Add(10 * time.Second))
		var out []byte
		for !bytes.Contains(out, []byte("\r\n$ ")) {
			var f termFrame
			if err := frameCodec.Receive(ws, &f); err != nil {
				t.Fatalf("waiting for %q, got %q: %v", want, out, err)
			}
			if f.typ == websocket.BinaryFrame {
				out = append(out, f.data...)
			}
		}
		if bytes.Contains(out, []byte(want)) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the PTY never reported %s", want)
}

func typeIn(t *testing.T, ws *websocket.Conn, s string) {
	t.Helper()
	if err := frameCodec.Send(ws, termFrame{websocket.BinaryFrame, []byte(s)}); err != nil {
		t.Fatal(err)
	}
}

// open trusts the host key and returns a ready terminal.
func (st *sshTest) open(t *testing.T) *websocket.Conn {
	t.Helper()
	ws, err := st.dial(t)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	hello(t, ws, map[string]any{"user": "root", "password": sshPassword, "cols": 80, "rows": 24,
		"trust": ssh.FingerprintSHA256(st.ssh.host.PublicKey())})
	if m := control(t, ws); m["type"] != "ready" {
		t.Fatalf("open: %v", m)
	}
	return ws
}

func TestSSHTerminalOff(t *testing.T) {
	_, h := newTestStore(t)
	rec := do(h, http.MethodGet, "/api/agents/1/ssh", "", login(t, h))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("switch off: %d, want 404", rec.Code)
	}
}

func TestSSHTerminalGuards(t *testing.T) {
	st := newSSHTest(t, false)
	for _, origin := range []string{"", "null", "https://evil.example", "http://127.0.0.1:1"} {
		req := httptest.NewRequest(http.MethodGet, "/api/agents/1/ssh", nil)
		req.Host = "127.0.0.1:8443"
		req.AddCookie(st.cookie)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		st.h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("Origin %q: %d, want 403", origin, rec.Code)
		}
	}
	if rec := do(st.h, http.MethodGet, "/api/agents/1/ssh", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no session: %d, want 401", rec.Code)
	}
	// a bad first message never dials the server
	for _, m := range []map[string]any{{"user": "root", "password": "x", "cols": 5000, "rows": 24},
		{"user": "", "password": "x", "cols": 80, "rows": 24}, {"user": "root", "cols": 80, "rows": 24}} {
		ws, err := st.dial(t)
		if err != nil {
			t.Fatal(err)
		}
		hello(t, ws, m)
		if c := control(t, ws); c["code"] != codeBadRequest {
			t.Errorf("%v: %v", m, c)
		}
		ws.Close()
	}
	if n := st.ssh.accepts.Load(); n != 0 {
		t.Errorf("bad first messages dialed the server %d times", n)
	}
}

func TestSSHTerminalSession(t *testing.T) {
	st := newSSHTest(t, false)
	fp := ssh.FingerprintSHA256(st.ssh.host.PublicKey())

	// first use: the fingerprint, nothing sent to the server
	ws, err := st.dial(t)
	if err != nil {
		t.Fatal(err)
	}
	hello(t, ws, map[string]any{"user": "root", "password": sshPassword, "cols": 80, "rows": 24})
	if m := control(t, ws); m["type"] != "hostkey" || m["fingerprint"] != fp {
		t.Fatalf("first use: %v", m)
	}
	ws.Close()
	// a confirmed fingerprint that is not the server's
	ws, _ = st.dial(t)
	hello(t, ws, map[string]any{"user": "root", "password": sshPassword, "cols": 80, "rows": 24, "trust": "SHA256:other"})
	if m := control(t, ws); m["type"] != "hostkey" {
		t.Fatalf("wrong trust: %v", m)
	}
	ws.Close()
	if n := st.ssh.attempts.Load(); n != 0 {
		t.Fatalf("credentials were sent before the host key was trusted (%d attempts)", n)
	}

	ws = st.open(t)
	waitFor(t, ws, "$ ")
	typeIn(t, ws, "echo $((6*7))\r")
	waitFor(t, ws, "\r\n42\r\n")
	b, _ := json.Marshal(map[string]any{"type": "resize", "cols": 120, "rows": 40})
	_ = frameCodec.Send(ws, termFrame{websocket.TextFrame, b})
	// binary input that happens to be resize JSON is typed, not obeyed
	typeIn(t, ws, `{"type":"resize","cols":1,"rows":1}`+"\r")
	waitForSize(t, ws, "120x40")
	typeIn(t, ws, "exit\r")
	if m := control(t, ws); m["code"] != codeClosed {
		t.Fatalf("exit: %v", m)
	}
	time.Sleep(100 * time.Millisecond) // ssh.close is written as the handler returns
	log := st.audit.String()
	for _, want := range []string{`"action":"ssh.hostkey.trust","target":"fs01"`, `"action":"ssh.open","target":"fs01:root","result":"ok"`, `"action":"ssh.close"`} {
		if !strings.Contains(log, want) {
			t.Errorf("audit lacks %s:\n%s", want, log)
		}
	}
	db, _ := os.ReadFile(st.db)
	wal, _ := os.ReadFile(st.db + "-wal")
	if strings.Contains(log, sshPassword) || bytes.Contains(db, []byte(sshPassword)) || bytes.Contains(wal, []byte(sshPassword)) {
		t.Fatal("the SSH password leaked into the audit log or the database")
	}
}

func TestSSHTerminalWrongPassword(t *testing.T) {
	st := newSSHTest(t, false)
	ws, _ := st.dial(t)
	defer ws.Close()
	hello(t, ws, map[string]any{"user": "root", "password": "wrong-Password-3", "cols": 80, "rows": 24,
		"trust": ssh.FingerprintSHA256(st.ssh.host.PublicKey())})
	if m := control(t, ws); m["code"] != codeAuth {
		t.Fatalf("wrong password: %v", m)
	}
	log := st.audit.String()
	if !strings.Contains(log, `"action":"ssh.open","target":"fs01:root","result":"auth_failed"`) {
		t.Errorf("audit lacks the failure: %s", log)
	}
	if strings.Contains(log, "wrong-Password-3") || strings.Contains(log, "unable to authenticate") {
		t.Fatalf("the audit log carries the password or the library's text: %s", log)
	}
}

func TestSSHTerminalKey(t *testing.T) {
	st := newSSHTest(t, false)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	st.ssh.userKey, _ = ssh.NewPublicKey(pub)
	blk, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte("key-Passphrase-5"))
	if err != nil {
		t.Fatal(err)
	}
	ws, _ := st.dial(t)
	defer ws.Close()
	hello(t, ws, map[string]any{"user": "root", "key": string(pem.EncodeToMemory(blk)), "passphrase": "key-Passphrase-5",
		"cols": 80, "rows": 24, "trust": ssh.FingerprintSHA256(st.ssh.host.PublicKey())})
	if m := control(t, ws); m["type"] != "ready" {
		t.Fatalf("key login: %v", m)
	}
	if strings.Contains(st.audit.String(), "key-Passphrase-5") {
		t.Fatal("the passphrase leaked into the audit log")
	}
}

func TestSSHTerminalHostKeyChanged(t *testing.T) {
	st := newSSHTest(t, false)
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	sg, _ := ssh.NewSignerFromKey(other)
	if ok, _ := st.s.store.TrustHostKey(1, 0, strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sg.PublicKey())))); !ok {
		t.Fatal("pin")
	}
	ws, _ := st.dial(t)
	defer ws.Close()
	hello(t, ws, map[string]any{"user": "root", "password": sshPassword, "cols": 80, "rows": 24,
		"trust": ssh.FingerprintSHA256(st.ssh.host.PublicKey())})
	if m := control(t, ws); m["code"] != codeHostChanged {
		t.Fatalf("changed key: %v", m)
	}
	if n := st.ssh.attempts.Load(); n != 0 {
		t.Fatalf("credentials sent to a server with a changed key (%d attempts)", n)
	}
	// the reset clears it; moving the agent to another host does too
	if rec := do(st.h, http.MethodDelete, "/api/agents/1/ssh/hostkey", "", st.cookie); rec.Code != http.StatusNoContent {
		t.Fatalf("reset: %d", rec.Code)
	}
	st.open(t).Close()
	rec := do(st.h, http.MethodPut, "/api/agents/1", `{"name":"fs01","url":"https://127.0.0.2:9443"}`, st.cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("move: %d %s", rec.Code, rec.Body)
	}
	if k, _, _ := st.s.store.HostKey(1); k != "" {
		t.Error("the key survived the move to another host")
	}
	if !strings.Contains(st.audit.String(), `"target":"fs01 (url changed)"`) {
		t.Error("the move's reset is not audited")
	}
}

func TestSSHTerminalLimitAndClose(t *testing.T) {
	st := newSSHTest(t, false)
	var open []*websocket.Conn
	for range maxTerminals {
		open = append(open, st.open(t))
	}
	if _, err := st.dial(t); err == nil {
		t.Fatal("a fifth terminal was admitted")
	}
	// switching off closes them
	b, _ := json.Marshal(Settings{Lang: "en", SessionMinutes: 30, InactiveDays: 90, PasswordLength: 14, Theme: themeDefault})
	if rec := do(st.h, http.MethodPut, "/api/settings", string(b), st.cookie); rec.Code != http.StatusOK {
		t.Fatalf("switch off: %d %s", rec.Code, rec.Body)
	}
	for _, ws := range open {
		_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			var f termFrame
			if err := frameCodec.Receive(ws, &f); err != nil {
				break
			}
		}
	}
	if n := openTerms(st.s); n != 0 {
		t.Fatalf("%d terminals left after switch-off", n)
	}
}

// openTerms counts the registered terminals once they settle: a slot is released just after its socket closes.
func openTerms(s *Server) int {
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		s.mu.Lock()
		n := len(s.terms)
		s.mu.Unlock()
		if n == 0 || time.Now().After(deadline) {
			return n
		}
	}
}

func TestSSHTerminalLogoutAndExpiry(t *testing.T) {
	old := termCheck
	termCheck = 50 * time.Millisecond
	t.Cleanup(func() { termCheck = old })
	st := newSSHTest(t, false)
	ws := st.open(t)
	if rec := do(st.h, http.MethodPost, "/api/logout", "", st.cookie); rec.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", rec.Code)
	}
	assertClosed(t, ws)

	st.cookie = login(t, st.h)
	ws = st.open(t)
	st.s.mu.Lock()
	st.s.sessions[st.cookie.Value].seen = time.Now().Add(-time.Hour) // idle with no panel request
	st.s.mu.Unlock()
	assertClosed(t, ws)
	time.Sleep(100 * time.Millisecond)
	if n := strings.Count(st.audit.String(), `"action":"ssh.close"`); n != 2 {
		t.Errorf("ssh.close lines = %d, want 2", n)
	}
	// input never revives an expired session
	st.s.mu.Lock()
	st.s.sessions["x"] = &session{user: "admin", seen: time.Now().Add(-time.Hour)}
	live := st.s.liveSession("x", true)
	_, kept := st.s.sessions["x"]
	st.s.mu.Unlock()
	if live || kept {
		t.Error("an expired session was revived")
	}
}

func assertClosed(t *testing.T, ws *websocket.Conn) {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		var f termFrame
		err := frameCodec.Receive(ws, &f)
		if err == nil {
			continue
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			t.Fatal("the terminal stayed open")
		}
		return
	}
}

func TestSSHTerminalStall(t *testing.T) {
	old := termStartWait
	termStartWait = 300 * time.Millisecond
	t.Cleanup(func() { termStartWait = old })
	st := newSSHTest(t, true)
	ws, _ := st.dial(t)
	defer ws.Close()
	hello(t, ws, map[string]any{"user": "root", "password": sshPassword, "cols": 80, "rows": 24})
	if m := control(t, ws); m["code"] != codeTimeout {
		t.Fatalf("stalled server: %v", m)
	}
	if openTerms(st.s) != 0 {
		t.Fatal("the slot was not released")
	}
}

func TestSSHTerminalSilentClient(t *testing.T) {
	old := termHelloWait
	termHelloWait = 200 * time.Millisecond
	t.Cleanup(func() { termHelloWait = old })
	st := newSSHTest(t, false)
	ws, _ := st.dial(t)
	defer ws.Close()
	assertClosed(t, ws)
	if st.ssh.accepts.Load() != 0 {
		t.Fatal("a silent client dialed the server")
	}
}

func TestHostKeyAlgos(t *testing.T) {
	if got := hostKeyAlgos(""); got[0] != ssh.KeyAlgoED25519 {
		t.Errorf("unpinned: %v, want Ed25519 first", got)
	}
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	sg, _ := ssh.NewSignerFromKey(priv)
	if got := hostKeyAlgos(string(ssh.MarshalAuthorizedKey(sg.PublicKey()))); len(got) != 1 || got[0] != ssh.KeyAlgoED25519 {
		t.Errorf("pinned Ed25519: %v", got)
	}
}

func TestSSHTerminalFailedUpgradeReleasesSlot(t *testing.T) {
	st := newSSHTest(t, false)
	for range maxTerminals + 1 { // same origin and session, but no WebSocket upgrade headers
		req, _ := http.NewRequest(http.MethodGet, st.web.URL+"/api/agents/1/ssh", nil)
		req.Header.Set("Origin", st.web.URL)
		req.AddCookie(&http.Cookie{Name: st.cookie.Name, Value: st.cookie.Value})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if n := openTerms(st.s); n != 0 {
		t.Fatalf("%d slots held by failed upgrades", n)
	}
	st.open(t).Close()
}

func TestSSHTerminalResizeIsNotActivity(t *testing.T) {
	st := newSSHTest(t, false)
	ws := st.open(t)
	old := time.Now().Add(-time.Minute)
	st.s.mu.Lock()
	st.s.sessions[st.cookie.Value].seen = old
	st.s.mu.Unlock()
	b, _ := json.Marshal(map[string]any{"type": "resize", "cols": 100, "rows": 30})
	_ = frameCodec.Send(ws, termFrame{websocket.TextFrame, b})
	waitForSize(t, ws, "100x30")
	st.s.mu.Lock()
	seen := st.s.sessions[st.cookie.Value].seen
	st.s.mu.Unlock()
	if !seen.After(old) {
		t.Fatal("typing did not count as activity")
	}
	st.s.mu.Lock()
	st.s.sessions[st.cookie.Value].seen = old
	st.s.mu.Unlock()
	_ = frameCodec.Send(ws, termFrame{websocket.TextFrame, b})
	time.Sleep(200 * time.Millisecond)
	st.s.mu.Lock()
	seen = st.s.sessions[st.cookie.Value].seen
	st.s.mu.Unlock()
	if !seen.Equal(old) {
		t.Fatal("a resize refreshed the session")
	}
}
