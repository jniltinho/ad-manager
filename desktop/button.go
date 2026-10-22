package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// volButton is a button with depth, like the web panel's .btn: vertical
// gradient, 1 px border, an inner highlight on the top edge and a short shadow
// below. Hover lightens it; pressing sinks it 1 px and drops the shadow.
// Importance picks the style from the theme roles: High = action, Danger = danger, Warning = brand action (the
// login button), Low = flat (toolbar link), anything else = secondary (action-2). The flat look drops the
// gradient, the highlight and the shadow. It takes the keyboard focus (Tab), shows the theme's focus ring and is
// pressed with Space or Enter, like the web's button; a click does not focus it (the web's :focus-visible).
type volButton struct {
	widget.DisableableWidget
	Text       string
	Icon       fyne.Resource
	Importance widget.Importance
	OnTapped   func()

	hovered, pressed, focused bool
}

// newButton returns a volume button with an optional icon.
func newButton(label string, icon fyne.Resource, onTap func()) *volButton {
	b := &volButton{Text: label, Icon: icon, OnTapped: onTap}
	b.ExtendBaseWidget(b)
	return b
}

// SetText changes the label.
func (b *volButton) SetText(s string) { b.Text = s; b.Refresh() }

// SetIcon changes the icon.
func (b *volButton) SetIcon(r fyne.Resource) { b.Icon = r; b.Refresh() }

// Tapped is a click: it also drops the focus the button may have from the keyboard (the ring is for keyboard use).
func (b *volButton) Tapped(*fyne.PointEvent) {
	if c := fyne.CurrentApp().Driver().CanvasForObject(b); c != nil && c.Focused() == b {
		c.Unfocus()
	}
	b.press()
}

func (b *volButton) press() {
	if !b.Disabled() && b.OnTapped != nil {
		b.OnTapped()
	}
}
func (b *volButton) FocusGained()   { b.focused = true; b.Refresh(); scrollIntoView(b) }
func (b *volButton) FocusLost()     { b.focused = false; b.Refresh() }
func (b *volButton) TypedRune(rune) {}

// TypedKey presses the button with Space or Enter (Fyne sends Space as a key, not as a rune).
func (b *volButton) TypedKey(k *fyne.KeyEvent) {
	if k.Name == fyne.KeySpace || k.Name == fyne.KeyReturn || k.Name == fyne.KeyEnter {
		b.press()
	}
}

func (b *volButton) MouseIn(*desktop.MouseEvent)    { b.hovered = true; b.Refresh() }
func (b *volButton) MouseMoved(*desktop.MouseEvent) {}
func (b *volButton) MouseOut()                      { b.hovered, b.pressed = false, false; b.Refresh() }
func (b *volButton) MouseDown(*desktop.MouseEvent)  { b.pressed = true; b.Refresh() }
func (b *volButton) MouseUp(*desktop.MouseEvent)    { b.pressed = false; b.Refresh() }
func (b *volButton) Cursor() desktop.Cursor {
	if b.Disabled() {
		return desktop.DefaultCursor
	}
	return desktop.PointerCursor
}

// palette of a style: gradient top/bottom, border, text, highlight.
type btnPalette struct {
	top, bottom, border, text, gloss color.Color
	iconColor                        fyne.ThemeColorName
	flat                             bool
}

func (b *volButton) palette() btnPalette {
	gloss := func(a uint8) color.Color { // light top edge, none when flat
		if look.flat {
			return color.Transparent
		}
		return rgba(255, 255, 255, a)
	}
	switch b.Importance {
	case widget.HighImportance:
		top, base := pair("action")
		return btnPalette{top, base, col("action-line"), col("on-action"), gloss(0x30), theme.ColorNameForegroundOnPrimary, false}
	case widget.DangerImportance:
		top, base := pair("danger")
		return btnPalette{top, base, col("danger-line"), col("on-danger"), gloss(0x30), theme.ColorNameForegroundOnError, false}
	case widget.WarningImportance:
		top, base := pair("brand-action")
		return btnPalette{top, base, col("brand-action-hover"), col("on-brand-action"), gloss(0x40), colorOnBrandAction, false}
	case widget.LowImportance:
		return btnPalette{color.Transparent, color.Transparent, color.Transparent, col("link"), color.Transparent, theme.ColorNameHyperlink, true}
	}
	top, base := pair("action-2")
	g := gloss(0xff)
	if look.p.scheme == "dark" {
		g = gloss(0x18)
	}
	return btnPalette{top, base, col("action-2-line"), col("on-action-2"), g, colorOnAction2, false}
}

func rgba(r, g, b, a uint8) color.NRGBA { return color.NRGBA{r, g, b, a} }

// hover is the hover tone of a button fill: 12% lighter, or 12% darker when lighter would bring its text under
// 4.5:1 (a light text on a mid-dark fill).
func hover(c, text color.Color) color.Color {
	min := max(look.p.minText, 4.5) // 7 on the AAA themes (Cristal)
	if l := lighten(c, 0.12); contrastOver(text, l) >= min || contrastOver(text, c) < min {
		return l
	}
	r, g, b, a := c.RGBA()
	d := func(v uint32) uint8 { return uint8(float32(v>>8) * 0.88) }
	return color.NRGBA{d(r), d(g), d(b), uint8(a >> 8)}
}

// lighten mixes c with white (hover).
func lighten(c color.Color, f float32) color.Color {
	r, g, bl, a := c.RGBA()
	mix := func(v uint32) uint8 { return uint8(float32(v>>8) + (255-float32(v>>8))*f) }
	return color.NRGBA{mix(r), mix(g), mix(bl), uint8(a >> 8)}
}

func (b *volButton) CreateRenderer() fyne.WidgetRenderer {
	r := &volRenderer{b: b,
		shadow: canvas.NewRectangle(theme.Color(theme.ColorNameShadow)),
		edge:   canvas.NewRectangle(col("line")),
		fill:   canvas.NewVerticalGradient(col("surface"), col("surface")),
		gloss:  canvas.NewRectangle(color.Transparent),
		icon:   widget.NewIcon(nil),
		label:  canvas.NewText("", col("on-action-2")),
		bevel:  relief(look.p.bevel),
	}
	r.label.TextStyle = fyne.TextStyle{Bold: true}
	r.label.TextSize = 13
	r.content = container.NewHBox(r.icon, r.label)
	r.Refresh()
	return newRingRenderer(r, &b.focused)
}

type volRenderer struct {
	b                   *volButton
	shadow, edge, gloss *canvas.Rectangle
	fill                *canvas.LinearGradient
	bevel               fyne.CanvasObject // the theme's relief (98), gone while pressed (the button sinks)
	icon                *widget.Icon
	label               *canvas.Text
	content             *fyne.Container
}

const (
	btnPadX, btnPadY = 12, 6
	btnHeight        = 30
)

func (r *volRenderer) MinSize() fyne.Size {
	c := r.content.MinSize()
	return fyne.NewSize(c.Width+2*btnPadX, max(btnHeight, c.Height+2*btnPadY)+2) // +2: room for the shadow
}

func (r *volRenderer) Layout(size fyne.Size) {
	h := size.Height - 2
	dy := float32(0)
	if r.b.pressed && !r.b.Disabled() {
		dy = 1 // sinks into the shadow
	}
	r.shadow.Move(fyne.NewPos(0, 2))
	r.shadow.Resize(fyne.NewSize(size.Width, h))
	r.edge.Move(fyne.NewPos(0, dy))
	r.edge.Resize(fyne.NewSize(size.Width, h))
	r.fill.Move(fyne.NewPos(1, dy+1))
	r.fill.Resize(fyne.NewSize(size.Width-2, h-2))
	r.gloss.Move(fyne.NewPos(1, dy+1))
	r.gloss.Resize(fyne.NewSize(size.Width-2, 1))
	r.bevel.Move(fyne.NewPos(0, dy))
	r.bevel.Resize(fyne.NewSize(size.Width, h))
	c := r.content.MinSize()
	r.content.Move(fyne.NewPos((size.Width-c.Width)/2, dy+(h-c.Height)/2))
	r.content.Resize(c)
}

func (r *volRenderer) Refresh() {
	p := r.b.palette()
	top, bottom := p.top, p.bottom
	if r.b.hovered && !r.b.Disabled() {
		if r.b.Importance == widget.WarningImportance { // the login button: the web's solid .button:hover
			top = col("brand-action-hover")
			bottom = top
		} else if p.flat {
			top = tint(col("link"), 0x14, p.text, col("surface"))
			bottom = top
		} else {
			top, bottom = hover(top, p.text), hover(bottom, p.text)
		}
	}
	r.fill.StartColor, r.fill.EndColor = top, bottom
	r.edge.FillColor, r.gloss.FillColor, r.label.Color = p.border, p.gloss, p.text
	r.shadow.FillColor = shadowColor()
	r.shadow.Hidden = p.flat || r.b.pressed || look.flat
	if p.flat || look.flat || r.b.Disabled() || r.b.pressed {
		r.bevel.Hide()
	} else {
		r.bevel.Show()
	}
	if p.flat {
		r.edge.FillColor = color.Transparent
	}
	iconColor := p.iconColor
	if r.b.Disabled() {
		r.label.Color, iconColor = mutedText, theme.ColorNameDisabled
		r.shadow.Hidden = true
		if !p.flat {
			r.fill.StartColor, r.fill.EndColor, r.edge.FillColor = col("surface-2"), col("surface-2"), col("line")
			r.gloss.FillColor = color.Transparent
		}
	}
	r.label.Text = r.b.Text
	r.label.Hidden = r.b.Text == ""
	if r.b.Icon == nil {
		r.icon.Hide()
	} else {
		res := r.b.Icon
		// recolor theme icons: white on dark styles, navy on light; an icon that
		// already names a color (e.g. white X on a navy header) keeps it
		if tr, themed := res.(*theme.ThemedResource); themed && (tr.ColorName == "" || r.b.Disabled()) {
			res = theme.NewColoredResource(tr, iconColor)
		}
		r.icon.SetResource(res)
		r.icon.Show()
	}
	for _, o := range []fyne.CanvasObject{r.shadow, r.edge, r.fill, r.gloss, r.label} {
		o.Refresh()
	}
	r.Layout(r.b.Size())
}

func (r *volRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.shadow, r.edge, r.fill, r.gloss, r.bevel, r.content}
}
func (r *volRenderer) Destroy() {}

// newTextButton returns a volume button without an icon.
func newTextButton(label string, onTap func()) *volButton { return newButton(label, nil, onTap) }
