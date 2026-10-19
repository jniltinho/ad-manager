package main

import (
	"image/color"
	"net/http"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ── Organizational units ────────────────────────────────────────────────────

// isOU tells an OU from a default container (CN=Users, CN=Computers).
func isOU(dn string) bool { return strings.HasPrefix(strings.ToUpper(dn), "OU=") }

func (u *ui) ousPage() fyne.CanvasObject {
	var ous []orgUnit
	sel := -1
	count := newText("", mutedText, 12, false)
	var del *volButton
	deletable := func() bool {
		return sel >= 0 && sel < len(ous) && isOU(ous[sel].DN) && !strings.EqualFold(ous[sel].Name, "Domain Controllers")
	}
	tab := newTable([]column{{t("Path"), 240, false}, {t("Type"), 100, true}, {t("Description"), 260, false}, {"DN", 220, false}},
		func() int { return len(ous) },
		func(r, c int) (string, color.Color) {
			o := ous[r]
			switch c {
			case 0:
				return ouPath(o.DN), strongText
			case 1:
				if isOU(o.DN) {
					return "OU", strongText
				}
				return t("container"), mutedText
			case 2:
				return o.Description, nil
			}
			return o.DN, mutedText
		}, func(r int) {
			sel = r
			if deletable() {
				del.Enable()
			} else {
				del.Disable() // containers and Domain Controllers cannot be deleted (the server refuses too)
			}
		})
	reload := func() {
		var r []orgUnit
		u.bgDo(func() (err error) { r, err = u.fetchOUs(); return err }, func() {
			ous, sel = r, -1
			del.Disable()
			tab.clearSelection()
			tab.Refresh()
			count.Text = tf("%d OU(s) and containers", len(r))
			count.Refresh()
		})
	}
	create := newButton(t("New OU"), theme.FolderNewIcon(), func() {
		var parents []orgUnit
		for _, o := range ous {
			if isOU(o.DN) {
				parents = append(parents, o)
			}
		}
		name, desc := newEntry(), newEntry()
		pick, parentOf := ouSelect(parents, "")
		root := t("domain root")
		pick.Options = append([]string{root}, pick.Options...) // root maps to "" = the domain base
		pick.SetSelected(root)
		form := widget.NewForm(widget.NewFormItem(t("Name"), name), widget.NewFormItem(t("Description"), desc), widget.NewFormItem(t("Inside"), pick))
		u.showModal(t("New OU"), form, t("Create"), false, 560, func() bool {
			n := strings.TrimSpace(name.Text)
			if n == "" {
				return false
			}
			in := map[string]string{"name": n, "description": desc.Text, "parent": parentOf()}
			u.bgDo(func() error { return u.panel.do(http.MethodPost, "/api/ous", in, nil) },
				func() { u.setStatus(tf("OU %s created.", n)); reload() })
			return true
		})
		u.w.Canvas().Focus(name)
	})
	create.Importance = widget.HighImportance
	del = newButton(t("Delete"), theme.DeleteIcon(), func() {
		if !deletable() {
			return
		}
		o := ous[sel]
		u.confirmTyped(t("Delete OU"), tf("Delete the OU %s? Only possible when it is empty.", ouPath(o.DN)), o.Name, func() {
			u.bgDo(func() error { return u.panel.do(http.MethodDelete, "/api/ous?dn="+url.QueryEscape(o.DN), nil, nil) },
				func() { u.setStatus(tf("OU %s deleted.", o.Name)); reload() })
		})
	})
	del.Importance = widget.DangerImportance
	del.Disable()
	reload()
	top := container.NewBorder(nil, nil, container.NewCenter(newText(tf("Organizational units and default containers of %s", u.me.Domain), mutedText, 12, false)),
		container.NewHBox(container.NewCenter(count), newButton("", theme.ViewRefreshIcon(), reload)))
	return container.NewPadded(card(t("Organizational units"), container.NewBorder(container.NewVBox(top, container.NewHBox(create, del)), nil, nil, nil, tab.view())))
}
