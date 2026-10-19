package main

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// close: two colors within a few steps per channel (gradients are sampled a few pixels in).
func close(a, b color.Color, d int) bool {
	x, y := color.NRGBAModel.Convert(a).(color.NRGBA), color.NRGBAModel.Convert(b).(color.NRGBA)
	in := func(p, q uint8) bool { return int(p)-int(q) <= d && int(q)-int(p) <= d }
	return in(x.R, y.R) && in(x.G, y.G) && in(x.B, y.B)
}

// TestVolumeDrawn (theme-conformity): what the desktop draws, in every theme and look. Roles: the header, the card
// title and the table head are drawn in the web's roles (header*, card-head*, table-head*: top and base colors).
// Volume: outside the flat look the primary button and those bands go from one color to another, and in the
// Classic gray theme the button and the bands carry the 98 relief; in the flat look they are one color and a card casts
// no shadow.
func TestVolumeDrawn(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	defer applyLook(nil, "default", false)
	w := a.NewWindow("t")
	defer w.Close()
	for _, id := range themeOrder {
		for _, flat := range []bool{false, true} {
			applyLook(a, id, flat)
			btn := newButton("Primary", nil, nil)
			btn.Importance = widget.HighImportance
			hdr := header("d.test", "op", func() {}, func() {})
			crd := card("Card", widget.NewLabel("body"))
			parts := map[string]fyne.CanvasObject{"button": btn, "card head": cardHead("Title"), "table head": tableHead(), "header": hdr}
			sizes := map[string]fyne.Size{"button": fyne.NewSize(160, 34), "card head": fyne.NewSize(300, 34),
				"table head": fyne.NewSize(300, 30), "header": fyne.NewSize(700, 80)}
			bg := canvas.NewRectangle(col("page"))
			rows := container.NewVBox()
			for _, n := range []string{"header", "button", "card head", "table head"} {
				rows.Add(container.NewHBox(fixedSize(parts[n], sizes[n].Width, sizes[n].Height)))
			}
			rows.Add(container.NewHBox(fixedSize(crd, 300, 90)))
			w.SetContent(container.NewStack(bg, rows))
			w.Resize(fyne.NewSize(720, 400))
			img := w.Canvas().Capture()
			sc := float32(img.Bounds().Dx()) / w.Canvas().Size().Width
			px := func(o fyne.CanvasObject, x, y float32) color.NRGBA {
				p := a.Driver().AbsolutePositionForObject(o)
				return color.NRGBAModel.Convert(img.At(int((p.X+x)*sc), int((p.Y+y)*sc))).(color.NRGBA)
			}
			relief98 := id == "classic-gray" && !flat
			for name, o := range parts {
				s := o.Size()
				x := s.Width - 12 // the right end: no text there
				if name == "header" {
					x = s.Width / 2
				}
				bottom := s.Height - 5
				if name == "header" {
					bottom = s.Height - 10 // above the line under the header
				}
				hi, lo := px(o, x, 4), px(o, x, bottom)
				switch {
				case relief98:
					if l, r := px(o, 1.5, s.Height/2), px(o, s.Width-1.5, s.Height/2); name != "header" && lum(l) <= lum(r) {
						t.Errorf("%s %s: no relief (left %v, right %v)", id, name, l, r)
					}
				case flat && !near(hi, lo):
					t.Errorf("%s flat %s: top %v ≠ bottom %v", id, name, hi, lo)
				case !flat && name != "header" && near(hi, lo):
					t.Errorf("%s %s: flat (top %v, bottom %v)", id, name, hi, lo)
				}
				role := map[string]string{"header": "header", "card head": "card-head", "table head": "table-head"}[name]
				if role != "" && !relief98 {
					top, base := pair(role)
					if !close(hi, top, 24) || !close(lo, base, 24) {
						t.Errorf("%s flat %v %s: drawn %v → %v, roles %s %v → %v", id, flat, name, hi, lo, role, top, base)
					}
				}
			}
			if flat { // no shadow under a card: the page right below it
				if under, page := px(crd, 150, 90+3), col("page"); !close(under, page, 2) {
					t.Errorf("%s flat: a shadow under the card (%v on %v)", id, under, page)
				}
			}
		}
	}
}
