package main

import (
	"errors"
	"image/color"
	"net/http"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ── Shares (straight to the agent, using the token from the config) ─────────

// shareRow is a published share with the user ACLs and visibility (GET /api/shares on the agent).
type shareRow struct {
	share
	RWUsers   []string `json:"rw_users"`
	ROUsers   []string `json:"ro_users"`
	Full      []string `json:"full_groups"`
	FullUsers []string `json:"full_users"`
	Browse    bool     `json:"browseable"`
	Recycle   bool     `json:"recycle"`
	ABE       bool     `json:"abe"`
	Audit     bool     `json:"audit"`
}

// splitList splits "a, b ,c" into its non-empty trimmed items.
func splitList(s string) []string {
	var out []string
	for _, g := range strings.Split(s, ",") {
		if g = strings.TrimSpace(g); g != "" {
			out = append(out, g)
		}
	}
	return out
}

func (u *ui) sharesPage() fyne.CanvasObject {
	if len(u.cfg.Agents) == 0 {
		return container.NewPadded(card(t("Shares"), widget.NewLabel(tf("No file server configured in %s\n([[agents]] with name, url and token).", configPath))))
	}
	names := make([]string, len(u.cfg.Agents))
	for i, a := range u.cfg.Agents {
		names[i] = a.Name
	}
	var ag *api
	var agName string
	var shares []shareRow
	var sizes map[string]int64
	measuring := false
	sel := -1
	health := newText("", mutedText, 12, true)
	total := newText("", mutedText, 12, false)
	sizeText := func(name string) string {
		if n, ok := sizes[name]; ok {
			return humanBytes(n)
		}
		if measuring {
			return "…"
		}
		return "—"
	}
	tab := newTable([]column{{t("Name"), 150, false}, {t("Path"), 200, false}, {t("Size"), 80, false},
		{t("Write access"), 200, false}, {t("Read access"), 190, false}, {t("Comment"), 140, false}},
		func() int { return len(shares) },
		func(r, c int) (string, color.Color) {
			s := shares[r]
			switch c {
			case 0:
				if !s.Browse {
					return s.Name + "  (" + t("hidden") + ")", strongText
				}
				return s.Name, strongText
			case 1:
				return s.Path, nil
			case 2:
				return sizeText(s.Name), nil
			case 3:
				return joinList(append(append([]string{}, s.RW...), s.RWUsers...)), nil
			case 4:
				return joinList(append(append([]string{}, s.RO...), s.ROUsers...)), nil
			}
			return s.Comment, mutedText
		}, func(r int) { sel = r })
	showTotal := func() {
		var sum int64
		for _, n := range sizes {
			sum += n
		}
		total.Text = tf("total %s", humanBytes(sum))
		if measuring {
			total.Text = tf("total %s", "…")
		}
		total.Refresh()
	}
	// loadSizes asks the agent for the folder sizes (du, cached 5 min there; refresh forces a new measure).
	loadSizes := func(target *api, refresh bool) {
		path := "/api/sizes"
		if refresh {
			path += "?refresh=1"
		}
		measuring = true
		showTotal()
		tab.Refresh()
		go func() {
			var r struct {
				Sizes map[string]int64 `json:"sizes"`
			}
			err := target.do(http.MethodGet, path, nil, &r)
			fyne.Do(func() {
				if target != ag {
					return
				}
				measuring, sizes = false, r.Sizes
				if err != nil {
					sizes = nil
				}
				showTotal()
				tab.Refresh()
			})
		}()
	}
	reload := func(refreshSizes bool) {
		if ag == nil {
			return
		}
		target := ag // quick server switch: a stale response is discarded
		go func() {
			var r []shareRow
			err := target.do(http.MethodGet, "/api/shares", nil, &r)
			fyne.Do(func() {
				if target != ag {
					return
				}
				if err != nil {
					health.Text, health.Color = "● "+t("unavailable"), errText
					health.Refresh()
					u.setStatus(agName + ": " + err.Error())
					return
				}
				health.Text, health.Color = "● "+t("online"), okText
				health.Refresh()
				shares, sel = r, -1
				tab.clearSelection()
				tab.Refresh()
				u.setStatus(tf("%s: %d share(s)", agName, len(r)))
				loadSizes(target, refreshSizes)
			})
		}()
	}
	picker := newSelect(names, func(n string) {
		for _, a := range u.cfg.Agents {
			if a.Name == n {
				ag, agName, sizes = newAPI(strings.TrimRight(a.URL, "/"), a.Token, a.Insecure), n, nil
			}
		}
		reload(false)
	})
	// enqueue submits a task to the agent and polls it until it finishes.
	enqueue := func(kind string, payload map[string]any, label string) {
		target := ag
		go func() {
			var tk task
			if err := target.do(http.MethodPost, "/api/tasks", map[string]any{"type": kind, "payload": payload}, &tk); err != nil {
				fyne.Do(func() { u.handleError(err) })
				return
			}
			fyne.Do(func() { u.setStatus(label + ": " + t("queued")) })
			// every second for 40 s, then every 5 s (24 h at most): a task waits for the ones queued before it, and
			// each may take up to the agent's 30-minute limit
			for i := range 40 + 17280 {
				time.Sleep(map[bool]time.Duration{true: time.Second, false: 5 * time.Second}[i < 40])
				var st task
				if target.do(http.MethodGet, "/api/tasks/"+esc(tk.ID), nil, &st) != nil {
					continue
				}
				if st.State == "completed" || st.State == "archived" {
					fyne.Do(func() {
						if st.State == "archived" {
							u.showError(errors.New(label + ": " + serverText(st.LastErr)))
						} else {
							u.setStatus(label + ": " + t("done"))
						}
						if target == ag {
							reload(false)
						}
					})
					return
				}
			}
		}()
	}
	// shareForm opens the new-share form, or the edit form of s (same share:create task).
	shareForm := func(s *shareRow) {
		if ag == nil {
			return
		}
		name, folder, comment := newEntry(), newEntry(), newEntry()
		rw, ro, rwUsers, roUsers := newEntry(), newEntry(), newEntry(), newEntry()
		full, fullUsers := newEntry(), newEntry()
		rw.SetPlaceHolder(u.me.Netbios + `\` + t("group") + ", …")
		ro.SetPlaceHolder(rw.PlaceHolder)
		full.SetPlaceHolder(rw.PlaceHolder)
		rwUsers.SetPlaceHolder(u.me.Netbios + `\` + t("user") + ", …")
		roUsers.SetPlaceHolder(rwUsers.PlaceHolder)
		fullUsers.SetPlaceHolder(rwUsers.PlaceHolder)
		hidden := newCheck(t("Hidden on the network (not shown when browsing)"), nil)
		recycle := newCheck(t("Recycle bin (deleted files go to .recycle, per user)"), nil)
		abe := newCheck(t("Hide what the user cannot open (access-based enumeration)"), nil)
		audit := newCheck(t("Audit file changes (renamed and deleted files, new folders → syslog)"), nil)
		createPath := newCheck(t("Create the folder (owner group = 1st with write access, inherited ACL)"), nil)
		createPath.SetChecked(true) // on edit it re-applies the folder ACLs; the content is untouched
		title, button := tf("New share on %s", agName), t("Create")
		var note fyne.CanvasObject = createPath
		if s != nil {
			title, button = tf("Edit share %s on %s", s.Name, agName), t("Save")
			name.SetText(s.Name)
			folder.SetText(s.Path)
			name.Disable()
			folder.Disable()
			comment.SetText(s.Comment)
			rw.SetText(strings.Join(s.RW, ", "))
			ro.SetText(strings.Join(s.RO, ", "))
			rwUsers.SetText(strings.Join(s.RWUsers, ", "))
			roUsers.SetText(strings.Join(s.ROUsers, ", "))
			full.SetText(strings.Join(s.Full, ", "))
			fullUsers.SetText(strings.Join(s.FullUsers, ", "))
			hidden.SetChecked(!s.Browse)
			recycle.SetChecked(s.Recycle)
			abe.SetChecked(s.ABE)
			audit.SetChecked(s.Audit)
			note = wrappedLabel(t("Saving republishes the share and reapplies the folder permissions, also on the files and subfolders that inherit them; their content is not changed."))
		} else {
			folder.SetText("/srv/shares/")
			name.OnChanged = func(v string) {
				folder.SetText("/srv/shares/" + strings.ToLower(strings.ReplaceAll(strings.TrimSpace(v), " ", "-")))
			}
		}
		form := widget.NewForm(widget.NewFormItem(t("Share name"), name), widget.NewFormItem(t("Folder"), folder),
			widget.NewFormItem(t("Comment"), comment),
			widget.NewFormItem(t("Groups with full control"), full),
			widget.NewFormItem(t("Groups with write access"), rw), widget.NewFormItem(t("Read-only groups"), ro),
			widget.NewFormItem(t("Users with full control"), fullUsers),
			widget.NewFormItem(t("Users with write access"), rwUsers), widget.NewFormItem(t("Read-only users"), roUsers),
			widget.NewFormItem("", hidden), widget.NewFormItem("", recycle), widget.NewFormItem("", abe),
			widget.NewFormItem("", audit), widget.NewFormItem("", note))
		server := agName
		u.showModal(title, form, button, false, 720, func() bool {
			if strings.TrimSpace(name.Text) == "" || strings.TrimSpace(folder.Text) == "" {
				return false
			}
			unc := `\\` + server + `\` + strings.TrimSpace(name.Text)
			label := tf("Create %s", unc)
			if s != nil {
				label = tf("Edit %s", unc)
			}
			enqueue("share:create", map[string]any{"name": strings.TrimSpace(name.Text), "path": strings.TrimSpace(folder.Text), "comment": comment.Text,
				"rw_groups": splitList(rw.Text), "ro_groups": splitList(ro.Text), "rw_users": splitList(rwUsers.Text), "ro_users": splitList(roUsers.Text),
				"full_groups": splitList(full.Text), "full_users": splitList(fullUsers.Text),
				"hidden": hidden.Checked, "create_path": createPath.Checked, "recycle": recycle.Checked, "abe": abe.Checked,
				"audit": audit.Checked}, label)
			return true
		})
	}
	selected := func() (shareRow, bool) {
		if ag == nil || sel < 0 || sel >= len(shares) {
			u.showInfo(t("Choose a share"), t("Select a share in the table before using this action."))
			return shareRow{}, false
		}
		return shares[sel], true
	}
	create := newButton(t("New share"), theme.FolderNewIcon(), func() { shareForm(nil) })
	create.Importance = widget.HighImportance
	edit := newButton(t("Edit"), theme.DocumentCreateIcon(), func() {
		if s, ok := selected(); ok {
			shareForm(&s)
		}
	})
	remove := newButton(t("Remove (keeps the files)"), theme.DeleteIcon(), func() {
		s, ok := selected()
		if !ok {
			return
		}
		target, name := ag, agName // the action stays bound to the server on screen
		unc := `\\` + name + `\` + s.Name
		u.confirmTyped(t("Remove share"), tf("Unpublish %s? The folder and its files stay on the server.", unc), s.Name, func() {
			if target != ag {
				u.showError(errors.New(t("The list changed servers; refresh and try again.")))
				return
			}
			enqueue("share:delete", map[string]any{"name": s.Name}, tf("Remove %s", unc))
		})
	})
	remove.Importance = widget.DangerImportance
	picker.SetSelectedIndex(0)
	top := container.NewBorder(nil, nil, container.NewHBox(widget.NewIcon(theme.StorageIcon()), newText(t("Server"), strongText, 13, true)),
		container.NewHBox(container.NewCenter(health), container.NewCenter(total),
			newButton(t("Refresh"), theme.ViewRefreshIcon(), func() { reload(true) })), picker)
	return container.NewPadded(card(t("Shares"), container.NewBorder(container.NewVBox(top, container.NewHBox(create, edit, remove)), nil, nil, nil, tab.view())))
}
