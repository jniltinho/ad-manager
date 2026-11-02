package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"ad-manager/internal/ad"
	"ad-manager/internal/agent"
	"ad-manager/internal/audit"
	"ad-manager/internal/config"
	"ad-manager/internal/store"

	"github.com/labstack/echo/v5"
)

// configTaskKey keeps the last panel:config task (agent and task id) to show its result after the restart.
const configTaskKey = "config_task"

// localAgent finds the one registered agent on the panel's host: its queue is the panel's own agent.queue (both read
// the same config.toml). The agent also checks the file's hash, so a duplicate queue elsewhere cannot apply it.
func (s *Server) localAgent(ctx context.Context) (*store.Agent, string) {
	if s.store == nil {
		return nil, "no local database"
	}
	list, err := s.store.Agents()
	if err != nil {
		return nil, err.Error()
	}
	var mu sync.Mutex
	var found []store.Agent
	var wg sync.WaitGroup
	for _, a := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			code, out, err := agentDo(hctx, &a, http.MethodGet, a.URL+"/api/health", nil)
			var h struct{ Queue string }
			if err == nil && code == http.StatusOK && json.Unmarshal(out, &h) == nil && h.Queue == s.cfg.Agent.Queue {
				mu.Lock()
				found = append(found, a)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	switch len(found) {
	case 1:
		return &found[0], ""
	case 0:
		return nil, "no registered server runs the agent on the panel's host (queue " + s.cfg.Agent.Queue + ")"
	}
	return nil, "more than one registered server answers with the panel's queue " + s.cfg.Agent.Queue
}

// configEnv lists the panel keys set by the environment (they win over config.toml).
func configEnv() map[string]string {
	env := map[string]string{}
	for k, v := range config.PanelKeyEnv {
		if os.Getenv(v) != "" {
			env[k] = v
		}
	}
	return env
}

// getConfig: GET /api/settings/config — the panel's editable config.toml keys, their environment locks, the local
// agent that applies them and the last apply's result.
func (s *Server) getConfig(c *echo.Context) error {
	// the values of config.toml as it is now (with the environment): what a change starts from
	cur := s.cfg
	if raw, err := os.ReadFile(s.cfg.File); err == nil {
		if c, err := config.Parse(raw, s.cfg.File); err == nil {
			cur = c
		}
	}
	values := map[string]any{}
	for _, k := range config.PanelKeys {
		values[k] = reflect.ValueOf(cur.PanelField(k)).Elem().Interface()
	}
	out := map[string]any{"keys": config.PanelKeys, "values": values, "env": configEnv(), "file": s.cfg.File}
	a, why := s.localAgent(c.Request().Context())
	if a != nil {
		out["agent"] = map[string]any{"id": a.ID, "name": a.Name}
	} else {
		out["agent_error"] = why
	}
	if s.store != nil {
		if b, err := s.store.Setting(configTaskKey); err == nil && len(b) > 0 {
			var last struct {
				Agent int64  `json:"agent"`
				Task  string `json:"task"`
				Keys  string `json:"keys"`
				User  string `json:"user"`
				At    string `json:"at"`
			}
			if json.Unmarshal(b, &last) == nil {
				res := map[string]any{"task": last.Task, "keys": last.Keys, "user": last.User, "at": last.At}
				if ag, err := s.store.Agent(last.Agent); err == nil {
					code, body, err := agentDo(c.Request().Context(), ag, http.MethodGet, ag.URL+"/api/tasks/"+last.Task, nil)
					var ti agent.TaskInfo
					if err == nil && code == http.StatusOK && json.Unmarshal(body, &ti) == nil {
						res["state"], res["error"] = ti.State, ti.LastErr
					}
				}
				out["last"] = res
			}
		}
	}
	return c.JSON(http.StatusOK, out)
}

// putConfig: PUT /api/settings/config {"keys": {...}} — checks the change (still an administrator; no lockout with the
// new values) and sends it to the local agent as a panel:config task, which writes config.toml and restarts the panel.
func (s *Server) putConfig(c *echo.Context) error {
	if s.store == nil {
		return noStore(c)
	}
	var in struct {
		Keys map[string]any `json:"keys"`
	}
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, msg("invalid data"))
	}
	if s.cfg.File == "" {
		return c.JSON(http.StatusConflict, msg("the panel runs without a config.toml"))
	}
	// the change is made on config.toml as it is now (what the agent edits), not on the values the panel started with
	raw, err := os.ReadFile(s.cfg.File)
	if err != nil {
		return fail(c, err)
	}
	cur, err := config.Parse(raw, s.cfg.File)
	if err != nil {
		return c.JSON(http.StatusConflict, msg(err.Error()))
	}
	// only what differs from the file; an environment-locked key cannot change
	env := configEnv()
	changed := map[string]any{}
	for k, v := range in.Keys {
		p := cur.PanelField(k)
		if p == nil {
			return c.JSON(http.StatusBadRequest, msg(k+": not editable from the panel"))
		}
		if s, ok := v.(string); ok {
			v = strings.TrimSpace(s)
		}
		if reflect.DeepEqual(reflect.ValueOf(p).Elem().Interface(), v) {
			continue
		}
		if env[k] != "" {
			return c.JSON(http.StatusBadRequest, msg(k+": set by the environment ("+env[k]+")"))
		}
		changed[k] = v
	}
	next := *cur
	if err := next.SetPanelKeys(changed); err != nil {
		return c.JSON(http.StatusBadRequest, msg(err.Error()))
	}
	ss := sess(c)
	if err := ad.CheckAdmin(&s.cfg.AD, ss.user, ss.pass, s.dial); err != nil {
		return c.JSON(http.StatusForbidden, msg("not an administrator under the running configuration"))
	}
	// the whole resulting [ad], so a change already in the file but not yet in effect is checked too
	if err := ad.CheckAdmin(&next.AD, ss.user, ss.pass, s.dial); err != nil {
		return c.JSON(http.StatusBadRequest, msg("ad: "+err.Error()))
	}
	sumB := sha256.Sum256(raw)
	sum := hex.EncodeToString(sumB[:])
	a, why := s.localAgent(c.Request().Context())
	if a == nil {
		return c.JSON(http.StatusConflict, msg(why))
	}
	// the agent reads the nonce back from this server's disk: a copied config.toml elsewhere does not pass
	nb := make([]byte, 16)
	_, _ = rand.Read(nb)
	nonce := hex.EncodeToString(nb)
	if err := os.WriteFile(filepath.Join(s.cfg.StateDir(), agent.PanelNonceFile), []byte(nonce+"\n"), 0o600); err != nil {
		return fail(c, err)
	}
	payload, _ := json.Marshal(agent.PanelConfigReq{ConfigSHA256: sum, Nonce: nonce, Keys: changed})
	body, _ := json.Marshal(agent.TaskReq{Type: agent.TypePanelConfig, Payload: payload})
	names := make([]string, 0, len(changed))
	for k := range changed {
		names = append(names, k)
	}
	sort.Strings(names)
	keys := strings.Join(names, ",")
	code, out, err := agentDo(c.Request().Context(), a, http.MethodPost, a.URL+"/api/tasks", body)
	var ti agent.TaskInfo
	if err == nil && code != http.StatusAccepted {
		var e struct{ Error string }
		_ = json.Unmarshal(out, &e)
		err = errors.New(e.Error)
	} else if err == nil {
		err = json.Unmarshal(out, &ti)
	}
	target := keys
	if ti.ID != "" {
		target += " task " + ti.ID
	}
	audit.Log(ss.user, c.RealIP(), "settings.config", target, err)
	if err != nil {
		status := http.StatusBadGateway
		if code == http.StatusBadRequest { // the agent refused the change itself
			status = http.StatusBadRequest
		}
		return c.JSON(status, msg("agent "+a.Name+": "+err.Error()))
	}
	last, _ := json.Marshal(map[string]any{"agent": a.ID, "task": ti.ID, "keys": keys, "user": ss.user, "at": time.Now().Format(time.RFC3339)})
	_ = s.store.SetSetting(configTaskKey, last)
	return c.JSON(http.StatusAccepted, map[string]any{"task": ti.ID, "keys": names})
}
