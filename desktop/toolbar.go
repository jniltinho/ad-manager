package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// toolbarRow lays out a list page's toolbar on as few lines as fit: the search field (it gives way first, down to
// searchMin), the filter and count (info), the action buttons, and the extras — or, when they do not fit, the ⋯ button
// that holds them. Wide: one line with everything; narrower: one line with ⋯; narrow: the actions on a second line,
// the extras still on the first; narrowest: the second line and ⋯.
type toolbarRow struct {
	searchMin float32
	searchMax float32         // beyond this the room left goes between the actions and the extras, as the web's toolbar
	lines     int             // lines the last layout used (MinSize reports their height)
	relayout  func()          // asks the window to lay out again when the number of lines changes
	box       *fyne.Container // [search, info, actions, extras, more]
}

// newToolbarRow builds the toolbar; relayout is called when it changes between one and two lines.
func newToolbarRow(search, info, actions, extras, more fyne.CanvasObject, relayout func()) fyne.CanvasObject {
	l := &toolbarRow{searchMin: 220, searchMax: 360, lines: 1, relayout: relayout}
	l.box = container.New(l, search, info, actions, extras, more)
	return l.box
}

func (l *toolbarRow) height(objs []fyne.CanvasObject) float32 {
	h := float32(0)
	for _, o := range objs {
		h = max(h, o.MinSize().Height)
	}
	return h
}

// MinSize is the narrowest layout's (two lines, ⋯) width and the height of the lines in use.
func (l *toolbarRow) MinSize(objs []fyne.CanvasObject) fyne.Size {
	p := theme.Padding()
	line := l.height(objs)
	w := max(l.searchMin+p+objs[1].MinSize().Width+p+objs[4].MinSize().Width, objs[2].MinSize().Width)
	return fyne.NewSize(w, line*float32(l.lines)+p*float32(l.lines-1))
}

func (l *toolbarRow) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	search, info, actions, extras, more := objs[0], objs[1], objs[2], objs[3], objs[4]
	p := theme.Padding()
	line := l.height(objs)
	iw, aw, ew, mw := info.MinSize().Width, actions.MinSize().Width, extras.MinSize().Width, more.MinSize().Width
	const breath = 24 // at least this between the actions (or the count) and the extras: they are another group
	lines, right := 1, ew
	switch {
	case size.Width >= l.searchMin+iw+aw+ew+3*p+breath: // everything on one line
	case size.Width >= l.searchMin+iw+aw+mw+3*p: // one line, the extras behind ⋯
		right = mw
	case size.Width >= l.searchMin+iw+ew+2*p+breath: // the actions on a second line, the extras stay on the first
		lines = 2
	default: // the actions on a second line, the extras behind ⋯
		lines, right = 2, mw
	}
	showIf(right == ew, extras)
	showIf(right == mw, more)
	place := func(o fyne.CanvasObject, x, y, w float32) {
		h := o.MinSize().Height
		o.Resize(fyne.NewSize(w, h))
		o.Move(fyne.NewPos(x, y+(line-h)/2))
	}
	sw := size.Width - iw - right - 2*p
	if lines == 1 {
		sw -= aw + p
	}
	if right == ew {
		sw -= breath
	}
	sw = min(sw, l.searchMax)
	place(search, 0, 0, sw)
	place(info, sw+p, 0, iw)
	if lines == 1 {
		place(actions, sw+p+iw+p, 0, aw)
	} else {
		place(actions, 0, line+p, aw)
	}
	if right == ew {
		place(extras, size.Width-ew, 0, ew)
	} else {
		place(more, size.Width-mw, 0, mw)
	}
	if lines != l.lines {
		l.lines = lines
		if l.relayout != nil {
			l.relayout()
		}
	}
}

// showIf shows o when on, hides it otherwise (only when that changes).
func showIf(on bool, o fyne.CanvasObject) {
	if on && !o.Visible() {
		o.Show()
	} else if !on && o.Visible() {
		o.Hide()
	}
}

// moreButton is the ⋯ button: a menu of the toolbar's extras, for when they do not fit.
func (u *ui) moreButton(items ...*fyne.MenuItem) *volButton {
	var b *volButton
	b = newButton("", theme.MoreHorizontalIcon(), func() {
		widget.ShowPopUpMenuAtRelativePosition(fyne.NewMenu("", items...), u.w.Canvas(), fyne.NewPos(0, b.Size().Height), b)
	})
	return b
}
