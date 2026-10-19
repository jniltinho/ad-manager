package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	_ "image/jpeg" // logo size check
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"ad-manager/internal/audit"
	"ad-manager/internal/buildinfo"
	"ad-manager/internal/store"

	"github.com/labstack/echo/v5"
)

// Settings are the panel options an administrator can change at runtime.
// They are stored in the local SQLite database and override config.toml.
type Settings struct {
	Lang           string `json:"lang"`            // default UI language: "pt-BR" or "en"
	SessionMinutes int    `json:"session_minutes"` // idle time before a session expires
	InactiveDays   int    `json:"inactive_days"`   // days without logon to flag an account as inactive
	PasswordLength int    `json:"password_length"` // length of generated passwords
	DefaultOU      string `json:"default_ou"`      // DN for new users; empty means CN=Users
	Company        string `json:"company"`         // company name; empty means the AD Manager brand
	SupportURL     string `json:"support_url"`     // support link in the footer and on the login page
	Swagger        bool   `json:"swagger"`         // API documentation at /swagger (administrators only; off by default)
	Theme          string `json:"theme"`           // visual theme for every operator, one of the registry (themes.go); default "default"
	Flat           bool   `json:"flat"`            // flat look: the theme without gradients and depth shadows
}

const (
	settingsKey = "settings"
	logoKey     = "logo"
	logoMax     = 512 << 10
	// largest PNG/JPEG logo, in pixels (3:1, the 195 × 65 login box and the 40 px header at QHD/4K and HiDPI);
	// an SVG scales
	logoMaxW = 660
	logoMaxH = 220

	themeDefault   = "default"
	themeADManager = "admanager"
)

// reOnAttr finds inline event handlers (onload=, onclick=…) in an SVG.
var reOnAttr = regexp.MustCompile(`\son[a-z]+\s*=`)

// defaults returns the settings derived from config.toml.
func (s *Server) defaults() Settings {
	return Settings{Lang: "en", SessionMinutes: s.cfg.Server.SessionMinutes, InactiveDays: 90, PasswordLength: 14, Theme: themeDefault}
}

// settings returns the current settings: defaults overlaid with what is stored.
func (s *Server) settings() Settings {
	st := s.defaults()
	if s.store == nil {
		return st
	}
	if b, err := s.store.Setting(settingsKey); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	if _, ok := themeByID(st.Theme); !ok {
		st.Theme = themeDefault // unknown stored value: fall back to the default theme
	}
	return st
}

// validate checks ranges and formats; it returns a user-facing message.
func (st *Settings) validate(baseDN string) string {
	st.Company, st.SupportURL, st.DefaultOU = strings.TrimSpace(st.Company), strings.TrimSpace(st.SupportURL), strings.TrimSpace(st.DefaultOU)
	if st.Theme == "" {
		st.Theme = themeDefault // clients that predate themes
	}
	switch {
	case st.Lang != "pt-BR" && st.Lang != "en":
		return "language: pt-BR or en"
	case !validTheme(st.Theme):
		return "theme: " + themeList()
	case st.SessionMinutes < 5 || st.SessionMinutes > 720:
		return "session time: 5 to 720 minutes"
	case st.InactiveDays < 7 || st.InactiveDays > 3650:
		return "inactive account: 7 to 3650 days"
	case st.PasswordLength < 10 || st.PasswordLength > 64:
		return "generated password length: 10 to 64"
	case len(st.Company) > 80:
		return "company name: up to 80 characters"
	case st.DefaultOU != "" && !strings.HasSuffix(strings.ToLower(st.DefaultOU), ","+strings.ToLower(baseDN)):
		return "default OU must be inside " + baseDN
	}
	if st.SupportURL != "" {
		u, err := url.Parse(st.SupportURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return "support link: enter an http(s):// URL"
		}
	}
	return ""
}

// getSettings: GET /api/settings — settings plus the read-only values from config.toml.
func (s *Server) getSettings(c *echo.Context) error {
	sc := s.cfg.Server
	_, logoErr := s.logo()
	return c.JSON(http.StatusOK, map[string]any{
		"settings": s.settings(),
		"logo":     logoErr == nil,
		"stored":   s.store != nil,
		"readonly": map[string]any{
			"ad_url": s.cfg.AD.URL, "domain": s.cfg.AD.Domain, "base_dn": s.cfg.AD.Base(), "ca_file": s.cfg.AD.CAFile,
			"admin_group": s.cfg.AD.AdminGroup, "listen": sc.Listen, "tls": sc.CertFile != "" || sc.SelfSigned,
			"self_signed": sc.SelfSigned && sc.CertFile == "", "trust_proxy": sc.TrustProxy, "audit_log": sc.AuditLog,
			"db": s.cfg.DB.Path, "redis": s.cfg.Redis.Addr, "version": buildinfo.Version,
		},
	})
}

// putSettings: PUT /api/settings — validates, stores and audits.
func (s *Server) putSettings(c *echo.Context) error {
	if s.store == nil {
		return noStore(c)
	}
	var st Settings
	if err := c.Bind(&st); err != nil {
		return c.JSON(http.StatusBadRequest, msg("invalid data"))
	}
	if m := st.validate(s.cfg.AD.Base()); m != "" {
		return c.JSON(http.StatusBadRequest, msg(m))
	}
	b, _ := json.Marshal(st)
	err := s.store.SetSetting(settingsKey, b)
	audit.Log(sess(c).user, c.RealIP(), "settings.update", "", err)
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(http.StatusOK, st)
}

// logo returns the stored logo (content type + bytes).
func (s *Server) logo() ([]byte, error) {
	if s.store == nil {
		return nil, store.ErrNotFound
	}
	return s.store.Setting(logoKey)
}

// logoType detects the image type from the content, never from the file name.
// SVG with scripts or event handlers is refused (it is also served under a CSP).
func logoType(b []byte) (string, error) {
	switch t := http.DetectContentType(b); {
	case t == "image/png" || t == "image/jpeg":
		return t, nil
	case strings.HasPrefix(t, "text/xml") || strings.HasPrefix(t, "text/plain"):
		low := bytes.ToLower(b)
		if bytes.Contains(low, []byte("<svg")) && !bytes.Contains(low, []byte("<script")) &&
			!bytes.Contains(low, []byte("javascript:")) && !reOnAttr.Match(low) {
			return "image/svg+xml", nil
		}
	}
	return "", errors.New("logo: send PNG, JPEG or SVG (no scripts)")
}

// putLogo: PUT /api/settings/logo (multipart "logo") — up to 512 KiB; PNG/JPEG up to 660 × 220 px.
func (s *Server) putLogo(c *echo.Context) error {
	if s.store == nil {
		return noStore(c)
	}
	fh, err := c.FormFile("logo")
	if err != nil {
		return c.JSON(http.StatusBadRequest, msg("send the logo file"))
	}
	f, err := fh.Open()
	if err != nil {
		return fail(c, err)
	}
	b, err := io.ReadAll(io.LimitReader(f, logoMax+1))
	f.Close()
	if err != nil {
		return fail(c, err)
	}
	if len(b) > logoMax {
		return c.JSON(http.StatusBadRequest, msg("logo: up to 512 KiB"))
	}
	t, err := logoType(b)
	if err != nil {
		return c.JSON(http.StatusBadRequest, msg(err.Error()))
	}
	if t != "image/svg+xml" {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
		if err != nil {
			return c.JSON(http.StatusBadRequest, msg("logo: send PNG, JPEG or SVG (no scripts)"))
		}
		if cfg.Width > logoMaxW || cfg.Height > logoMaxH {
			return c.JSON(http.StatusBadRequest, msg("logo: at most 660 × 220 px (width × height)"))
		}
	}
	err = s.store.SetSetting(logoKey, append([]byte(t+"\n"), b...))
	audit.Log(sess(c).user, c.RealIP(), "settings.logo", t, err)
	if err != nil {
		return fail(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// deleteLogo: DELETE /api/settings/logo — back to the default brand.
func (s *Server) deleteLogo(c *echo.Context) error {
	if s.store == nil {
		return noStore(c)
	}
	err := s.store.SetSetting(logoKey, nil)
	audit.Log(sess(c).user, c.RealIP(), "settings.logo", "removed", err)
	if err != nil {
		return fail(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// brandLogo: GET /api/brand/logo — public (login page). The CSP keeps an SVG
// opened directly from running anything.
func (s *Server) brandLogo(c *echo.Context) error {
	b, err := s.logo()
	if err != nil {
		return c.NoContent(http.StatusNotFound)
	}
	t, data, _ := bytes.Cut(b, []byte("\n"))
	h := c.Response().Header()
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	h.Set("Cache-Control", "no-cache")
	return c.Blob(http.StatusOK, string(t), data)
}

// brand is the public part of the settings (login page and footer).
func (s *Server) brand() map[string]any {
	st := s.settings()
	_, err := s.logo()
	return map[string]any{"lang": st.Lang, "company": st.Company, "support_url": st.SupportURL, "logo": err == nil, "theme": st.Theme, "flat": st.Flat, "themes": themes}
}
