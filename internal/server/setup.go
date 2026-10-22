package server

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ad-manager/internal/agent"
	"ad-manager/internal/audit"
	"ad-manager/internal/buildinfo"

	"github.com/labstack/echo/v5"
)

// setupCookie is the session of the setup wizard (separate from the operators' sessions).
const setupCookie = "adc_setup"

// setupTTL is how long a setup token stays valid after it was created.
const setupTTL = 24 * time.Hour

// setupState is the panel's side of the setup wizard (config setup = true).
type setupState struct {
	mu       sync.Mutex
	hash     [32]byte
	created  time.Time
	sessions map[string]time.Time // session → expiry
	fails    []time.Time
}

// setupTokenFile keeps the one-time setup token (0600, read with sudo cat): never printed to the journal.
func (s *Server) setupTokenFile() string { return filepath.Join(s.cfg.StateDir(), "setup-token") }

// loadSetupToken reads the token file, creating it with a new token when missing.
func (s *Server) loadSetupToken() (*setupState, error) {
	p := s.setupTokenFile()
	b, err := os.ReadFile(p)
	if _, used := os.Stat(p + ".used"); errors.Is(err, os.ErrNotExist) && used == nil {
		// spent by a sign-in: no new token until the operator deletes the .used marker (a deliberate restart)
		return nil, fmt.Errorf("the setup token was used already: delete %s.used and restart ad-manager for a new one", p)
	}
	if errors.Is(err, os.ErrNotExist) {
		raw := make([]byte, 24)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		tok := hex.EncodeToString(raw)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, []byte(tok+"\n"), 0o600); err != nil {
			return nil, err
		}
		b = []byte(tok)
	} else if err != nil {
		return nil, err
	}
	fi, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	st := &setupState{hash: sha256.Sum256([]byte(strings.TrimSpace(string(b)))), created: fi.ModTime(), sessions: map[string]time.Time{}}
	slog.Info("panel in setup mode: open it in a browser; the setup token is in the file", "file", p)
	return st, nil
}

// setupRoutes are the only routes of a panel in setup mode: health (setup: true), the token sign-in and the
// wizard's calls, passed to the agent's loopback setup server; every other API answers 503.
func (s *Server) setupRoutes(api *echo.Group) {
	st, err := s.loadSetupToken()
	if err != nil {
		slog.Error("setup token", "err", err)
	}
	api.GET("/health", func(c *echo.Context) error {
		return c.JSON(http.StatusOK, map[string]any{"setup": true, "version": buildinfo.Version, "brand": s.brand(), "ldap": "setup"})
	})
	api.POST("/setup/login", func(c *echo.Context) error {
		if st == nil {
			return c.JSON(http.StatusInternalServerError, msg("setup token unavailable (see the panel's log)"))
		}
		var in struct {
			Token string `json:"token"`
		}
		_ = c.Bind(&in)
		st.mu.Lock()
		defer st.mu.Unlock()
		now := time.Now()
		recent := st.fails[:0]
		for _, f := range st.fails {
			if now.Sub(f) < 15*time.Minute {
				recent = append(recent, f)
			}
		}
		st.fails = recent
		if len(st.fails) >= 5 {
			return c.JSON(http.StatusTooManyRequests, msg("too many attempts: wait 15 minutes"))
		}
		if now.Sub(st.created) > setupTTL {
			return c.JSON(http.StatusForbidden, msg("the setup token expired: delete it and restart ad-manager for a new one"))
		}
		h := sha256.Sum256([]byte(strings.TrimSpace(in.Token)))
		if subtle.ConstantTimeCompare(h[:], st.hash[:]) != 1 {
			st.fails = append(st.fails, now)
			audit.Log("setup", c.RealIP(), "setup.login", "", errors.New("wrong token"))
			return c.JSON(http.StatusUnauthorized, msg("wrong setup token"))
		}
		raw := make([]byte, 32)
		_, _ = rand.Read(raw)
		id := hex.EncodeToString(raw)
		st.sessions[id] = now.Add(time.Hour)
		// one use: the token is spent (a lost session needs a new token: delete the file, restart ad-manager)
		st.hash = sha256.Sum256(raw) // a value no token hashes to in practice
		_ = os.Remove(s.setupTokenFile())
		_ = os.WriteFile(s.setupTokenFile()+".used", []byte(now.Format(time.RFC3339)+"\n"), 0o600)
		audit.Log("setup", c.RealIP(), "setup.login", "", nil)
		c.SetCookie(&http.Cookie{Name: setupCookie, Value: id, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
		return c.NoContent(http.StatusNoContent)
	})
	auth := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			ck, err := c.Cookie(setupCookie)
			if st == nil || err != nil {
				return c.JSON(http.StatusUnauthorized, msg("sign in with the setup token"))
			}
			st.mu.Lock()
			exp, ok := st.sessions[ck.Value]
			if ok && time.Now().Before(exp) {
				st.sessions[ck.Value] = time.Now().Add(time.Hour) // sliding: a long setup keeps its session
			}
			st.mu.Unlock()
			if !ok || time.Now().After(exp) {
				return c.JSON(http.StatusUnauthorized, msg("sign in with the setup token"))
			}
			return next(c)
		}
	}
	g := api.Group("/setup", auth)
	g.GET("/diagnose", func(c *echo.Context) error { return s.toSetupAgent(c, http.MethodGet, "/setup/diagnose", nil) })
	g.GET("/status", func(c *echo.Context) error {
		return s.toSetupAgent(c, http.MethodGet, "/setup/status?from="+fmt.Sprint(atoiOr(c.QueryParam("from"), 0)), nil)
	})
	g.POST("/plan", func(c *echo.Context) error {
		body, err := io.ReadAll(io.LimitReader(c.Request().Body, 1<<16))
		if err != nil {
			return c.JSON(http.StatusBadRequest, msg("invalid request body"))
		}
		return s.toSetupAgent(c, http.MethodPost, "/setup/plan", body)
	})
	g.POST("/run", func(c *echo.Context) error {
		body, err := io.ReadAll(io.LimitReader(c.Request().Body, 1<<16))
		if err != nil {
			return c.JSON(http.StatusBadRequest, msg("invalid request body"))
		}
		var p agent.SetupPlan
		_ = json.Unmarshal(body, &p) // the audit line: the plan, never the password
		err = s.toSetupAgent(c, http.MethodPost, "/setup/run", body)
		target := p.Role + " " + p.Realm
		if p.ImportAD {
			target += " import_ad"
		}
		audit.Log("setup", c.RealIP(), "setup.run", target, nil)
		return err
	})
	api.Any("/*", func(c *echo.Context) error {
		return c.JSON(http.StatusServiceUnavailable, msg("the panel is being set up: open it in a browser and use the setup token"))
	})
}

func atoiOr(s string, def int) int {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil || n < 0 {
		return def
	}
	return n
}

// toSetupAgent calls the agent's loopback setup server with the local key and answers with what it says.
func (s *Server) toSetupAgent(c *echo.Context, method, path string, body []byte) error {
	key, err := os.ReadFile(agent.SetupKeyFile(s.cfg))
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, msg("the agent is not in setup mode on this server (systemctl enable --now ad-manager-agent)"))
	}
	req, err := http.NewRequestWithContext(c.Request().Context(), method, "http://"+agent.SetupListen+path, bytes.NewReader(body))
	if err != nil {
		return fail(c, err)
	}
	req.Header.Set("X-Setup-Key", strings.TrimSpace(string(key)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, msg("the agent is not in setup mode on this server (systemctl enable --now ad-manager-agent)"))
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return c.JSONBlob(resp.StatusCode, out)
}
