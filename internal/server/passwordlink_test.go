package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"ad-manager/internal/ad"
)

func (f *fakeDir) PrepareLinkReset(sam, temp string) (string, error) {
	if sam == "Administrator" {
		return "", ad.ErrProtected
	}
	if f.reset != nil {
		return "João", f.reset(sam, temp)
	}
	return "João", nil
}

// TestPasswordLink walks the whole flow: create, first access claims the link,
// other browsers are refused, a policy error keeps it, success burns it.
func TestPasswordLink(t *testing.T) {
	s, h := newTestStore(t)
	var temp string
	s.dial = func(_ *ad.Config, _, pass string) (directory, error) {
		if pass != goodPassword {
			return nil, ad.ErrBadLogin
		}
		return &fakeDir{admin: true, reset: func(_, pwd string) error { temp = pwd; return nil }}, nil
	}
	s.kpasswd = func(_ *ad.Config, sam, old, next string) error {
		if sam != "joao" || old != temp {
			return ad.ErrBadLogin
		}
		if len(next) < 8 {
			return ad.ErrPwdPolicy
		}
		return nil
	}
	admin := login(t, h)

	if rec := do(h, http.MethodPost, "/api/users/joao/password-link", `{"hours":0}`, admin); rec.Code != http.StatusBadRequest {
		t.Fatalf("hours 0: %d", rec.Code)
	}
	if rec := do(h, http.MethodPost, "/api/users/Administrator/password-link", `{"hours":24}`, admin); rec.Code != http.StatusForbidden {
		t.Fatalf("protected: %d", rec.Code)
	}
	if rec := do(h, http.MethodPost, "/api/users/joao/password-link", `{"hours":24}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without session: %d", rec.Code)
	}
	rec := do(h, http.MethodPost, "/api/users/joao/password-link", `{"hours":24}`, admin)
	if rec.Code != http.StatusOK || temp == "" {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var out struct{ Token string }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	url := "/api/password-link/" + out.Token

	rec = do(h, http.MethodGet, url, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("first access: %d %s", rec.Code, rec.Body)
	}
	var owner *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == linkCookie {
			owner = c
		}
	}
	if owner == nil {
		t.Fatal("first access did not set the claim cookie")
	}

	steps := []struct {
		name   string
		method string
		body   string
		cookie []*http.Cookie
		want   int
	}{
		{"other browser", http.MethodGet, "", nil, http.StatusGone},
		{"other browser posting", http.MethodPost, `{"password":"Nova-Senha-1"}`, nil, http.StatusGone},
		{"owner reloads", http.MethodGet, "", []*http.Cookie{owner}, http.StatusOK},
		{"policy error", http.MethodPost, `{"password":"curta"}`, []*http.Cookie{owner}, http.StatusUnprocessableEntity},
		{"success", http.MethodPost, `{"password":"Nova-Senha-1"}`, []*http.Cookie{owner}, http.StatusNoContent},
		{"single use", http.MethodGet, "", []*http.Cookie{owner}, http.StatusNotFound},
		{"wrong token", http.MethodGet, "", nil, http.StatusNotFound},
	}
	for _, st := range steps {
		u := url
		if st.name == "wrong token" {
			u = "/api/password-link/x" + out.Token
		}
		s.fails = map[string][]time.Time{} // the rate limit is tested elsewhere
		if rec := do(h, st.method, u, st.body, st.cookie...); rec.Code != st.want {
			t.Fatalf("%s: got %d, want %d (%s)", st.name, rec.Code, st.want, rec.Body)
		}
	}
}
