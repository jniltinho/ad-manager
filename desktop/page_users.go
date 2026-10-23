package main

import (
	"crypto/rand"
	"errors"
	"image/color"
	"math/big"
	"net/http"
	"slices"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ── Users ───────────────────────────────────────────────────────────────────

// userDetail is a user with the extra fields the list and the dialogs need
// (GET /api/users and GET /api/users/:sam).
type userDetail struct {
	user
	DN, UPN, SID, Surname, Phone, Title, Description string
	GivenName                                        string `json:"given_name"`
	Groups                                           []string
	PwdLastSet                                       time.Time `json:"pwd_last_set"`
	Created                                          time.Time
}

// userFields are the editable attributes of a user (ad.UserInput without the
// login), in form order: the first four go in the left column.
var userFields = []struct{ key, label string }{
	{"name", "Display name"}, {"given_name", "First name"}, {"surname", "Last name"}, {"mail", "E-mail"},
	{"phone", "Phone"}, {"title", "Job title"}, {"department", "Department"}, {"description", "Description"},
}

// userStatus returns the status label of x and its color.
func userStatus(x user) (string, color.Color) {
	switch {
	case !x.Enabled:
		return strings.ToUpper(t("deactivated")), mutedText
	case x.Locked:
		return strings.ToUpper(t("locked")), errText
	}
	return strings.ToUpper(t("active")), okText
}

// inactive reports an enabled account without logon for more than days days
// (never logged on counts as inactive), as in the web panel.
func inactive(x user, days int) bool {
	return x.Enabled && (x.LastLogon.IsZero() || int(time.Since(x.LastLogon).Hours()/24) > days)
}

// dateOrDash formats d in the interface language ("—" when unset).
func dateOrDash(d time.Time) string {
	if d.IsZero() {
		return "—"
	}
	return d.Local().Format(dateLayout())
}

// enableIf enables or disables the buttons.
func enableIf(on bool, bs ...*volButton) {
	for _, b := range bs {
		if on {
			b.Enable()
		} else {
			b.Disable()
		}
	}
}

// usersContainer returns the DN of the default "Users" container ("" if absent).
func usersContainer(ous []orgUnit) string {
	for _, o := range ous {
		if o.Name == "Users" {
			return o.DN
		}
	}
	return ""
}

// groupNames returns the names of all groups, sorted.
func (u *ui) groupNames() ([]string, error) {
	var gs []group
	if err := u.panel.do(http.MethodGet, "/api/groups", nil, &gs); err != nil {
		return nil, err
	}
	names := make([]string, len(gs))
	for i, g := range gs {
		names[i] = g.Name
	}
	slices.SortFunc(names, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	return names, nil
}

func (u *ui) usersPage() fyne.CanvasObject {
	var all, visible []userDetail
	days := u.inactiveDays()
	search := newEntry()
	search.SetPlaceHolder(t("Filter by login, name, e-mail…"))
	search.ActionItem = widget.NewIcon(theme.SearchIcon())
	filters := []func(userDetail) bool{
		func(userDetail) bool { return true },
		func(x userDetail) bool { return x.Enabled },
		func(x userDetail) bool { return !x.Enabled },
		func(x userDetail) bool { return x.Locked },
		func(x userDetail) bool { return inactive(x.user, days) },
	}
	filter := newSelect([]string{t("All"), t("Enabled"), t("Disabled"), t("Locked"), tf("Inactive for %d+ days", days)}, nil)
	filter.SetSelectedIndex(0)
	count := newText("", mutedText, 12, false)
	var reload func()
	// editable: built-in accounts and the operator's own login are not disabled, moved or deleted here (the panel
	// refuses to disable or delete them anyway), as the web Users page
	editable := func(x userDetail) bool { return !x.Protected && !strings.EqualFold(x.Sam, u.me.User) }
	// the row actions, each on the account it is given (the row it was tapped on, never the selected one)
	act := func(x userDetail, verb, msg string) {
		u.bgDo(func() error { return u.panel.do(http.MethodPost, "/api/users/"+esc(x.Sam)+"/"+verb, nil, nil) }, func() {
			u.setStatus(tf(msg, x.Sam))
			reload()
		})
	}
	edit := func(x userDetail) { u.editUserDialog(x.Sam, reload) }
	password := func(x userDetail) { u.passwordDialog(x.user, reload) }
	copyUser := func(x userDetail) {
		var full userDetail // the list may lack the groups: read the template itself
		u.bgDo(func() error { return u.panel.do(http.MethodGet, "/api/users/"+esc(x.Sam), nil, &full) }, func() {
			u.newUserDialog(&full, reload)
		})
	}
	unlock := func(x userDetail) { act(x, "unlock", "%s unlocked.") }
	toggle := func(x userDetail) {
		if !x.Enabled {
			act(x, "enable", "%s enabled.")
			return
		}
		u.confirm(t("Disable user"), tf("Disable %s (%s)? The user will no longer be able to log on.", x.Name, x.Sam), t("Disable"), true,
			func() { act(x, "disable", "%s disabled.") })
	}
	move := func(x userDetail) { u.moveUserDialog(x, reload) }
	remove := func(x userDetail) {
		u.confirmTyped(t("Delete user"), tf("Delete the account %s? Prefer disabling it: deletion cannot be undone.", x.Name), x.Sam, func() {
			u.bgDo(func() error { return u.panel.do(http.MethodDelete, "/api/users/"+esc(x.Sam), nil, nil) }, func() {
				u.setStatus(tf("%s deleted.", x.Sam))
				reload()
			})
		})
	}
	// the columns: one per action at the end, as the web's (move and delete blank on a non-editable account)
	const colLogin, colStatus, colTag, colPassword, colCopy, colMove, colDelete = 0, 4, 5, 7, 8, 9, 10
	tab := newTable([]column{{t("Login"), 130, false}, {t("Name"), 140, false}, {t("E-mail"), 160, false}, {t("OU"), 100, false},
		{t("Status"), 105, true}, {"", 130, true}, {t("Last logon"), 140, false},
		{"", 105, false}, {"", 90, false}, {"", 85, false}, {"", 90, false}},
		func() int { return len(visible) },
		func(r, c int) (string, color.Color) {
			x := visible[r]
			switch c {
			case colLogin:
				return x.Sam, nil
			case 1:
				return x.Name, nil
			case 2:
				return x.Mail, nil
			case 3:
				return ouPath(parentDN(x.DN)), mutedText
			case colStatus:
				return userStatus(x.user)
			case colTag: // system and unlock are independent: a locked built-in account shows both
				if x.Protected {
					return t("system"), strongText
				}
				if x.Locked {
					return t("unlock"), nil
				}
				return "", nil
			case colPassword:
				return t("password"), nil
			case colCopy:
				return t("copy"), nil
			case colMove:
				if editable(x) {
					return t("move"), nil
				}
				return "", nil
			case colDelete:
				if editable(x) {
					return t("delete"), errText
				}
				return "", nil
			}
			switch {
			case x.LastLogon.IsZero():
				return t("never"), mutedText
			case inactive(x.user, days):
				return dateOrDash(x.LastLogon), warnText
			}
			return dateOrDash(x.LastLogon), nil
		}, nil)
	// compact (1280 px): the action links as icons, unlock too (its column keeps room for the system tag)
	tab.actions = map[int]float32{colTag: 104, colPassword: compactWidth, colCopy: compactWidth, colMove: compactWidth, colDelete: compactWidth}
	// the links of a cell, bound to the account drawn in it
	tab.link = func(r, c int) *cellLink {
		if r < 0 || r >= len(visible) {
			return nil
		}
		x := visible[r]
		on := func(f func(userDetail)) func() { return func() { f(x) } }
		switch {
		case c == colLogin:
			return &cellLink{hint: tf("Edit %s", x.Sam), run: on(edit)}
		case c == colStatus && editable(x):
			hint := t("Click to disable")
			if !x.Enabled {
				hint = t("Click to enable")
			}
			return &cellLink{hint: hint, run: on(toggle)}
		case c == colTag && x.Locked:
			return &cellLink{icon: theme.MailReplyIcon(), hint: tf("Unlock %s", x.Sam), run: on(unlock), tag: x.Protected}
		case c == colPassword:
			return &cellLink{icon: theme.VisibilityIcon(), hint: tf("Set a password for %s", x.Sam), run: on(password)}
		case c == colCopy:
			return &cellLink{icon: theme.ContentCopyIcon(), hint: tf("New user copied from %s", x.Sam), run: on(copyUser)}
		case c == colMove && editable(x):
			return &cellLink{icon: theme.FolderOpenIcon(), hint: tf("Move %s to another OU", x.Sam), run: on(move)}
		case c == colDelete && editable(x):
			return &cellLink{icon: theme.DeleteIcon(), hint: tf("Delete %s", x.Sam), run: on(remove)}
		}
		return nil
	}
	// a link's hint covers the status line while hovered; leaving it puts back what was there, unless a newer message
	// (an action's result) replaced the hint meanwhile
	saved, shown, hovering := "", "", false
	tab.hover = func(hint string) {
		if hint != "" {
			if !hovering || u.status.Text != shown {
				saved = u.status.Text
			}
			hovering, shown = true, hint
			u.setStatus(hint)
			return
		}
		if hovering && u.status.Text == shown {
			u.setStatus(saved)
		}
		hovering = false
	}
	// Enter on the selected row: a menu of its actions (the same ones, the same rules), for the keyboard
	tab.onEnter = func(r int) {
		if r < 0 || r >= len(visible) {
			return
		}
		x := visible[r]
		on := func(f func(userDetail)) func() { return func() { f(x) } }
		items := []*fyne.MenuItem{fyne.NewMenuItem(t("Edit"), on(edit)), fyne.NewMenuItem(t("Password"), on(password)),
			fyne.NewMenuItem(t("Copy"), on(copyUser))}
		if x.Locked {
			items = append(items, fyne.NewMenuItem(t("Unlock"), on(unlock)))
		}
		if editable(x) {
			toggleLabel := t("Disable")
			if !x.Enabled {
				toggleLabel = t("Enable")
			}
			items = append(items, fyne.NewMenuItem(toggleLabel, on(toggle)), fyne.NewMenuItem(t("Move"), on(move)),
				fyne.NewMenuItem(t("Delete"), on(remove)))
		}
		pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(tab)
		widget.ShowPopUpMenuAtPosition(fyne.NewMenu(x.Sam, items...), u.w.Canvas(), pos.Add(fyne.NewPos(24, 40)))
	}
	apply := func() {
		q := strings.ToLower(strings.TrimSpace(search.Text))
		keep := filters[max(filter.SelectedIndex(), 0)]
		visible = visible[:0]
		for _, x := range all {
			if keep(x) && (q == "" || strings.Contains(strings.ToLower(x.Sam+" "+x.Name+" "+x.Mail+" "+x.Department), q)) {
				visible = append(visible, x)
			}
		}
		tab.clearSelection()
		count.Text = tf("%d of %d", len(visible), len(all))
		count.Refresh()
	}
	reload = func() {
		go func() {
			var r []userDetail
			err := u.panel.do(http.MethodGet, "/api/users", nil, &r)
			onUI(func() {
				if err != nil {
					u.handleError(err)
					return
				}
				slices.SortFunc(r, func(a, b userDetail) int { return strings.Compare(strings.ToLower(a.Sam), strings.ToLower(b.Sam)) })
				all = r
				apply()
			})
		}()
	}
	search.OnChanged = func(string) { apply() }
	filter.OnChanged = func(string) { apply() }

	newBtn := newButton(t("New user"), theme.ContentAddIcon(), func() { u.newUserDialog(nil, reload) })
	newBtn.Importance = widget.HighImportance
	export := func() { u.downloadExport("users", t("users.csv")) }
	reload()
	// one line: search, filter and count, then the extras on the right (behind ⋯ when narrow), as the web's
	toolbar := newToolbarRow(search, container.NewHBox(filter, container.NewCenter(count)), container.NewHBox(),
		container.NewHBox(newButton(t("Password policy"), theme.SettingsIcon(), u.policyDialog),
			newButton(t("Export CSV"), theme.DownloadIcon(), export), newBtn, newButton("", theme.ViewRefreshIcon(), reload)),
		u.moreButton(fyne.NewMenuItem(t("New user"), func() { u.newUserDialog(nil, reload) }),
			fyne.NewMenuItem(t("Password policy"), u.policyDialog), fyne.NewMenuItem(t("Export CSV"), export),
			fyne.NewMenuItem(t("Refresh"), reload)),
		func() {
			if c := u.w.Content(); c != nil { // laid out again with the toolbar's new height
				c.Refresh()
			}
		})
	return container.NewPadded(card(t("Domain users"), container.NewBorder(toolbar, nil, nil, nil, tab.view())))
}

// userEntries builds one entry per userFields item, filled from x.
func userEntries(x userDetail) map[string]*ringEntry {
	vals := map[string]string{"name": x.Name, "given_name": x.GivenName, "surname": x.Surname, "mail": x.Mail,
		"phone": x.Phone, "title": x.Title, "department": x.Department, "description": x.Description}
	e := map[string]*ringEntry{}
	for _, f := range userFields {
		e[f.key] = newEntry()
		e[f.key].SetText(vals[f.key])
	}
	return e
}

// userFieldsForm lays the entries out in two columns.
func userFieldsForm(e map[string]*ringEntry) fyne.CanvasObject {
	left, right := widget.NewForm(), widget.NewForm()
	for i, f := range userFields {
		item := widget.NewFormItem(t(f.label), e[f.key])
		if i < 4 {
			left.AppendItem(item)
		} else {
			right.AppendItem(item)
		}
	}
	return container.NewGridWithColumns(2, left, right)
}

// userInput is the JSON body of the entries (ad.UserInput without the login).
func userInput(e map[string]*ringEntry) map[string]any {
	m := map[string]any{}
	for k, v := range e {
		m[k] = strings.TrimSpace(v.Text)
	}
	return m
}

// groupList edits a list of groups: one row per group with a remove button and
// a picker to add one. add/remove run first and an error keeps the list as it
// is (the edit dialog calls the API, the new-user dialog only collects).
// chosen returns the current list.
func (u *ui) groupList(names, groups []string, add, remove func(g string) error) (view fyne.CanvasObject, chosen func() []string) {
	groups = slices.Clone(groups)
	slices.Sort(groups)
	rows := container.NewVBox()
	picker := newSelectEntry(nil)
	picker.SetPlaceHolder(t("Add to a group…"))
	var render func()
	render = func() {
		rows.RemoveAll()
		for _, g := range groups {
			rm := newButton(t("remove"), theme.DeleteIcon(), func() {
				if err := remove(g); err != nil {
					u.handleError(err)
					return
				}
				groups = slices.DeleteFunc(groups, func(x string) bool { return x == g })
				render()
			})
			rm.Importance = widget.LowImportance
			rows.Add(container.NewBorder(nil, nil, nil, rm, widget.NewLabel(g)))
		}
		if len(groups) == 0 {
			rows.Add(widget.NewLabel(t("Only the primary group (Domain Users).")))
		}
		picker.SetOptions(slices.DeleteFunc(slices.Clone(names), func(n string) bool { return slices.Contains(groups, n) }))
	}
	addBtn := newButton(t("Add"), theme.ContentAddIcon(), func() {
		g := strings.TrimSpace(picker.Text)
		if g == "" || slices.Contains(groups, g) {
			return
		}
		if err := add(g); err != nil {
			u.handleError(err)
			return
		}
		groups = append(groups, g)
		slices.Sort(groups)
		picker.SetText("")
		render()
	})
	render()
	scroll := container.NewVScroll(ringRoom(rows))
	scroll.SetMinSize(fyne.NewSize(0, 130))
	return container.NewBorder(container.NewBorder(nil, nil, nil, addBtn, picker), nil, nil, nil, scroll),
		func() []string { return groups }
}

// newUserDialog creates a user; with tpl it is a copy of that user (OU, job
// title, department, description and groups). after runs on success.
func (u *ui) newUserDialog(tpl *userDetail, after func()) {
	ous, err := u.fetchOUs()
	if err != nil {
		u.handleError(err)
		return
	}
	names, err := u.groupNames()
	if err != nil {
		u.handleError(err)
		return
	}
	title, base, ou, groups := t("New user"), userDetail{}, u.me.Settings.DefaultOU, []string(nil)
	if ou == "" {
		ou = usersContainer(ous)
	}
	if tpl != nil {
		title = tf("New user from %s", tpl.Sam)
		base.Title, base.Department, base.Description = tpl.Title, tpl.Department, tpl.Description
		ou = parentDN(tpl.DN)
		groups = slices.DeleteFunc(slices.Clone(tpl.Groups), func(g string) bool { return strings.EqualFold(g, "Domain Users") })
	}
	sam := newEntry()
	e := userEntries(base)
	ouSel, ouDN := ouSelect(ous, ou)
	pw, pwRow := u.passwordField()
	mustChange := newCheck(t("Require change at next logon"), nil)
	mustChange.SetChecked(true)
	none := func(string) error { return nil }
	groupBox, chosen := u.groupList(names, groups, none, none)
	body := container.NewVBox(
		widget.NewForm(widget.NewFormItem(t("Login (sAMAccountName)"), sam), widget.NewFormItem(t("OU"), ouSel)),
		userFieldsForm(e),
		widget.NewForm(widget.NewFormItem(t("Initial password"), pwRow), widget.NewFormItem("", mustChange)),
		newText(t("Groups"), strongText, 12, true), groupBox)
	u.showModal(title, body, t("Create"), false, 780, func() bool {
		in := userInput(e)
		in["sam"], in["ou"], in["password"], in["must_change"] = strings.TrimSpace(sam.Text), ouDN(), pw.Text, mustChange.Checked
		var created user
		if err := u.panel.do(http.MethodPost, "/api/users", in, &created); err != nil {
			u.handleError(err)
			return false
		}
		var failed []string
		for _, g := range chosen() {
			if err := u.panel.do(http.MethodPost, "/api/groups/"+esc(g)+"/members", map[string]string{"member": created.Sam}, nil); err != nil {
				failed = append(failed, g+": "+err.Error())
			}
		}
		u.setStatus(tf("User %s created.", created.Sam))
		after()
		if len(failed) > 0 {
			u.showError(errors.New(strings.Join(failed, "\n")))
		}
		return true
	})
	u.w.Canvas().Focus(sam)
}

// editUserDialog edits the details of sam (Account tab) and its group
// memberships (Groups tab, applied right away as in the web panel).
func (u *ui) editUserDialog(sam string, after func()) {
	var x userDetail
	if err := u.panel.do(http.MethodGet, "/api/users/"+esc(sam), nil, &x); err != nil {
		u.handleError(err)
		return
	}
	names, err := u.groupNames()
	if err != nil {
		u.handleError(err)
		return
	}
	e := userEntries(x)
	small := func(s string) *widget.Label {
		l := widget.NewLabel(s)
		l.SizeName = theme.SizeNameCaptionText
		return l
	}
	ids := small(x.DN + " · " + x.SID)
	ids.Wrapping = fyne.TextWrapBreak
	account := container.NewVBox(
		widget.NewForm(widget.NewFormItem(t("Login"), widget.NewLabel(x.Sam+" · "+x.UPN))),
		userFieldsForm(e), widget.NewSeparator(),
		container.NewGridWithColumns(3, small(tf("Created: %s", dateOrDash(x.Created))),
			small(tf("Last logon: %s", dateOrDash(x.LastLogon))), small(tf("Password changed: %s", dateOrDash(x.PwdLastSet)))),
		ids)
	member := func(g string, add bool) error {
		var err error
		if add {
			err = u.panel.do(http.MethodPost, "/api/groups/"+esc(g)+"/members", map[string]string{"member": x.Sam}, nil)
		} else {
			err = u.panel.do(http.MethodDelete, "/api/groups/"+esc(g)+"/members/"+esc(x.Sam), nil, nil)
		}
		if err == nil {
			u.setStatus(tf(map[bool]string{true: "Added to %s.", false: "Removed from %s."}[add], g))
		}
		return err
	}
	groupBox, _ := u.groupList(names, x.Groups, func(g string) error { return member(g, true) }, func(g string) error { return member(g, false) })
	tabs := newTabs(tabItem{t("Account"), theme.AccountIcon(), account}, tabItem{t("Groups"), theme.ListIcon(), groupBox})
	u.showModal(x.Name+" ("+x.Sam+")", tabs.View(), t("Save"), false, 780, func() bool {
		if err := u.panel.do(http.MethodPut, "/api/users/"+esc(x.Sam), userInput(e), nil); err != nil {
			u.handleError(err)
			return false
		}
		u.setStatus(t("Details saved."))
		after()
		return true
	})
}

// moveUserDialog moves x to another OU.
func (u *ui) moveUserDialog(x userDetail, after func()) {
	ous, err := u.fetchOUs()
	if err != nil {
		u.handleError(err)
		return
	}
	dest, dn := ouSelect(ous, parentDN(x.DN))
	u.showModal(tf("Move %s", x.Sam), widget.NewForm(widget.NewFormItem(t("Destination"), dest)), t("Move"), false, 560, func() bool {
		if dn() == "" {
			return false
		}
		if err := u.panel.do(http.MethodPost, "/api/users/"+esc(x.Sam)+"/move", map[string]string{"ou": dn()}, nil); err != nil {
			u.handleError(err)
			return false
		}
		u.setStatus(t("User moved."))
		after()
		return true
	})
}

// passwordField is a monospace entry with a generated password plus buttons
// to generate another one and to copy it.
func (u *ui) passwordField() (*ringEntry, fyne.CanvasObject) {
	pw := newEntry()
	pw.TextStyle = fyne.TextStyle{Monospace: true}
	pw.SetText(u.newPassword())
	gen := newButton("", theme.ViewRefreshIcon(), func() { pw.SetText(u.newPassword()) })
	cp := newButton("", theme.ContentCopyIcon(), func() {
		u.a.Clipboard().SetContent(pw.Text)
		u.setStatus(t("Password copied."))
	})
	return pw, container.NewBorder(nil, nil, nil, container.NewHBox(gen, cp), pw)
}

// passwordDialog resets the password of x, ADUC style: generator, copy,
// change at next logon and optional unlock. The second tab generates a
// single-use link for the user to choose the new password. after runs on
// success.
func (u *ui) passwordDialog(x user, after func()) {
	pw, pwRow := u.passwordField()
	mustChange := newCheck(t("Require change at next logon"), nil)
	mustChange.SetChecked(true)
	unlock := newCheck(t("Unlock the account too"), nil)
	unlock.SetChecked(x.Locked)
	set := widget.NewForm(widget.NewFormItem(t("New password"), pwRow), widget.NewFormItem("", mustChange), widget.NewFormItem("", unlock))

	validity := []struct {
		label string
		hours int
	}{{t("1 hour"), 1}, {t("24 hours"), 24}, {t("3 days"), 72}, {t("7 days"), 168}}
	labels := make([]string, len(validity))
	for i, v := range validity {
		labels[i] = v.label
	}
	hours := newSelect(labels, nil)
	hours.SetSelectedIndex(1)
	link := container.NewVBox(widget.NewForm(widget.NewFormItem(t("Valid for"), hours)),
		wrappedLabel(t("The current password stops working now and the account is unlocked. The user sets the new one through the link.")+" "+
			t("Only the first browser that opens the link can use it, within 30 minutes; it works once.")))

	tabs := newTabs(tabItem{t("Set password"), theme.AccountIcon(), set}, tabItem{t("Send link to the user"), theme.MailSendIcon(), link})
	u.showModal(t("Reset password")+" — "+x.Name+" ("+x.Sam+")", tabs.View(), t("OK"), false, 600, func() bool {
		if tabs.SelectedIndex() == 1 {
			var r struct {
				Token   string    `json:"token"`
				Expires time.Time `json:"expires"`
			}
			if err := u.panel.do(http.MethodPost, "/api/users/"+esc(x.Sam)+"/password-link", map[string]any{"hours": validity[hours.SelectedIndex()].hours}, &r); err != nil {
				u.handleError(err)
				return false
			}
			// after this modal closes: hiding an overlay also removes the ones above it
			go onUI(func() { u.showLink(x, strings.TrimRight(u.panel.base, "/")+"/password/"+r.Token, r.Expires) })
			after()
			return true
		}
		err := u.panel.do(http.MethodPost, "/api/users/"+esc(x.Sam)+"/password", map[string]any{"password": pw.Text, "must_change": mustChange.Checked}, nil)
		if err == nil && unlock.Checked {
			err = u.panel.do(http.MethodPost, "/api/users/"+esc(x.Sam)+"/unlock", nil, nil)
		}
		if err != nil {
			u.handleError(err)
			return false
		}
		u.setStatus(tf("Password of %s reset.", x.Sam))
		after()
		return true
	})
}

// showLink shows a generated password link with a copy button.
func (u *ui) showLink(x user, url string, expires time.Time) {
	e := newEntry()
	e.TextStyle = fyne.TextStyle{Monospace: true}
	e.SetText(url)
	cp := newButton(t("copy"), theme.ContentCopyIcon(), func() {
		u.a.Clipboard().SetContent(url)
		u.setStatus(t("Link copied."))
	})
	body := container.NewVBox(container.NewBorder(nil, nil, nil, cp, e),
		wrappedLabel(tf("Valid until %s. Send it to the user through a trusted channel.", expires.Local().Format(dateLayout()))))
	u.showModal(tf("Link for %s", x.Sam), body, t("Close"), false, 640, nil)
	u.setStatus(tf("Password link for %s generated.", x.Sam))
}

// newPassword generates a password of the configured length (14 when unset).
func (u *ui) newPassword() string {
	n := u.me.Settings.PasswordLength
	if n <= 0 {
		n = 14
	}
	return generatePassword(n)
}

// generatePassword returns n random characters (at least one of each class:
// upper, lower, digit, symbol), which satisfies AD password complexity.
func generatePassword(n int) string {
	classes := []string{"ABCDEFGHJKLMNPQRSTUVWXYZ", "abcdefghijkmnopqrstuvwxyz", "23456789", "@#%+=!?"}
	all := strings.Join(classes, "")
	rnd := func(upper int) int {
		v, _ := rand.Int(rand.Reader, big.NewInt(int64(upper)))
		return int(v.Int64())
	}
	b := make([]byte, 0, max(n, len(classes)))
	for _, c := range classes {
		b = append(b, c[rnd(len(c))])
	}
	for len(b) < n {
		b = append(b, all[rnd(len(all))])
	}
	for i := len(b) - 1; i > 0; i-- {
		j := rnd(i + 1)
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}
