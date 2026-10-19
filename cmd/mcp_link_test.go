package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPanelLink checks the panel conversation: login, link with the session
// cookie, URL built from AD_PANEL_URL, and the panel's error message passed on.
func TestPanelLink(t *testing.T) {
	var hours int
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/login":
			var in struct{ User, Password string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in.Password != "certa" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"invalid user or password"}`))
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "adc_session", Value: "s1", Path: "/"})
		case r.URL.Path == "/api/users/ana.souza/password-link":
			if c, err := r.Cookie("adc_session"); err != nil || c.Value != "s1" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			var in struct{ Hours int }
			_ = json.NewDecoder(r.Body).Decode(&in)
			hours = in.Hours
			_, _ = w.Write([]byte(`{"token":"abc123","expires":"2026-10-18T20:00:00Z"}`))
		case r.URL.Path == "/api/logout":
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	t.Setenv("AD_PANEL_URL", srv.URL+"/")
	t.Setenv("AD_PANEL_INSECURE", "true")
	t.Setenv("AD_USER", "administrator")

	t.Setenv("AD_PASSWORD", "certa")
	out, err := panelLink(linkIn{Sam: "ana.souza"})
	if err != nil {
		t.Fatal(err)
	}
	if out.URL != srv.URL+"/password/abc123" || hours != 24 || out.Expires.IsZero() {
		t.Fatalf("got %+v (hours %d)", out, hours)
	}

	t.Setenv("AD_PASSWORD", "errada")
	if _, err := panelLink(linkIn{Sam: "ana.souza"}); err == nil || !strings.Contains(err.Error(), "invalid user or password") {
		t.Fatalf("wrong password: %v", err)
	}

	t.Setenv("AD_PANEL_URL", "")
	if _, err := panelLink(linkIn{Sam: "ana.souza"}); err == nil || !strings.Contains(err.Error(), "AD_PANEL_URL") {
		t.Fatalf("without AD_PANEL_URL: %v", err)
	}
}
