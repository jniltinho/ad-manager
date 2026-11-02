package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"ad-manager/internal/ad"
	"ad-manager/internal/agent"
	"ad-manager/internal/store"
)

// configTest is a panel with a config.toml and a registered agent; the directory admits only admin_group "" or
// "Domain Admins", and the agent records the tasks it gets.
func configTest(t *testing.T, queue string) (*Server, http.Handler, *http.Cookie, func() []agent.TaskReq) {
	var mu sync.Mutex
	var got []agent.TaskReq
	ag := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			fmt.Fprintf(w, `{"status":"ok","queue":%q}`, queue)
		case "/api/tasks":
			var tr agent.TaskReq
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &tr)
			mu.Lock()
			got = append(got, tr)
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprint(w, `{"id":"t1","state":"pending"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ag.Close)
	s, _ := newTestStore(t)
	s.cfg.File = filepath.Join(t.TempDir(), "config.toml")
	s.cfg.DB.Path = filepath.Join(filepath.Dir(s.cfg.File), "t.db") // the state dir: the nonce goes there
	if err := os.WriteFile(s.cfg.File, []byte("[ad]\nurl = \"ldaps://127.0.0.1:1\"\ndomain = \"empresa.test\"\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	s.cfg.Agent.Queue = "fs-panel"
	if err := s.store.SaveAgent(&store.Agent{Name: "dc1", URL: ag.URL, Token: "t"}); err != nil {
		t.Fatal(err)
	}
	s.dial = func(cfg *ad.Config, _, pass string) (directory, error) {
		if pass != goodPassword {
			return nil, ad.ErrBadLogin
		}
		return &fakeDir{admin: cfg.AdminGroup == "" || cfg.AdminGroup == "Domain Admins"}, nil
	}
	h := s.routes()
	return s, h, login(t, h), func() []agent.TaskReq { mu.Lock(); defer mu.Unlock(); return got }
}

func TestPutConfigSendsTheTask(t *testing.T) {
	s, h, c, tasks := configTest(t, "fs-panel")
	rec := do(h, http.MethodPut, "/api/settings/config",
		`{"keys":{"ad.url":"ldaps://dc2.empresa.test","ad.domain":"empresa.test","ad.admin_group":"Domain Admins"}}`, c)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	got := tasks()
	if len(got) != 1 || got[0].Type != agent.TypePanelConfig {
		t.Fatalf("tasks: %+v", got)
	}
	var p agent.PanelConfigReq
	_ = json.Unmarshal(got[0].Payload, &p)
	raw, _ := os.ReadFile(s.cfg.File)
	sumB := sha256.Sum256(raw)
	sum := hex.EncodeToString(sumB[:])
	nonce, _ := os.ReadFile(filepath.Join(s.cfg.StateDir(), agent.PanelNonceFile))
	// ad.domain did not change: only the two changed keys travel
	if p.ConfigSHA256 != sum || p.Nonce == "" || strings.TrimSpace(string(nonce)) != p.Nonce || len(p.Keys) != 2 || p.Keys["ad.url"] != "ldaps://dc2.empresa.test" {
		t.Fatalf("payload: %+v", p)
	}
	rec = do(h, http.MethodGet, "/api/settings/config", "", c)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"dc1"`) || !strings.Contains(rec.Body.String(), `"task":"t1"`) {
		t.Fatalf("get: %d %s", rec.Code, rec.Body)
	}
}

func TestPutConfigRefuses(t *testing.T) {
	_, h, c, tasks := configTest(t, "fs-panel")
	for name, tt := range map[string]struct {
		body string
		code int
	}{
		"lockout":      {`{"keys":{"ad.admin_group":"Nobody"}}`, http.StatusBadRequest},
		"agent key":    {`{"keys":{"agent.sync_unconfined":true}}`, http.StatusBadRequest},
		"redis":        {`{"keys":{"redis.addr":"evil:6379"}}`, http.StatusBadRequest},
		"bad url":      {`{"keys":{"ad.url":"https://dc2"}}`, http.StatusBadRequest},
		"nothing":      {`{"keys":{"ad.url":"ldaps://127.0.0.1:1"}}`, http.StatusBadRequest},
		"generic task": {`{"type":"panel:config","payload":{}}`, http.StatusBadRequest},
	} {
		path := "/api/settings/config"
		if name == "generic task" {
			path = "/api/agents/1/tasks"
		}
		method := http.MethodPut
		if name == "generic task" {
			method = http.MethodPost
		}
		if rec := do(h, method, path, tt.body, c); rec.Code != tt.code {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	if len(tasks()) != 0 {
		t.Fatalf("a task was sent: %+v", tasks())
	}
	t.Setenv("AD_URL", "ldaps://127.0.0.1:1")
	if rec := do(h, http.MethodPut, "/api/settings/config", `{"keys":{"ad.url":"ldaps://dc2.empresa.test"}}`, c); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "AD_URL") {
		t.Fatalf("env lock: %d %s", rec.Code, rec.Body)
	}
}

// TestPutConfigChecksTheFile: a group changed by hand in config.toml (not yet in effect) is checked with the change.
func TestPutConfigChecksTheFile(t *testing.T) {
	s, h, c, tasks := configTest(t, "fs-panel")
	_ = os.WriteFile(s.cfg.File, []byte("[ad]\nurl = \"ldaps://127.0.0.1:1\"\ndomain = \"empresa.test\"\nadmin_group = \"Nobody\"\n"), 0o640)
	rec := do(h, http.MethodPut, "/api/settings/config", `{"keys":{"ad.url":"ldaps://dc2.empresa.test"}}`, c)
	if rec.Code != http.StatusBadRequest || len(tasks()) != 0 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestPutConfigWithoutLocalAgent(t *testing.T) {
	_, h, c, tasks := configTest(t, "fs-other")
	rec := do(h, http.MethodPut, "/api/settings/config", `{"keys":{"ad.url":"ldaps://dc2.empresa.test"}}`, c)
	if rec.Code != http.StatusConflict || len(tasks()) != 0 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := do(h, http.MethodGet, "/api/settings/config", "", c); !strings.Contains(rec.Body.String(), `"agent_error"`) {
		t.Fatalf("get: %s", rec.Body)
	}
}

func TestPutConfigAgentSwitches(t *testing.T) {
	_, h, c, tasks := configTest(t, "fs-panel")
	rec := do(h, http.MethodPut, "/api/settings/config", `{"keys":{"agent.import_modes":true,"agent.dc_file_server":false}}`, c)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var p agent.PanelConfigReq
	_ = json.Unmarshal(tasks()[0].Payload, &p)
	// dc_file_server is already false in the file: only import_modes travels
	if len(p.Keys) != 1 || p.Keys["agent.import_modes"] != true {
		t.Fatalf("payload: %+v", p)
	}
}
