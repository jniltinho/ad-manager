package server

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ad-manager/internal/ad"
	"ad-manager/internal/store"
)

// TestProxySlowAndLarge: a slow agent answer (folder sizes) outlives the server's WriteTimeout, and an answer larger
// than the panel passes on is an error, never a cut JSON.
func TestProxySlowAndLarge(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/sizes":
			time.Sleep(1200 * time.Millisecond)
			fmt.Fprint(w, `{"sizes":{"Finance":42}}`)
		case "/api/shares":
			fmt.Fprint(w, `["`+strings.Repeat("x", maxAgentAnswer)+`"]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer agent.Close()
	s, h := newTestStore(t)
	a := &store.Agent{Name: "fs1", URL: agent.URL, Token: "t"}
	if err := s.store.SaveAgent(a); err != nil {
		t.Fatal(err)
	}
	s.dial = func(_ *ad.Config, _, pass string) (directory, error) {
		if pass != goodPassword {
			return nil, ad.ErrBadLogin
		}
		return &fakeDir{admin: true}, nil
	}
	c := login(t, h)
	srv := httptest.NewUnstartedServer(h)
	srv.Config.WriteTimeout = 500 * time.Millisecond // stands for the panel's 60 s
	srv.Start()
	defer srv.Close()
	get := func(path string) (int, string, error) {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		req.AddCookie(c)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, "", err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b), err
	}
	if code, body, err := get(fmt.Sprintf("/api/agents/%d/sizes", a.ID)); err != nil || code != http.StatusOK || !strings.Contains(body, `"Finance":42`) {
		t.Fatalf("slow sizes: %d %q %v", code, body, err)
	}
	if code, body, err := get(fmt.Sprintf("/api/agents/%d/shares", a.ID)); err != nil || code != http.StatusBadGateway || !strings.Contains(body, "larger than") {
		t.Fatalf("oversized answer: %d %.80q %v", code, body, err)
	}
}
