package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/labstack/echo/v5"
)

func TestThemeRegistry(t *testing.T) {
	t.Parallel()
	if themes[0].ID != themeDefault {
		t.Fatalf("the default theme must be the first: %+v", themes[0])
	}
	seen := map[string]bool{}
	for _, th := range themes {
		if seen[th.ID] || th.Label == "" || (th.Scheme != "light" && th.Scheme != "dark") {
			t.Errorf("bad registry entry %+v", th)
		}
		seen[th.ID] = true
	}
	if got, ok := themeByID("neon"); ok || got.ID != themeDefault {
		t.Errorf("unknown theme: got %+v, %v", got, ok)
	}
	// "a, b, … or z": every id once, the last one after "or"
	got := themeList()
	last := themes[len(themes)-1].ID
	if !strings.HasPrefix(got, "default, ") || !strings.HasSuffix(got, " or "+last) || strings.Count(got, ", ") != len(themes)-2 {
		t.Errorf("themeList = %q", got)
	}
}

// TestThemeEnumMatchesRegistry keeps the theme enum of openapi.yaml in step with the registry.
func TestThemeEnumMatchesRegistry(t *testing.T) {
	t.Parallel()
	spec, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`theme: \{ type: string, enum: \[([^\]]*)\]`).FindSubmatch(spec)
	if m == nil {
		t.Fatal("theme enum not found in openapi.yaml")
	}
	var ids []string
	for _, th := range themes {
		ids = append(ids, th.ID)
	}
	if enum := strings.Split(strings.ReplaceAll(string(m[1]), " ", ""), ","); !slices.Equal(enum, ids) {
		t.Errorf("openapi.yaml theme enum %v, registry %v", enum, ids)
	}
}

func TestRenderIndex(t *testing.T) {
	t.Parallel()
	in := []byte(`<html data-theme="%AD_THEME%" data-flat="%AD_FLAT%"><meta name="color-scheme" content="%AD_SCHEME%">`)
	got := renderIndex(in, Theme{ID: "x", Scheme: "dark"}, true)
	if got != `<html data-theme="x" data-flat="true"><meta name="color-scheme" content="dark">` {
		t.Errorf("renderIndex = %s", got)
	}
}

// TestSPADocument: every SPA document (/, client routes, /password/:token) comes with the theme and is never
// cached; assets stay immutable; the Swagger files are hidden while it is off; without index.html the panel
// answers 503 instead of an empty page.
func TestSPADocument(t *testing.T) {
	t.Parallel()
	dist := fstest.MapFS{
		"index.html":       {Data: []byte(`<html data-theme="%AD_THEME%" data-flat="%AD_FLAT%"><meta name="color-scheme" content="%AD_SCHEME%">`)},
		"assets/app.js":    {Data: []byte("x")},
		"swagger/index.js": {Data: []byte("s")},
	}
	e := echo.New()
	spa(e, dist, func() bool { return false }, func() (Theme, bool) { return Theme{ID: "admanager", Scheme: "light"}, true })
	get := func(method, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec
	}
	for _, path := range []string{"/", "/index.html", "/users", "/password/" + strings.Repeat("a", 64), "/swagger/index.js"} {
		rec := get(http.MethodGet, path)
		if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" || !strings.Contains(rec.Body.String(), `data-theme="admanager" data-flat="true"`) ||
			!strings.Contains(rec.Body.String(), `content="light"`) {
			t.Errorf("GET %s: %d %q %s", path, rec.Code, rec.Header().Get("Cache-Control"), rec.Body)
		}
	}
	if rec := get(http.MethodHead, "/users"); rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("HEAD /users: %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	if rec := get(http.MethodGet, "/assets/app.js"); rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("asset: %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	if rec := get(http.MethodGet, "/api/nope"); rec.Code != http.StatusNotFound {
		t.Errorf("/api/nope: %d", rec.Code)
	}

	empty := echo.New()
	spa(empty, fstest.MapFS{".gitkeep": {}}, func() bool { return false }, func() (Theme, bool) { return themes[0], false })
	rec := httptest.NewRecorder()
	empty.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("without index.html: %d %s", rec.Code, rec.Body)
	}
}

// TestBrandThemes: the public brand carries the saved theme and the registry.
func TestBrandThemes(t *testing.T) {
	_, h := newTestStore(t)
	ck := login(t, h)
	body := `{"lang":"pt-BR","session_minutes":60,"inactive_days":90,"password_length":14,"theme":"admanager"}`
	if rec := do(h, http.MethodPut, "/api/settings", body, ck); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	var health struct {
		Brand struct {
			Theme  string  `json:"theme"`
			Themes []Theme `json:"themes"`
		}
	}
	_ = json.Unmarshal(do(h, http.MethodGet, "/api/health", "").Body.Bytes(), &health)
	if health.Brand.Theme != themeADManager || !slices.Equal(health.Brand.Themes, themes) {
		t.Errorf("brand: %+v", health.Brand)
	}
}
