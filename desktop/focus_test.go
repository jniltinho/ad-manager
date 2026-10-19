package main

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
)

// TestFocusRing: a focused entry, select, check and select entry get the theme's ring (3 px on Cristal, 2 px elsewhere) just
// outside their bounds, in the focus color; it goes away with the focus.
func TestFocusRing(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	defer applyLook(nil, "default", false)
	for _, th := range []string{"crystal", "default"} {
		applyLook(a, th, false)
		e, s, c, se, r := newEntry(), newSelect([]string{"a"}, nil), newCheck("c", nil), newSelectEntry([]string{"b"}), newRadioList([]string{"r"})
		w := a.NewWindow("t")
		w.SetContent(container.NewPadded(container.NewVBox(e, s, c, se, r.Container)))
		w.Resize(fyne.NewSize(300, 320))
		for _, f := range []fyne.Focusable{e, s, c, se, r.items[0]} {
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
