package main

import (
	"image/color"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ── Groups ──────────────────────────────────────────────────────────────────

func (u *ui) groupsPage() fyne.CanvasObject {
	var gs []group
	var members []string
	gsel, msel := -1, -1
	kind := map[string]string{"security": t("Security"), "distribution": t("Distribution")}
	title := newText(t("Choose a group"), strongText, 13, true)
	var gtab, mtab *gridTable
	var descBtn, delBtn, addBtn, remBtn *volButton
	current := func() (group, bool) {
		if gsel < 0 || gsel >= len(gs) {
			return group{}, false
		}
		return gs[gsel], true
	}
	updateButtons := func() {
		g, ok := current()
		enableIf(ok, descBtn, addBtn)
		enableIf(ok && !g.Protected, delBtn) // built-in groups cannot be deleted (the server refuses too)
		enableIf(ok && msel >= 0 && msel < len(members), remBtn)
	}
	// open shows the members of the selected group and refreshes its row.
	open := func() {
		g, ok := current()
		if !ok {
			return
		}
		var d group
		if err := u.panel.do(http.MethodGet, "/api/groups/"+esc(g.Name), nil, &d); err != nil {
			u.handleError(err)
			return
		}
		members, msel = d.Members, -1
		slices.Sort(members)
		gs[gsel].MemberCount, gs[gsel].Description = len(members), d.Description
		title.Text = tf("%s — %d member(s)", d.Name, len(members))
		title.Refresh()
		gtab.Refresh()
		mtab.clearSelection()
		updateButtons()
	}
	gtab = newTable([]column{{t("Group"), 200, false}, {"", 75, true}, {t("Group type"), 100, false}, {t("Members"), 75, false}, {t("Description"), 160, false}},
		func() int { return len(gs) },
		func(r, c int) (string, color.Color) {
			g := gs[r]
			switch c {
			case 0:
				return g.Name, strongText
			case 1:
				if g.Protected {
					return t("system"), strongText
				}
				return "", nil
			case 2:
				return kind[g.Type], nil
			case 3:
				return strconv.Itoa(g.MemberCount), nil
			}
			return g.Description, mutedText
		}, func(r int) { gsel = r; open() })
	mtab = newTable([]column{{t("Member (login)"), 320, false}},
		func() int { return len(members) },
		func(r, _ int) (string, color.Color) { return members[r], nil },
		func(r int) { msel = r; updateButtons() })
	reload := func() {
		go func() {
			var r []group
			err := u.panel.do(http.MethodGet, "/api/groups", nil, &r)
			fyne.Do(func() {
				if err != nil {
					u.handleError(err)
					return
				}
				slices.SortFunc(r, func(a, b group) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
				gs, gsel, members, msel = r, -1, nil, -1
				title.Text = t("Choose a group")
				title.Refresh()
				gtab.clearSelection()
				mtab.clearSelection()
				updateButtons()
			})
		}()
	}

	newBtn := newButton(t("New group"), theme.ContentAddIcon(), func() { u.newGroupDialog(reload) })
	newBtn.Importance = widget.HighImportance
	descBtn = newButton(t("Edit description"), theme.DocumentCreateIcon(), func() {
		g, ok := current()
		if !ok {
			return
		}
		desc := newEntry()
		desc.SetText(g.Description)
		row := gsel
		u.showModal(tf("Group %s", g.Name), widget.NewForm(widget.NewFormItem(t("Description"), desc)), t("Save description"), false, 560, func() bool {
			if err := u.panel.do(http.MethodPut, "/api/groups/"+esc(g.Name), map[string]string{"description": desc.Text}, nil); err != nil {
				u.handleError(err)
				return false
			}
			if row < len(gs) && gs[row].Name == g.Name {
				gs[row].Description = desc.Text
				gtab.Refresh()
			}
			u.setStatus(t("Description saved."))
			return true
		})
	})
	delBtn = newButton(t("Delete"), theme.DeleteIcon(), func() {
		g, ok := current()
		if !ok || g.Protected {
			return
		}
		u.confirmTyped(t("Delete group"), tf("Delete the group %s? Its members lose the permissions it grants.", g.Name), g.Name, func() {
			u.bgDo(func() error { return u.panel.do(http.MethodDelete, "/api/groups/"+esc(g.Name), nil, nil) }, func() {
				u.setStatus(tf("Group %s deleted.", g.Name))
				reload()
			})
		})
	})
	delBtn.Importance = widget.DangerImportance

	newMember := newEntry()
	newMember.SetPlaceHolder(t("New member's login (computers end with $)"))
	addBtn = newButton(t("Add"), theme.ContentAddIcon(), func() {
		g, ok := current()
		m := strings.TrimSpace(newMember.Text)
		if !ok || m == "" {
			return
		}
		if err := u.panel.do(http.MethodPost, "/api/groups/"+esc(g.Name)+"/members", map[string]string{"member": m}, nil); err != nil {
			u.handleError(err)
			return
		}
		u.setStatus(tf("%s added.", m))
		newMember.SetText("")
		open()
	})
	addBtn.Importance = widget.HighImportance
	newMember.OnSubmitted = func(string) { addBtn.OnTapped() }
	remBtn = newButton(t("Remove"), theme.ContentRemoveIcon(), func() {
		g, ok := current()
		if !ok || msel < 0 || msel >= len(members) {
			return
		}
		m := members[msel]
		u.confirm(t("Remove member"), tf("Remove %s from the group %s?", m, g.Name), t("Remove"), true, func() {
			if err := u.panel.do(http.MethodDelete, "/api/groups/"+esc(g.Name)+"/members/"+esc(m), nil, nil); err != nil {
				u.handleError(err)
				return
			}
			u.setStatus(tf("%s removed.", m))
			open()
		})
	})
	remBtn.Importance = widget.DangerImportance
	updateButtons()
	reload()

	toolbar := container.NewHBox(newBtn, descBtn, delBtn, newButton("", theme.ViewRefreshIcon(), reload))
	left := card(t("Groups"), container.NewBorder(toolbar, nil, nil, nil, gtab.view()))
	right := card(t("Members"), container.NewBorder(container.NewPadded(title),
		container.NewBorder(nil, nil, nil, container.NewHBox(addBtn, remBtn), newMember), nil, nil, mtab.view()))
	split := container.NewHSplit(left, right)
	split.Offset = 0.62
	return container.NewPadded(split)
}

// newGroupDialog creates a group (name, description, type, scope and OU).
// after runs on success.
func (u *ui) newGroupDialog(after func()) {
	ous, err := u.fetchOUs()
	if err != nil {
		u.handleError(err)
		return
	}
	name, desc := newEntry(), newEntry()
	types, scopes := []string{"security", "distribution"}, []string{"global", "domainlocal", "universal"}
	typeSel := newSelect([]string{t("Security"), t("Distribution")}, nil)
	typeSel.SetSelectedIndex(0)
	scopeSel := newSelect([]string{t("Global"), t("Domain local"), t("Universal")}, nil)
	scopeSel.SetSelectedIndex(0)
	ouSel, ouDN := ouSelect(ous, usersContainer(ous))
	form := widget.NewForm(widget.NewFormItem(t("Name"), name), widget.NewFormItem(t("Description"), desc),
		widget.NewFormItem(t("Group type"), typeSel), widget.NewFormItem(t("Scope"), scopeSel), widget.NewFormItem(t("OU"), ouSel))
	u.showModal(t("New group"), form, t("Create"), false, 600, func() bool {
		in := map[string]string{"name": strings.TrimSpace(name.Text), "description": desc.Text,
			"type": types[max(typeSel.SelectedIndex(), 0)], "scope": scopes[max(scopeSel.SelectedIndex(), 0)], "ou": ouDN()}
		if err := u.panel.do(http.MethodPost, "/api/groups", in, nil); err != nil {
			u.handleError(err)
			return false
		}
		u.setStatus(tf("Group %s created.", in["name"]))
		after()
		return true
	})
	u.w.Canvas().Focus(name)
}
