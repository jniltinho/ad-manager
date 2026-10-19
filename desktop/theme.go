package main

//go:generate go run ./internal/themegen/gen

import (
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// palette is a theme of the web panel (themes_gen.go, generated from its CSS): the color of each role.
type palette struct {
	scheme      string // "light" or "dark"
	loginShadow boxShadow
	loginRelief []boxShadow // inset layers of the login box (the 98 relief), first on top
	bevel       []boxShadow // inset layers of --bevel-raised (the 98 relief of buttons), first on top
	segmented   bool        // tabs as a segmented control, Finder-like sidebar (macOS: Cristal)
	minText     float64     // contrast the text keeps (7 on the AAA themes)
	focusRing   float32     // the focus ring around a focused field, select or check (--focus-ring-w)
	cardBarW    float32     // the bar left of a card title (--card-head-bar-w)
	dialogBarW  float32     // the bar under a dialog title (--dialog-head-bar-w)
	cardStripe  float32     // the stripe on top of the Home tiles (--card-stripe)
	subtab      []boxShadow // inset layers of an inner tab (--shadow-subtab), first on top
	subtabOn    []boxShadow // of the current inner tab (--shadow-subtab-on: its bar or relief)
	tabOn       []boxShadow // of the current top tab (--shadow-tab-on): the mark of the current sidebar item
	c           map[string]color.NRGBA
}

// boxShadow is the outer layer of a web box-shadow (px); a zero color means none.
type boxShadow struct {
	dx, dy, blur float32
	c            color.NRGBA
}

// look is the active theme: the panel's Settings choice (brand.theme, brand.flat), Default until the panel answers.
var look = struct {
	id   string
	p    palette
	flat bool
}{id: "default", p: palettes["default"]}

// col returns a role of the active theme.
func col(role string) color.NRGBA { return look.p.c[role] }

// pair returns the two colors of a gradient role (role-top → role); the flat look uses the base color for both.
func pair(role string) (color.Color, color.Color) {
	if look.flat {
		return col(role), col(role)
	}
	return col(role + "-top"), col(role)
}

// Colors read by the widgets when they are created; applyLook sets them (the screens are rebuilt after a theme
// change: login and shell).
var (
	strongText, mutedText, okText, errText, warnText color.Color // text roles
	accent                                           color.Color // stripes and bars
)

// applyLook switches to theme id (unknown: Default) and the flat look, for the Fyne widgets too.
func applyLook(a fyne.App, id string, flat bool) {
	p, ok := palettes[id]
	if !ok {
		id, p = themeOrder[0], palettes[themeOrder[0]]
	}
	look.id, look.p, look.flat = id, p, flat
	strongText, mutedText = col("fg-strong"), col("fg-muted")
	okText, errText, warnText = col("ok"), col("err"), col("warn")
	accent = col("accent")
	if a != nil {
		a.Settings().SetTheme(panelTheme{})
	}
}

func init() { applyLook(nil, "default", false) }

// lum is the WCAG relative luminance of c.
func lum(c color.Color) float64 {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	ch := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(n.R) + 0.7152*ch(n.G) + 0.0722*ch(n.B)
}

// contrast is the WCAG contrast ratio of two opaque colors.
func contrast(a, b color.Color) float64 {
	x, y := lum(a), lum(b)
	if x < y {
		x, y = y, x
	}
	return (x + 0.05) / (y + 0.05)
}

// contrastOver is the contrast of text (maybe translucent) drawn over the opaque bg.
func contrastOver(text, bg color.Color) float64 {
	t := color.NRGBAModel.Convert(text).(color.NRGBA)
	b := color.NRGBAModel.Convert(bg).(color.NRGBA)
	a := float64(t.A) / 255
	m := func(f, g uint8) uint8 { return uint8(float64(f)*a + float64(g)*(1-a) + 0.5) }
	return contrast(color.NRGBA{m(t.R, b.R), m(t.G, b.G), m(t.B, b.B), 0xff}, b)
}

// dark tells whether c is a dark background (light ink logo, light text).
func dark(c color.Color) bool { return lum(c) < 0.2 }

// colorOnBar names the text color on the title bar color (header, sidebar, dialog title) for themed icons.
const colorOnBar fyne.ThemeColorName = "adm-on-bar"

// colorOnTab, colorOnTabHover and colorOnTabOn are the text colors of the sidebar items (the web's top tabs).
const (
	colorOnTab      fyne.ThemeColorName = "adm-on-tab"
	colorOnTabHover fyne.ThemeColorName = "adm-on-tab-hover"
	colorOnTabOn    fyne.ThemeColorName = "adm-on-tab-on"
	colorTabOnIcon  fyne.ThemeColorName = "adm-tab-on-icon" // the icon of the current item (the web's current top tab)
)

// colorOnAction2 and colorOnBrandAction name the text colors of the secondary and the login buttons (their icons).
const (
	colorOnAction2     fyne.ThemeColorName = "adm-on-action-2"
	colorOnBrandAction fyne.ThemeColorName = "adm-on-brand-action"
	colorOnSubtab      fyne.ThemeColorName = "adm-on-subtab"    // icons of the other tabs (tabs.go)
	colorOnSubtabOn    fyne.ThemeColorName = "adm-on-subtab-on" // icon of the current tab"
)

// panelTheme gives the Fyne widgets the active theme's roles and square corners (every radius is 0), matching
// the web panel.
type panelTheme struct{}

func (panelTheme) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch n {
	case colorOnBar:
		return col("on-dialog-head")
	case colorOnAction2:
		return col("on-action-2")
	case colorOnBrandAction:
		return col("on-brand-action")
	case colorOnTab:
		return col("on-tab")
	case colorOnTabHover:
		return col("on-tab-hover")
	case colorOnTabOn:
		return col("on-tab-on")
	case colorTabOnIcon:
		return col("tab-on-icon")
	case colorOnSubtab:
		return col("on-subtab")
	case colorOnSubtabOn:
		return onTabOn()
	case theme.ColorNamePrimary:
		return col("action")
	case theme.ColorNameHyperlink:
		return col("link")
	case theme.ColorNameForegroundOnPrimary:
		return col("on-action")
	case theme.ColorNameForegroundOnError:
		return col("on-err-fill")
	case theme.ColorNameForegroundOnWarning:
		return col("on-warn-fill")
	case theme.ColorNameForegroundOnSuccess:
		return col("on-ok-fill")
	case theme.ColorNameFocus:
		// Fyne paints it as the background of a focused select, button or menu item (not only as a ring): a tint,
		// as in Fyne's own themes, as strong as it can be with the text on it still at 4.5:1 (the contrast test)
		return tint(col("focus"), 0x66, col("fg"), col("field"))
	case theme.ColorNameSelection:
		c := col("selection")
		c.A = 0x55
		return c
	case theme.ColorNameBackground:
		return col("surface-2")
	case theme.ColorNameHeaderBackground:
		return col("table-head")
	case theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return col("surface")
	case theme.ColorNameInputBackground:
		return col("field")
	case theme.ColorNameInputBorder:
		return col("field-line")
	case theme.ColorNameSeparator:
		return col("line")
	case theme.ColorNameForeground:
		return col("fg")
	case theme.ColorNamePlaceHolder:
		return col("fg-muted")
	case theme.ColorNameButton:
		return col("action-2")
	// the Fyne state colors paint text (labels by importance, field validation): the text roles; the fills are
	// only drawn by the desktop's own widgets (buttons, tags)
	case theme.ColorNameSuccess:
		return col("ok")
	case theme.ColorNameError:
		return col("err")
	case theme.ColorNameWarning:
		return col("warn")
	case theme.ColorNameShadow:
		if look.flat { // no depth, but the modal popups still dim what is behind them (Fyne uses this color)
			return col("backdrop")
		}
		if look.p.scheme == "dark" {
			return color.NRGBA{0, 0, 0, 0x88}
		}
		return color.NRGBA{0x0e, 0x17, 0x2d, 0x55} // stronger shadow: gives cards depth
	case theme.ColorNameDisabled:
		return col("fg-subtle")
	case theme.ColorNameScrollBar:
		return col("field-line")
	}
	v := theme.VariantLight
	if look.p.scheme == "dark" {
		v = theme.VariantDark
	}
	return theme.DefaultTheme().Color(n, v)
}

func (panelTheme) Font(s fyne.TextStyle) fyne.Resource     { return theme.DefaultTheme().Font(s) }
func (panelTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }

func (panelTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameInputRadius, theme.SizeNameSelectionRadius, theme.SizeNameScrollBarRadius,
		theme.SizeNameButtonRadius, theme.SizeNameCardRadius, theme.SizeNameDialogRadius,
		theme.SizeNameInnerWindowRadius, theme.SizeNameMenuRadius, theme.SizeNamePopupRadius,
		theme.SizeNameWindowButtonRadius:
		return 0 // square corners everywhere
	case theme.SizeNameText:
		return 13
	}
	return theme.DefaultTheme().Size(n)
}

// over composites a translucent color over an opaque background.
func over(fg, bg color.NRGBA) color.NRGBA {
	a := float64(fg.A) / 255
	m := func(f, b uint8) uint8 { return uint8(float64(f)*a + float64(b)*(1-a) + 0.5) }
	return color.NRGBA{m(fg.R, bg.R), m(fg.G, bg.G), m(fg.B, bg.B), 0xff}
}

// tint is c at alpha a, or lighter where text drawn over it on bg would fall under the theme's contrast (4.5:1, 7:1
// on the AAA themes): hover and focus backgrounds never cost the text its contrast.
func tint(c color.Color, a uint8, text, bg color.Color) color.NRGBA {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	b := color.NRGBAModel.Convert(bg).(color.NRGBA)
	b.A = 0xff
	for n.A = a; n.A >= 4 && contrastOver(text, over(n, b)) < max(4.5, look.p.minText); n.A -= 4 {
	}
	return n
}
