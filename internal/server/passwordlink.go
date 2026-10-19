package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"ad-manager/internal/ad"
	"ad-manager/internal/audit"
	"ad-manager/internal/store"

	"github.com/labstack/echo/v5"
)

// Self-service password links: the administrator generates a single-use link;
// the account gets a random temporary password (must change at next logon) and
// the user sets the new one on the public page /password/<token>, which
// changes it with Kerberos using the temporary password. The first browser
// that opens the link owns it (cookie) and has linkClaimTTL to finish; any
// other access is refused. Neither the token nor the temporary password is
// stored in clear text, and no admin credential is kept.
const (
	maxLinkHours    = 7 * 24
	maxLinkAttempts = 5
	linkClaimTTL    = 30 * time.Minute
	linkCookie      = "adc_link"
)

var (
	errLinkGone   = errors.New("invalid or expired link")
	errLinkOpened = errors.New("this link was already opened in another browser; ask the administrator for a new one")
)

// linkKeys derives the stored ID and the encryption key from a link token.
func linkKeys(token string) (id string, key []byte) {
	h := sha256.Sum256([]byte("id:" + token))
	k := sha256.Sum256([]byte("key:" + token))
	return hex.EncodeToString(h[:]), k[:]
}

// linkCipher is AES-256-GCM with a random nonce prepended to the ciphertext.
func linkCipher(key []byte) cipher.AEAD {
	b, _ := aes.NewCipher(key) // key is always 32 bytes
	g, _ := cipher.NewGCMWithRandomNonce(b)
	return g
}

// createPasswordLink handles POST /api/users/:sam/password-link {hours}.
func (s *Server) createPasswordLink(c *echo.Context) error {
	if s.store == nil {
		return c.JSON(http.StatusServiceUnavailable, msg("local database disabled ([db] path)"))
	}
	var in struct {
		Hours int `json:"hours"`
	}
	if err := c.Bind(&in); err != nil || in.Hours < 1 || in.Hours > maxLinkHours {
		return c.JSON(http.StatusBadRequest, msg("invalid validity"))
	}
	sam, token, temp := param(c, "sam"), newID(), ad.TempPassword()
	s.linkMu.Lock() // reset and save in the same order as other generations
	defer s.linkMu.Unlock()
	var name string
	err := s.change(c, "password.link", sam, func(d directory) (err error) {
		name, err = d.PrepareLinkReset(sam, temp)
		return err
	})
	if err != nil || committed(c) {
		return err
	}
	id, key := linkKeys(token)
	exp := time.Now().Add(time.Duration(in.Hours) * time.Hour)
	l := &store.PasswordLink{ID: id, Sam: sam, Name: name, Secret: linkCipher(key).Seal(nil, nil, []byte(temp), nil), Expires: exp}
	if err := s.store.SavePasswordLink(l); err != nil {
		return c.JSON(http.StatusInternalServerError, msg(err.Error()))
	}
	return c.JSON(http.StatusOK, map[string]any{"token": token, "expires": exp})
}

// findLink loads the link of the :token parameter and its temporary password,
// claiming it for this browser at the first access.
func (s *Server) findLink(c *echo.Context) (*store.PasswordLink, string, error) {
	if s.store == nil {
		return nil, "", errLinkGone
	}
	id, key := linkKeys(c.Param("token"))
	l, err := s.store.PasswordLink(id)
	if err != nil {
		return nil, "", errLinkGone
	}
	temp, err := linkCipher(key).Open(nil, nil, l.Secret, nil)
	if err != nil {
		return nil, "", errLinkGone
	}
	if l.Claim == "" {
		secret := newID()
		claim, _ := linkKeys(secret)
		if ok, err := s.store.ClaimPasswordLink(l.ID, claim, time.Now().Add(linkClaimTTL)); err != nil || !ok {
			return nil, "", errLinkOpened
		}
		// the path holds the token: links of other accounts keep their own cookie
		c.SetCookie(&http.Cookie{Name: linkCookie, Value: secret, Path: "/api/password-link/" + c.Param("token"), MaxAge: int(linkClaimTTL.Seconds()),
			HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
		return l, string(temp), nil
	}
	ck, err := c.Cookie(linkCookie)
	if err != nil {
		return nil, "", errLinkOpened
	}
	if claim, _ := linkKeys(ck.Value); claim != l.Claim {
		return nil, "", errLinkOpened
	}
	return l, string(temp), nil
}

// passwordLinkInfo handles GET /api/password-link/:token (public).
func (s *Server) passwordLinkInfo(c *echo.Context) error {
	if s.tooMany(c.RealIP()) { // also prunes the failure list
		return c.JSON(http.StatusTooManyRequests, msg("too many attempts; wait a few minutes"))
	}
	s.linkMu.Lock()
	defer s.linkMu.Unlock()
	l, _, err := s.findLink(c)
	if err != nil {
		s.fail(c.RealIP())
		return c.JSON(linkStatus(err), msg(err.Error()))
	}
	return c.JSON(http.StatusOK, map[string]any{"user": l.Sam, "name": l.Name, "expires": l.Expires})
}

// usePasswordLink handles POST /api/password-link/:token {password} (public).
func (s *Server) usePasswordLink(c *echo.Context) error {
	ip := c.RealIP()
	if s.tooMany(ip) {
		return c.JSON(http.StatusTooManyRequests, msg("too many attempts; wait a few minutes"))
	}
	var in struct {
		Password string `json:"password"`
	}
	if err := c.Bind(&in); err != nil || in.Password == "" {
		return c.JSON(http.StatusBadRequest, msg("enter the new password"))
	}
	// ponytail: one link use at a time (keeps the attempt limit exact); volume is tiny
	s.linkMu.Lock()
	defer s.linkMu.Unlock()
	l, temp, err := s.findLink(c)
	if err != nil {
		s.fail(ip)
		return c.JSON(linkStatus(err), msg(err.Error()))
	}
	err = s.kpasswd(&s.cfg.AD, l.Sam, temp, in.Password)
	audit.Log(l.Sam, ip, "password.link.use", l.Sam, err)
	switch {
	case err == nil:
		_ = s.store.DeletePasswordLink(l.ID)
		return c.NoContent(http.StatusNoContent)
	case errors.Is(err, ad.ErrPwdPolicy):
		if n, err := s.store.PasswordLinkFailed(l.ID); err != nil || n >= maxLinkAttempts { // fail closed
			_ = s.store.DeletePasswordLink(l.ID)
		}
		return c.JSON(http.StatusUnprocessableEntity, msg(err.Error()))
	case errors.Is(err, ad.ErrBadLogin): // reset or disabled meanwhile: the link is useless
		_ = s.store.DeletePasswordLink(l.ID)
		return c.JSON(http.StatusGone, msg("the link is no longer valid: the account changed after it was generated"))
	}
	return c.JSON(http.StatusBadGateway, msg("directory unavailable: "+err.Error()))
}

// linkStatus is 410 for a link opened elsewhere and 404 otherwise.
func linkStatus(err error) int {
	if errors.Is(err, errLinkOpened) {
		return http.StatusGone
	}
	return http.StatusNotFound
}
