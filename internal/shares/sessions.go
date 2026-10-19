package shares

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Session is a client connected to the file server (one smbd process serves it).
type Session struct {
	ID         string   `json:"id"`
	PID        int      `json:"pid"`
	User       string   `json:"user"`
	Group      string   `json:"group"`
	Machine    string   `json:"machine"`
	Dialect    string   `json:"dialect"`
	Encryption string   `json:"encryption"`
	Signing    string   `json:"signing"`
	Since      string   `json:"since"`
	Shares     []string `json:"shares"`
}

// OpenFile is a file a session holds open.
type OpenFile struct {
	Path     string `json:"path"`
	Share    string `json:"share_path"`
	Name     string `json:"name"`
	PID      int    `json:"pid"`
	User     string `json:"user"`
	Machine  string `json:"machine"` // of the session(s) the smbd process serves
	Access   string `json:"access"`  // R, W, RW…
	OpenedAt string `json:"opened_at"`
}

// Status is what the file server serves now.
type Status struct {
	Sessions []Session  `json:"sessions"`
	Files    []OpenFile `json:"open_files"`
}

// smbstatus is the part of `smbstatus --json` (Samba ≥ 4.16) read here.
type smbstatus struct {
	Sessions map[string]struct {
		SessionID  string                  `json:"session_id"`
		Server     struct{ PID string }    `json:"server_id"`
		UID        int                     `json:"uid"`
		Username   string                  `json:"username"`
		Groupname  string                  `json:"groupname"`
		Remote     string                  `json:"remote_machine"`
		Dialect    string                  `json:"session_dialect"`
		Created    string                  `json:"creation_time"`
		Encryption struct{ Degree string } `json:"encryption"`
		Signing    struct{ Degree string } `json:"signing"`
	} `json:"sessions"`
	Tcons map[string]struct {
		Service   string `json:"service"`
		SessionID string `json:"session_id"`
	} `json:"tcons"`
	OpenFiles map[string]struct {
		ServicePath string `json:"service_path"`
		Filename    string `json:"filename"`
		Opens       map[string]struct {
			Server    struct{ PID string }  `json:"server_id"`
			UID       int                   `json:"uid"`
			Sharemode struct{ Text string } `json:"access_mask"`
			OpenedAt  string                `json:"opened_at"`
		} `json:"opens"`
	} `json:"open_files"`
}

// parseStatus turns `smbstatus --json` into sessions (oldest first) and open files (by path).
func parseStatus(out []byte) (*Status, error) {
	var raw smbstatus
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("smbstatus: invalid JSON: %w", err)
	}
	st := &Status{Sessions: []Session{}, Files: []OpenFile{}}
	users := map[int]string{}
	machines := map[int][]string{}
	for _, s := range raw.Sessions {
		pid, _ := strconv.Atoi(s.Server.PID)
		users[s.UID] = s.Username
		if !slices.Contains(machines[pid], s.Remote) {
			machines[pid] = append(machines[pid], s.Remote)
		}
		shares := []string{} // none yet while the client authenticates
		for _, t := range raw.Tcons {
			if t.SessionID == s.SessionID && !slices.Contains(shares, t.Service) {
				shares = append(shares, t.Service)
			}
		}
		slices.Sort(shares)
		st.Sessions = append(st.Sessions, Session{ID: s.SessionID, PID: pid, User: s.Username, Group: s.Groupname,
			Machine: s.Remote, Dialect: s.Dialect, Encryption: s.Encryption.Degree, Signing: s.Signing.Degree,
			Since: s.Created, Shares: shares})
	}
	slices.SortFunc(st.Sessions, func(a, b Session) int { return strings.Compare(a.Since+a.ID, b.Since+b.ID) })
	for pid := range machines {
		slices.Sort(machines[pid])
	}
	for path, f := range raw.OpenFiles {
		for _, o := range f.Opens {
			pid, _ := strconv.Atoi(o.Server.PID)
			st.Files = append(st.Files, OpenFile{Path: path, Share: f.ServicePath, Name: f.Filename, PID: pid,
				User: users[o.UID], Machine: strings.Join(machines[pid], ", "), Access: o.Sharemode.Text, OpenedAt: o.OpenedAt})
		}
	}
	slices.SortFunc(st.Files, func(a, b OpenFile) int { return strings.Compare(a.Path+a.OpenedAt, b.Path+b.OpenedAt) })
	return st, nil
}

// Status lists the connected sessions and open files (`smbstatus --json`).
func (m *Manager) Status(ctx context.Context) (*Status, error) {
	out, err := m.Run(ctx, "smbstatus", "--json")
	if err != nil {
		return nil, err
	}
	return parseStatus([]byte(out))
}

// CloseReq asks to disconnect a session: the smbd process serving it and the session itself (both must match what
// smbstatus shows).
type CloseReq struct {
	PID       int    `json:"pid"`
	SessionID string `json:"session_id"`
}

// served returns every session the process of r serves, once r is one of them.
func (m *Manager) served(ctx context.Context, r CloseReq) ([]Session, error) {
	st, err := m.Status(ctx)
	if err != nil {
		return nil, err
	}
	var all []Session
	found := false
	for _, s := range st.Sessions {
		if s.PID == r.PID {
			all = append(all, s)
			found = found || s.ID == r.SessionID
		}
	}
	if !found {
		return nil, fmt.Errorf("%w: session %s (pid %d) is no longer connected", ErrInvalid, r.SessionID, r.PID)
	}
	return all, nil
}

// CloseSession disconnects a session by ending the smbd process that serves it (SIGTERM: smbd exits cleanly, the
// client loses its open files and reconnects on its next access). A process may serve more than one session: all of
// them end, and all are returned. The process is pinned before it is checked (Kill), so a reused pid is never hit.
func (m *Manager) CloseSession(ctx context.Context, r CloseReq) ([]Session, error) {
	if r.PID <= 1 || r.SessionID == "" {
		return nil, fmt.Errorf("%w: pid and session_id required", ErrInvalid)
	}
	kill := m.Kill
	if kill == nil {
		kill = killPinned
	}
	var closed []Session
	err := kill(r.PID, func() (err error) {
		closed, err = m.served(ctx, r)
		return err
	})
	if err != nil {
		return nil, err
	}
	return closed, nil
}
