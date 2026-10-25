package main

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// TestFocusRing: a focused entry, select, check, select entry, radio and button get the theme's ring (3 px on Cristal and High
// contrast, 2 px elsewhere) just outside their bounds, in the focus color; it goes away with the focus.
func TestFocusRing(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	defer applyLook(nil, "default", false)
	for _, th := range []string{"crystal", "high-contrast", "matrix", "default"} {
		applyLook(a, th, false)
		e, s, c, se, r := newEntry(), newSelect([]string{"a"}, nil), newCheck("c", nil), newSelectEntry([]string{"b"}), newRadioList([]string{"r"})
		btn := newButton("b", nil, nil)
		w := a.NewWindow("t")
		w.SetContent(container.NewPadded(container.NewVBox(e, s, c, se, r.Container, container.NewHBox(btn))))
		w.Resize(fyne.NewSize(300, 360))
		for _, f := range []fyne.Focusable{e, s, c, se, r.items[0], btn} {
			w.Canvas().Focus(f)
			o := f.(fyne.CanvasObject)
			pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(o)
			ring := look.p.focusRing
			img := w.Canvas().Capture()
			sc := w.Canvas().Scale()
			px := func(x, y float32) color.NRGBA {
				return color.NRGBAModel.Convert(img.At(int(x*sc), int(y*sc))).(color.NRGBA)
			}
			mid := pos.Y + o.Size().Height/2
			for _, x := range []float32{pos.X - ring + 0.5, pos.X - 0.5} { // the whole band left of the widget
				if got := px(x, mid); got != col("focus") {
					t.Errorf("%s %T: at %v px outside, %v, want the focus color %v", th, f, pos.X-x, got, col("focus"))
				}
			}
			if got := px(pos.X-ring-1.5, mid); got == col("focus") {
				t.Errorf("%s %T: the ring is wider than %v px", th, f, ring)
			}
		}
		w.Canvas().Unfocus()
		img := w.Canvas().Capture()
		pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(e)
		if got := color.NRGBAModel.Convert(img.At(int(pos.X-1), int(pos.Y+e.Size().Height/2))).(color.NRGBA); got == col("focus") {
			t.Errorf("%s: the ring stays without the focus", th)
		}
		w.Close()
	}
}

// TestButtonKeyboard: Tab reaches a button (and skips a disabled one), Space and Enter press it, a disabled button is
// not pressed; the ring goes away when the focus moves on, and a click presses it without focusing it.
func TestButtonKeyboard(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	presses := 0
	b := newButton("Save", nil, func() { presses++ })
	off := newButton("Off", nil, func() { presses += 100 })
	off.Disable()
	e := newEntry()
	w := a.NewWindow("t")
	defer w.Close()
	w.SetContent(container.NewVBox(e, off, b))
	w.Canvas().Focus(e)
	w.Canvas().FocusNext()
	if w.Canvas().Focused() != b {
		t.Fatalf("Tab from the entry focused %T, want the enabled button", w.Canvas().Focused())
	}
	for _, k := range []fyne.KeyName{fyne.KeySpace, fyne.KeyReturn, fyne.KeyEnter} {
		b.TypedKey(&fyne.KeyEvent{Name: k})
	}
	off.TypedKey(&fyne.KeyEvent{Name: fyne.KeySpace})
	if presses != 3 {
		t.Errorf("presses = %d, want 3 (Space, Return, Enter; none on the disabled button)", presses)
	}
	if !b.focused {
		t.Error("the focused button has no ring")
	}
	w.Canvas().Unfocus()
	if b.focused {
		t.Error("the ring stays after the focus left")
	}
	test.Tap(b) // a click presses it without focusing it (the web's :focus-visible)
	if presses != 4 || b.focused {
		t.Errorf("click: presses %d (want 4), focused %v (want false)", presses, b.focused)
	}
	w.Canvas().Focus(b) // a click on a button focused from the keyboard drops the ring too
	test.Tap(b)
	if presses != 5 || b.focused {
		t.Errorf("click on the focused button: presses %d (want 5), focused %v (want false)", presses, b.focused)
	}
}

// TestFocusScrollsIntoView: a button that Tab reaches below the visible part of a scroll (the Groups list of a user)
// is scrolled into view with its ring, and back up for the first one.
func TestFocusScrollsIntoView(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	defer applyLook(nil, "default", false)
	applyLook(a, "high-contrast", false) // 3 px ring
	rows := container.NewVBox()
	var btns []*volButton
	for range 20 {
		b := newButton("remove", nil, nil)
		btns = append(btns, b)
		rows.Add(container.NewBorder(nil, nil, nil, b, widget.NewLabel("group")))
	}
	scroll := container.NewVScroll(ringRoom(rows)) // as the Groups list
	scroll.SetMinSize(fyne.NewSize(0, 130))
	w := a.NewWindow("t")
	defer w.Close()
	w.SetContent(container.NewBorder(newEntry(), nil, nil, nil, scroll))
	w.Resize(fyne.NewSize(300, 200))
	d := a.Driver()
	visible := func(b *volButton) bool {
		y := d.AbsolutePositionForObject(b).Y - d.AbsolutePositionForObject(scroll).Y
		ring := look.p.focusRing
		return y-ring >= 0 && y+b.Size().Height+ring <= scroll.Size().Height
	}
	last := btns[len(btns)-1]
	if visible(last) {
		t.Fatal("the last button is visible before the focus: the test does not overflow")
	}
	w.Canvas().Focus(last)
	if !visible(last) || scroll.Offset.Y == 0 {
		t.Errorf("focused last button not scrolled into view (offset %v)", scroll.Offset.Y)
	}
	w.Canvas().Focus(btns[0])
	if !visible(btns[0]) {
		t.Errorf("focused first button not scrolled back into view (offset %v)", scroll.Offset.Y)
	}
	// the same list inside a dialog (Fyne wraps the popup in an internal overlay container)
	scroll.ScrollToTop()
	w.SetContent(container.NewVBox())
	w.Resize(fyne.NewSize(600, 500))
	u := &ui{a: a, w: w}
	u.showModal("Groups", container.NewBorder(newEntry(), nil, nil, nil, scroll), "OK", false, 280, nil)
	w.Canvas().Focus(last)
	if !visible(last) || scroll.Offset.Y == 0 {
		t.Errorf("in a dialog: focused last button not scrolled into view (offset %v)", scroll.Offset.Y)
	}
}

// TestRingRefreshBeforeLayout: a button sized before its renderer exists keeps its content inside it after a refresh
// (the ring once laid the widget out at zero size, hiding the Home page's Open button and moving its icon).
func TestRingRefreshBeforeLayout(t *testing.T) {
	test.NewApp()
	b := newButton("Open", nil, nil)
	b.Resize(fyne.NewSize(120, 32)) // lays out the cached renderer
	r := b.CreateRenderer()         // a fresh one, refreshed before any layout of its own
	r.Refresh()
	for _, o := range r.Objects() {
		if c, ok := o.(*fyne.Container); ok && (c.Position().X < 0 || c.Position().Y < 0) {
			t.Errorf("content at %v, outside the button", c.Position())
		}
	}
}
