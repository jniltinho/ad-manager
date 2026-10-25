package themegen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The Rust desktop's themes.json (desktop-rust/crates/core/assets/themes.json): the same colors as themes_gen.go plus
// what the egui app needs to draw like the web — the named size tokens, each part's gradient and shadow layers, the
// font stacks and the parts the flat look clears.

// RustGradient is a linear-gradient: its direction, the color space it interpolates in and its stops.
type RustGradient struct {
	Dir   string     `json:"dir"`   // "to bottom", "to right", "to top", "to left" or "<n>deg"
	Space string     `json:"space"` // "oklab" or "srgb"
	Stops []RustStop `json:"stops"`
}

// RustStop is a gradient stop at pos (0–1).
type RustStop struct {
	Color string  `json:"color"`
	Pos   float64 `json:"pos"`
}

// RustLayer is one box-shadow layer, in CSS order (the first is on top).
type RustLayer struct {
	Inset  bool    `json:"inset,omitempty"`
	DX     float64 `json:"dx"`
	DY     float64 `json:"dy"`
	Blur   float64 `json:"blur,omitempty"`
	Spread float64 `json:"spread,omitempty"`
	Color  string  `json:"color"`
}

type rustTheme struct {
	Label      string                   `json:"label"`
	Scheme     string                   `json:"scheme"`
	TabMark    *string                  `json:"tab_mark"`
	Depthless  bool                     `json:"depthless"`
	MinText    float64                  `json:"min_text"`
	FocusRing  float64                  `json:"focus_ring"`
	CardStripe float64                  `json:"card_stripe"`
	C          map[string]string        `json:"c"`
	Tokens     map[string]float64       `json:"tokens"`
	Text       map[string]string        `json:"text"`
	Bg         map[string]*RustGradient `json:"bg"`
	BgOther    map[string]string        `json:"bg_other,omitempty"` // images other than one linear gradient (the page's dot grid), as CSS
	Pattern    map[string]*RustPattern  `json:"pattern,omitempty"`  // the ones the egui app paints: the page's dots or lines
	Shadow     map[string][]RustLayer   `json:"shadow"`
	Fonts      map[string][]string      `json:"fonts"`
}

// RustPattern is a background repeated every 22 px (the web's background-size): a dot (an ellipse of radii W and H
// at the tile's center) or horizontal lines (H thick, every Period px).
type RustPattern struct {
	Kind   string  `json:"kind"` // dots or lines
	Color  string  `json:"color"`
	W      float64 `json:"w,omitempty"`
	H      float64 `json:"h"`
	Period float64 `json:"period,omitempty"`
}

var (
	reDots  = regexp.MustCompile(`^radial-gradient\(\s*(?:([\d.]+)px\s+([\d.]+)px\s+at\s+50%\s+50%\s*,\s*)?(.+?)\s+([\d.]+)(px|%)\s*,\s*transparent\s+[\d.]+(?:px|%)\s*\)$`)
	reLines = regexp.MustCompile(`^repeating-linear-gradient\(\s*to bottom\s*,\s*(.+?)\s+0px\s*,\s*.+?\s+([\d.]+)px\s*,\s*transparent\s+[\d.]+px\s*,\s*transparent\s+([\d.]+)px\s*\)$`)
)

// parsePattern reads the page backgrounds the themes use: radial-gradient(color 1px, transparent 1px),
// radial-gradient(1px 4px at 50% 50%, color 100%, transparent 100%) and a repeating-linear-gradient of scan lines.
func parsePattern(v string) (*RustPattern, error) {
	v = strings.Join(strings.Fields(v), " ")
	if m := reDots.FindStringSubmatch(v); m != nil {
		c, err := parseColor(m[3])
		if err != nil {
			return nil, err
		}
		p := &RustPattern{Kind: "dots", Color: hex(c)}
		if m[1] != "" { // an ellipse filled to its edge
			p.W, _ = strconv.ParseFloat(m[1], 64)
			p.H, _ = strconv.ParseFloat(m[2], 64)
		} else { // a circle as wide as the color stop
			p.W, _ = strconv.ParseFloat(m[4], 64)
			p.H = p.W
		}
		return p, nil
	}
	if m := reLines.FindStringSubmatch(v); m != nil {
		c, err := parseColor(m[1])
		if err != nil {
			return nil, err
		}
		h, _ := strconv.ParseFloat(m[2], 64)
		period, _ := strconv.ParseFloat(m[3], 64)
		return &RustPattern{Kind: "lines", Color: hex(c), H: h, Period: period}, nil
	}
	return nil, fmt.Errorf("not a page pattern: %s", v)
}

// rustTokens are the size variables exported in px (rem at 16 px).
var rustTokens = []string{
	"control-h", "control-text", "input-px", "select-pr", "button-px", "toolbar-gap", "toolbar-mb",
	"th-px", "th-py", "td-px", "td-py", "tag-px", "tag-py", "tag-size", "font-size-base",
	"card-head-size", "card-head-weight", "card-head-bar-w", "card-stripe", "focus-ring-w",
	"dialog-line-w", "dialog-head-bar-w", "dialog-head-size",
	"subtabs-line-w", "subtabs-gap", "subtabs-pad", "subtab-inset-top", "subtab-on-overlap",
	"cardtabs-pad", "cardtabs-gap", "cardtab-inset-top", "cardtab-on-overlap", "cardtab-bar-w",
	"tab-gap", "tab-inset-top", "tabs-pad", "footer-bar-w", "footer-gap", "nav-on-last-line-w", "logo-plate-pad",
}

// rustText are the keyword parameters exported as they are.
var rustText = []string{"card-head-transform", "cardtab-transform", "dialog-head-pad", "table-head-h", "footer-pane-pad"}

// rustBg and rustShadow are the parts whose --bg-* and --shadow-* recipes the egui app paints.
var (
	rustBg = []string{"page", "header", "tabs", "tab-on", "action", "action-hover", "action-2", "brand-action", "danger",
		"table-head", "card-head", "dialog-head", "dialog-foot", "footer"}
	rustShadow = []string{"btn", "btn-2", "btn-danger", "brand-action", "btn-active", "card", "card-hover", "field",
		"field-focus", "table-head", "app", "tabs", "tab-on", "login-box", "dialog", "subtab", "subtab-on", "cardtab",
		"cardtab-on", "popup", "toast", "nav-on", "tab"}
	// rustRelief are shadows the web names without the --shadow- prefix (the footer's line and panes).
	rustRelief = []string{"footer-line", "footer-pane-relief"}
)

// The web's monospace stack (Tailwind's font-mono) when a theme does not set --font-mono.
var tailwindMono = []string{"ui-monospace", "SFMono-Regular", "Menlo", "Monaco", "Consolas", "Liberation Mono", "Courier New", "monospace"}

var (
	reFlat  = regexp.MustCompile(`(?s):root\[data-flat='true'\]\[data-theme\]\s*\{(.*?)\}`)
	reStopN = regexp.MustCompile(`^(.*?)\s+(-?[\d.]+)%$`)
)

func hex(c [4]uint8) string { return fmt.Sprintf("#%02x%02x%02x%02x", c[0], c[1], c[2], c[3]) }

// parseGradient reads none or one linear-gradient(<dir> [in <space>], <color> [<pos>%], …); other images (url(), a
// second layer) give an error.
func parseGradient(v string) (*RustGradient, error) {
	v = strings.TrimSpace(v)
	if v == "none" || v == "" {
		return nil, nil
	}
	if len(layers(v)) != 1 || !strings.HasPrefix(v, "linear-gradient(") || !strings.HasSuffix(v, ")") {
		return nil, fmt.Errorf("unsupported background %q", v)
	}
	args := layers(strings.TrimSuffix(strings.TrimPrefix(v, "linear-gradient("), ")"))
	g := &RustGradient{Dir: "to bottom", Space: "srgb"}
	first := strings.TrimSpace(args[0])
	if strings.HasPrefix(first, "to ") || strings.HasPrefix(first, "in ") || strings.HasSuffix(strings.Fields(first)[0], "deg") {
		f := strings.Fields(first)
		for i := 0; i < len(f); i++ {
			switch {
			case f[i] == "in" && i+1 < len(f):
				g.Space = f[i+1]
				i++
			case f[i] == "to" && i+1 < len(f):
				g.Dir = "to " + f[i+1]
				i++
			case strings.HasSuffix(f[i], "deg"):
				g.Dir = f[i]
			}
		}
		args = args[1:]
	}
	for i, a := range args {
		a = strings.TrimSpace(a)
		pos := -1.0
		if m := reStopN.FindStringSubmatch(a); m != nil {
			a = m[1]
			p, _ := strconv.ParseFloat(m[2], 64)
			pos = p / 100
		}
		c, err := parseColor(a)
		if err != nil {
			return nil, err
		}
		if pos < 0 { // no position: spread evenly, as CSS does
			pos = float64(i) / float64(max(1, len(args)-1))
		}
		g.Stops = append(g.Stops, RustStop{Color: hex(c), Pos: pos})
	}
	if len(g.Stops) < 2 {
		return nil, fmt.Errorf("gradient with fewer than 2 stops: %q", v)
	}
	return g, nil
}

// parseShadows reads every layer of a box-shadow, in CSS order; none and transparent layers give none.
func parseShadows(v string) ([]RustLayer, error) {
	v = strings.TrimSpace(v)
	if v == "none" || v == "" {
		return nil, nil
	}
	var out []RustLayer
	for _, l := range layers(v) {
		f := strings.Fields(strings.TrimSpace(l))
		r := RustLayer{}
		if len(f) > 0 && f[0] == "inset" {
			r.Inset, f = true, f[1:]
		}
		if len(f) > 0 && f[len(f)-1] == "inset" {
			r.Inset, f = true, f[:len(f)-1]
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
		if len(n) < 2 {
			return nil, fmt.Errorf("bad shadow layer %q", l)
		}
		c, err := parseColor(strings.Join(f[k:], " "))
		if err != nil {
			return nil, err
		}
		if c[3] == 0 {
			continue
		}
		r.DX, r.DY, r.Color = n[0], n[1], hex(c)
		if len(n) > 2 {
			r.Blur = n[2]
		}
		if len(n) > 3 {
			r.Spread = n[3]
		}
		out = append(out, r)
	}
	return out, nil
}

// stack splits a font-family list into its family names, without quotes.
func stack(v string) []string {
	var out []string
	for _, f := range layers(v) {
		if f = strings.Trim(strings.TrimSpace(f), `'"`); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// Rust returns the JSON of desktop-rust/crates/core/assets/themes.json.
func Rust(root string) ([]byte, error) {
	themes, err := Load(root)
	if err != nil {
		return nil, err
	}
	style, err := os.ReadFile(filepath.Join(root, "frontend/src/style.css"))
	if err != nil {
		return nil, err
	}
	flat := []string{}
	if m := reFlat.FindSubmatch(reComment.ReplaceAll(style, nil)); m != nil {
		for _, d := range reDecl.FindAllStringSubmatch(string(m[1]), -1) {
			flat = append(flat, d[1])
		}
	}
	sort.Strings(flat)
	out := struct {
		Order  []string             `json:"order"`
		Flat   []string             `json:"flat"`
		Themes map[string]rustTheme `json:"themes"`
	}{Flat: flat, Themes: map[string]rustTheme{}}
	for _, t := range themes {
		out.Order = append(out.Order, t.ID)
		rt := rustTheme{Label: t.Label, Scheme: t.Scheme, Depthless: t.Depthless, MinText: t.MinText, FocusRing: t.FocusRing,
			CardStripe: t.CardStripe, C: map[string]string{}, Tokens: map[string]float64{}, Text: map[string]string{},
			Bg: map[string]*RustGradient{}, BgOther: map[string]string{}, Pattern: map[string]*RustPattern{}, Shadow: map[string][]RustLayer{}, Fonts: map[string][]string{}}
		for _, s := range t.TabOn { // the current sidebar item's mark: a band along a side (dx 0, dy not 0)
			if s.DX == 0 && s.DY != 0 {
				m := hex(s.Color)
				rt.TabMark = &m
				break
			}
		}
		for k, c := range t.Colors {
			rt.C[k] = hex(c)
		}
		get := func(name string) (string, bool) {
			v, err := resolve(t.vals, name, 0)
			return strings.TrimSpace(v), err == nil
		}
		for _, n := range []string{"scroll-thumb", "scroll-track"} { // the Rust desktop's scroll bars (Fyne drew none)
			if v, ok := get("--color-" + n); ok {
				if c, err := parseColor(v); err == nil {
					rt.C[n] = hex(c)
				}
			}
		}
		for _, n := range rustTokens {
			if v, ok := get("--" + n); ok {
				scale := 1.0
				if r, ok := strings.CutSuffix(v, "rem"); ok {
					v, scale = r, 16
				}
				if f, err := strconv.ParseFloat(strings.TrimSuffix(v, "px"), 64); err == nil {
					rt.Tokens[n] = f * scale
				}
			}
		}
		for _, n := range rustText {
			if v, ok := get("--" + n); ok {
				rt.Text[n] = v
			}
		}
		// the image under the login box: the dark shadow, the light glow of dark pages, or none
		if v, ok := get("--login-shadow-img"); ok {
			switch {
			case v == "none":
				rt.Text["login-shadow"] = "none"
			case strings.Contains(v, "light"):
				rt.Text["login-shadow"] = "light"
			default:
				rt.Text["login-shadow"] = "dark"
			}
		}
		for _, p := range rustBg {
			if v, ok := get("--bg-" + p); ok {
				g, err := parseGradient(v)
				if err != nil {
					rt.BgOther[p] = v // drawn by its own painter (or not at all), never as a wrong gradient
					if pt, err := parsePattern(v); err == nil {
						rt.Pattern[p] = pt
					}
					continue
				}
				rt.Bg[p] = g
			}
		}
		for _, p := range rustShadow {
			if v, ok := get("--shadow-" + p); ok {
				s, err := parseShadows(v)
				if err != nil {
					return nil, fmt.Errorf("%s --shadow-%s: %w", t.ID, p, err)
				}
				rt.Shadow[p] = append([]RustLayer{}, s...) // [] rather than null for none
			}
		}
		for _, p := range rustRelief {
			if v, ok := get("--" + p); ok {
				s, err := parseShadows(v)
				if err != nil {
					return nil, fmt.Errorf("%s --%s: %w", t.ID, p, err)
				}
				rt.Shadow[p] = append([]RustLayer{}, s...)
			}
		}
		if v, ok := get("--font-sans"); ok {
			rt.Fonts["sans"] = stack(v)
		}
		rt.Fonts["mono"] = tailwindMono
		if v, ok := get("--font-mono"); ok {
			rt.Fonts["mono"] = stack(v)
		}
		out.Themes[t.ID] = rt
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetIndent("", " ")
	if err := enc.Encode(out); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
