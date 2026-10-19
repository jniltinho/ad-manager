package main

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	"image/png"
	"math"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

var (
	//go:embed assets/ad-manager-logo-light.png
	logoWhite []byte
	//go:embed assets/login_shadow.png
	loginShadow []byte
	//go:embed assets/icon.png
	appIcon []byte
	//go:embed assets/ad-manager-logo.png
	logoNavy []byte

	resLogoWhite   = fyne.NewStaticResource("ad-manager-logo-light.png", logoWhite)
	resLoginShadow = fyne.NewStaticResource("login_shadow.png", loginShadow)
	resLogoNavy    = fyne.NewStaticResource("ad-manager-logo.png", logoNavy)
	resAppIcon     = fyne.NewStaticResource("icon.png", appIcon)
)

// logoOn returns the product logo readable on bg (light ink on dark backgrounds).
func logoOn(bg color.Color) fyne.Resource {
	if dark(bg) {
		return resLogoWhite
	}
	return resLogoNavy
}

// darken mixes c with black (f = 0.06 is 6% darker).
func darken(c color.Color, f float32) color.Color {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	d := func(v uint8) uint8 { return uint8(float32(v) * (1 - f)) }
	return color.NRGBA{d(n.R), d(n.G), d(n.B), n.A}
}

// framed draws a 1 px frame of color line around o.
func framed(o fyne.CanvasObject, line color.Color) fyne.CanvasObject {
	return container.NewStack(canvas.NewRectangle(line), container.New(layout.NewCustomPaddedLayout(1, 1, 1, 1), o))
}

// dropShadow draws o (w × h) over the web's box-shadow s: a rectangle blurred with a Gaussian (sigma = blur/2,
// as browsers do), offset by dx/dy, in the shadow color. below is placed under o inside the same frame (the
// login's elliptic shadow image), outside the blurred rectangle.
// pad is the room kept around for the shadow (the same in every theme keeps the box in place; a shadow needing
// more is clipped to it); a zero color draws no shadow but keeps the room.
func dropShadow(o fyne.CanvasObject, w, h float32, s boxShadow, below fyne.CanvasObject, belowH, pad float32) fyne.CanvasObject {
	content := container.New(layout.NewCustomPaddedVBoxLayout(0), fixedSize(o, w, h), fixedSize(below, w, belowH))
	if s.c.A == 0 {
		return fixedSize(container.New(layout.NewCustomPaddedLayout(pad, pad, pad, pad), content), w+2*pad, h+belowH+2*pad)
	}
	W, H := float64(w+2*pad), float64(h+belowH+2*pad)
	x0, x1 := float64(pad+s.dx), float64(pad+s.dx+w)
	y0, y1 := float64(pad+s.dy), float64(pad+s.dy+h)
	k := 1 / (math.Sqrt2 * math.Max(float64(s.blur)/2, 0.01))
	cover := func(v, a, b float64) float64 { return 0.5 * (math.Erf((v-a)*k) - math.Erf((v-b)*k)) }
	raster := canvas.NewRasterWithPixels(func(px, py, pw, ph int) color.Color {
		lx, ly := (float64(px)+0.5)*W/float64(pw), (float64(py)+0.5)*H/float64(ph)
		a := float64(s.c.A) * cover(lx, x0, x1) * cover(ly, y0, y1)
		return color.NRGBA{s.c.R, s.c.G, s.c.B, uint8(math.Round(a))}
	})
	return fixedSize(container.NewStack(raster, container.New(layout.NewCustomPaddedLayout(pad, pad, pad, pad), content)), float32(W), float32(H))
}

func abs32(v float32) float32 { return float32(math.Abs(float64(v))) }

// fade returns c with alpha a (secondary text and overlays on the bars).
func fade(c color.Color, a uint8) color.Color {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	n.A = a
	return n
}

// newText creates a canvas.Text with the given color, size and weight.
func newText(s string, c color.Color, size float32, bold bool) *canvas.Text {
	t := canvas.NewText(s, c)
	t.TextSize = size
	t.TextStyle = fyne.TextStyle{Bold: bold}
	return t
}

// fixedSize gives an object a fixed minimum size (logos, stripes).
func fixedSize(o fyne.CanvasObject, w, h float32) fyne.CanvasObject {
	return container.New(layout.NewGridWrapLayout(fyne.NewSize(w, h)), o)
}

// header builds the top bar like the web panel's header (the theme's header roles: a short vertical gradient, the
// logo for its ink, "Active Directory" with the domain after the header bar, the operator on the right) and a thin
// line underneath.
func header(domain, operator string, logout, settings func()) fyne.CanvasObject {
	top, base := pair("header")
	bg := canvas.NewVerticalGradient(top, base)
	on, muted := col("on-header"), col("on-header-muted")
	logo := canvas.NewImageFromResource(logoOn(base))
	logo.FillMode = canvas.ImageFillContain
	bar := canvas.NewRectangle(col("header-bar"))
	bar.SetMinSize(fyne.NewSize(2, 0))
	title := container.NewBorder(nil, nil, bar, nil, container.New(layout.NewCustomPaddedLayout(0, 0, 10, 0),
		container.NewVBox(newText("Active Directory", on, 16, true), newText(domain, muted, 12, false))))
	who := container.NewVBox(newText(operator, on, 13, true), newText(t("domain administrator"), muted, 11, false))
	btn := newButton(t("Sign out"), theme.LogoutIcon(), logout)
	cfgBtn := newButton(t("Configure"), theme.SettingsIcon(), settings)
	row := container.NewBorder(nil, nil,
		container.NewHBox(fixedSize(logo, 180, 60), container.NewCenter(title)),
		container.NewHBox(container.NewCenter(who), container.NewCenter(cfgBtn), container.NewCenter(btn)))
	line := canvas.NewRectangle(col("line"))
	line.SetMinSize(fyne.NewSize(0, 1))
	return container.NewBorder(nil, line, nil, nil, container.NewStack(bg, container.NewPadded(row)))
}

// footer builds the status bar (the theme's footer) with the version and the status message.
// footer is the bottom bar: the company (the panel's, as in the web footer), the status and the version.
func footer(status *canvas.Text, version, company string) fyne.CanvasObject {
	if company == "" {
		company = "AD Manager"
	}
	bg := canvas.NewRectangle(col("footer"))
	bg.SetMinSize(fyne.NewSize(0, 26))
	strong := col("footer-strong")
	return container.NewStack(bg, container.NewBorder(nil, nil,
		container.NewPadded(newText("© "+company, strong, 11, false)),
		container.NewPadded(newText("ad-manager desktop "+version, strong, 11, false)),
		container.NewCenter(status)))
}

// card wraps content in a widget.Card (which has a shadow) with the theme's bar left of the title, as in the web
// panel.
func card(title string, content fyne.CanvasObject) fyne.CanvasObject {
	body := container.NewStack(canvas.NewRectangle(col("surface")), container.NewBorder(cardHead(title), nil, nil, nil, content))
	switch {
	case look.flat: // no shadow: a frame in the line color (the Fyne card's shadow would stay)
		return container.NewPadded(framed(body, col("line")))
	case look.p.scheme == "dark": // dark themes: a light glow around the card instead of a shadow (as on the web)
		glow := canvas.NewRectangle(shadowColor())
		return container.NewPadded(container.NewStack(glow, container.New(layout.NewCustomPaddedLayout(0, 2, 1, 1), framed(body, col("line")))))
	}
	return widget.NewCard("", "", body)
}

// shadowColor is the color of the shadows the desktop draws itself (buttons, cards): dark on light themes, a light
// glow on dark ones, where a black shadow would not show (as on the web). Fyne's own shadow color stays dark: it
// also dims the screen behind modal dialogs.
func shadowColor() color.Color {
	if look.p.scheme == "dark" {
		return color.NRGBA{0xff, 0xff, 0xff, 0x2e}
	}
	return theme.Color(theme.ColorNameShadow)
}

// loginShadowImage is the elliptic shadow under the login box: as is on light themes, inverted (a light glow) on
// dark ones.
func loginShadowImage() fyne.Resource {
	if look.p.scheme != "dark" {
		return resLoginShadow
	}
	img, err := png.Decode(bytes.NewReader(loginShadow))
	if err != nil {
		return resLoginShadow
	}
	b := img.Bounds()
	out := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			out.SetNRGBA(x, y, color.NRGBA{255 - c.R, 255 - c.G, 255 - c.B, c.A})
		}
	}
	var buf bytes.Buffer
	if png.Encode(&buf, out) != nil {
		return resLoginShadow
	}
	return fyne.NewStaticResource("login_shadow_light.png", buf.Bytes())
}

// cardHead is a card's title band: the theme's card head gradient with its bar on the left, if the theme has one
// (and the 98 relief).
func cardHead(title string) fyne.CanvasObject {
	bar := canvas.NewRectangle(col("card-head-bar"))
	bar.SetMinSize(fyne.NewSize(look.p.cardBarW, 0)) // --card-head-bar-w: 0 in the themes without it
	bar.Hidden = look.p.cardBarW == 0
	top, base := pair("card-head")
	return withBevel(container.NewStack(canvas.NewVerticalGradient(top, base), container.NewBorder(nil, nil, bar, nil,
		container.NewPadded(newText(title, col("on-card-head"), 13, true)))))
}

// tableHead is a table's header cell: the theme's table head gradient under the column title (and the 98 relief).
func tableHead() fyne.CanvasObject {
	top, base := pair("table-head")
	// 14 px left = cell padding (8) + badge padding (6): titles line up with the text
	return withBevel(container.NewStack(canvas.NewVerticalGradient(top, base),
		container.New(layout.NewCustomPaddedLayout(6, 6, 14, 8), newText("", col("on-table-head"), 12, true))))
}

// withBevel lays the theme's relief (--bevel-raised, the 98) over o, outside the flat look.
func withBevel(o *fyne.Container) *fyne.Container {
	if !look.flat && len(look.p.bevel) > 0 {
		o.Add(relief(look.p.bevel))
	}
	return o
}

// statTile shows a large colored number with a label and, as the web's Home cards, a stripe on top in the theme's
// decor color (decor-ok, decor-strong…) and width (--card-stripe: none in the 98). The stripe is returned for the
// page to recolor it when the number arrives.
func statTile(label string, value *canvas.Text, decor string) (fyne.CanvasObject, *canvas.Rectangle) {
	stripe := canvas.NewRectangle(col(decor))
	stripe.SetMinSize(fyne.NewSize(0, look.p.cardStripe))
	stripe.Hidden = look.p.cardStripe == 0
	end := col("surface-2")
	if look.flat {
		end = col("surface")
	}
	body := container.NewStack(canvas.NewVerticalGradient(col("surface"), end),
		container.NewPadded(container.NewVBox(newText(label, mutedText, 11, true), value)))
	tile := container.NewBorder(stripe, nil, nil, nil, body)
	if look.flat { // a Fyne card always has its shadow: the flat look has none
		return container.NewPadded(tile), stripe
	}
	return widget.NewCard("", "", tile), stripe
}

// badgeColors maps a cell's state text color to the tag fill and the text on it, like the web .tag.
func badgeColors(c color.Color) (fill, text color.Color) {
	switch c {
	case okText:
		return col("ok-fill"), col("on-ok-fill")
	case errText:
		return col("err-fill"), col("on-err-fill")
	case warnText:
		return col("warn-fill"), col("on-warn-fill")
	case mutedText:
		return col("neutral-fill"), col("on-neutral-fill")
	}
	return col("strong-fill"), col("on-strong-fill")
}

// tag is a fixed-width colored tag with the text in capitals (the state's fill), like the web .tag.
func tag(text string, state color.Color, width float32) fyne.CanvasObject {
	fill, on := badgeColors(state)
	bg := canvas.NewRectangle(fill)
	lbl := newText(strings.ToUpper(text), on, 11, true)
	lbl.Alignment = fyne.TextAlignCenter
	return container.NewCenter(fixedSize(container.NewStack(bg, container.NewCenter(lbl)), width, 20))
}

// column describes one table column. badge draws the cell as a colored tag
// (the state's fill), like the status tags of the web panel.
type column struct {
	title string
	width float32
	badge bool
}

// gridTable is a widget.Table styled like the web grid: navy header, zebra
// rows separated by a thin line, the whole selected row highlighted, and the
// last column stretched to the table width.
type gridTable struct {
	widget.Table
	cols []column
	sel  int
}

const scrollbarRoom = 14 // keeps the last column clear of the vertical scrollbar

// Resize stretches the last column to fill the width.
func (g *gridTable) Resize(size fyne.Size) {
	used := float32(0)
	for _, c := range g.cols[:len(g.cols)-1] {
		used += c.width
	}
	last := g.cols[len(g.cols)-1].width
	if w := size.Width - used - scrollbarRoom; w > last {
		last = w
	}
	g.SetColumnWidth(len(g.cols)-1, last)
	g.Table.Resize(size)
}

// selected returns the selected row (-1 for none).
func (g *gridTable) selected() int { return g.sel }

// clearSelection forgets the selected row.
func (g *gridTable) clearSelection() {
	g.sel = -1
	g.Refresh()
}

// newTable builds a gridTable. cell returns the text and color (nil = default)
// of a cell; onSelect, if not nil, receives the selected row.
func newTable(cols []column, rows func() int, cell func(row, col int) (string, color.Color), onSelect func(row int)) *gridTable {
	g := &gridTable{cols: cols, sel: -1}
	g.Length = func() (int, int) { return rows(), len(cols) }
	g.CreateCell = func() fyne.CanvasObject {
		bg := canvas.NewRectangle(col("surface"))
		line := canvas.NewRectangle(col("line"))
		line.SetMinSize(fyne.NewSize(0, 1))
		tag := canvas.NewRectangle(color.Transparent)
		lbl := canvas.NewText("", theme.Color(theme.ColorNameForeground))
		lbl.TextSize = 13
		// minW gives badges the same width in a column (text centered), like the web tags
		minW := canvas.NewRectangle(color.Transparent)
		badge := container.NewStack(minW, tag, container.New(layout.NewCustomPaddedLayout(1, 1, 6, 6), lbl))
		content := container.New(layout.NewCustomPaddedLayout(6, 6, 8, 8), container.NewHBox(badge))
		return container.NewStack(bg, container.NewBorder(nil, line, nil, nil), content)
	}
	g.UpdateCell = func(id widget.TableCellID, o fyne.CanvasObject) {
		st := o.(*fyne.Container)
		bg := st.Objects[0].(*canvas.Rectangle)
		badge := st.Objects[2].(*fyne.Container).Objects[0].(*fyne.Container).Objects[0].(*fyne.Container)
		minW := badge.Objects[0].(*canvas.Rectangle)
		tag := badge.Objects[1].(*canvas.Rectangle)
		lbl := badge.Objects[2].(*fyne.Container).Objects[0].(*canvas.Text)
		switch {
		case id.Row == g.sel:
			bg.FillColor = col("selection") // whole row
		case id.Row%2 == 1:
			bg.FillColor = col("row-alt")
		default:
			bg.FillColor = col("surface")
		}
		txt, c := cell(id.Row, id.Col)
		// truncate with an ellipsis to the column width (canvas.Text does not truncate by itself)
		if limit := int(g.colWidth(id.Col)/7.2) - 2; limit > 3 && len([]rune(txt)) > limit {
			txt = string([]rune(txt)[:limit-1]) + "…"
		}
		if c == nil {
			c = theme.Color(theme.ColorNameForeground)
		}
		lbl.Text, lbl.TextStyle = txt, fyne.TextStyle{}
		if cols[id.Col].badge && txt != "" {
			tag.FillColor, lbl.Color = badgeColors(c)
			lbl.TextSize, lbl.TextStyle.Bold, lbl.Alignment = 11, true, fyne.TextAlignCenter
			minW.SetMinSize(fyne.NewSize(min(cols[id.Col].width-22, 92), 0))
		} else {
			if id.Row == g.sel {
				c = col("on-selection")
			}
			tag.FillColor, lbl.Color, lbl.TextSize, lbl.Alignment = color.Transparent, c, 13, fyne.TextAlignLeading
			minW.SetMinSize(fyne.NewSize(0, 0))
		}
		st.Objects[2].Refresh() // re-layout: the badge width may have changed
		bg.Refresh()
		tag.Refresh()
		lbl.Refresh()
	}
	g.ShowHeaderRow = true
	g.HideSeparators = true
	g.CreateHeader = tableHead
	g.UpdateHeader = func(id widget.TableCellID, o fyne.CanvasObject) {
		if id.Col >= 0 && id.Col < len(cols) {
			o.(*fyne.Container).Objects[1].(*fyne.Container).Objects[0].(*canvas.Text).Text = cols[id.Col].title
		}
	}
	g.ExtendBaseWidget(g)
	for i, c := range cols {
		g.SetColumnWidth(i, c.width)
	}
	g.OnSelected = func(id widget.TableCellID) {
		g.sel = id.Row
		g.UnselectAll() // the row highlight replaces Fyne's single-cell selection
		g.Refresh()
		if onSelect != nil {
			onSelect(id.Row)
		}
	}
	return g
}

// noPadTheme removes the theme padding inside a table: Fyne uses it as the
// gap between cells, which left white stripes between the zebra cells.
type noPadTheme struct{ fyne.Theme }

func (n noPadTheme) Size(s fyne.ThemeSizeName) float32 {
	if s == theme.SizeNamePadding {
		return 0
	}
	return n.Theme.Size(s)
}

// view is the table to place in a layout (with the padding override).
func (g *gridTable) view() fyne.CanvasObject {
	return container.NewThemeOverride(g, noPadTheme{panelTheme{}})
}

// colWidth is the current width of column c (the last one may be stretched).
func (g *gridTable) colWidth(c int) float32 {
	if c == len(g.cols)-1 {
		if w := g.Size().Width; w > 0 {
			used := float32(0)
			for _, x := range g.cols[:c] {
				used += x.width
			}
			if w-used-scrollbarRoom > g.cols[c].width {
				return w - used - scrollbarRoom
			}
		}
	}
	return g.cols[c].width
}

// relief draws a box's inset shadows as lines inside it (the Windows 98 relief of --bevel-raised): a layer with a
// positive offset lights the top and left edges, a negative one the bottom and right; the first layer is on top.
func relief(layers []boxShadow) fyne.CanvasObject {
	var objs []fyne.CanvasObject
	for i := len(layers) - 1; i >= 0; i-- { // bottom layer first, so the first one ends on top
		l := layers[i]
		n := abs32(l.dx)
		h, v := canvas.NewRectangle(l.c), canvas.NewRectangle(l.c)
		h.SetMinSize(fyne.NewSize(0, n))
		v.SetMinSize(fyne.NewSize(n, 0))
		if l.dx > 0 {
			objs = append(objs, container.NewBorder(h, nil, v, nil))
		} else {
			objs = append(objs, container.NewBorder(nil, h, nil, v))
		}
	}
	return container.NewStack(objs...)
}

// wrapWords breaks s into lines that fit width at the given text size; a word longer than the line (a URL, a DN) is
// cut where it no longer fits.
func wrapWords(s string, size, width float32) []string {
	fits := func(t string) bool { return fyne.MeasureText(t, size, fyne.TextStyle{}).Width <= width }
	var lines []string
	cur := ""
	for _, w := range strings.Fields(s) {
		next := strings.TrimSpace(cur + " " + w)
		if fits(next) {
			cur = next
			continue
		}
		if cur != "" {
			lines, cur = append(lines, cur), ""
		}
		for !fits(w) { // a word alone too long: as much as fits per line
			r := []rune(w)
			n := len(r) - 1
			for n > 1 && !fits(string(r[:n])) {
				n--
			}
			lines, w = append(lines, string(r[:n])), string(r[n:])
		}
		cur = w
	}
	return append(lines, cur)
}

// ellipsis shortens s with "…" until it fits width at the given size and style.
func ellipsis(s string, size float32, style fyne.TextStyle, width float32) string {
	if fyne.MeasureText(s, size, style).Width <= width {
		return s
	}
	r := []rune(s)
	for len(r) > 1 && fyne.MeasureText(string(r)+"…", size, style).Width > width {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}
