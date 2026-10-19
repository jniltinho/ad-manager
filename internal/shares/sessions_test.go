package shares

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"
)

// testdata/smbstatus.json: `smbstatus --json` of Samba 4.23 with one user holding a file open.
func statusRunner(t *testing.T, calls *[][]string) Runner {
	out, err := os.ReadFile("testdata/smbstatus.json")
	if err != nil {
		t.Fatal(err)
	}
	return func(_ context.Context, name string, args ...string) (string, error) {
		*calls = append(*calls, append([]string{name}, args...))
		if name == "smbstatus" {
			return string(out), nil
		}
		return "", nil
	}
}

func TestStatus(t *testing.T) {
	var calls [][]string
	m := &Manager{Run: statusRunner(t, &calls)}
	st, err := m.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Sessions) != 1 || len(st.Files) != 1 {
		t.Fatalf("got %+v", st)
	}
	s, f := st.Sessions[0], st.Files[0]
	if s.User != "ana" || s.PID != 281 || s.Machine != "127.0.0.1" || s.Dialect != "SMB3_11" || !slices.Equal(s.Shares, []string{"Financeiro"}) {
		t.Fatalf("session: %+v", s)
	}
	if f.Path != "/srv/shares/financeiro/planilha.xlsx" || f.User != "ana" || f.PID != 281 || f.Access != "RW" || f.OpenedAt == "" {
		t.Fatalf("open file: %+v", f)
	}
	if _, err := parseStatus([]byte("not json")); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}

func TestCloseSession(t *testing.T) {
	// fakeKill records the pid it would end, only when the check passes
	fakeKill := func(killed *int) func(int, func() error) error {
		return func(pid int, check func() error) error {
			if err := check(); err != nil {
				return err
			}
			*killed = pid
			return nil
		}
	}
	for _, tt := range []struct {
		name string
		req  CloseReq
	}{
		{name: "no pid", req: CloseReq{SessionID: "1041753017"}},
		{name: "init", req: CloseReq{PID: 1, SessionID: "1041753017"}},
		{name: "pid of another session", req: CloseReq{PID: 282, SessionID: "1041753017"}},
		{name: "session gone", req: CloseReq{PID: 281, SessionID: "1"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var calls [][]string
			killed := 0
			m := &Manager{Run: statusRunner(t, &calls), Kill: fakeKill(&killed)}
			if _, err := m.CloseSession(context.Background(), tt.req); !errors.Is(err, ErrInvalid) || killed != 0 {
				t.Fatalf("got %v (killed %d), want ErrInvalid and nothing ended", err, killed)
			}
		})
	}
	var calls [][]string
	killed := 0
	m := &Manager{Run: statusRunner(t, &calls), Kill: fakeKill(&killed)}
	closed, err := m.CloseSession(context.Background(), CloseReq{PID: 281, SessionID: "1041753017"})
	if err != nil || killed != 281 || len(closed) != 1 || closed[0].User != "ana" {
		t.Fatalf("close: %+v %v (killed %d)", closed, err, killed)
	}
}

func TestStatusEdges(t *testing.T) {
	// two sessions in one smbd process (one still authenticating, no share yet)
	st, err := parseStatus([]byte(`{"sessions": {
		"1": {"session_id": "1", "server_id": {"pid": "50"}, "uid": 10, "username": "ana", "remote_machine": "10.0.0.5"},
		"2": {"session_id": "2", "server_id": {"pid": "50"}, "uid": 11, "username": "joao", "remote_machine": "10.0.0.6"}},
		"tcons": {}, "open_files": {"/srv/a": {"service_path": "/srv", "filename": "a",
		"opens": {"50/1": {"server_id": {"pid": "50"}, "uid": 10, "access_mask": {"text": "R"}}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if st.Sessions[0].Shares == nil || st.Files[0].Machine != "10.0.0.5, 10.0.0.6" {
		t.Fatalf("got %+v", st)
	}
	var calls [][]string
	m := &Manager{Run: func(_ context.Context, name string, args ...string) (string, error) {
		calls = append(calls, args)
		return `{"sessions": {"1": {"session_id": "1", "server_id": {"pid": "50"}}, "2": {"session_id": "2", "server_id": {"pid": "50"}}}}`, nil
	}, Kill: func(_ int, check func() error) error { return check() }}
	closed, err := m.CloseSession(context.Background(), CloseReq{PID: 50, SessionID: "2"})
	if err != nil || len(closed) != 2 {
		t.Fatalf("every session of the process is reported: %+v %v", closed, err)
	}
}
