package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ad-manager/internal/config"
	"ad-manager/internal/shares"
)

// TypePanelConfig changes the panel's allowlisted keys of config.toml (the agent on the panel's host, as root),
// restarts the panel and rolls back when it does not come back.
const TypePanelConfig = "panel:config"

// PanelConfigReq is the panel:config payload: the keys and the SHA-256 of the config.toml the panel read, which ties
// the task to this host's file and to that version of it.
type PanelConfigReq struct {
	ConfigSHA256 string         `json:"config_sha256"`
	Nonce        string         `json:"nonce"` // also in the panel's state dir (PanelNonceFile): proves the same disk
	Keys         map[string]any `json:"keys"`
}

// PanelNonceFile is where, in the state dir, the panel writes the nonce of its last panel:config task: a copy of
// config.toml on another server does not make that server's agent the panel's.
const PanelNonceFile = "panel-config.nonce"

// panelUnit tells whether the panel runs here under systemd (a supervisord image rewrites config.toml at each start).
var panelUnit = func() error {
	if os.Getenv("SUPERVISOR_ENABLED") != "" {
		return errors.New("the panel's configuration is managed by the container image")
	}
	out, _ := exec.Command("systemctl", "show", "-p", "LoadState", "--value", "ad-manager").Output()
	if strings.TrimSpace(string(out)) != "loaded" {
		return errors.New("the panel does not run on this server (no ad-manager unit)")
	}
	return nil
}

// restartPanel restarts the panel's unit.
var restartPanel = func(ctx context.Context) error {
	if out, err := exec.CommandContext(ctx, "systemctl", "restart", "ad-manager").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl restart ad-manager: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// restartAgent queues a restart of this agent's unit.
var restartAgent = func() error {
	if out, err := exec.Command("systemctl", "restart", "--no-block", "ad-manager-agent").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl restart ad-manager-agent: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// panelReady polls the panel's own /api/health on loopback until it answers (200, or 503 with the DC down: it runs)
// or ctx ends. The certificate is not verified: this only checks that the panel started.
var panelReady = func(ctx context.Context, file string) error {
	cfg, err := config.Load(file)
	if err != nil {
		return err
	}
	host, port, err := net.SplitHostPort(cfg.Server.Listen)
	if err != nil {
		return fmt.Errorf("server.listen: %w", err)
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	scheme := "http"
	if cfg.Server.CertFile != "" || cfg.Server.SelfSigned {
		scheme = "https"
	}
	// url.URL escapes an IPv6 zone ("fe80::1%eth0")
	target := (&url.URL{Scheme: scheme, Host: net.JoinHostPort(host, port), Path: "/api/health"}).String()
	hc := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} //nolint:gosec // readiness only, no data sent
	last := errors.New("no answer")
	for {
		if exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", "ad-manager").Run() == nil {
			if last = probe(ctx, hc, target); last == nil {
				return nil
			}
		} else {
			last = errors.New("the ad-manager unit is not active")
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("the panel did not come back: %w", last)
		case <-time.After(2 * time.Second):
		}
	}
}

// probe accepts only the panel's health answer (it has "ldap"; the agent's has "queue").
func probe(ctx context.Context, hc *http.Client, target string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var h map[string]any
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&h) != nil || h["ldap"] == nil || h["queue"] != nil {
		return fmt.Errorf("%s did not answer as the panel (HTTP %d)", target, resp.StatusCode)
	}
	return nil
}

// fileSHA256 is the hex SHA-256 of a file.
func fileSHA256(path string) (string, []byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), b, nil
}

// checkPanelConfig validates a panel:config payload against this host's config.toml and returns the edited text.
func (a *Agent) checkPanelConfig(payload []byte) (*PanelConfigReq, []byte, []byte, error) {
	var r PanelConfigReq
	if err := json.Unmarshal(payload, &r); err != nil {
		return nil, nil, nil, fmt.Errorf("%w: %v", shares.ErrInvalid, err)
	}
	if err := panelUnit(); err != nil {
		return nil, nil, nil, fmt.Errorf("%w: %v", shares.ErrInvalid, err)
	}
	if a.configFile == "" {
		return nil, nil, nil, fmt.Errorf("%w: the agent runs without a config.toml", shares.ErrInvalid)
	}
	sum, old, err := fileSHA256(a.configFile)
	if err != nil {
		return nil, nil, nil, err
	}
	nonce, _ := os.ReadFile(filepath.Join(a.stateDir, PanelNonceFile))
	if r.ConfigSHA256 != sum || r.Nonce == "" || subtle.ConstantTimeCompare([]byte(r.Nonce), bytes.TrimSpace(nonce)) != 1 {
		return nil, nil, nil, fmt.Errorf("%w: config.toml is not the panel's (another server, or it changed): reload the page", shares.ErrInvalid)
	}
	text, err := config.Edit(old, r.Keys)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: %v", shares.ErrInvalid, err)
	}
	return &r, old, text, nil
}

// markerFile is written before config.toml is replaced and removed once the panel answers (or after the rollback):
// found at the agent's start, it means an apply was cut short.
func (a *Agent) markerFile() string { return a.configFile + ".applying" }

// applyPanelConfig is the panel:config task: check again, keep a .bak, replace config.toml, restart the panel and
// roll back when it does not answer within 60 s.
func (a *Agent) applyPanelConfig(ctx context.Context, payload []byte) error {
	r, old, text, err := a.checkPanelConfig(payload)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(r.Keys))
	panel, self := false, false
	for k := range r.Keys {
		names = append(names, k)
		if config.RestartsAgent(k) {
			self = true
		} else {
			panel = true
		}
	}
	sort.Strings(names)
	keys := strings.Join(names, ",")
	if err := writeLike(a.configFile+".bak", old, a.configFile); err != nil {
		return fmt.Errorf("config.toml.bak: %w", err)
	}
	if !panel { // only this agent's switches: nothing for the panel to answer
		if err := writeLike(a.configFile, text, a.configFile); err != nil {
			return err
		}
		return a.restartSelf(keys)
	}
	if err := os.WriteFile(a.markerFile(), []byte(keys+"\n"), 0o600); err != nil {
		return err
	}
	if err := writeLike(a.configFile, text, a.configFile); err != nil {
		_ = os.Remove(a.markerFile())
		return err
	}
	// the rollback must run even when the task's context ends
	wait, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	err = restartPanel(wait)
	if err == nil {
		err = panelReady(wait, a.configFile)
	}
	cancel()
	if err == nil {
		_ = os.Remove(a.markerFile())
		if self {
			return a.restartSelf(keys)
		}
		slog.Info("config.apply", "keys", keys, "result", "ok")
		return nil
	}
	rbErr := a.rollbackPanelConfig(context.WithoutCancel(ctx))
	slog.Warn("config.apply", "keys", keys, "result", "rolled back", "err", err, "rollback_err", rbErr)
	if rbErr != nil {
		return fmt.Errorf("%w: %v; the rollback failed too: %v (restore %s.bak by hand)", shares.ErrInvalid, err, rbErr, a.configFile)
	}
	return fmt.Errorf("%w: rolled back: %v", shares.ErrInvalid, err)
}

// restartSelf asks systemd to restart this agent, which reads its [agent] switches only at start. --no-block: the
// task ends first (the worker's shutdown waits for it), so its result is kept.
func (a *Agent) restartSelf(keys string) error {
	if err := restartAgent(); err != nil {
		return fmt.Errorf("config.toml written; restart ad-manager-agent by hand: %w", err)
	}
	slog.Info("config.apply", "keys", keys, "result", "ok, the agent restarts")
	return nil
}

// rollbackPanelConfig puts config.toml.bak back, restarts the panel and checks it answers; only then is the marker
// removed.
func (a *Agent) rollbackPanelConfig(ctx context.Context) error {
	b, err := os.ReadFile(a.configFile + ".bak")
	if err != nil {
		return err
	}
	if err := writeLike(a.configFile, b, a.configFile); err != nil {
		return err
	}
	wait, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := restartPanel(wait); err != nil {
		return err
	}
	if err := panelReady(wait, a.configFile); err != nil {
		return err // the marker stays: the agent's next start tries again
	}
	return os.Remove(a.markerFile())
}

// recoverPanelConfig finishes an apply cut short by the agent's own stop: the marker means config.toml may hold
// values the panel never answered with.
func (a *Agent) recoverPanelConfig(ctx context.Context) {
	if a.configFile == "" {
		return
	}
	if _, err := os.Stat(a.markerFile()); err != nil {
		return
	}
	err := a.rollbackPanelConfig(ctx)
	slog.Warn("config.apply interrupted: config.toml.bak restored", "err", err)
}

// writeLike writes b to path atomically (a temporary file beside it), with the owner and mode of ref.
func writeLike(path string, b []byte, ref string) error {
	fi, err := os.Stat(ref)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, fi.Mode().Perm()); err != nil {
		return err
	}
	if err := chownLike(tmp, fi); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
