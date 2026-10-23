package main

import (
	"image/color"
	"net/http"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ── Home: stat tiles, alerts and the quick password reset ───────────────────

func (u *ui) homePage() fyne.CanvasObject {
	num := func(c color.Color) *canvas.Text { return newText("…", c, 34, true) }
	// as the web: locked and inactive in the muted color while there are none
	enabled, disabled, locked, idle, groups, computers := num(okText), num(mutedText), num(mutedText), num(mutedText), num(strongText), num(strongText)
	var lockedStripe, idleStripe *canvas.Rectangle // recolored when the numbers arrive
	days := u.inactiveDays()
	page := container.NewVBox() // relaid out when the alerts arrive (their card grows)
	// alerts as rows like the web: a colored tag, the text (wraps) and a link to the page that solves it
	alerts := container.NewVBox(wrappedLabel(t("Loading…")))
	// open goes where the alert is solved: Users (page 1), and for the policy its dialog over Users, as the web
	alertRow := func(label string, state color.Color, text, link string, open func()) fyne.CanvasObject {
		var action fyne.CanvasObject = layout.NewSpacer()
		if link != "" {
			b := newButton(link, nil, open)
			b.Importance = widget.LowImportance
			action = container.NewCenter(b)
		}
		// one line each, cut with "…" (a wrapping label in a VBox misreports its height in Fyne)
		msg := widget.NewLabel(text)
		msg.Truncation = fyne.TextTruncateEllipsis
		return container.NewBorder(nil, nil, tag(label, state, 96), action, msg)
	}
	toUsers := func() {
		if u.goTo != nil {
			u.goTo(1)
		}
	}
	load := func() {
		go func() {
			var s struct {
				Users     map[string]int `json:"users"`
				Groups    int            `json:"groups"`
				Computers int            `json:"computers"`
				Policy    struct {
					Complexity bool `json:"complexity"`
					MinLength  int  `json:"min_length"`
				} `json:"policy"`
			}
			var users []user // the summary has no inactive count: computed here, as the web panel does
			err := u.panel.do(http.MethodGet, "/api/summary", nil, &s)
			if err == nil {
				err = u.panel.do(http.MethodGet, "/api/users", nil, &users)
			}
			fyne.Do(func() {
				if err != nil {
					u.handleError(err)
					return
				}
				var lockedUsers []string // names shown in the alert: the first three
				inactiveCount := 0
				for _, x := range users {
					if x.Locked {
						lockedUsers = append(lockedUsers, x.Name+" ("+x.Sam+")")
					}
					if inactive(x, days) {
						inactiveCount++
					}
				}
				for txt, v := range map[*canvas.Text]int{enabled: s.Users["enabled"], disabled: s.Users["disabled"], locked: s.Users["locked"],
					idle: inactiveCount, groups: s.Groups, computers: s.Computers} {
					txt.Text = strconv.Itoa(v)
					txt.Refresh()
				}
				// some locked or inactive: their number and stripe in the alert's color, as the web
				for _, a := range []struct {
					txt    *canvas.Text
					stripe *canvas.Rectangle
					n      int
					on     color.Color
					decor  string
				}{{locked, lockedStripe, s.Users["locked"], errText, "decor-err"}, {idle, idleStripe, inactiveCount, warnText, "decor-warn"}} {
					a.txt.Color, a.stripe.FillColor = mutedText, col("decor-muted")
					if a.n > 0 {
						a.txt.Color, a.stripe.FillColor = a.on, col(a.decor)
					}
					a.txt.Refresh()
					a.stripe.Refresh()
				}
				var rows []fyne.CanvasObject
				if !s.Policy.Complexity || s.Policy.MinLength < 8 {
					msg := "Password policy: minimum length of %d characters."
					if !s.Policy.Complexity {
						msg = "Password policy: minimum length of %d characters, no complexity."
					}
					rows = append(rows, alertRow(t("policy"), warnText, tf(msg, s.Policy.MinLength), t("see ›"), func() {
						if u.goTo != nil {
							u.goTo(1)
						}
						u.policyDialog()
					}))
				}
				if len(lockedUsers) > 0 {
					names := lockedUsers
					if len(names) > 3 {
						names = append(names[:3:3], "…")
					}
					rows = append(rows, alertRow(t("Locked"), errText,
						tf("%d locked account(s): %s.", len(lockedUsers), strings.Join(names, ", ")), t("unlock ›"), toUsers))
				}
				if inactiveCount > 0 {
					rows = append(rows, alertRow(t("inactive"), mutedText,
						tf("%d enabled account(s) with no logon for more than %d days.", inactiveCount, days), t("review ›"), toUsers))
				}
				if len(rows) == 0 {
					rows = append(rows, alertRow("ok", okText, t("Nothing pending."), "", nil))
				}
				alerts.Objects = rows
				alerts.Refresh()
				page.Refresh()
			})
		}()
	}
	login := newEntry()
	login.SetPlaceHolder(t("user login"))
	open := func() {
		var x user
		if err := u.panel.do(http.MethodGet, "/api/users/"+esc(strings.TrimSpace(login.Text)), nil, &x); err != nil {
			u.handleError(err)
			return
		}
		u.passwordDialog(x, func() { login.SetText(""); load() })
	}
	login.OnSubmitted = func(string) { open() }
	openBtn := newButton(t("Open"), theme.AccountIcon(), open)
	openBtn.Importance = widget.HighImportance
	hint := newText(t("Set a password, send a reset link or unlock the account."), mutedText, 11, false)
	quick := card(t("Reset password / unlock"), container.NewVBox(
		container.NewBorder(nil, nil, nil, container.NewCenter(openBtn), login), hint))
	tile := func(label string, v *canvas.Text, decor string) fyne.CanvasObject {
		o, _ := statTile(strings.ToUpper(label), v, decor)
		return o
	}
	lockedTile, lockedStripe0 := statTile(strings.ToUpper(t("Locked")), locked, "decor-muted")
	idleTile, idleStripe0 := statTile(strings.ToUpper(tf("Inactive %d+ days", days)), idle, "decor-muted")
	lockedStripe, idleStripe = lockedStripe0, idleStripe0
	load() // after the tiles: a quick answer recolors their stripes
	row := container.NewGridWithColumns(6, tile(t("Enabled users"), enabled, "decor-ok"), tile(t("Disabled"), disabled, "decor-muted"),
		lockedTile, idleTile, tile(t("Groups"), groups, "decor-strong"), tile(t("Computers"), computers, "decor-strong"))
	refresh := newButton("", theme.ViewRefreshIcon(), load)
	refresh.Importance = widget.LowImportance
	alertsCard := card(t("Alerts"), container.NewBorder(nil, nil, nil, container.NewVBox(refresh), alerts))
	page.Objects = []fyne.CanvasObject{row, container.NewGridWithColumns(2, quick, alertsCard)}
	return container.NewPadded(page)
}
