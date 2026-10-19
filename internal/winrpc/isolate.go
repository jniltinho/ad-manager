package winrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"
)

// Request is one call for the helper process.
type Request struct {
	Op     string  `json:"op"` // zones, records, sessions, add_record, delete_record or create_zone
	Target Target  `json:"target"`
	Cred   Cred    `json:"cred"`
	Zone   string  `json:"zone,omitempty"`
	Node   string  `json:"node,omitempty"`
	Record *Record `json:"record,omitempty"`
}

// Response is the helper's answer.
type Response struct {
	Zones    []Zone    `json:"zones,omitempty"`
	Records  []Record  `json:"records,omitempty"`
	Nodes    []Node    `json:"nodes,omitempty"`
	Sessions []Session `json:"sessions,omitempty"`
	Error    string    `json:"error,omitempty"`
}

// Serve runs the request read from r and writes the response to w: the body of `ad-manager winrpc`, the helper
// process of Isolated.
func Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	var req Request
	if err := json.NewDecoder(io.LimitReader(r, 64<<10)).Decode(&req); err != nil {
		return err
	}
	var resp Response
	var err error
	switch req.Op {
	case "zones":
		resp.Zones, err = DNSZones(ctx, req.Target, req.Cred)
	case "records":
		resp.Records, resp.Nodes, err = Records(ctx, req.Target, req.Zone, req.Node, req.Cred)
	case "sessions":
		resp.Sessions, err = Sessions(ctx, req.Target, req.Cred)
	case "add_record", "delete_record":
		if req.Record == nil {
			err = errors.New("no record")
			break
		}
		err = UpdateRecord(ctx, req.Target, req.Zone, *req.Record, req.Op == "delete_record", req.Cred)
	case "create_zone":
		err = CreateZone(ctx, req.Target, req.Zone, req.Cred)
	default:
		err = fmt.Errorf("unknown operation %q", req.Op)
	}
	if err != nil {
		resp.Error = err.Error()
	}
	return json.NewEncoder(w).Encode(resp)
}

// Isolated runs every call in a child process (`<Exe> winrpc`), with the credential on its standard input: a panic
// or a connection that hangs inside the RPC libraries ends that process — killed at the timeout, which also
// releases its sockets — and never the panel.
type Isolated struct {
	Exe     string
	Timeout time.Duration // zero: the call's timeout plus a margin
}

func (i Isolated) run(ctx context.Context, req Request) (*Response, error) {
	limit := i.Timeout
	if limit == 0 {
		limit = timeout + 5*time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	in, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	ctx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	cmd := exec.CommandContext(ctx, i.Exe, "winrpc")
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8"}
	cmd.Stdin = bytes.NewReader(in)
	out := &capped{max: maxReply, stop: stop}
	cmd.Stdout = out
	if err := cmd.Run(); err != nil {
		if cause := context.Cause(ctx); cause != nil {
			return nil, fmt.Errorf("%s: %w", req.Target.Host, cause)
		}
		return nil, fmt.Errorf("%s: the RPC helper failed (%v)", req.Target.Host, err)
	}
	var resp Response
	if err := json.Unmarshal(out.buf.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("%s: invalid reply from the RPC helper", req.Target.Host)
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}
	return &resp, nil
}

// DNSZones is DNSZones in a child process.
func (i Isolated) DNSZones(ctx context.Context, t Target, c Cred) ([]Zone, error) {
	r, err := i.run(ctx, Request{Op: "zones", Target: t, Cred: c})
	if err != nil {
		return nil, err
	}
	return nonNil(r.Zones), nil
}

// Records is Records in a child process.
func (i Isolated) Records(ctx context.Context, t Target, zone, node string, c Cred) ([]Record, []Node, error) {
	r, err := i.run(ctx, Request{Op: "records", Target: t, Cred: c, Zone: zone, Node: node})
	if err != nil {
		return nil, nil, err
	}
	return nonNil(r.Records), nonNil(r.Nodes), nil
}

// Sessions is Sessions in a child process.
func (i Isolated) Sessions(ctx context.Context, t Target, c Cred) ([]Session, error) {
	r, err := i.run(ctx, Request{Op: "sessions", Target: t, Cred: c})
	if err != nil {
		return nil, err
	}
	return nonNil(r.Sessions), nil
}

// UpdateRecord is UpdateRecord in a child process.
func (i Isolated) UpdateRecord(ctx context.Context, t Target, zone string, r Record, del bool, c Cred) error {
	op := "add_record"
	if del {
		op = "delete_record"
	}
	_, err := i.run(ctx, Request{Op: op, Target: t, Cred: c, Zone: zone, Record: &r})
	return err
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// maxReply caps the helper's answer (a DNS zone listing is far smaller).
const maxReply = 8 << 20

// errReplyTooLarge ends a helper whose answer passes maxReply.
var errReplyTooLarge = errors.New("the RPC helper's answer is too large")

// capped keeps at most max bytes; past that it stops (kills) the helper instead of growing. The buffer is a named
// field: an embedded one would lend capped its ReadFrom, and io.Copy would bypass Write.
type capped struct {
	buf  bytes.Buffer
	max  int
	stop context.CancelCauseFunc
}

func (c *capped) Write(p []byte) (int, error) {
	if c.buf.Len()+len(p) > c.max {
		c.stop(errReplyTooLarge)
		return 0, errReplyTooLarge
	}
	return c.buf.Write(p)
}

// CreateZone runs CreateZone in the helper process.
func (i Isolated) CreateZone(ctx context.Context, t Target, zone string, c Cred) error {
	_, err := i.run(ctx, Request{Op: "create_zone", Target: t, Cred: c, Zone: zone})
	return err
}
