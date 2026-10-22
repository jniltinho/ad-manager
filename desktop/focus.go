package main

import (
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// The focus ring of the web (:focus-visible: --focus-ring-w in --color-focus, 3 px on Cristal) around the focusable
// widgets: Fyne marks a focused entry only by its 1 px border in the primary color, and a select or check by a
// background tint, neither as visible as the web's ring. These types are the Fyne widgets with that ring drawn
// outside their bounds while they have the focus.

// ringRenderer is a widget's own renderer plus the ring: four solid bars (a 3 px stroke, centered on its edge,
// would land on half pixels and blur).
type ringRenderer struct {
	fyne.WidgetRenderer
	bars    [4]*canvas.Rectangle // top, bottom, left, right
	focused *bool
	size    fyne.Size
}

func newRingRenderer(r fyne.WidgetRenderer, focused *bool) *ringRenderer {
	rr := &ringRenderer{WidgetRenderer: r, focused: focused}
	for i := range rr.bars {
		rr.bars[i] = canvas.NewRectangle(nil)
		rr.bars[i].Hide()
	}
	return rr
}

func (r *ringRenderer) Objects() []fyne.CanvasObject {
	objs := r.WidgetRenderer.Objects()
	for _, b := range r.bars {
		objs = append(objs, b)
	}
	return objs
}

func (r *ringRenderer) Layout(s fyne.Size) {
	r.WidgetRenderer.Layout(s)
	r.size = s
	w := look.p.focusRing
	for i, g := range [4][4]float32{
		{-w, -w, s.Width + 2*w, w}, {-w, s.Height, s.Width + 2*w, w}, {-w, 0, w, s.Height}, {s.Width, 0, w, s.Height},
	} {
		r.bars[i].Move(fyne.NewPos(g[0], g[1]))
		r.bars[i].Resize(fyne.NewSize(g[2], g[3]))
	}
}

func (r *ringRenderer) Refresh() {
	r.WidgetRenderer.Refresh()
	r.Layout(r.size) // the width may have changed with the theme
	for _, b := range r.bars {
		b.FillColor, b.Hidden = col("focus"), !*r.focused
		b.Refresh()
	}
}

// scrollIntoView scrolls every scroll that holds o so that o and its focus ring show: Fyne moves the focus with Tab
// into clipped children without scrolling to them.
// shortcut: the path to o is found through containers, scrolls, cards and popups only (a button inside another
// widget's renderer is not found); upgrade when a focusable control lives inside another widget in a scroll.
func scrollIntoView(o fyne.CanvasObject) {
	d := fyne.CurrentApp().Driver()
	c := d.CanvasForObject(o)
	if c == nil {
		return
	}
	roots := append([]fyne.CanvasObject{c.Content()}, c.Overlays().List()...)
	for _, r := range roots {
		for _, s := range scrollsHolding(r, o) {
			ring := look.p.focusRing
			y := d.AbsolutePositionForObject(o).Y - d.AbsolutePositionForObject(s.Content).Y
			off := s.Offset.Y
			if top := y - ring; top < off {
				off = top
			} else if bottom := y + o.Size().Height + ring; bottom > off+s.Size().Height {
				off = bottom - s.Size().Height
			}
			if off != s.Offset.Y {
				s.ScrollToOffset(fyne.NewPos(s.Offset.X, max(0, off)))
			}
		}
	}
}

// ringRoom pads a scroll's content by the widest focus ring (3 px), so the ring of a control at its edge is not
// clipped by the scroll.
func ringRoom(o fyne.CanvasObject) fyne.CanvasObject {
	return container.New(layout.NewCustomPaddedLayout(3, 3, 3, 3), o)
}

// scrollsHolding returns the scrolls on the path from root down to o (outermost first), or nil if o is not found.
func scrollsHolding(root, o fyne.CanvasObject) []*container.Scroll {
	if root == o {
		return []*container.Scroll{}
	}
	var kids []fyne.CanvasObject
	switch v := root.(type) {
	case *fyne.Container:
		kids = v.Objects
	case *container.Scroll:
		kids = []fyne.CanvasObject{v.Content}
	case *widget.Card:
		kids = []fyne.CanvasObject{v.Content}
	case *widget.PopUp:
		kids = []fyne.CanvasObject{v.Content}
	}
	for _, k := range kids {
		if k == nil {
			continue
		}
		if path := scrollsHolding(k, o); path != nil {
			if s, ok := root.(*container.Scroll); ok {
				return append([]*container.Scroll{s}, path...)
			}
			return path
		}
	}
	return nil
}

// ringEntry is a widget.Entry with the focus ring.
type ringEntry struct {
	widget.Entry
	focused bool
}

func newEntry() *ringEntry {
	e := &ringEntry{}
	e.Wrapping = fyne.TextWrap(fyne.TextTruncateClip) // as widget.NewEntry
	e.ExtendBaseWidget(e)
	return e
}

// newPasswordEntry is a hidden entry with a button that shows what was typed (widget.NewPasswordEntry's revealer is
// internal to Fyne).
func newPasswordEntry() *ringEntry {
	e := newEntry()
	e.Password = true
	var reveal *widget.Button
	reveal = widget.NewButtonWithIcon("", theme.VisibilityOffIcon(), func() {
		e.Password = !e.Password
		reveal.SetIcon(map[bool]fyne.Resource{true: theme.VisibilityOffIcon(), false: theme.VisibilityIcon()}[e.Password])
		e.Refresh()
	})
	reveal.Importance = widget.LowImportance
	e.ActionItem = reveal
	return e
}

func newMultiLineEntry() *ringEntry {
	e := newEntry()
	e.MultiLine = true
	return e
}

func (e *ringEntry) CreateRenderer() fyne.WidgetRenderer {
	return newRingRenderer(e.Entry.CreateRenderer(), &e.focused)
}

func (e *ringEntry) FocusGained() {
	e.focused = true
	e.Entry.FocusGained()
	e.Refresh()
	scrollIntoView(e)
}
func (e *ringEntry) FocusLost() { e.focused = false; e.Entry.FocusLost(); e.Refresh() }

// ringSelect is a widget.Select with the focus ring.
type ringSelect struct {
	widget.Select
	focused bool
}

func newSelect(options []string, changed func(string)) *ringSelect {
	s := &ringSelect{}
	s.Options, s.OnChanged = options, changed
	s.PlaceHolder = widget.NewSelect(nil, nil).PlaceHolder // Fyne's translated "(Select one)"
	s.ExtendBaseWidget(s)
	return s
}

func (s *ringSelect) CreateRenderer() fyne.WidgetRenderer {
	return newRingRenderer(s.Select.CreateRenderer(), &s.focused)
}

func (s *ringSelect) FocusGained() {
	s.focused = true
	s.Select.FocusGained()
	s.Refresh()
	scrollIntoView(s)
}
func (s *ringSelect) FocusLost() { s.focused = false; s.Select.FocusLost(); s.Refresh() }

// ringCheck is a widget.Check with the focus ring.
type ringCheck struct {
	widget.Check
	focused bool
}

func newCheck(label string, changed func(bool)) *ringCheck {
	c := &ringCheck{}
	c.Text, c.OnChanged = label, changed
	c.ExtendBaseWidget(c)
	return c
}

func (c *ringCheck) CreateRenderer() fyne.WidgetRenderer {
	return newRingRenderer(c.Check.CreateRenderer(), &c.focused)
}

func (c *ringCheck) FocusGained() {
	c.focused = true
	c.Check.FocusGained()
	c.Refresh()
	scrollIntoView(c)
}
func (c *ringCheck) FocusLost() { c.focused = false; c.Check.FocusLost(); c.Refresh() }

// ringSelectEntry is a widget.SelectEntry (a field with a list of suggestions) with the focus ring.
type ringSelectEntry struct {
	widget.SelectEntry
	focused bool
}

func newSelectEntry(options []string) *ringSelectEntry {
	e := &ringSelectEntry{}
	e.ExtendBaseWidget(e)
	e.Wrapping = fyne.TextWrap(fyne.TextTruncateClip) // as widget.NewSelectEntry
	e.SetOptions(options)
	return e
}

func (e *ringSelectEntry) CreateRenderer() fyne.WidgetRenderer {
	return newRingRenderer(e.SelectEntry.CreateRenderer(), &e.focused)
}

func (e *ringSelectEntry) FocusGained() {
	e.focused = true
	e.SelectEntry.FocusGained()
	e.Refresh()
	scrollIntoView(e)
}
func (e *ringSelectEntry) FocusLost() { e.focused = false; e.SelectEntry.FocusLost(); e.Refresh() }

// checkList is a widget.CheckGroup made of ringChecks: Fyne's group keeps the focus in items of its own, without
// the ring. Selected and SetSelected work as the group's; the screen shows its Container (Fyne only walks a
// *fyne.Container itself, not a type embedding one).
type checkList struct {
	*fyne.Container
	Selected []string
	checks   []*ringCheck
}

func newCheckList(options []string) *checkList {
	l := &checkList{Container: container.NewVBox()}
	for _, o := range options {
		c := newCheck(o, func(bool) { l.sync() })
		l.checks = append(l.checks, c)
		l.Add(c)
	}
	return l
}

func (l *checkList) sync() {
	l.Selected = nil
	for _, c := range l.checks {
		if c.Checked {
			l.Selected = append(l.Selected, c.Text)
		}
	}
}

func (l *checkList) SetSelected(sel []string) {
	for _, c := range l.checks {
		c.SetChecked(slices.Contains(sel, c.Text))
	}
	l.sync()
}

// radioList is a widget.RadioGroup of radioItems (focusable, with the ring): one option chosen, by click, Space or
// Enter. Selected, SetSelected and OnChanged work as the group's.
type radioList struct {
	*fyne.Container
	Selected  string
	OnChanged func(string)
	items     []*radioItem
}

func newRadioList(options []string) *radioList {
	l := &radioList{Container: container.NewVBox()}
	for _, o := range options {
		it := &radioItem{label: o, list: l}
		it.ExtendBaseWidget(it)
		l.items = append(l.items, it)
		l.Add(it)
	}
	return l
}

func (l *radioList) SetSelected(v string) {
	if l.Selected == v {
		return
	}
	l.Selected = v
	for _, it := range l.items {
		it.Refresh()
	}
	if l.OnChanged != nil {
		l.OnChanged(v)
	}
}

// radioItem is one option of a radioList: the theme's radio icon and the label.
type radioItem struct {
	widget.BaseWidget
	label   string
	list    *radioList
	focused bool
	icon    *widget.Icon
}

func (it *radioItem) CreateRenderer() fyne.WidgetRenderer {
	it.icon = widget.NewIcon(it.iconRes())
	text := newText(it.label, col("fg"), 13, false)
	return newRingRenderer(widget.NewSimpleRenderer(container.NewBorder(nil, nil, it.icon, nil, text)), &it.focused)
}

func (it *radioItem) iconRes() fyne.Resource {
	if it.list.Selected == it.label {
		return theme.NewPrimaryThemedResource(theme.RadioButtonCheckedIcon())
	}
	return theme.RadioButtonIcon()
}

func (it *radioItem) Refresh() {
	if it.icon != nil {
		it.icon.SetResource(it.iconRes())
	}
	it.BaseWidget.Refresh()
}

func (it *radioItem) Tapped(*fyne.PointEvent) {
	if c := fyne.CurrentApp().Driver().CanvasForObject(it); c != nil {
		c.Focus(it)
	}
	it.list.SetSelected(it.label)
}

func (it *radioItem) FocusGained() { it.focused = true; it.Refresh(); scrollIntoView(it) }
func (it *radioItem) FocusLost()   { it.focused = false; it.Refresh() }
func (it *radioItem) TypedRune(r rune) {
	if r == ' ' {
		it.list.SetSelected(it.label)
	}
}

func (it *radioItem) TypedKey(k *fyne.KeyEvent) {
	if k.Name == fyne.KeyReturn || k.Name == fyne.KeyEnter {
		it.list.SetSelected(it.label)
	}
}
