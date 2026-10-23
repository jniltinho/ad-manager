package main

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
)

// TestToolbarRow: everything on one line when it fits, the extras behind ⋯ when they do not, the actions on a
// second line when even that does not fit; the search field gives way first and asks for a relayout only when the
// number of lines changes.
func TestToolbarRow(t *testing.T) {
	rect := func(w float32) *canvas.Rectangle {
		r := canvas.NewRectangle(nil)
		r.SetMinSize(fyne.NewSize(w, 30))
		return r
	}
	search, info, actions, extras, more := rect(10), rect(200), rect(760), rect(270), rect(40)
	relayouts := 0
	box := newToolbarRow(search, info, actions, extras, more, func() { relayouts++ }).(*fyne.Container)
	p := theme.Padding()
	for _, c := range []struct {
		width         float32
		extras, lines int
	}{
		{1700, 1, 1}, // 220 + 200 + 760 + 270 + 3p fits
		{1300, 0, 1}, // without the extras: 220 + 200 + 760 + 40 + 3p
		{900, 1, 2},  // the actions below, the extras on the first line: 220 + 200 + 270 + 2p
		{600, 0, 2},  // the actions below, the extras behind ⋯
		{1700, 1, 1},
	} {
		box.Resize(fyne.NewSize(c.width, 100))
		box.Layout.Layout(box.Objects, box.Size())
		if extras.Visible() != (c.extras == 1) || more.Visible() == (c.extras == 1) {
			t.Fatalf("%v: extras %v, more %v", c.width, extras.Visible(), more.Visible())
		}
		lines := 1
		if actions.Position().Y > 0 {
			lines = 2
		}
		if lines != c.lines || box.MinSize().Height != 30*float32(c.lines)+p*float32(c.lines-1) {
			t.Fatalf("%v: %d lines, min height %v", c.width, lines, box.MinSize().Height)
		}
		if search.Size().Width < 220 {
			t.Fatalf("%v: search %v wide", c.width, search.Size().Width)
		}
	}
	if relayouts != 2 { // to two lines and back
		t.Fatalf("%d relayouts", relayouts)
	}
}
