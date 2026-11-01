package panel

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// terminalPanel is an HTTPS panel whose /api/agents/1/ssh echoes frames back with their type, only with the session
// cookie and the panel's own origin (coder/websocket's Accept compares Origin with Host, as the panel does).
func terminalPanel(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/login":
			http.SetCookie(w, &http.Cookie{Name: "adm_session", Value: "s3cr3t", Path: "/", HttpOnly: true, Secure: true})
			_, _ = io.WriteString(w, `{"ok":true}`)
		case "/api/agents/1/ssh":
			if c, err := r.Cookie("adm_session"); err != nil || c.Value != "s3cr3t" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			c, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer c.CloseNow()
			for {
				typ, b, err := c.Read(context.Background())
				if err != nil || c.Write(context.Background(), typ, b) != nil {
					return
				}
			}
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"the SSH terminal is off"}`)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func TestTerminal(t *testing.T) {
	s := terminalPanel(t)
	p := pinned(t, s)
	ctx := context.Background()
	if _, _, err := p.Terminal(ctx, p.Generation(), 1); !errors.Is(err, ErrSessionEnded) {
		t.Fatalf("signed out: %v", err)
	}
	do(t, p, http.MethodPost, "/api/login", "")
	gen := p.Generation()
	if _, _, err := p.Terminal(ctx, gen-1, 1); !errors.Is(err, ErrStale) {
		t.Fatalf("stale generation: %v", err)
	}
	if _, _, err := p.Terminal(ctx, gen, 2); err == nil || err.Error() != "the SSH terminal is off" {
		t.Fatalf("refused upgrade: %v", err)
	}
	c, release, err := p.Terminal(ctx, gen, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for _, text := range []bool{true, false} { // the frame's type comes back as sent
		if err := Send(c, text, []byte(`{"type":"resize"}`)); err != nil {
			t.Fatal(err)
		}
		typ, b, err := c.Read(ctx)
		if err != nil || (typ == websocket.MessageText) != text || string(b) != `{"type":"resize"}` {
			t.Fatalf("echo: %v %q %v", typ, b, err)
		}
	}
	// signing out closes it
	do(t, p, http.MethodPost, "/api/logout", "")
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, _, err := c.Read(rctx); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the terminal outlived its session: %v", err)
	}
}

func TestTerminalPinnedCertificate(t *testing.T) {
	s := terminalPanel(t)
	// every httptest server has the same certificate: a pin of other bytes stands for another one
	p, err := New(s.URL, CertPin([]byte("another certificate")), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Terminal(context.Background(), p.Generation(), 1); !errors.Is(err, ErrCertChanged) {
		t.Fatalf("another certificate: %v", err)
	}
}
