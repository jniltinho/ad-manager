package panel

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"
)

// Terminal opens the SSH terminal WebSocket of agent agentID on session gen (GET /api/agents/{id}/ssh), as a browser
// on the panel's own page would: the session's cookie and the panel's origin, over the panel's TLS (the pinned
// certificate) on HTTP/1.1, which the upgrade needs; no redirect is followed. It closes when its session ends;
// release drops that hook once the terminal is done (the session may last much longer).
func (p *Panel) Terminal(ctx context.Context, gen uint64, agentID int64) (c *websocket.Conn, release func(), err error) {
	se, err := p.session(gen)
	if err != nil {
		return nil, nil, err
	}
	tc := p.tls.Clone()
	tc.NextProtos = []string{"http/1.1"}
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	tr := &http.Transport{TLSClientConfig: tc, DialContext: d.DialContext, TLSHandshakeTimeout: 10 * time.Second}
	defer tr.CloseIdleConnections() // a refused upgrade's connection must not stay in this one-off transport
	hc := &http.Client{Jar: se.hc.Jar, Transport: tr,
		CheckRedirect: func(*http.Request, []*http.Request) error { return ErrRedirect }}
	origin := p.base.Scheme + "://" + p.base.Host
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(se.ctx, cancel) // a session that ends during the handshake
	defer stop()
	var res *http.Response
	c, res, err = websocket.Dial(ctx, "wss://"+p.base.Host+"/api/agents/"+strconv.FormatInt(agentID, 10)+"/ssh",
		&websocket.DialOptions{HTTPClient: hc, HTTPHeader: http.Header{"Origin": {origin}}})
	if err != nil {
		if se.ctx.Err() != nil {
			return nil, nil, ErrStale
		}
		if res != nil && res.StatusCode != http.StatusSwitchingProtocols {
			if res.StatusCode == http.StatusUnauthorized {
				return nil, nil, ErrSessionEnded
			}
			return nil, nil, panelError(res)
		}
		return nil, nil, cause(err)
	}
	c.SetReadLimit(1 << 20) // the panel's frames are at most 32 KiB
	if !stop() || se.ctx.Err() != nil {
		c.CloseNow()
		return nil, nil, ErrStale
	}
	unhook := context.AfterFunc(se.ctx, func() { c.CloseNow() })
	return c, func() { unhook() }, nil
}

// errTerminalClosed is a frame sent to a terminal that has ended.
var errTerminalClosed = errors.New("the terminal has closed")

// Send writes one frame to a terminal: text (control messages) or binary (keystrokes).
func Send(c *websocket.Conn, text bool, b []byte) error {
	typ := websocket.MessageBinary
	if text {
		typ = websocket.MessageText
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.Write(ctx, typ, b); err != nil {
		return errTerminalClosed
	}
	return nil
}
