package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"ad-manager/internal/store"
)

// newTestStore is newTest with a real SQLite store in a temp dir.
func newTestStore(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s, _ := newTest(t, true)
	s.store = st
	return s, s.routes()
}

func TestSettingsValidate(t *testing.T) {
	t.Parallel()
	ok := Settings{Lang: "en", SessionMinutes: 60, InactiveDays: 90, PasswordLength: 14}
	tests := []struct {
		name string
		mod  func(*Settings)
		bad  bool
	}{
		{"valid", func(*Settings) {}, false},
		{"session zero", func(s *Settings) { s.SessionMinutes = 0 }, true},
		{"short password", func(s *Settings) { s.PasswordLength = 5 }, true},
		{"unknown language", func(s *Settings) { s.Lang = "fr" }, true},
		{"admanager theme", func(s *Settings) { s.Theme = "admanager" }, false},
		{"unknown theme", func(s *Settings) { s.Theme = "dark" }, true},
		{"support link without scheme", func(s *Settings) { s.SupportURL = "suporte.empresa.com" }, true},
		{"javascript link", func(s *Settings) { s.SupportURL = "javascript:alert(1)" }, true},
		{"OU outside the domain", func(s *Settings) { s.DefaultOU = "OU=X,DC=outro,DC=test" }, true},
		{"OU inside the domain", func(s *Settings) { s.DefaultOU = "OU=Tatuape,DC=empresa,DC=test" }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := ok
			tt.mod(&st)
			if got := st.validate("DC=empresa,DC=test"); (got != "") != tt.bad {
				t.Errorf("validate() = %q, bad = %v", got, tt.bad)
			}
		})
	}
}

func TestLogoType(t *testing.T) {
	t.Parallel()
	png := []byte("\x89PNG\r\n\x1a\n0000000000000")
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{"png", png, "image/png"},
		{"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`), "image/svg+xml"},
		{"svg with script", []byte(`<svg><script>alert(1)</script></svg>`), ""},
		{"svg with onload", []byte(`<svg onload="alert(1)"></svg>`), ""},
		{"html renamed", []byte(`<!doctype html><html><body>x</body></html>`), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := logoType(tt.in)
			if got != tt.want {
				t.Errorf("logoType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSettingsAPI(t *testing.T) {
	s, h := newTestStore(t)
	if rec := do(h, http.MethodPut, "/api/settings", `{}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without session: %d", rec.Code)
	}
	ck := login(t, h)
	if rec := do(h, http.MethodPut, "/api/settings", `{"lang":"en","session_minutes":0,"inactive_days":90,"password_length":14}`, ck); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid range accepted: %d", rec.Code)
	}
	body := `{"lang":"en","session_minutes":60,"inactive_days":30,"password_length":20,"company":"Empresa X","support_url":"https://help.x.test"}`
	if rec := do(h, http.MethodPut, "/api/settings", body, ck); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	if s.ttl().Minutes() != 60 {
		t.Errorf("session ttl = %v, want 60m", s.ttl())
	}
	var health struct{ Brand map[string]any }
	_ = json.Unmarshal(do(h, http.MethodGet, "/api/health", "").Body.Bytes(), &health)
	if health.Brand["company"] != "Empresa X" || health.Brand["lang"] != "en" {
		t.Errorf("public brand = %v", health.Brand)
	}

	// logo: a PNG larger than 660 × 220 px is refused
	upload := func(name string, data []byte) *httptest.ResponseRecorder {
		var b bytes.Buffer
		mw := multipart.NewWriter(&b)
		fw, _ := mw.CreateFormFile("logo", name)
		fw.Write(data)
		mw.Close()
		req := httptest.NewRequest(http.MethodPut, "/api/settings/logo", &b)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.AddCookie(ck)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	for _, size := range [][2]int{{661, 100}, {300, 221}} {
		var png bytes.Buffer
		_ = pngEncode(&png, size[0], size[1])
		if rec := upload("big.png", png.Bytes()); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "660") {
			t.Errorf("logo %v: %d %s", size, rec.Code, rec.Body)
		}
	}
	var fits bytes.Buffer
	_ = pngEncode(&fits, 660, 220)
	if rec := upload("ok.png", fits.Bytes()); rec.Code != http.StatusNoContent {
		t.Errorf("logo 660 × 220: %d %s", rec.Code, rec.Body)
	}

	// logo: upload, public download with CSP, removal
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("logo", "logo.png")
	fw.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><circle r="4"/></svg>`))
	mw.Close()
	req := httptest.NewRequest(http.MethodPut, "/api/settings/logo", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(ck)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logo upload: %d %s", rec.Code, rec.Body)
	}
	rec = do(h, http.MethodGet, "/api/brand/logo", "")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/svg+xml" || rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("logo download: %d %v", rec.Code, rec.Header())
	}
	do(h, http.MethodDelete, "/api/settings/logo", "", ck)
	if rec = do(h, http.MethodGet, "/api/brand/logo", ""); rec.Code != http.StatusNotFound {
		t.Errorf("logo after removal: %d", rec.Code)
	}
}

func TestSettingsTheme(t *testing.T) {
	s, h := newTestStore(t)
	if got := s.settings().Theme; got != themeDefault {
		t.Fatalf("default theme = %q", got)
	}
	ck := login(t, h)
	theme := func(v string) string {
		return `{"lang":"pt-BR","session_minutes":60,"inactive_days":90,"password_length":14,"theme":"` + v + `"}`
	}
	if rec := do(h, http.MethodPut, "/api/settings", theme("dark"), ck); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown theme accepted: %d", rec.Code)
	}
	if rec := do(h, http.MethodPut, "/api/settings", theme("admanager"), ck); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	var health struct{ Brand map[string]any }
	_ = json.Unmarshal(do(h, http.MethodGet, "/api/health", "").Body.Bytes(), &health)
	if health.Brand["theme"] != themeADManager {
		t.Errorf("public brand theme = %v", health.Brand["theme"])
	}

	// a value stored by hand (or by a newer version) falls back to the default
	if err := s.store.SetSetting(settingsKey, []byte(`{"lang":"en","theme":"neon"}`)); err != nil {
		t.Fatal(err)
	}
	if got := s.settings(); got.Theme != themeDefault || got.Lang != "en" {
		t.Errorf("invalid stored theme: got %+v", got)
	}
}

// pngEncode writes a blank w × h PNG.
func pngEncode(w io.Writer, width, height int) error {
	return png.Encode(w, image.NewNRGBA(image.Rect(0, 0, width, height)))
}
