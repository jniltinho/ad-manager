package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const panelConf = `# panel
[ad]
url = "ldaps://dc1.example.local" # first DC
domain = "example.local"

[agent]
import_modes = false
`

// agentRestarts counts restartAgent calls (reset by fakePanel).
var agentRestarts int

// fakePanel replaces the systemd calls; ready answers in turn (the last one repeats).
func fakePanel(t *testing.T, ready ...error) *int {
	t.Helper()
	restarts := 0
	agentRestarts = 0
	u, r, p, ra := panelUnit, restartPanel, panelReady, restartAgent
	t.Cleanup(func() { panelUnit, restartPanel, panelReady, restartAgent = u, r, p, ra })
	panelUnit = func() error { return nil }
	restartAgent = func() error { agentRestarts++; return nil }
	restartPanel = func(context.Context) error { restarts++; return nil }
	panelReady = func(context.Context, string) error {
		err := ready[0]
		if len(ready) > 1 {
			ready = ready[1:]
		}
		return err
	}
	return &restarts
}

func panelAgent(t *testing.T) (*Agent, string) {
	f := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(f, []byte(panelConf), 0o640); err != nil {
		t.Fatal(err)
	}
	return &Agent{configFile: f, stateDir: filepath.Dir(f)}, f
}

func payload(t *testing.T, f string, keys map[string]any) []byte {
	sum, _, err := fileSHA256(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(f), PanelNonceFile), []byte("n1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(PanelConfigReq{ConfigSHA256: sum, Nonce: "n1", Keys: keys})
	return b
}

func TestPanelConfigApplies(t *testing.T) {
	restarts := fakePanel(t, nil)
	a, f := panelAgent(t)
	if err := a.applyPanelConfig(context.Background(), payload(t, f, map[string]any{"ad.url": "ldaps://dc2.example.local"})); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(f)
	bak, _ := os.ReadFile(f + ".bak")
	if !strings.Contains(string(b), "url = 'ldaps://dc2.example.local'\n") || !strings.Contains(string(b), "# panel\n") ||
		string(bak) != panelConf || *restarts != 1 {
		t.Fatalf("restarts %d, file:\n%s", *restarts, b)
	}
	if _, err := os.Stat(a.markerFile()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("marker left behind")
	}
}

func TestPanelConfigRollsBack(t *testing.T) {
	restarts := fakePanel(t, errors.New("no answer"), nil)
	a, f := panelAgent(t)
	err := a.applyPanelConfig(context.Background(), payload(t, f, map[string]any{"ad.url": "ldaps://dc2.example.local"}))
	if err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(f); string(b) != panelConf || *restarts != 2 {
		t.Fatalf("restarts %d, file:\n%s", *restarts, b)
	}
}

func TestPanelConfigRefuses(t *testing.T) {
	fakePanel(t, nil)
	a, f := panelAgent(t)
	for name, p := range map[string][]byte{
		"agent key":  payload(t, f, map[string]any{"agent.sync_unconfined": true}),
		"redis":      payload(t, f, map[string]any{"redis.addr": "evil:6379"}),
		"bad url":    payload(t, f, map[string]any{"ad.url": "http://dc2"}),
		"other file": []byte(`{"config_sha256":"00","nonce":"n1","keys":{"ad.url":"ldaps://dc2.example.local"}}`),
		"copied file, other disk": func() []byte {
			p := payload(t, f, map[string]any{"ad.url": "ldaps://dc2.example.local"})
			return []byte(strings.Replace(string(p), `"nonce":"n1"`, `"nonce":"n2"`, 1))
		}(),
	} {
		if err := a.validate(TaskReq{Type: TypePanelConfig, Payload: p}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if b, _ := os.ReadFile(f); string(b) != panelConf {
		t.Fatal("config.toml changed")
	}
	panelUnit = func() error { return errors.New("no ad-manager unit") }
	if a.validate(TaskReq{Type: TypePanelConfig, Payload: payload(t, f, map[string]any{"ad.url": "ldaps://dc2.example.local"})}) == nil {
		t.Fatal("accepted without the panel on this host")
	}
}

func TestPanelConfigRollbackKeepsTheMarker(t *testing.T) {
	fakePanel(t, errors.New("no answer")) // the panel never comes back, not even with the previous file
	a, f := panelAgent(t)
	if err := a.applyPanelConfig(context.Background(), payload(t, f, map[string]any{"ad.url": "ldaps://dc2.example.local"})); err == nil {
		t.Fatal("no error")
	}
	if _, err := os.Stat(a.markerFile()); err != nil {
		t.Fatal("the marker is gone: the next start would not try again")
	}
}

func TestPanelConfigRecovers(t *testing.T) {
	restarts := fakePanel(t, nil)
	a, f := panelAgent(t)
	_ = os.WriteFile(f+".bak", []byte(panelConf), 0o640)
	_ = os.WriteFile(f, []byte("half applied"), 0o640)
	_ = os.WriteFile(a.markerFile(), []byte("ad.url\n"), 0o600)
	a.recoverPanelConfig(context.Background())
	if b, _ := os.ReadFile(f); string(b) != panelConf || *restarts != 1 {
		t.Fatalf("not restored: %s", b)
	}
	if _, err := os.Stat(a.markerFile()); err == nil {
		t.Fatal("marker left behind")
	}
}

func TestPanelConfigAgentSwitches(t *testing.T) {
	restarts := fakePanel(t, nil)
	a, f := panelAgent(t)
	keys := map[string]any{"agent.import_modes": true, "agent.dc_file_server": true}
	if err := a.applyPanelConfig(context.Background(), payload(t, f, keys)); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(f)
	if !strings.Contains(string(b), "import_modes = true\n") || !strings.Contains(string(b), "dc_file_server = true\n") ||
		*restarts != 0 || agentRestarts != 1 {
		t.Fatalf("panel restarts %d, agent restarts %d, file:\n%s", *restarts, agentRestarts, b)
	}
}
