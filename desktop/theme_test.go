package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"ad-manager/desktop/internal/themegen"
)

// TestThemesGenUpToDate: themes_gen.go is what the generator makes from the web theme files now.
func TestThemesGenUpToDate(t *testing.T) {
	want, err := themegen.Generate("..")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("themes_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("themes_gen.go is out of date with the web themes: run go generate in desktop/")
	}
}

// TestPalettes: every registered theme has every role the desktop reads, and the scheme of the registry.
func TestPalettes(t *testing.T) {
	if themeOrder[0] != "default" {
		t.Fatalf("default theme = %s", themeOrder[0])
	}
	for _, id := range themeOrder {
		p := palettes[id]
		if p.scheme != "light" && p.scheme != "dark" {
			t.Errorf("%s: scheme %q", id, p.scheme)
		}
		for _, r := range themegen.Roles {
			if _, ok := p.c[r]; !ok {
				t.Errorf("%s: no %s", id, r)
			}
		}
	}
}

// TestFynePairsContrast: the colors the widgets really draw are readable in every theme — the Fyne pairs through
// panelTheme (the same mapping the app uses) and the desktop buttons at rest and under the mouse. The known failures
// of Default and ADManager (the web matrix's phase B pending list) are allowed under the same recipe id.
func TestFynePairsContrast(t *testing.T) {
	var m struct{ Pending map[string][]string }
	b, err := os.ReadFile("../frontend/src/themes/matrix.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	defer applyLook(nil, "default", false)
	white := color.NRGBA{0xff, 0xff, 0xff, 0xff}
	opaque := func(c color.Color) color.NRGBA { return over(color.NRGBAModel.Convert(c).(color.NRGBA), white) }
	ratio := func(fg, bg color.Color) float64 {
		b := opaque(bg)
		return contrast(over(color.NRGBAModel.Convert(fg).(color.NRGBA), b), b)
	}
	fy := func(n fyne.ThemeColorName) color.Color { return panelTheme{}.Color(n, theme.VariantLight) }
	for _, id := range themeOrder {
		for _, flat := range []bool{false, true} {
			applyLook(nil, id, flat)
			pending := map[string]bool{}
			for _, r := range m.Pending[id] {
				pending[r] = true
			}
			txt := max(4.5, look.p.minText) // 7:1 on the AAA themes (Cristal)
			check := func(recipe, what string, fg, bg color.Color, min float64) {
				if r := ratio(fg, bg); r < min && !pending[recipe] {
					t.Errorf("%s (flat %v): %s = %.2f (< %.1f)", id, flat, what, r, min)
				}
			}
			check("action", "ForegroundOnPrimary/Primary", fy(theme.ColorNameForegroundOnPrimary), fy(theme.ColorNamePrimary), txt)
			check("text/surface", "Foreground/OverlayBackground", fy(theme.ColorNameForeground), fy(theme.ColorNameOverlayBackground), txt)
			check("field/text", "Foreground/InputBackground", fy(theme.ColorNameForeground), fy(theme.ColorNameInputBackground), txt)
			check("placeholder", "PlaceHolder/InputBackground", fy(theme.ColorNamePlaceHolder), fy(theme.ColorNameInputBackground), txt)
			check("field/line", "InputBorder/InputBackground", fy(theme.ColorNameInputBorder), fy(theme.ColorNameInputBackground), 3)
			// a focused select or menu item: the text over the focus tint on the field (Fyne paints focus as a background)
			focused := over(color.NRGBAModel.Convert(fy(theme.ColorNameFocus)).(color.NRGBA), opaque(fy(theme.ColorNameInputBackground)))
			check("field/text", "Foreground on a focused select", fy(theme.ColorNameForeground), focused, txt)
			check("state/err", "Error/OverlayBackground", fy(theme.ColorNameError), fy(theme.ColorNameOverlayBackground), txt)
			check("state/warn", "Warning/OverlayBackground", fy(theme.ColorNameWarning), fy(theme.ColorNameOverlayBackground), txt)
			check("state/ok", "Success/OverlayBackground", fy(theme.ColorNameSuccess), fy(theme.ColorNameOverlayBackground), txt)
			check("link/surface", "Hyperlink/OverlayBackground", fy(theme.ColorNameHyperlink), fy(theme.ColorNameOverlayBackground), txt)
			for _, k := range []struct {
				imp    widget.Importance
				recipe string
			}{{widget.HighImportance, "action/top"}, {widget.DangerImportance, "danger/top"}, {widget.MediumImportance, "action-2/top"}, {widget.WarningImportance, "brand-action/hover"}, {widget.LowImportance, "link/surface"}} {
				p := (&volButton{Importance: k.imp}).palette()
				fills := []color.Color{p.top, p.bottom, hover(p.top, p.text), hover(p.bottom, p.text)}
				if p.flat { // a link button: no fill, a light tint of the link color on hover (over the surface)
					fills = []color.Color{col("surface"), over(tint(col("link"), 0x14, p.text, col("surface")), opaque(col("surface")))}
				}
				for _, fill := range fills {
					// AAA themes (Cristal): 7:1 in every drawn state, hover included
					check(k.recipe, fmt.Sprintf("button %v text over its fill", k.imp), p.text, fill, max(4.5, look.p.minText))
				}
			}
		}
	}
}

// TestApplyLook: an unknown theme falls back to Default; the flat look turns the gradient pairs into one color.
func TestApplyLook(t *testing.T) {
	defer applyLook(nil, "default", false)
	applyLook(nil, "nope", false)
	if look.id != "default" || col("action") != palettes["default"].c["action"] {
		t.Fatalf("fallback: %s", look.id)
	}
	applyLook(nil, "graphite", true)
	if top, base := pair("action"); top != base || strongText != palettes["graphite"].c["fg-strong"] {
		t.Errorf("flat graphite: %v %v", top, base)
	}
	applyLook(nil, "graphite", false)
	if top, base := pair("action"); top != palettes["graphite"].c["action-top"] || base != palettes["graphite"].c["action"] {
		t.Errorf("graphite pair: %v %v", top, base)
	}
}

// TestScreensInEveryTheme builds the themed pieces (header, footer, sidebar, card, table, buttons, modal) in every
// theme and both looks: nothing may panic or miss a role, and the Fyne colors follow the theme.
func TestScreensInEveryTheme(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	defer applyLook(nil, "default", false)
	w := a.NewWindow("t")
	defer w.Close()
	cfg := &config{}
	cfg.Panel.URL = "http://127.0.0.1:9" // no panel: the public theme request fails at once
	u := &ui{a: a, w: w, cfg: cfg, panel: newAPI(cfg.Panel.URL, "", false), status: newText("", col("footer-strong"), 11, false)}
	for _, id := range themeOrder {
		for _, flat := range []bool{false, true} {
			applyLook(a, id, flat)
			if got := theme.Color(theme.ColorNamePrimary); got != col("action") {
				t.Errorf("%s: Fyne primary %v, want the theme's action", id, got)
			}
			tbl := newTable([]column{{"Name", 120, false}, {"State", 90, true}}, func() int { return 3 },
				func(row, c int) (string, color.Color) {
					if c == 1 {
						return "ok", okText
					}
					return "x", strongText
				}, nil)
			btns := container.NewHBox(newButton("a", theme.ConfirmIcon(), nil), func() *volButton {
				b := newButton("b", nil, nil)
				b.Importance = widget.HighImportance
				return b
			}())
			w.SetContent(container.NewBorder(header("d.test", "op", func() {}, func() {}), footer(u.status, "v", ""), nil, nil,
				func() fyne.CanvasObject {
					v, _ := navigation([]page{{"Home", theme.HomeIcon(), container.NewVBox(card("Card", btns), tbl.view(), func() fyne.CanvasObject { o, _ := statTile("S", newText("1", okText, 26, true), "decor-ok"); return o }())}})
					return v
				}()))
			w.Resize(fyne.NewSize(900, 600))
			for o := w.Canvas().Overlays().Top(); o != nil; o = w.Canvas().Overlays().Top() { // the last theme's modal
				w.Canvas().Overlays().Remove(o)
			}
			if dir := os.Getenv("SCREEN_SHOTS"); dir != "" { // the conformity mosaic (theme-conformity): the pieces, and
				// the real home page in the shell
				f, _ := os.Create(filepath.Join(dir, fmt.Sprintf("desktop-%s-%v.png", id, flat)))
				_ = png.Encode(f, w.Canvas().Capture())
				f.Close()
				// a panel with the web mosaic's numbers (its HAR): 8 enabled, 3 disabled, 43 groups, 1 computer
				srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/api/summary":
						fmt.Fprint(rw, `{"users":{"enabled":8,"disabled":3,"locked":0},"groups":43,"computers":1,"policy":{"complexity":false,"min_length":3}}`)
					default:
						fmt.Fprint(rw, `[]`)
					}
				}))
				u.panel = newAPI(srv.URL, "", false)
				nav, _ := navigation([]page{{tf("Home"), theme.HomeIcon(), u.homePage()}, {tf("Users"), theme.AccountIcon(), container.NewVBox()},
					{tf("Groups"), theme.ListIcon(), container.NewVBox()}, {tf("Computers"), theme.ComputerIcon(), container.NewVBox()}})
				w.SetContent(container.NewBorder(header("test.local", "diego.rocha", func() {}, func() {}), footer(u.status, "v", ""), nil, nil, nav))
				w.Resize(fyne.NewSize(1100, 700))
				time.Sleep(300 * time.Millisecond) // the failed request answers the alerts
				f, _ = os.Create(filepath.Join(dir, fmt.Sprintf("desktop-home-%s-%v.png", id, flat)))
				_ = png.Encode(f, w.Canvas().Capture())
				f.Close()
				srv.Close()
				u.panel = newAPI(cfg.Panel.URL, "", false)
			}
			u.showModal("Modal", widget.NewLabel("body"), "OK", false, 400, nil)
			u.brand.theme, u.brand.flat = id, flat
			u.brandFor = cfg.Panel.URL // the login screen keeps this theme (no request)
			u.login()
			if look.id != id || look.flat != flat {
				t.Errorf("login screen: theme %s flat %v, want %s %v", look.id, look.flat, id, flat)
			}
			u.setup(nil)
		}
	}
}
