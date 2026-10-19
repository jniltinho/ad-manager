package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // logo decoding
	_ "image/png"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"github.com/fyne-io/oksvg"
	"github.com/srwiley/rasterx"
)

// api talks to the panel (session cookie) or to a file-server agent (Bearer token).
type api struct {
	base  string
	token string // empty = panel (cookie session)
	hc    *http.Client
}

// newAPI returns a client for base; token selects agent (Bearer) mode and
// insecure skips TLS certificate verification.
func newAPI(base, token string, insecure bool) *api {
	jar, _ := cookiejar.New(nil)
	return &api{base: base, token: token, hc: &http.Client{Timeout: 20 * time.Second, Jar: jar,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure, MinVersion: tls.VersionTLS12}}}} //nolint:gosec // explicit config option for self-signed certificates
}

// withTimeout is a copy of a whose requests may take up to d (a folder count, a connection test).
func (a *api) withTimeout(d time.Duration) *api {
	c, hc := *a, *a.hc
	hc.Timeout = d
	c.hc = &hc
	return &c
}

// errSessionExpired is returned when the panel session cookie is no longer valid.
var errSessionExpired = errors.New("session expired: sign in again")

// do sends in as JSON to path and decodes the JSON response into out (both
// optional). Error responses become errors carrying the server message.
func (a *api) do(method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, a.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	resp, err := a.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode == http.StatusUnauthorized && a.token == "" && path != "/api/login" {
		return errSessionExpired
	}
	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			return errors.New(serverText(e.Error))
		}
		return fmt.Errorf("%s: HTTP %d", path, resp.StatusCode)
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// esc escapes s for use as a URL path segment.
func esc(s string) string { return url.PathEscape(s) }

// Types used by the UI (a subset of the API JSON).
type (
	user struct {
		Sam, Name, Mail, Department string
		Enabled, Locked, Protected  bool
		LastLogon                   time.Time `json:"last_logon"`
	}
	group struct {
		Name, Type, Scope, Description string
		Protected                      bool
		MemberCount                    int `json:"member_count"`
		Members                        []string
	}
	share struct {
		Name, Path, Comment string
		RW                  []string `json:"rw_groups"`
		RO                  []string `json:"ro_groups"`
	}
	task struct {
		ID, State string
		LastErr   string `json:"last_error"`
	}
	me struct {
		User, Domain, Netbios, Version string
		Brand                          struct {
			Theme   string `json:"theme"`
			Flat    bool   `json:"flat"`
			Company string `json:"company"` // the panel's company name (Settings); empty: the default
		} `json:"brand"`
		Settings struct {
			Lang           string `json:"lang"`
			InactiveDays   int    `json:"inactive_days"`
			PasswordLength int    `json:"password_length"`
			DefaultOU      string `json:"default_ou"`
		} `json:"settings"`
	}
)

// health reads the panel's public /api/health into out: also when it answers 503 (the directory is down), since the
// body still carries the domain and the brand.
func (a *api) health(out any) error {
	resp, err := a.hc.Get(a.base + "/api/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusServiceUnavailable {
		return fmt.Errorf("/api/health: HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

// image downloads a PNG, JPEG or SVG of at most 2 MiB (the company logo) as a resource named for its type, and only
// when it decodes: a broken image must fall back to the product logo, not draw nothing.
func (a *api) image(path string) (fyne.Resource, error) {
	resp, err := a.hc.Get(a.base + path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", path, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20+1))
	if err == nil && len(b) > 2<<20 {
		err = fmt.Errorf("%s: larger than 2 MiB", path)
	}
	if err != nil {
		return nil, err
	}
	switch ct {
	case "image/png", "image/jpeg":
		if _, _, err := image.Decode(bytes.NewReader(b)); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return fyne.NewStaticResource("company-logo."+strings.TrimPrefix(ct, "image/"), b), nil
	case "image/svg+xml":
		if !wellFormedSVG(b) || !drawableSVG(b) {
			return nil, fmt.Errorf("%s: not an SVG Fyne can draw", path)
		}
		return fyne.NewStaticResource("company-logo.svg", b), nil // the name tells Fyne to draw it as SVG
	}
	return nil, fmt.Errorf("%s: type %q", path, ct)
}

// drawableSVG reports an SVG that Fyne draws: it reads it with the same parser (fyne-io/oksvg), sizes it by its
// viewBox (or width and height) and rasterizes it. An attribute the parser refuses, no size or nothing drawn would
// leave the login without a logo instead of the product's.
func drawableSVG(b []byte) (ok bool) {
	defer func() {
		if recover() != nil { // the parser and the rasterizer panic on some inputs (Fyne's own drawing guards too)
			ok = false
		}
	}()
	icon, err := oksvg.ReadReplacingCurrentColor(bytes.NewReader(b), "#000000")
	if err != nil || icon.ViewBox.W <= 0 || icon.ViewBox.H <= 0 {
		return false
	}
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	icon.SetTarget(0, 0, 64, 64)
	icon.Draw(rasterx.NewDasher(64, 64, rasterx.NewScannerGV(64, 64, img, img.Bounds())), 1)
	for k := 3; k < len(img.Pix); k += 4 { // something drawn (oksvg skips what it does not know, as <text>)
		if img.Pix[k] != 0 {
			return true
		}
	}
	return false
}

// wellFormedSVG reports a complete XML document whose root element is <svg> (a truncated file is not).
func wellFormedSVG(b []byte) bool {
	d := xml.NewDecoder(bytes.NewReader(b))
	root := ""
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			return root == "svg"
		}
		if err != nil {
			return false
		}
		if se, ok := tok.(xml.StartElement); ok && root == "" {
			root = se.Name.Local
		}
	}
}
