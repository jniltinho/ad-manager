package main

import (
	"context"
	"encoding/base64"
	"errors"
	"sync"
	"time"

	"github.com/coder/websocket"

	"ad-manager/desktop/internal/panel"
)

// Terminals is the service behind the SPA's SSH terminal in the desktop: the webview cannot open the panel's
// WebSocket (its asset proxy carries none and the session cookie lives in Go), so Go opens it and passes its frames to
// the page as "adm:terminal" events: {id, text} for the panel's control messages, {id, data} (base64) for terminal
// output, {id, closed} at the end.
type Terminals struct {
	d *desktop

	mu   sync.Mutex
	next int
	open map[int]*websocket.Conn
}

// errNoTerminal is a call for a terminal that is not open.
var errNoTerminal = errors.New("the terminal has closed")

// Open opens the terminal of agent agentID on session gen and returns its id.
func (t *Terminals) Open(gen uint64, agentID int64) (int, error) {
	p := t.d.current()
	if p == nil {
		return 0, errNoPanel
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, release, err := p.Terminal(ctx, gen, agentID)
	if err != nil {
		return 0, err
	}
	t.mu.Lock()
	if t.open == nil {
		t.open = map[int]*websocket.Conn{}
	}
	t.next++
	id := t.next
	t.open[id] = c
	t.mu.Unlock()
	go t.pump(id, c, release)
	return id, nil
}

// pump passes the panel's frames to the page until the terminal ends.
func (t *Terminals) pump(id int, c *websocket.Conn, release func()) {
	defer func() {
		t.mu.Lock()
		delete(t.open, id)
		t.mu.Unlock()
		c.CloseNow()
		release()
		t.d.app.Event.Emit("adm:terminal", map[string]any{"id": id, "closed": true})
	}()
	for {
		typ, b, err := c.Read(context.Background())
		if err != nil {
			return
		}
		if typ == websocket.MessageText {
			t.d.app.Event.Emit("adm:terminal", map[string]any{"id": id, "text": string(b)})
		} else {
			t.d.app.Event.Emit("adm:terminal", map[string]any{"id": id, "data": base64.StdEncoding.EncodeToString(b)})
		}
	}
}

func (t *Terminals) conn(id int) (*websocket.Conn, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if c := t.open[id]; c != nil {
		return c, nil
	}
	return nil, errNoTerminal
}

// Control sends a text frame (the first message with the credential, a resize) to terminal id.
func (t *Terminals) Control(id int, text string) error {
	c, err := t.conn(id)
	if err != nil {
		return err
	}
	return panel.Send(c, true, []byte(text))
}

// Input sends keystrokes to terminal id (a binary frame).
func (t *Terminals) Input(id int, text string) error {
	c, err := t.conn(id)
	if err != nil {
		return err
	}
	return panel.Send(c, false, []byte(text))
}

// Close ends terminal id.
func (t *Terminals) Close(id int) {
	if c, err := t.conn(id); err == nil {
		_ = c.Close(websocket.StatusNormalClosure, "")
	}
}

// Clipboard is the system clipboard's text, for Shift+Insert (the window denies clipboard reads to the page).
func (t *Terminals) Clipboard() string {
	s, _ := t.d.app.Clipboard.Text()
	return s
}
