package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// tabItem is one tab: its label, icon and the view it shows.
type tabItem struct {
	label string
	icon  fyne.Resource
	view  fyne.CanvasObject
}

// themedTabs replaces Fyne's AppTabs with tabs in the theme's inner tab roles, like the web panel (Logs): the other
// tabs are boxes a bit lower, the current one is taller, in the content color with an accent bar on top and open
// into the content below.
type themedTabs struct {
	items      []tabItem
	btns       []*tabButton
	head, body *fyne.Container
	sel        int
	OnSelected func(i int)
	view       fyne.CanvasObject
}

// newTabs builds the tabs with the first one selected: the strip on top of a framed panel in the current tab's
// color, so the current tab opens into it (the strip's base line is the panel's top edge).
func newTabs(items ...tabItem) *themedTabs {
	tb := &themedTabs{head: container.NewHBox(), body: container.NewStack()}
	if look.p.segmented { // a segmented control: the buttons touch, split only by the 1 px divider
		tb.head = container.New(layout.NewCustomPaddedHBoxLayout(0))
	}
	edge := func(w, h float32) *canvas.Rectangle {
		r := canvas.NewRectangle(col("subtab-line"))
		r.SetMinSize(fyne.NewSize(w, h))
		return r
	}
	gap := canvas.NewRectangle(color.Transparent) // the tabs start a little in from the panel's corner
	gap.SetMinSize(fyne.NewSize(6, 0))
	strip := container.NewStack(container.NewBorder(nil, edge(0, 1), nil, nil),
		container.NewBorder(nil, nil, container.NewHBox(gap, tb.head), nil))
	panel := container.NewStack(canvas.NewRectangle(col("surface")),
		container.NewBorder(nil, edge(0, 1), edge(1, 0), edge(1, 0)), container.NewPadded(tb.body))
	tb.view = container.NewBorder(strip, nil, nil, nil, panel)
	for _, it := range items {
		tb.Append(it)
	}
	return tb
}

// Append adds a tab at the end.
func (tb *themedTabs) Append(it tabItem) {
	i := len(tb.items)
	tb.items = append(tb.items, it)
	b := &tabButton{label: it.label, icon: it.icon, onTap: func() { tb.Select(i) }}
	b.ExtendBaseWidget(b)
	tb.btns = append(tb.btns, b)
	if i > 0 { // a gap between tabs; a divider line in a segmented control (macOS)
		gap := canvas.NewRectangle(color.Transparent)
		gap.SetMinSize(fyne.NewSize(2, 0))
		if look.p.segmented {
			gap.FillColor = col("subtab-line")
			gap.SetMinSize(fyne.NewSize(1, 0))
		}
		tb.head.Add(gap)
	}
	tb.head.Add(b)
	tb.body.Add(it.view)
	tb.show()
}

// Select shows tab i and calls OnSelected.
func (tb *themedTabs) Select(i int) {
	if i < 0 || i >= len(tb.items) || i == tb.sel {
		return
	}
	tb.sel = i
	tb.show()
	if tb.OnSelected != nil {
		tb.OnSelected(i)
	}
}

// SetLabel changes the label of tab i.
func (tb *themedTabs) SetLabel(i int, label string) {
	tb.items[i].label, tb.btns[i].label = label, label
	tb.btns[i].Refresh()
}

// SelectedIndex is the current tab.
func (tb *themedTabs) SelectedIndex() int { return tb.sel }

// View is the object to place in a layout.
func (tb *themedTabs) View() fyne.CanvasObject { return tb.view }

func (tb *themedTabs) show() {
	for k, b := range tb.btns {
		b.on = k == tb.sel
		b.Refresh()
		if k == tb.sel {
			tb.items[k].view.Show()
		} else {
			tb.items[k].view.Hide()
		}
	}
	tb.body.Refresh()
}

// tabButton is one tab of themedTabs.
type tabButton struct {
	widget.BaseWidget
	label     string
	icon      fyne.Resource
	on, hover bool
	onTap     func()
}

func (b *tabButton) Tapped(*fyne.PointEvent)        { b.onTap() }
func (b *tabButton) MouseIn(*desktop.MouseEvent)    { b.hover = true; b.Refresh() }
func (b *tabButton) MouseMoved(*desktop.MouseEvent) {}
func (b *tabButton) MouseOut()                      { b.hover = false; b.Refresh() }
func (b *tabButton) Cursor() desktop.Cursor         { return desktop.PointerCursor }

const tabInset = 3 // the other tabs start this much lower

// onTabOn is the text of the current tab, which has the content's color: the theme's own color when its inner tab
// already is the content's (folder-style themes), else the strong text (Default's inner tab is the selection navy).
func onTabOn() color.Color {
	if look.p.segmented || col("subtab-on") == col("surface") { // a segment: its own fill (Cristal's blue)
		return col("on-subtab-on")
	}
	return col("fg-strong")
}

func (b *tabButton) CreateRenderer() fyne.WidgetRenderer {
	r := &tabRenderer{b: b, bg: canvas.NewRectangle(color.Transparent), bottom: canvas.NewRectangle(color.Transparent),
		icon: widget.NewIcon(nil), label: newText(b.label, col("on-subtab"), 12, true)}
	for i := range r.edges {
		r.edges[i] = canvas.NewRectangle(color.Transparent)
	}
	r.content = container.NewHBox(r.icon, r.label)
	if b.icon == nil {
		r.icon.Hide()
	}
	return r
}

type tabRenderer struct {
	b          *tabButton
	bg, bottom *canvas.Rectangle
	edges      [8]*canvas.Rectangle // the theme's inset layers (--shadow-subtab, -on): two bands each, up to 4; they
	// mark the state, so the flat look keeps them (as the web)
	layers  []boxShadow
	icon    *widget.Icon
	label   *canvas.Text
	content *fyne.Container
}

func (r *tabRenderer) MinSize() fyne.Size {
	c := r.content.MinSize()
	return fyne.NewSize(c.Width+28, c.Height+12+tabInset)
}

func (r *tabRenderer) Layout(size fyne.Size) {
	y := float32(tabInset)
	if r.b.on || look.p.segmented { // segmented: every tab the same height
		y = 0
	}
	h := size.Height - y
	r.bg.Move(fyne.NewPos(0, y))
	r.bg.Resize(fyne.NewSize(size.Width, h))
	layoutInset(r.edges[:], r.layers, y, size)
	r.bottom.Move(fyne.NewPos(0, size.Height-1))
	r.bottom.Resize(fyne.NewSize(size.Width, 1))
	c := r.content.MinSize()
	r.content.Move(fyne.NewPos((size.Width-c.Width)/2, y+(h-c.Height)/2+1))
	r.content.Resize(c)
}

func (r *tabRenderer) Refresh() {
	r.bottom.FillColor = col("subtab-line")
	name := colorOnSubtab
	r.layers = look.p.subtab
	switch {
	case r.b.on:
		r.bg.FillColor, r.label.Color, name = col("surface"), onTabOn(), colorOnSubtabOn
		r.bottom.FillColor, r.layers = color.Transparent, look.p.subtabOn // open into the content, with its bar
		if look.p.segmented {                                             // the selected segment: the theme's colors
			r.bg.FillColor, r.label.Color = col("subtab-on"), col("on-subtab-on")
		}
	case r.b.hover:
		r.bg.FillColor, r.label.Color = col("subtab-hover"), col("on-subtab")
	default:
		r.bg.FillColor, r.label.Color = col("subtab"), col("on-subtab")
	}
	r.label.Text = r.b.label
	if r.b.icon != nil {
		r.icon.SetResource(theme.NewColoredResource(r.b.icon, name))
	}
	r.Layout(r.b.Size())
	for _, o := range append([]fyne.CanvasObject{r.bg, r.bottom, r.label}, r.objs(r.edges[:])...) {
		o.Refresh()
	}
}

func (r *tabRenderer) Objects() []fyne.CanvasObject {
	return append(append([]fyne.CanvasObject{r.bg, r.bottom}, r.objs(r.edges[:])...), r.content)
}

// layoutInset draws layers as the web draws an inset box-shadow without blur, in the box from y down: each layer a
// band along the side its offset points to (dx > 0 left, < 0 right; dy > 0 top, < 0 bottom), two rectangles of rs
// per layer (up to len(rs)/2 layers), the first layer last, on top.
func layoutInset(rs []*canvas.Rectangle, layers []boxShadow, y float32, size fyne.Size) {
	h := size.Height - y
	for i, e := range rs {
		e.Hide()
		k := len(layers) - 1 - i/2
		if k < 0 {
			continue
		}
		l := layers[k]
		if i%2 == 0 && l.dx != 0 {
			n := abs32(l.dx)
			e.Move(fyne.NewPos(map[bool]float32{true: 0, false: size.Width - n}[l.dx > 0], y))
			e.Resize(fyne.NewSize(n, h))
			e.FillColor = l.c
			e.Show()
		}
		if i%2 == 1 && l.dy != 0 {
			n := abs32(l.dy)
			e.Move(fyne.NewPos(0, map[bool]float32{true: y, false: size.Height - n}[l.dy > 0]))
			e.Resize(fyne.NewSize(size.Width, n))
			e.FillColor = l.c
			e.Show()
		}
		e.Refresh()
	}
}

func (r *tabRenderer) objs(rs []*canvas.Rectangle) []fyne.CanvasObject {
	out := make([]fyne.CanvasObject, len(rs))
	for i, o := range rs {
		out[i] = o
	}
	return out
}
func (r *tabRenderer) Destroy() {}
