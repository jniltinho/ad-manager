package cmd

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"ad-manager/internal/sambabridge"
)

// linkIn and linkOut are the password link tool's input and output.
type (
	linkIn struct {
		Sam   string `json:"sam" jsonschema:"user login"`
		Hours int    `json:"hours,omitempty" jsonschema:"validity in hours, 1 to 168 (default 24)"`
	}
	linkOut struct {
		URL     string    `json:"url"`
		Expires time.Time `json:"expires"`
	}
)

// panelSession signs in to the panel (AD_PANEL_URL) with AD_USER/AD_PASSWORD
// and returns a JSON caller and a logout function. AD_PANEL_INSECURE=true
// accepts a self-signed panel certificate.
func panelSession() (base string, call func(method, path string, body, out any) error, logout func(), err error) {
	user, pass := os.Getenv("AD_USER"), os.Getenv("AD_PASSWORD")
	if user == "" || pass == "" {
		return "", nil, nil, errors.New("set AD_USER and AD_PASSWORD")
	}
	return panelSessionAs(user, pass)
}

// panelSessionAs is panelSession with the given account (the CLI asks the password on the terminal).
func panelSessionAs(user, pass string) (base string, call func(method, path string, body, out any) error, logout func(), err error) {
	base = strings.TrimRight(os.Getenv("AD_PANEL_URL"), "/")
	if base == "" {
		return "", nil, nil, errors.New("set AD_PANEL_URL (e.g. https://dc01.example.local:8443): this tool uses the panel")
	}
	if u, err := url.Parse(base); err != nil || (u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname()))) {
		return "", nil, nil, errors.New("AD_PANEL_URL must be https:// (http only on 127.0.0.1/localhost): the password goes in the login")
	}
	insecure, _ := strconv.ParseBool(os.Getenv("AD_PANEL_INSECURE"))
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Timeout: 30 * time.Second, Jar: jar,
		// a redirect could resend the login (with the password) elsewhere
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure, MinVersion: tls.VersionTLS12}}} //nolint:gosec // explicit option for a self-signed panel
	call = func(method, path string, body, out any) error {
		var rd io.Reader
		ctype, client := "application/json", hc
		switch b := body.(type) {
		case nil:
		case io.Reader: // a file being sent (a backup): as it is, with no total timeout
			c := *hc
			c.Timeout = 0
			rd, ctype, client = b, "application/octet-stream", &c
		default:
			j, _ := json.Marshal(body)
			rd = bytes.NewReader(j)
		}
		req, err := http.NewRequest(method, base+path, rd)
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", ctype)
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("panel: %w", err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if resp.StatusCode >= 300 {
			var e struct{ Error string }
			if json.Unmarshal(data, &e) == nil && e.Error != "" {
				return errors.New(e.Error)
			}
			return fmt.Errorf("panel: %s: HTTP %d", path, resp.StatusCode)
		}
		if out != nil {
			return json.Unmarshal(data, out)
		}
		return nil
	}
	if err := call(http.MethodPost, "/api/login", map[string]string{"user": user, "password": pass}, nil); err != nil {
		return "", nil, nil, err
	}
	return base, call, func() { _ = call(http.MethodPost, "/api/logout", nil, nil) }, nil
}

// panelLink asks the panel for a single-use password link: the link and its
// temporary password live in the panel's database and are served by its
// public page.
func panelLink(in linkIn) (linkOut, error) {
	if in.Hours == 0 {
		in.Hours = 24
	}
	base, call, logout, err := panelSession()
	if err != nil {
		return linkOut{}, err
	}
	defer logout()
	var r struct {
		Token   string    `json:"token"`
		Expires time.Time `json:"expires"`
	}
	if err := call(http.MethodPost, "/api/users/"+url.PathEscape(in.Sam)+"/password-link", map[string]int{"hours": in.Hours}, &r); err != nil {
		return linkOut{}, err
	}
	return linkOut{URL: base + "/password/" + r.Token, Expires: r.Expires}, nil
}

// aclIn is the share ACL tool's input.
type aclIn struct {
	Server string `json:"server" jsonschema:"name of the file server registered in the panel (e.g. fs01)"`
	Path   string `json:"path" jsonschema:"folder on the server (e.g. /srv/shares/Finance)"`
}

// panelACL reads a folder's Windows permissions through the panel and the
// file server's agent.
func panelACL(in aclIn) (*sambabridge.ACL, error) {
	_, call, logout, err := panelSession()
	if err != nil {
		return nil, err
	}
	defer logout()
	var agents []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := call(http.MethodGet, "/api/agents", nil, &agents); err != nil {
		return nil, err
	}
	for _, a := range agents {
		if strings.EqualFold(a.Name, in.Server) {
			var acl sambabridge.ACL
			err := call(http.MethodGet, fmt.Sprintf("/api/agents/%d/acl?path=%s", a.ID, url.QueryEscape(in.Path)), nil, &acl)
			return &acl, err
		}
	}
	return nil, fmt.Errorf("server %q is not registered in the panel", in.Server)
}

// isLoopback reports whether host is localhost or a loopback IP.
func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
