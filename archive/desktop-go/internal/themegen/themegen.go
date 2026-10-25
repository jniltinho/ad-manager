// Package themegen builds the desktop palettes from the web panel's theme files, so both never drift apart:
// the role contract (every @theme block of frontend/src/style.css: the common palette and the roles), each
// :root[data-theme='id'] block of frontend/src/themes/*.css and the theme registry of internal/server/themes.go.
package themegen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Roles are the color roles the desktop uses (without the --color- prefix).
var Roles = []string{
	"fg", "fg-strong", "fg-muted", "fg-subtle", "link", "page", "surface", "surface-2", "row-alt", "row-hover", "line",
	"field", "on-field", "field-line", "focus", "selection", "on-selection", "accent", "pop-line", "line-soft",
	"ok", "err", "warn", "ok-fill", "err-fill", "warn-fill", "on-ok-fill", "on-err-fill", "on-warn-fill",
	"neutral-fill", "on-neutral-fill", "strong-fill", "on-strong-fill",
	"action-top", "action", "action-line", "on-action", "action-2-top", "action-2", "action-2-line", "on-action-2",
	"danger-top", "danger", "danger-line", "on-danger", "brand-action-top", "brand-action", "brand-action-hover", "on-brand-action",
	"dialog-head-top", "dialog-head", "on-dialog-head", "dialog-line", "footer", "on-footer", "footer-strong",
	"header-top", "header", "on-header", "on-header-muted", "header-bar", "logo-plate", "nav-on", "on-nav-on", "nav-on-line", "on-login-box-strong", "on-login-box-faint", "login-field-line", "login-focus", "err-soft", "on-err-soft", "toast-err", "warn-soft", "on-warn-soft", "cardtabs", "cardtabs-line", "cardtab", "on-cardtab", "cardtab-hover", "cardtab-on", "on-cardtab-on", "cardtab-bar", "check-line",
	"table-head-top", "table-head", "on-table-head", "card-head-top", "card-head", "on-card-head", "card-head-bar",
	"subtab-line", "subtab", "on-subtab", "subtab-hover", "subtab-on", "on-subtab-on",
	"tabs-top", "tabs", "tab", "on-tab", "tab-hover", "on-tab-hover", "tab-on-top", "tab-on", "on-tab-on", "tab-on-icon",
	"dialog-head-bar", "decor-ok", "decor-err", "decor-warn", "decor-muted", "decor-strong",
	"login-page", "login-box", "login-box-line", "login-box-bar", "on-login-box", "on-login-box-muted", "login-title", "backdrop",
	// the Rust desktop's web parity: soft status tags, the third surface, link and field hovers, header links, footer,
	// and the right-hand top tabs (API, Queues)
	"ok-soft", "on-ok-soft", "surface-3", "link-hover", "field-focus", "header-link", "header-link-hover",
	"on-header-subtle", "footer-bar", "footer-top", "on-tab-aux", "tab-aux-hover", "on-tab-aux-hover",
	"chart-1", "card-hover-line",
}

// Theme is one registered theme with its resolved colors and its login box shadow.
type Theme struct {
	ID, Scheme  string
	Label       string              // the registry's name ("Classic blue (XP)"), translated by the catalogs
	Colors      map[string][4]uint8 // role → RGBA
	LoginShadow Shadow              // outer layer of --shadow-login-box (zero: none, e.g. only a relief)
	LoginRelief []Shadow            // inset layers of --shadow-login-box (the 98 relief), first on top
	Bevel       []Shadow            // inset layers of --bevel-raised (the 98 relief of buttons), first on top
	Segmented   bool                // tabs joined into a segmented control (no gap, no lower tabs: macOS)
	MinText     float64             // contrast the text must keep (7 on the aaa themes of matrix.json, else 4.5)
	FocusRing   float64             // width of the focus ring in px (--focus-ring-w)
	CardBarW    float64             // the bar left of a card title (--card-head-bar-w)
	DialogBarW  float64             // the bar under a dialog title (--dialog-head-bar-w)
	CardStripe  float64             // the stripe on top of the Home tiles (--card-stripe)
	Subtab      []Shadow            // inset layers of an inner tab (--shadow-subtab), first on top
	SubtabOn    []Shadow            // the same of the current inner tab (--shadow-subtab-on: its bar or relief)
	TabOn       []Shadow            // the same of the current top tab (--shadow-tab-on: the mark of the sidebar)
	Depthless   bool                // no shadow on buttons, cards or the login box (High contrast): always the flat look

	vals map[string]string // every variable of the theme (the contract's, then the theme's), unresolved
}

// Shadow is one outer box-shadow layer: offsets and blur in px, and its color.
type Shadow struct {
	DX, DY, Blur float64
	Color        [4]uint8
}

var (
	reComment  = regexp.MustCompile(`(?s)/\*.*?\*/`)
	reDecl     = regexp.MustCompile(`(--[a-z0-9-]+)\s*:\s*([^;]+);`)
	reThemeAt  = regexp.MustCompile(`@theme(?:\s+static)?\s*\{`)
	reRootAt   = regexp.MustCompile(`(?m)^:root\s*\{`) // the contract's parameters (shadows, backgrounds…)
	reBlock    = regexp.MustCompile(`:root\[data-theme='([a-z0-9-]+)'\]\s*\{([^}]*)\}`)
	reRegistry = regexp.MustCompile(`\{ID: (?:"([^"]+)"|(theme\w+)), Label: "([^"]+)", Scheme: "(\w+)"\}`)
	reConst    = regexp.MustCompile(`(theme\w+)\s*=\s*"([^"]+)"`)
	reVar      = regexp.MustCompile(`var\((--[a-z0-9-]+)\)`)
)

// block returns the body of the {…} that opens at s[i-1] (balanced braces).
func block(s string, i int) string {
	depth := 1
	for j := i; j < len(s); j++ {
		switch s[j] {
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return s[i:j]
			}
		}
	}
	return s[i:]
}

func decls(body string, into map[string]string) {
	for _, m := range reDecl.FindAllStringSubmatch(body, -1) {
		into[m[1]] = strings.TrimSpace(m[2])
	}
}

// Load reads the theme sources under the repository root.
// arrowFill finds the color of the select arrow's data-URL SVG.
var arrowFill = regexp.MustCompile(`fill='%23([0-9a-fA-F]{6})'`)

func Load(root string) ([]Theme, error) {
	read := func(p string) (string, error) {
		b, err := os.ReadFile(filepath.Join(root, p))
		return reComment.ReplaceAllString(string(b), ""), err
	}
	style, err := read("frontend/src/style.css")
	if err != nil {
		return nil, err
	}
	base := map[string]string{} // common palette + contract (the Default values)
	for _, loc := range reThemeAt.FindAllStringIndex(style, -1) {
		decls(block(style, loc[1]), base)
	}
	for _, loc := range reRootAt.FindAllStringIndex(style, -1) {
		decls(block(style, loc[1]), base)
	}
	blocks := map[string]map[string]string{}
	files, _ := filepath.Glob(filepath.Join(root, "frontend/src/themes/*.css"))
	for _, f := range files {
		css, err := read(strings.TrimPrefix(f, root+string(filepath.Separator)))
		if err != nil {
			return nil, err
		}
		for _, m := range reBlock.FindAllStringSubmatch(css, -1) {
			if blocks[m[1]] == nil {
				blocks[m[1]] = map[string]string{}
			}
			decls(m[2], blocks[m[1]])
		}
	}
	reg, err := read("internal/server/themes.go")
	if err != nil {
		return nil, err
	}
	set, err := read("internal/server/settings.go")
	if err != nil {
		return nil, err
	}
	var matrix struct{ AAA []string }
	if b, err := os.ReadFile(filepath.Join(root, "frontend/src/themes/matrix.json")); err == nil {
		_ = json.Unmarshal(b, &matrix)
	}
	aaa := map[string]bool{}
	for _, id := range matrix.AAA {
		aaa[id] = true
	}
	consts := map[string]string{}
	for _, m := range reConst.FindAllStringSubmatch(set, -1) {
		consts[m[1]] = m[2]
	}
	var out []Theme
	for _, m := range reRegistry.FindAllStringSubmatch(reg, -1) {
		id := m[1]
		if id == "" {
			id = consts[m[2]]
		}
		vals := map[string]string{}
		for k, v := range base {
			vals[k] = v
		}
		for k, v := range blocks[id] {
			vals[k] = v
		}
		t := Theme{ID: id, Label: m[3], Scheme: m[4], Colors: map[string][4]uint8{}, vals: vals}
		for _, r := range Roles {
			v, err := resolve(vals, "--color-"+r, 0)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", id, err)
			}
			c, err := parseColor(v)
			if err != nil {
				return nil, fmt.Errorf("%s: --color-%s: %w", id, r, err)
			}
			t.Colors[r] = c
		}
		// the placeholder: the theme's --placeholder-color, or the browser's (the field's ink at 50% over the field)
		ph, field := t.Colors["on-field"], t.Colors["field"]
		if v, err := resolve(vals, "--placeholder-color", 0); err == nil {
			if c, err := parseColor(v); err == nil {
				ph, field = c, c
			}
		}
		for i := 0; i < 3; i++ {
			ph[i] = uint8((int(ph[i]) + int(field[i])) / 2)
		}
		if ph[3] == 0 {
			ph[3] = 255
		}
		t.Colors["placeholder"] = ph
		// the select's arrow: the fill of the theme's --bg-select-arrow SVG (the field's ink when it has none)
		arrow := t.Colors["on-field"]
		if v, err := resolve(vals, "--bg-select-arrow", 0); err == nil {
			if m := arrowFill.FindStringSubmatch(v); m != nil {
				if c, err := parseColor("#" + m[1]); err == nil {
					arrow = c
				}
			}
		}
		t.Colors["select-arrow"] = arrow
		if v, err := resolve(vals, "--shadow-login-box", 0); err == nil {
			t.LoginShadow = outerShadow(v)
			t.LoginRelief = insetShadows(v)
		}
		if v, err := resolve(vals, "--bevel-raised", 0); err == nil {
			t.Bevel = insetShadows(v)
		}
		zero := func(name string) bool {
			v, err := resolve(vals, name, 0)
			return err == nil && (strings.TrimSpace(v) == "0" || strings.TrimSpace(v) == "0px")
		}
		t.Segmented = zero("--subtabs-gap") && zero("--subtab-inset-top")
		px := func(name string, def float64) float64 {
			if v, err := resolve(vals, name, 0); err == nil {
				v = strings.TrimSpace(v)
				scale := 1.0
				if r, ok := strings.CutSuffix(v, "rem"); ok { // 0.25rem: 4 px
					v, scale = r, 16
				}
				if f, err := strconv.ParseFloat(strings.TrimSuffix(v, "px"), 64); err == nil {
					return f * scale
				}
			}
			return def
		}
		t.FocusRing, t.CardBarW, t.DialogBarW = px("--focus-ring-w", 2), px("--card-head-bar-w", 4), px("--dialog-head-bar-w", 3)
		t.CardStripe = px("--card-stripe", 4)
		for _, f := range []struct {
			name string
			to   *[]Shadow
		}{{"--shadow-subtab", &t.Subtab}, {"--shadow-subtab-on", &t.SubtabOn}, {"--shadow-tab-on", &t.TabOn}} {
			if v, err := resolve(vals, f.name, 0); err == nil {
				*f.to = insetShadows(v)
			}
		}
		t.Depthless = true // a 1 px line around the box (no blur, no offset: High contrast) is a frame, not depth
		for _, n := range []string{"--shadow-btn", "--shadow-card", "--shadow-login-box"} {
			v, err := resolve(vals, n, 0)
			o := outerShadow(v)
			if err != nil || (o.Color[3] != 0 && (o.Blur > 0 || o.DX != 0 || o.DY != 0)) || len(insetShadows(v)) > 0 {
				t.Depthless = false
			}
		}
		t.MinText = 4.5
		if aaa[id] {
			t.MinText = 7
		}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no theme in the registry")
	}
	return out, nil
}

func resolve(vals map[string]string, name string, depth int) (string, error) {
	v, ok := vals[name]
	if !ok {
		return "", fmt.Errorf("undefined %s", name)
	}
	if depth > 20 {
		return "", fmt.Errorf("var() loop at %s", name)
	}
	var err error
	out := reVar.ReplaceAllStringFunc(v, func(s string) string {
		r, e := resolve(vals, reVar.FindStringSubmatch(s)[1], depth+1)
		if e != nil {
			err = e
		}
		return r
	})
	return out, err
}

// outerShadow picks the largest outer layer (no inset, the most blur) of a box-shadow value; relief layers of
// the classic themes (inset) give none.
// layers splits a box-shadow value at its top-level commas.
func layers(v string) []string {
	depth, start := 0, 0
	var out []string
	for i, ch := range v {
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, v[start:i])
				start = i + 1
			}
		}
	}
	return append(out, v[start:])
}

// insetShadows returns the hard inset layers (no blur) of a box-shadow — a relief drawn with lines: dx/dy > 0 light
// the top/left edges, < 0 the bottom/right ones.
func insetShadows(v string) []Shadow {
	var out []Shadow
	for _, l := range layers(v) {
		f := strings.Fields(strings.TrimSpace(l))
		if len(f) < 4 || f[0] != "inset" {
			continue
		}
		dx, err1 := strconv.ParseFloat(strings.TrimSuffix(f[1], "px"), 64)
		dy, err2 := strconv.ParseFloat(strings.TrimSuffix(f[2], "px"), 64)
		rest := f[3:]
		for len(rest) > 1 && (rest[0] == "0" || rest[0] == "0px") { // a zero blur or spread: a solid band
			rest = rest[1:]
		}
		c, err3 := parseColor(strings.Join(rest, " "))
		if err1 != nil || err2 != nil || err3 != nil || c[3] == 0 {
			continue
		}
		out = append(out, Shadow{DX: dx, DY: dy, Color: c})
	}
	return out
}

func outerShadow(v string) Shadow {
	var best Shadow
	depth, start := 0, 0
	var layers []string
	for i, ch := range v {
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				layers = append(layers, v[start:i])
				start = i + 1
			}
		}
	}
	layers = append(layers, v[start:])
	for _, l := range layers {
		f := strings.Fields(strings.TrimSpace(l))
		if len(f) < 3 || strings.Contains(l, "inset") {
			continue
		}
		var n []float64
		k := 0
		for ; k < len(f) && k < 4; k++ {
			x, err := strconv.ParseFloat(strings.TrimSuffix(f[k], "px"), 64)
			if err != nil {
				break
			}
			n = append(n, x)
		}
		c, err := parseColor(strings.Join(f[k:], " "))
		if err != nil || len(n) < 2 || c[3] == 0 {
			continue
		}
		s := Shadow{DX: n[0], DY: n[1], Color: c}
		if len(n) > 2 {
			s.Blur = n[2]
		}
		if s.Blur >= best.Blur {
			best = s
		}
	}
	return best
}

// parseColor reads #hex, rgb()/rgba() (numbers or %), oklch() and transparent.
func parseColor(v string) ([4]uint8, error) {
	v = strings.TrimSpace(v)
	if v == "transparent" {
		return [4]uint8{}, nil
	}
	if strings.HasPrefix(v, "#") {
		h := v[1:]
		if len(h) == 3 || len(h) == 4 {
			var b strings.Builder
			for _, c := range h {
				b.WriteString(strings.Repeat(string(c), 2))
			}
			h = b.String()
		}
		if len(h) == 6 {
			h += "ff"
		}
		n, err := strconv.ParseUint(h, 16, 32)
		if err != nil || len(h) != 8 {
			return [4]uint8{}, fmt.Errorf("bad color %s", v)
		}
		return [4]uint8{uint8(n >> 24), uint8(n >> 16), uint8(n >> 8), uint8(n)}, nil
	}
	fn, args, ok := strings.Cut(strings.TrimSuffix(v, ")"), "(")
	if !ok {
		return [4]uint8{}, fmt.Errorf("unsupported color %s", v)
	}
	p := strings.FieldsFunc(args, func(r rune) bool { return r == ',' || r == ' ' || r == '/' })
	num := func(s string, scale float64) float64 {
		if strings.HasSuffix(s, "%") {
			f, _ := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
			return f / 100 * scale
		}
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}
	alpha := 1.0
	if len(p) == 4 {
		alpha = num(p[3], 1)
	}
	switch fn {
	case "rgb", "rgba":
		if len(p) != 3 && len(p) != 4 {
			return [4]uint8{}, fmt.Errorf("bad color %s", v)
		}
		return [4]uint8{byte8(num(p[0], 255)), byte8(num(p[1], 255)), byte8(num(p[2], 255)), byte8(alpha * 255)}, nil
	case "oklch":
		if len(p) != 3 && len(p) != 4 {
			return [4]uint8{}, fmt.Errorf("bad color %s", v)
		}
		r, g, b := oklchToRGB(num(p[0], 1), num(p[1], 1), num(p[2], 1))
		return [4]uint8{byte8(r), byte8(g), byte8(b), byte8(alpha * 255)}, nil
	}
	return [4]uint8{}, fmt.Errorf("unsupported color %s", v)
}

func byte8(f float64) uint8 { return uint8(math.Round(math.Max(0, math.Min(255, f)))) }

// oklchToRGB converts OKLCH (L 0-1) to sRGB 0-255, as the theme tests of the web do.
func oklchToRGB(L, C, H float64) (float64, float64, float64) {
	h := H * math.Pi / 180
	a, b := C*math.Cos(h), C*math.Sin(h)
	l := math.Pow(L+0.3963377774*a+0.2158037573*b, 3)
	m := math.Pow(L-0.1055613458*a-0.0638541728*b, 3)
	s := math.Pow(L-0.0894841775*a-1.291485548*b, 3)
	g := func(x float64) float64 {
		if x > 0.0031308 {
			x = 1.055*math.Pow(x, 1/2.4) - 0.055
		} else {
			x *= 12.92
		}
		return 255 * math.Min(1, math.Max(0, x))
	}
	return g(4.0767416621*l - 3.3077115913*m + 0.2309699292*s),
		g(-1.2684380046*l + 2.6097574011*m - 0.3413193965*s),
		g(-0.0041960863*l - 0.7034186147*m + 1.707614701*s)
}

// Generate returns the Go source of archive/desktop-go/themes_gen.go.
func Generate(root string) ([]byte, error) {
	themes, err := Load(root)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString("// Code generated by internal/themegen from the web panel's theme files; DO NOT EDIT.\n\npackage main\n\n")
	b.WriteString("import \"image/color\"\n\n")
	b.WriteString("// themeOrder is the registry order of internal/server/themes.go (the first is the default).\n")
	b.WriteString("var themeOrder = []string{")
	for i, t := range themes {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q", t.ID)
	}
	b.WriteString("}\n\n// palettes holds the resolved colors of every theme (only the roles the desktop uses).\n")
	b.WriteString("var palettes = map[string]palette{\n")
	for _, t := range themes {
		sh := t.LoginShadow
		fmt.Fprintf(&b, "\t%q: {scheme: %q, loginShadow: boxShadow{dx: %g, dy: %g, blur: %g, c: color.NRGBA{0x%02x, 0x%02x, 0x%02x, 0x%02x}},",
			t.ID, t.Scheme, sh.DX, sh.DY, sh.Blur, sh.Color[0], sh.Color[1], sh.Color[2], sh.Color[3])
		for _, f := range []struct {
			name   string
			layers []Shadow
		}{{"loginRelief", t.LoginRelief}, {"bevel", t.Bevel}, {"subtab", t.Subtab}, {"subtabOn", t.SubtabOn}, {"tabOn", t.TabOn}} {
			if len(f.layers) == 0 {
				continue
			}
			fmt.Fprintf(&b, " %s: []boxShadow{", f.name)
			for _, r := range f.layers {
				fmt.Fprintf(&b, "{dx: %g, dy: %g, c: color.NRGBA{0x%02x, 0x%02x, 0x%02x, 0x%02x}}, ", r.DX, r.DY, r.Color[0], r.Color[1], r.Color[2], r.Color[3])
			}
			b.WriteString("},")
		}
		fmt.Fprintf(&b, " segmented: %v, minText: %g, focusRing: %g, cardBarW: %g, dialogBarW: %g, cardStripe: %g,", t.Segmented, t.MinText, t.FocusRing, t.CardBarW, t.DialogBarW, t.CardStripe)
		if t.Depthless {
			b.WriteString(" depthless: true,")
		}
		b.WriteString(" c: map[string]color.NRGBA{\n")
		keys := make([]string, 0, len(t.Colors))
		for k := range t.Colors {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			c := t.Colors[k]
			fmt.Fprintf(&b, "\t\t%q: {0x%02x, 0x%02x, 0x%02x, 0x%02x},\n", k, c[0], c[1], c[2], c[3])
		}
		b.WriteString("\t}},\n")
	}
	b.WriteString("}\n")
	return format.Source(b.Bytes())
}
