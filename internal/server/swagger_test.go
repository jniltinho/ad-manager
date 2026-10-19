package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// TestSwagger: /swagger is switched on in Settings and, like /asynqmon, needs a
// signed-in administrator; off, neither the spec nor the Swagger UI files are served.
func TestSwagger(t *testing.T) {
	s, h := newTestStore(t)
	admin := login(t, h)
	spec := func(cookies ...*http.Cookie) *httptest.ResponseRecorder {
		return do(h, http.MethodGet, "/swagger/openapi.yaml", "", cookies...)
	}
	if rec := spec(admin); rec.Code != http.StatusNotFound {
		t.Fatalf("off: %d", rec.Code)
	}
	st := s.settings()
	st.Swagger = true
	b, _ := json.Marshal(st)
	if rec := do(h, http.MethodPut, "/api/settings", string(b), admin); rec.Code != http.StatusOK {
		t.Fatalf("enable: %d %s", rec.Code, rec.Body)
	}
	if rec := spec(); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/?next=/swagger" {
		t.Fatalf("on without session: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := spec(admin); rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), "openapi: 3") {
		t.Fatalf("on with session: %d", rec.Code)
	}
}

func TestAgentToken(t *testing.T) {
	_, h := newTest(t, true)
	if rec := do(h, http.MethodPost, "/api/agents/token", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without session: %d", rec.Code)
	}
	rec := do(h, http.MethodPost, "/api/agents/token", "", login(t, h))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"hash"`) || !strings.Contains(rec.Body.String(), "agent.token") {
		t.Fatalf("token: %d %s", rec.Code, rec.Body)
	}
}

// TestOpenAPICoversRoutes keeps openapi.yaml in step with the router: every
// /api route must be documented (path and method) and nothing documented may
// be missing from the router. Fix it in internal/server/openapi.yaml.
func TestOpenAPICoversRoutes(t *testing.T) {
	s, _ := newTestStore(t)
	e := s.routes()
	param := regexp.MustCompile(`:(\w+)`)
	routes := map[string]bool{}
	for _, r := range e.Router().Routes() {
		if strings.HasPrefix(r.Path, "/api/") && !strings.HasPrefix(r.Method, "echo_") { // echo_route_not_found is internal
			routes[strings.ToLower(r.Method)+" "+param.ReplaceAllString(r.Path, "{$1}")] = true
		}
	}
	documented := map[string]bool{}
	var path string
	for _, line := range strings.Split(string(openAPI), "\n") {
		if m := regexp.MustCompile(`^  (/api/\S+):\s*$`).FindStringSubmatch(line); m != nil {
			path = m[1]
		} else if m := regexp.MustCompile(`^    (get|post|put|delete|patch):`).FindStringSubmatch(line); m != nil && path != "" {
			documented[m[1]+" "+path] = true
		} else if !strings.HasPrefix(line, " ") {
			path = ""
		}
	}
	for r := range routes {
		if !documented[r] {
			t.Errorf("route %q is not in openapi.yaml", r)
		}
	}
	for d := range documented {
		if !routes[d] {
			t.Errorf("openapi.yaml documents %q, which the router does not have", d)
		}
	}
}
