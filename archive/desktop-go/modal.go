package main

import (
	"image/color"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// showModal opens a dialog styled like the web panel: navy gradient header
// with the title, a colored stripe, a white body and a footer with the
// buttons. The modal PopUp dims the background and draws the drop shadow.
// When action is nil only a single button that closes the dialog is shown;
// otherwise a Cancel button is added and action returning false keeps the
// dialog open. It returns what closes the dialog (a wizard closes itself when done).
func (u *ui) showModal(title string, body fyne.CanvasObject, button string, danger bool, width float32, action func() bool) (closeIt func()) {
	var pop *widget.PopUp
	closeModal := func() { pop.Hide() }
	// X in the title bar's text color (an explicit color is kept by volButton)
	x := newButton("", theme.NewColoredResource(theme.CancelIcon(), colorOnBar), closeModal)
	x.Importance = widget.LowImportance
	// header with depth: vertical gradient and a light line on the top edge
	gloss := canvas.NewRectangle(rgba(255, 255, 255, 0x2a))
	gloss.SetMinSize(fyne.NewSize(0, 1))
	gloss.Hidden = look.flat || !dark(col("dialog-head"))
	top, base := pair("dialog-head")
	head := container.NewStack(canvas.NewVerticalGradient(top, base),
		container.NewBorder(gloss, nil, nil, nil),
		container.NewPadded(container.NewBorder(nil, nil, nil, x, container.NewCenter(container.NewHBox(newText(title, col("on-dialog-head"), 14, true))))))
	stripeColor := col("dialog-head-bar") // the web's .pnl head: --color-dialog-head-bar, --dialog-head-bar-w thick
	if danger {
		stripeColor = col("err-fill")
	}
	stripe := canvas.NewRectangle(stripeColor)
	stripe.SetMinSize(fyne.NewSize(0, look.p.dialogBarW))
	stripe.Hidden = look.p.dialogBarW == 0
	buttons := []fyne.CanvasObject{layout.NewSpacer()}
	if action != nil {
		cancel := newButton(t("Cancel"), theme.CancelIcon(), closeModal)
		ok := newButton(button, theme.ConfirmIcon(), func() {
			if action() {
				closeModal()
			}
		})
		ok.Importance = widget.HighImportance
		if danger {
			ok.Importance = widget.DangerImportance
		}
		buttons = append(buttons, cancel, ok)
	} else {
		ok := newButton(button, theme.ConfirmIcon(), closeModal)
		ok.Importance = widget.HighImportance
		buttons = append(buttons, ok)
	}
	// footer set apart by a line, slightly darker gradient
	footLine := canvas.NewRectangle(col("line"))
	footLine.SetMinSize(fyne.NewSize(0, 1))
	footTop := col("surface")
	if look.flat {
		footTop = col("surface-2")
	}
	footBg := canvas.NewVerticalGradient(footTop, col("surface-2"))
	foot := container.NewBorder(footLine, nil, nil, nil,
		container.NewStack(footBg, container.New(layout.NewCustomPaddedLayout(8, 8, 12, 12), container.NewHBox(buttons...))))
	content := container.NewStack(canvas.NewRectangle(col("surface")), container.New(layout.NewCustomPaddedLayout(14, 14, 16, 16), body))
	inner := container.NewBorder(container.NewVBox(head, stripe), foot, nil, nil, content)
	// 1 px dark outline: the box stands out from the dimmed background; a theme without depth (High contrast) has
	// no shadow to do that, so it draws the web's dialog frame (--color-dialog-line, 2 px)
	frame, w := col("dialog-head"), float32(1)
	if look.p.depthless {
		frame, w = col("dialog-line"), 2
	}
	card := container.NewStack(canvas.NewRectangle(frame),
		container.New(layout.NewCustomPaddedLayout(w, w, w, w), container.NewStack(canvas.NewRectangle(col("surface")), inner)))
	pop = widget.NewModalPopUp(card, u.w.Canvas())
	pop.Resize(fyne.NewSize(width, card.MinSize().Height))
	// as the web: the top at 10% of the window height, centered across; Fyne does not move a popup shown at a
	// position again, so watchResize keeps it there when the window resizes
	at := sheetPos(u.w.Canvas().Size(), pop.Size())
	if look.p.segmented { // Cristal: a sheet sliding down into place, as on the web (--dialog-rise: -24px)
		pop.ShowAtPosition(fyne.NewPos(at.X, max(0, at.Y-24)))
		slide := fyne.NewAnimation(150*time.Millisecond, func(f float32) {
			// the place of this frame's window: a resize meanwhile ends the slide where watchResize puts it
			to := sheetPos(u.w.Canvas().Size(), pop.Size())
			pop.Move(fyne.NewPos(to.X, max(0, to.Y-24)+(to.Y-max(0, to.Y-24))*f))
		})
		slide.Curve = fyne.AnimationEaseOut
		slide.Start()
	} else {
		pop.ShowAtPosition(at)
	}
	u.sheets = append(u.sheets, sheet{pop})
	return closeModal
}

// sheet is an open dialog.
type sheet struct{ pop *widget.PopUp }

// sheetPos is where a dialog of size d opens in a window of size: its top at 10% of the height (higher when it
// would not fit), centered across. x stays > 0: (0, 0) means "center" to Fyne.
func sheetPos(size, d fyne.Size) fyne.Position {
	return fyne.NewPos(max(1, (size.Width-d.Width)/2), max(0, min(size.Height*0.1, size.Height-d.Height)))
}

// watchResize stacks its objects (as container.NewStack) and moves the open dialogs back to 10% of the height,
// centered, when the window is resized.
type watchResize struct{ u *ui }

func (r watchResize) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	layout.NewStackLayout().Layout(objs, size)
	live := r.u.sheets[:0]
	for _, sh := range r.u.sheets {
		if sh.pop.Visible() {
			sh.pop.Move(sheetPos(r.u.w.Canvas().Size(), sh.pop.Size()))
			live = append(live, sh)
		}
	}
	r.u.sheets = live
}

func (watchResize) MinSize(objs []fyne.CanvasObject) fyne.Size {
	return layout.NewStackLayout().MinSize(objs)
}

// wrappedLabel returns a label that word-wraps long text.
func wrappedLabel(s string) *widget.Label {
	l := widget.NewLabel(s)
	l.Wrapping = fyne.TextWrapWord
	return l
}

// showInfo shows a message with a single OK button.
func (u *ui) showInfo(title, msg string) {
	u.showModal(title, wrappedLabel(msg), "OK", false, 460, nil)
}

// showError shows err in a modal with a red stripe.
func (u *ui) showError(err error) {
	ic := widget.NewIcon(theme.ErrorIcon())
	u.showModal(t("Could not complete"), container.NewBorder(nil, nil, ic, nil, wrappedLabel(err.Error())), "OK", true, 480, nil)
}

// confirm asks before running action; danger paints the dialog red.
func (u *ui) confirm(title, msg, button string, danger bool, action func()) {
	u.showModal(title, wrappedLabel(msg), button, danger, 480, func() bool { action(); return true })
}

// confirmTyped guards a destructive action: it only runs once the user types
// expected (case-insensitive), as in the web panel.
func (u *ui) confirmTyped(title, msg, expected string, action func()) {
	e := newEntry()
	e.SetPlaceHolder(expected)
	warning := canvas.NewText("", errText)
	warning.TextSize = 12
	body := container.NewVBox(wrappedLabel(msg), newText(t("Type ")+expected+t(" to confirm:"), strongText, 12, true), e, warning)
	u.showModal(title, body, t("Confirm"), true, 500, func() bool {
		if !strings.EqualFold(strings.TrimSpace(e.Text), expected) {
			warning.Text = t("The text does not match.")
			warning.Refresh()
			return false
		}
		action()
		return true
	})
}

// ── sidebar with depth ──────────────────────────────────────────────────────

// navItem is a sidebar button, the desktop's top tabs: the theme's tab colors (tab, hover and current, with the
// mark of --shadow-tab-on on the left); on Cristal a Finder list item.
type navItem struct {
	widget.BaseWidget
	label  string
	icon   fyne.Resource
	active bool
	hover  bool
	onTap  func()
	bg     *canvas.LinearGradient
	bar    *canvas.Rectangle
	ic     *widget.Icon
	text   *canvas.Text
	edges  [8]*canvas.Rectangle // a theme without a bar in --shadow-tab-on (the 98): its relief on the current item
	relief []boxShadow
}

// insetLayout lays out the rectangles as the inset layers of the current item (layoutInset).
type insetLayout struct{ i *navItem }

func (insetLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.Size{} }
func (l insetLayout) Layout(_ []fyne.CanvasObject, s fyne.Size) {
	layoutInset(l.i.edges[:], l.i.relief, 0, s)
}

func newNavItem(label string, icon fyne.Resource, onTap func()) *navItem {
	i := &navItem{label: label, icon: icon, onTap: onTap}
	i.ExtendBaseWidget(i)
	return i
}

func (i *navItem) CreateRenderer() fyne.WidgetRenderer {
	i.bg = canvas.NewVerticalGradient(color.Transparent, color.Transparent)
	i.bar = canvas.NewRectangle(color.Transparent)
	i.bar.SetMinSize(fyne.NewSize(3, 0)) // as the web's 3 px inset mark
	i.ic = widget.NewIcon(nil)
	i.text = newText(i.label, col("on-tab"), 13, true)
	content := container.NewBorder(nil, nil, i.bar, nil, container.NewPadded(container.NewHBox(i.ic, container.NewCenter(i.text))))
	objs := make([]fyne.CanvasObject, len(i.edges))
	for k := range i.edges {
		i.edges[k] = canvas.NewRectangle(color.Transparent)
		objs[k] = i.edges[k]
	}
	edges := container.New(insetLayout{i}, objs...)
	i.updateColors()
	return widget.NewSimpleRenderer(container.NewStack(i.bg, edges, content))
}

// tabMark is the color of the current top tab's mark: its --shadow-tab-on band along a side (none: no mark).
func tabMark() color.Color {
	for _, l := range look.p.tabOn {
		if l.dx == 0 && l.dy != 0 {
			return l.c
		}
	}
	return color.Transparent
}

// updateColors repaints the item for the active/hover state.
func (i *navItem) updateColors() {
	if i.bg == nil {
		return
	}
	top, base := color.Color(color.Transparent), color.Color(color.Transparent)
	var relief []boxShadow
	ink, name, mark := col("on-tab"), colorOnTab, color.Color(color.Transparent)
	if !look.p.segmented { // an item at rest is the web's inactive top tab (the strip shows through where it is clear)
		top, base = col("tab"), col("tab")
	}
	switch {
	case look.p.segmented && i.active: // Finder: the current item in the theme's nav-on, with the focus color mark
		top, base, mark = col("nav-on"), col("nav-on"), col("focus") // (the tint alone is ~1.3:1; the mark: >= 3:1)
		ink, name = col("fg"), theme.ColorNameForeground
	case look.p.segmented:
		ink, name = col("fg"), theme.ColorNameForeground
		if i.hover {
			top, base = col("row-hover"), col("row-hover")
		}
	case i.active:
		top, base = pair("tab-on")
		ink, name, mark = col("on-tab-on"), colorTabOnIcon, tabMark() // the icon in --color-tab-on-icon, as the web
		if mark == color.Transparent {                                // no bar to turn into a side mark: the tab's own relief (the 98)
			relief = look.p.tabOn
		}
	case i.hover:
		top, base = col("tab-hover"), col("tab-hover")
		ink, name = col("on-tab-hover"), colorOnTabHover
	}
	i.bg.StartColor, i.bg.EndColor, i.bar.FillColor, i.text.Color = top, base, mark, ink
	i.relief = relief
	layoutInset(i.edges[:], relief, 0, i.Size())
	i.ic.SetResource(theme.NewColoredResource(i.icon, name))
	i.bg.Refresh()
	i.bar.Refresh()
	i.text.Refresh()
}

func (i *navItem) Tapped(*fyne.PointEvent)        { i.onTap() }
func (i *navItem) MouseIn(*desktop.MouseEvent)    { i.hover = true; i.updateColors() }
func (i *navItem) MouseMoved(*desktop.MouseEvent) {}
func (i *navItem) MouseOut()                      { i.hover = false; i.updateColors() }
func (i *navItem) Cursor() desktop.Cursor         { return desktop.PointerCursor }

// page is one sidebar entry and the view it shows.
type page struct {
	label string
	icon  fyne.Resource
	view  fyne.CanvasObject
}

// navigation builds the sidebar (the theme's top tabs colors) and the content area, showing the first page
// initially; show switches to page k (links inside the pages).
func navigation(pages []page) (view fyne.CanvasObject, show func(k int)) {
	area := container.NewStack()
	items := make([]*navItem, len(pages))
	show = func(k int) {
		for j, p := range pages {
			items[j].active = j == k
			items[j].updateColors()
			if j == k {
				p.view.Show()
			} else {
				p.view.Hide()
			}
		}
		area.Refresh()
	}
	list := container.NewVBox()
	for k, p := range pages {
		items[k] = newNavItem(p.label, p.icon, func() { show(k) })
		list.Add(items[k])
		area.Add(p.view)
	}
	top, base := pair("tabs") // the web's top tabs strip
	if look.p.segmented {     // Finder: a light sidebar in the page color
		top, base = col("page"), col("page")
	}
	sidebar := container.NewStack(canvas.NewVerticalGradient(top, base), container.NewPadded(list))
	shadow := canvas.NewHorizontalGradient(color.NRGBA{0, 0, 0, 0x30}, color.Transparent) // sidebar shadow over the content
	shadow.SetMinSize(fyne.NewSize(6, 0))
	shadow.Hidden = look.flat
	show(0)
	return container.NewBorder(nil, nil, container.NewHBox(fixedWidth(sidebar, 200)), nil,
		container.NewStack(canvas.NewRectangle(col("surface-2")), container.NewBorder(nil, nil, shadow, nil, area))), show
}

// fixedWidth gives o a minimum width of w.
func fixedWidth(o fyne.CanvasObject, w float32) fyne.CanvasObject {
	r := canvas.NewRectangle(color.Transparent)
	r.SetMinSize(fyne.NewSize(w, 0))
	return container.NewStack(r, o)
}
