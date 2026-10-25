package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
)

// TestLoginScreen draws the login in every theme and look (desktop-login): the box is always in the same
// place and size, the flat look draws no shadow, the 98 relief and the frame of the dark themes are there.
// LOGIN_SHOTS=<dir> also writes the captures.
func TestLoginScreen(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	defer applyLook(nil, "default", false)
	w := a.NewWindow("t")
	defer w.Close()
	w.Resize(fyne.NewSize(1000, 700))
	cfg := &config{}
	cfg.Panel.URL = "http://127.0.0.1:9"
	u := &ui{a: a, w: w, cfg: cfg, panel: newAPI(cfg.Panel.URL, "", false), status: newText("", col("footer-strong"), 11, false)}
	u.brandFor = cfg.Panel.URL
	u.brand.domain, u.brand.netbios = "empresa.net", "EMPRESA"
	var pos fyne.Position
	var size fyne.Size
	for _, id := range themeOrder {
		for _, flat := range []bool{false, true} {
			u.brand.theme, u.brand.flat = id, flat
			u.login()
			img := w.Canvas().Capture()
			p, s := a.Driver().AbsolutePositionForObject(u.loginBox), u.loginBox.Size()
			if pos == (fyne.Position{}) {
				pos, size = p, s
			} else if p != pos || s != size {
				t.Errorf("%s flat %v: box at %v %v, want %v %v", id, flat, p, s, pos, size)
			}
			if dir := os.Getenv("LOGIN_SHOTS"); dir != "" {
				f, _ := os.Create(filepath.Join(dir, fmt.Sprintf("login-%s-%v.png", id, flat)))
				_ = png.Encode(f, img)
				f.Close()
			}
			// image coordinates: the capture may be scaled
			sc := float32(img.Bounds().Dx()) / w.Canvas().Size().Width
			at := func(x, y float32) color.NRGBA {
				return color.NRGBAModel.Convert(img.At(int((p.X+x)*sc), int((p.Y+y)*sc))).(color.NRGBA)
			}
			page := color.NRGBAModel.Convert(col("login-page")).(color.NRGBA)
			below := at(s.Width/2, s.Height+4) // under the box: the elliptic shadow and the theme's
			if flat && !near(below, page) {
				t.Errorf("%s flat: shadow pixel %v under the box (page %v)", id, below, page)
			}
			if !flat && look.p.loginShadow.c.A > 0 && look.p.scheme == "light" && lum(below) >= lum(page) {
				t.Errorf("%s: no shadow under the box (%v on page %v)", id, below, page)
			}
			if id == "classic-gray" && !flat {
				if tl, br := at(0.5+1, s.Height/2), at(s.Width-1.5, s.Height/2); lum(tl) <= lum(br) {
					t.Errorf("98 relief: left %v, right %v", tl, br)
				}
			}
			if look.p.scheme == "dark" && !flat {
				if c := contrast(at(0.5, s.Height/2), page); c < 1.5 {
					t.Errorf("%s: frame %.2f:1 on the page", id, c)
				}
			}
		}
	}
}

// near: the same color within a rounding step.
func near(a, b color.NRGBA) bool {
	d := func(x, y uint8) bool { return x-y < 3 || y-x < 3 }
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B)
}

var _ image.Image

// TestPanelBrand503: with the directory down the panel answers /api/health with 503 and still its brand: the login
// takes its theme, domain, NetBIOS name and logo, and keeps what was typed. An answer for a panel no longer
// configured, or after the sign-in, changes nothing; a logo that does not decode leaves the product logo.
func TestPanelBrand503(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	defer applyLook(nil, "default", false)
	theme, logo := "graphite", resAppIcon.Content()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"ldap":"down","domain":"empresa.net","netbios":"EMPRESA","brand":{"theme":"` + theme + `","logo":true}}`))
		case "/api/brand/logo":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(logo)
		}
	}))
	defer srv.Close()
	w := a.NewWindow("t")
	defer w.Close()
	cfg := &config{}
	cfg.Panel.URL = srv.URL
	u := &ui{a: a, w: w, cfg: cfg, panel: newAPI(srv.URL, "", false), status: newText("", col("footer-strong"), 11, false)}
	u.brandFor = srv.URL
	u.login()
	u.typed[0] = "ana" // typed before the answer
	u.panelBrand(u.panel, srv.URL)
	if u.brand.theme != "graphite" || u.brand.domain != "empresa.net" || u.brand.netbios != "EMPRESA" || u.brand.logo == nil || look.id != "graphite" {
		t.Fatalf("brand %+v, look %s", u.brand, look.id)
	}
	if e, ok := findEntry(w.Content(), "ana"); !ok || e == nil {
		t.Fatal("the redrawn login lost the typed user")
	}
	theme = "cone" // the same server, now another brand
	u.panelBrand(newAPI(srv.URL, "", false), "http://another")
	if u.brand.theme != "graphite" || look.id != "graphite" {
		t.Fatalf("an answer for another panel changed the brand: %s / %s", u.brand.theme, look.id)
	}
	u.screen = "shell" // signed in meanwhile
	u.panelBrand(u.panel, srv.URL)
	if look.id != "graphite" {
		t.Fatalf("an answer after the sign-in redrew the screen: %s", look.id)
	}
	logo = []byte("\x89PNG broken")
	if r, err := u.panel.image("/api/brand/logo"); err == nil || r != nil {
		t.Fatalf("a broken logo was accepted: %v", err)
	}
}

// findEntry looks for an entry showing text in the object tree.
func findEntry(o fyne.CanvasObject, text string) (*ringEntry, bool) {
	if e, ok := o.(*ringEntry); ok && e.Text == text {
		return e, true
	}
	if c, ok := o.(*fyne.Container); ok {
		for _, k := range c.Objects {
			if e, ok := findEntry(k, text); ok {
				return e, true
			}
		}
	}
	return nil, false
}

// TestLoginLongTexts: a long domain is cut with "…" and a long error (with a long URL) wraps inside the box width,
// in an 800 × 600 window.
func TestLoginLongTexts(t *testing.T) {
	long := "um-dominio-muito-longo-de-uma-empresa-com-muitas-filiais.exemplo.com.br"
	if s := ellipsis("EMPRESA ("+long+")", 11, fyne.TextStyle{Monospace: true}, 200); fyne.MeasureText(s, 11, fyne.TextStyle{Monospace: true}).Width > 200 || s[len(s)-3:] != "…" {
		t.Fatalf("domain: %q", s)
	}
	for _, l := range wrapWords("falha em https://"+long+"/api/login/"+long+": connection refused", 12, loginW-30) {
		if wd := fyne.MeasureText(l, 12, fyne.TextStyle{}).Width; wd > loginW-30 {
			t.Fatalf("line %q is %.0f wide", l, wd)
		}
	}
}

// TestLoginLongErrorWindow: a long error (with a long URL) in an 800 × 600 window stays within the window, across
// and down (the message is cut at loginMsgLines lines).
func TestLoginLongErrorWindow(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	defer applyLook(nil, "default", false)
	w := a.NewWindow("t")
	defer w.Close()
	w.Resize(fyne.NewSize(800, 600))
	cfg := &config{}
	cfg.Panel.URL = "http://127.0.0.1:9/" + strings.Repeat("caminho-muito-longo/", 40)
	u := &ui{a: a, w: w, cfg: cfg, panel: newAPI(cfg.Panel.URL, "", false), status: newText("", col("footer-strong"), 11, false)}
	u.brandFor = cfg.Panel.URL
	// the longest case: a long domain in the hint, besides the long error
	u.brand.domain, u.brand.netbios = "um-dominio-muito-longo-de-uma-empresa-com-muitas-filiais.exemplo.com.br", "EMPRESA"
	u.login()
	w.Canvas().Focused().(*ringEntry).OnSubmitted("x") // the request fails with an error naming the long URL
	waitSignIn(t, u)
	// the error arrived (not just "Signing in…"), cut at loginMsgLines lines ending in "…" (one more line has the URL)
	var long []string
	for _, s := range texts(w.Content()) {
		if strings.Contains(s, "caminho-muito-longo") {
			long = append(long, s)
		}
	}
	if len(long) < 3 || len(long) > loginMsgLines+1 || !slices.ContainsFunc(long, func(s string) bool { return strings.HasSuffix(s, "…") }) {
		t.Fatalf("message lines %q in %q", long, texts(w.Content()))
	}
	if m := w.Content().MinSize(); m.Width > 800 || 600*0.1-loginPad+m.Height > 600 {
		t.Fatalf("the login needs %v from 10%% of the height, more than the 800 × 600 window", m)
	}
}

// TestLoginRebuiltWhileSigningIn: the panel's brand rebuilds the login screen during a sign-in; its error still
// shows, on the screen rebuilt.
func TestLoginRebuiltWhileSigningIn(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	defer applyLook(nil, "default", false)
	w := a.NewWindow("t")
	defer w.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		rw.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(rw, `{"error":"invalid user or password"}`)
	}))
	defer srv.Close()
	cfg := &config{}
	cfg.Panel.URL = srv.URL
	u := &ui{a: a, w: w, cfg: cfg, panel: newAPI(srv.URL, "", false), status: newText("", col("footer-strong"), 11, false)}
	u.brandFor = srv.URL
	u.login()
	w.Canvas().Focused().(*ringEntry).OnSubmitted("x")
	fyne.DoAndWait(u.login) // the brand arrived: a new screen while the request runs
	waitSignIn(t, u)
	fyne.DoAndWait(u.login) // and again after the error: it stays on the screen
	// the panel's message, translated by the server catalog (pt-BR here) or not
	if !slices.ContainsFunc(texts(w.Content()), func(s string) bool { return strings.Contains(s, "inválidos") || strings.Contains(s, "invalid user") }) {
		t.Fatalf("the error is not on the screen: %q", texts(w.Content()))
	}
}

// TestLoginPanelChangedWhileSigningIn: the panel is changed (Configure) while a sign-in to the old one runs; its
// success must not open the shell with the new panel's client (no session there).
func TestLoginPanelChangedWhileSigningIn(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	defer applyLook(nil, "default", false)
	w := a.NewWindow("t")
	defer w.Close()
	// the old panel signs in at once and holds /api/me until the panel was changed
	// (buffered sends: this package has its own close)
	meAsked, release := make(chan struct{}, 1), make(chan struct{}, 1)
	old := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/me" {
			meAsked <- struct{}{}
			<-release
			fmt.Fprint(rw, `{"user":"admin","domain":"a.local"}`)
		}
	}))
	defer old.Close()
	defer func() {
		select {
		case release <- struct{}{}: // a failed test: let the handler finish
		default:
		}
	}()
	cfg := &config{}
	cfg.Panel.URL = old.URL
	u := &ui{a: a, w: w, cfg: cfg, panel: newAPI(old.URL, "", false), status: newText("", col("footer-strong"), 11, false)}
	u.brandFor = old.URL
	u.login()
	w.Canvas().Focused().(*ringEntry).OnSubmitted("x")
	select {
	case <-meAsked:
	case <-time.After(5 * time.Second):
		t.Fatal("the sign-in never asked /api/me")
	}
	fyne.DoAndWait(func() { // Configure saved another panel and came back to the login
		cfg.Panel.URL = "http://127.0.0.1:9"
		u.panel = newAPI(cfg.Panel.URL, "", false)
		u.brandFor = cfg.Panel.URL
		u.login()
	})
	release <- struct{}{} // only now the old panel answers who signed in
	waitSignIn(t, u)
	var screen, user string
	var ready bool
	fyne.DoAndWait(func() { screen, user, ready = u.screen, u.me.User, !u.loginBtn.Disabled() })
	if screen != "login" || user != "" {
		t.Fatalf("screen %q as %q: the old panel's sign-in went through", screen, user)
	}
	// the new panel's screen is ready to sign in: the button enabled, no "Signing in…" left
	if !ready || slices.ContainsFunc(texts(w.Content()), func(s string) bool { return s == "Entrando…" || s == "Signing in…" }) {
		t.Fatalf("the login stayed busy: button ready %v, texts %q", ready, texts(w.Content()))
	}
}

// waitSignIn waits until the result of the sign-in just started is on screen.
func waitSignIn(t *testing.T, u *ui) {
	t.Helper()
	select {
	case <-u.signInDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the sign-in did not end")
	}
}

// texts lists the text drawn under o.
func texts(o fyne.CanvasObject) []string {
	var out []string
	switch x := o.(type) {
	case *canvas.Text:
		out = append(out, x.Text)
	case *fyne.Container:
		for _, c := range x.Objects {
			out = append(out, texts(c)...)
		}
	case fyne.Widget:
		out = append(out, texts(container.NewWithoutLayout(test.WidgetRenderer(x).Objects()...))...)
	}
	return out
}

// TestDialogFollowsResize: a dialog opens with its top at 10% of the window height, centered across, in every theme
// (Cristal included), and stays there when the window is resized (Fyne does not move a popup shown at a position).
func TestDialogFollowsResize(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	defer applyLook(nil, "default", false)
	for _, th := range []string{"default", "crystal"} {
		w := a.NewWindow("t")
		w.Resize(fyne.NewSize(900, 600))
		cfg := &config{}
		cfg.Panel.URL = "http://127.0.0.1:9"
		u := &ui{a: a, w: w, cfg: cfg, panel: newAPI(cfg.Panel.URL, "", false), status: newText("", col("footer-strong"), 11, false)}
		u.brandFor, u.brand.theme = cfg.Panel.URL, th
		u.login()
		u.showInfo("t", "m")
		if len(u.sheets) != 1 {
			t.Fatalf("%s: dialogs: %d", th, len(u.sheets))
		}
		pop := u.sheets[0].pop
		if th == "crystal" { // a sheet slides down to its place in 150 ms (Fyne's test driver ends it at once)
			time.Sleep(400 * time.Millisecond)
		}
		check := func(when string) {
			c, p := w.Canvas().Size(), pop.Position()
			if p.Y != c.Height*0.1 || p.X != (c.Width-pop.Size().Width)/2 {
				t.Errorf("%s %s: at %v in %v, want the top at 10%% and centered", th, when, p, c)
			}
		}
		check("shown")
		w.Resize(fyne.NewSize(1200, 800))
		w.Content().Refresh()
		check("after the resize")
		w.Close()
	}
}

// TestLoginFitsNarrowWindow: the box is 480 wide and at most the window less 32 (the web's rule). Fyne never makes the
// window narrower than its content, so the content's minimum width must leave those 32.
func TestLoginFitsNarrowWindow(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	defer w.Close()
	cfg := &config{}
	cfg.Panel.URL = "http://127.0.0.1:9"
	u := &ui{a: a, w: w, cfg: cfg, panel: newAPI(cfg.Panel.URL, "", false), status: newText("", col("footer-strong"), 11, false)}
	u.login()
	if min := w.Content().MinSize().Width; loginW > min-32 {
		t.Fatalf("content at least %v wide: the 480 box would not keep 32 px of the window", min)
	}
}
