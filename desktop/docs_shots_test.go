//go:build docsshots

package main

// Screenshots of the desktop app for docs/screenshots, against a real panel (the all-in-one image with domain.local,
// see docs/screenshots/README.md). The password comes from the environment only:
//
//	DOCS_URL=https://127.0.0.1:18443 DOCS_PASS=… DOCS_SHOTS_DIR=../docs/screenshots \
//	  DOCS_AGENT_URL=https://<container IP>:9443 DOCS_AGENT_TOKEN=… \
//	  go test -tags docsshots -run TestDocsShots -count=1 .

import (
	"image/png"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

func TestDocsShots(t *testing.T) {
	url, pass, dir := os.Getenv("DOCS_URL"), os.Getenv("DOCS_PASS"), os.Getenv("DOCS_SHOTS_DIR")
	if url == "" || pass == "" || dir == "" {
		t.Skip("DOCS_URL, DOCS_PASS and DOCS_SHOTS_DIR not set")
	}
	if !localURL(url, false) {
		t.Fatal("only a local panel")
	}
	if a := os.Getenv("DOCS_AGENT_URL"); a != "" && !localURL(a, true) {
		t.Fatal("only a local agent (loopback, or a container's private address)")
	}
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("AD Manager")
	defer w.Close()
	w.Resize(fyne.NewSize(1280, 800))
	setLang("en")
	if v := os.Getenv("DOCS_VERSION"); v != "" {
		version = v
	}
	cfg := &config{}
	cfg.Panel.URL, cfg.Panel.Insecure = url, true
	if tok := os.Getenv("DOCS_AGENT_TOKEN"); tok != "" { // the Shares page talks to the file server's agent directly
		cfg.Agents = []agentConfig{{Name: "dc1", URL: os.Getenv("DOCS_AGENT_URL"), Token: tok, Insecure: true}}
	}
	u := &ui{a: a, w: w, cfg: cfg, panel: newAPI(url, "", true), status: newText("", col("footer-strong"), 11, false)}

	shot := func(name string, wait time.Duration) {
		time.Sleep(wait)
		var img = w.Canvas().Capture()
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		_ = png.Encode(f, img)
		f.Close()
		t.Log("ok", name)
	}
	closeAll := func() {
		fyne.DoAndWait(func() {
			for o := w.Canvas().Overlays().Top(); o != nil; o = w.Canvas().Overlays().Top() {
				w.Canvas().Overlays().Remove(o)
			}
			u.sheets = nil
		})
	}

	fyne.DoAndWait(u.login)
	shot("desktop-01-login", 2*time.Second) // the panel's brand (theme, domain) arrives and rebuilds the screen
	fyne.DoAndWait(func() { u.setup(u.login) })
	shot("desktop-02-configure", 300*time.Millisecond)
	fyne.DoAndWait(func() {
		u.typed = [2]string{"Administrator", pass}
		u.login()
	})
	time.Sleep(500 * time.Millisecond)
	fyne.DoAndWait(func() {
		pwd := w.Canvas().Focused().(*ringEntry)
		for _, e := range entries(w.Content()) {
			if e != pwd {
				e.SetText("Administrator") // the user field
				break
			}
		}
		pwd.SetText(pass)
		pwd.OnSubmitted(pass)
	})
	deadline := time.Now().Add(20 * time.Second)
	for {
		var screen string
		fyne.DoAndWait(func() { screen = u.screen })
		if screen == "shell" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no sign-in: %q", texts(w.Content()))
		}
		time.Sleep(200 * time.Millisecond)
	}

	pages := []struct {
		page int
		name string
	}{{0, "desktop-03-home"}, {1, "desktop-04-users"}, {2, "desktop-06-groups"}, {3, "desktop-12-ous"},
		{4, "desktop-07-computers"}, {5, "desktop-08-shares"}, {6, "desktop-13-password-policy"},
		{7, "desktop-14-backup"}, {8, "desktop-15-sync"}, {9, "desktop-09-logs"}}
	for _, p := range pages {
		fyne.DoAndWait(func() { u.goTo(p.page) })
		shot(p.name, 3*time.Second)
	}

	fyne.DoAndWait(func() { u.goTo(1) })
	time.Sleep(time.Second)
	fyne.DoAndWait(func() { u.newUserDialog(nil, func() {}) })
	shot("desktop-05-new-user", 2*time.Second)
	closeAll()

	ana := user{Sam: "ana.souza", Name: "Ana Souza"}
	fyne.DoAndWait(func() { u.passwordDialog(ana, func() {}) })
	time.Sleep(300 * time.Millisecond)
	fyne.DoAndWait(func() {
		if tab := tappableWith(w.Canvas().Overlays().Top(), "Send link to the user"); tab != nil {
			test.Tap(tab)
		}
	})
	shot("desktop-10-password-link", 500*time.Millisecond)
	closeAll()
	var r struct {
		Token   string    `json:"token"`
		Expires time.Time `json:"expires"`
	}
	if err := u.panel.do(http.MethodPost, "/api/users/ana.souza/password-link", map[string]any{"hours": 24}, &r); err != nil {
		t.Fatal(err)
	}
	fyne.DoAndWait(func() { u.showLink(ana, strings.TrimRight(url, "/")+"/password/"+r.Token, r.Expires) })
	shot("desktop-11-link-generated", 500*time.Millisecond)
	closeAll()
}

// localURL accepts an https URL of this machine (or, with private, of a private address such as a container's), without
// user info.
func localURL(s string, private bool) bool {
	u, err := neturl.Parse(s)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return false
	}
	if h := u.Hostname(); h == "localhost" {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return ip != nil && (ip.IsLoopback() || private && ip.IsPrivate())
}

// entries lists the text entries under o, in order.
func entries(o fyne.CanvasObject) []*ringEntry {
	if e, ok := o.(*ringEntry); ok {
		return []*ringEntry{e}
	}
	var out []*ringEntry
	switch x := o.(type) {
	case *fyne.Container:
		for _, k := range x.Objects {
			out = append(out, entries(k)...)
		}
	case fyne.Widget:
		for _, k := range test.WidgetRenderer(x).Objects() {
			out = append(out, entries(k)...)
		}
	}
	return out
}

// tappableWith finds, under o, the tappable widget that draws a text starting with prefix.
func tappableWith(o fyne.CanvasObject, prefix string) fyne.Tappable {
	if o == nil {
		return nil
	}
	var kids []fyne.CanvasObject
	switch x := o.(type) {
	case *fyne.Container:
		kids = x.Objects
	case fyne.Widget:
		kids = test.WidgetRenderer(x).Objects()
	}
	for _, k := range kids { // the innermost one: the tab, not the tab bar around it
		if tp := tappableWith(k, prefix); tp != nil {
			return tp
		}
	}
	if tp, ok := o.(fyne.Tappable); ok {
		for _, s := range texts(o) {
			if strings.HasPrefix(s, prefix) {
				return tp
			}
		}
	}
	return nil
}
