package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/labstack/echo/v5"

	"ad-manager/internal/ad"
	"ad-manager/internal/audit"
	"ad-manager/internal/config"
	"ad-manager/internal/secretbox"
	"ad-manager/internal/store"
)

// The import credential: the target domain's administrator the DC agent's import write modes use (GPO restore and
// import through samba-tool), kept sealed in the panel's database with the sync passwords' secret box and sent to the
// agent with each task — never a file on the DC, never returned by the API.
const importCredKey = "import_credential"

// errNoImportCred: no import credential is kept.
var errNoImportCred = errors.New("the import credential is not set (Settings → Import from an existing AD)")

var importCredUser = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

type importCred struct {
	User   string `json:"user"`
	Sealed []byte `json:"sealed"`
}

// importCredCtx binds the sealed password to its user (a value moved to another user opens nothing).
func importCredCtx(user string) string { return "import-admin:" + strings.ToLower(user) }

// importCredential is the kept credential (errNoImportCred when none).
func (s *Server) importCredential() (user, password string, err error) {
	if s.store == nil {
		return "", "", errNoImportCred
	}
	raw, err := s.store.Setting(importCredKey)
	if errors.Is(err, store.ErrNotFound) {
		return "", "", errNoImportCred
	}
	if err != nil {
		return "", "", err
	}
	var kc importCred
	if json.Unmarshal(raw, &kc) != nil || kc.User == "" {
		return "", "", errNoImportCred
	}
	box, err := s.syncBox()
	if err != nil {
		return "", "", err
	}
	pw, err := box.Open(kc.Sealed, importCredCtx(kc.User))
	if err != nil {
		return "", "", fmt.Errorf("import credential: %w", err)
	}
	return kc.User, string(pw), nil
}

// importAdmin is the credential as the agent's request field ({user, password}), nil when none is kept (the agent
// is one dcAgentCap checked against the directory).
func (s *Server) importAdmin() map[string]string {
	u, p, err := s.importCredential()
	if err != nil {
		return nil
	}
	return map[string]string{"user": u, "password": p}
}

// dcHost: host is a domain controller of cs by its DNS name, or by an address that name resolves to.
func dcHost(ctx context.Context, host string, cs []ad.Computer) bool {
	for _, c := range cs {
		if !c.IsDC || c.DNSName == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSuffix(host, "."), c.DNSName) {
			return true
		}
		if net.ParseIP(host) == nil {
			continue
		}
		addrs, _ := net.DefaultResolver.LookupHost(ctx, c.DNSName)
		for _, x := range addrs {
			if net.ParseIP(x).Equal(net.ParseIP(host)) {
				return true
			}
		}
	}
	return false
}

// getImportCredential: GET /api/settings/import-credential — {user, set}, never the password.
func (s *Server) getImportCredential(c *echo.Context) error {
	u, _, err := s.importCredential()
	return c.JSON(http.StatusOK, map[string]any{"user": u, "set": err == nil})
}

// putImportCredential: PUT /api/settings/import-credential {user, password} — checked as a Domain Admin of this
// domain, then sealed; audited as settings.import_credential (the user, never the password).
func (s *Server) putImportCredential(c *echo.Context) error {
	if s.store == nil {
		return noStore(c)
	}
	var in struct {
		User     string `json:"user"`
		Password string `json:"password"`
	}
	if err := c.Bind(&in); err != nil || !importCredUser.MatchString(in.User) || in.Password == "" ||
		strings.ContainsAny(in.Password, "\r\n") {
		return c.JSON(http.StatusBadRequest, msg("enter the administrator's user (letters, digits, . _ -) and password"))
	}
	refuse := func(code int, err error) error {
		audit.Log(sess(c).user, c.RealIP(), "settings.import_credential", in.User, err)
		return c.JSON(code, msg(err.Error()))
	}
	// always as a Domain Admin (RID 512), whatever group the panel's sign-in requires: samba-tool needs one
	cfg := s.cfg.AD
	cfg.AdminGroup = ""
	d, err := s.dial(&cfg, in.User, in.Password)
	if err != nil {
		if errors.Is(err, ad.ErrBadLogin) {
			return refuse(http.StatusUnauthorized, err)
		}
		return refuse(http.StatusBadGateway, errors.New("directory unavailable: "+err.Error()))
	}
	ok, err := d.IsAdmin()
	d.Close()
	if err != nil || !ok {
		return refuse(http.StatusForbidden, errors.New("not a domain administrator"))
	}
	if err := StoreImportCredential(s.cfg, s.store, in.User, in.Password); err != nil {
		return refuse(http.StatusInternalServerError, err)
	}
	audit.Log(sess(c).user, c.RealIP(), "settings.import_credential", in.User, nil)
	return c.JSON(http.StatusOK, map[string]any{"user": in.User, "set": true})
}

// deleteImportCredential: DELETE /api/settings/import-credential — the GPO write mode has no credential until set.
func (s *Server) deleteImportCredential(c *echo.Context) error {
	if s.store == nil {
		return noStore(c)
	}
	err := s.store.SetSetting(importCredKey, nil)
	audit.Log(sess(c).user, c.RealIP(), "settings.import_credential.delete", "", err)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, msg(err.Error()))
	}
	return c.JSON(http.StatusOK, map[string]any{"user": "", "set": false})
}

// StoreImportCredential seals the import credential into the panel's database with the server's secret box (the
// state folder's secret.key); an empty password removes it. Shared by the Settings route and `ad-manager
// import-credential` (the installer, run as the service user).
func StoreImportCredential(c *config.Config, st *store.Store, user, password string) error {
	if password == "" {
		return st.SetSetting(importCredKey, nil)
	}
	if !importCredUser.MatchString(user) || strings.ContainsAny(password, "\r\n") {
		return errors.New("enter the administrator's user (letters, digits, . _ -) and password")
	}
	box, err := secretbox.Load(filepath.Join(c.StateDir(), "secret.key"))
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(importCred{User: user, Sealed: box.Seal([]byte(password), importCredCtx(user))})
	return st.SetSetting(importCredKey, raw)
}

// sourceSecretCtx binds a profile's sealed password to the profile and to the server and account it is for: a
// profile later saved over with another source (or a concurrent save) leaves a value that opens nothing.
func sourceSecretCtx(x *store.Source) string {
	return fmt.Sprintf("source:%d:%s:%s:%s:%s", x.ID, x.Name, x.URL, strings.ToLower(x.Domain), strings.ToLower(x.User))
}

// keepSourcePassword seals a saved profile's password (on failure no older value is left behind).
func (s *Server) keepSourcePassword(x *store.Source, pw string) error {
	box, err := s.syncBox()
	if err == nil {
		err = s.store.SetSourceSecret(x.ID, box.Seal([]byte(pw), sourceSecretCtx(x)))
	}
	if err != nil {
		_ = s.store.SetSourceSecret(x.ID, nil)
	}
	return err
}

// keptSourcePassword is a profile's kept password for that very source ("" when none, when it no longer opens, or
// when the URL, domain or user asked differ from the profile's: it never goes to another server or account).
func (s *Server) keptSourcePassword(id int64, url, domain, user string) string {
	if s.store == nil || id == 0 {
		return ""
	}
	x, err := s.store.Source(id)
	if err != nil || x.URL != url || !strings.EqualFold(x.Domain, domain) || !strings.EqualFold(x.User, user) {
		return ""
	}
	sealed, err := s.store.SourceSecret(id)
	if err != nil || sealed == nil {
		return ""
	}
	box, err := s.syncBox()
	if err != nil {
		return ""
	}
	pw, err := box.Open(sealed, sourceSecretCtx(x))
	if err != nil {
		return ""
	}
	return string(pw)
}
