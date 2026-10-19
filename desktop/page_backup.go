package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ── Backup ──────────────────────────────────────────────────────────────────

// backupManifest is one backup (GET /api/backups).
type backupManifest struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Origin  string    `json:"origin"`
	By      string    `json:"by"`
	Chain   string    `json:"chain"`
	Full    bool      `json:"full"`
	File    string    `json:"file"`
	Size    int64     `json:"size"`
	SHA256  string    `json:"sha256"`
	Started time.Time `json:"started"`
	Status  string    `json:"status"`
	Error   string    `json:"error"`
	Log     []string  `json:"log"`
	Folders []string  `json:"folders"`
}

// restoreStatus is a server's last restoration.
type restoreStatus struct {
	Backup    string    `json:"backup"`
	Kind      string    `json:"kind"`
	Dest      string    `json:"dest"`
	PreBackup string    `json:"pre_backup"`
	Started   time.Time `json:"started"`
	Status    string    `json:"status"`
	Error     string    `json:"error"`
	Log       []string  `json:"log"`
}

// backupServer is the panel (id 0) or an agent with its backups.
type backupServer struct {
	ID      int64            `json:"id"`
	Name    string           `json:"name"`
	DC      bool             `json:"dc"`
	Kinds   []string         `json:"kinds"`
	Roots   []string         `json:"roots"`
	Backups []backupManifest `json:"backups"`
	Error   string           `json:"error"`
	Restore *restoreStatus   `json:"restore"`
	// what may be sent to this server, up to what size
	UploadKinds  []string `json:"upload_kinds"`
	UploadMaxMiB int64    `json:"upload_max_mib"`
}

// backupSchedule is a recurring backup kept by an agent.
type backupSchedule struct {
	ID            string   `json:"id"`
	Kind          string   `json:"kind"`
	Cron          string   `json:"cron"`
	Folders       []string `json:"folders,omitempty"`
	FullEveryDays int      `json:"full_every_days,omitempty"`
	Retention     struct {
		Chains int `json:"chains"`
		Days   int `json:"days"`
	} `json:"retention"`
	Enabled bool `json:"enabled"`
}

func backupKindName(k string) string {
	switch k {
	case "dc":
		return t("Domain controller")
	case "fileserver-config":
		return t("Configuration and permissions")
	case "fileserver-data":
		return t("Data folders")
	case "panel":
		return t("Panel")
	case "samba-files":
		return t("Samba files (before the setup)")
	case "all":
		return t("Complete (configuration + all share roots, full)")
	}
	return k
}

// backupOrigin names where a backup came from.
func backupOrigin(o string) string {
	switch o {
	case "manual":
		return t("manual")
	case "upload":
		return t("sent")
	case "pre-restore":
		return t("before a restoration")
	case "setup":
		return t("before the setup")
	}
	if id, ok := strings.CutPrefix(o, "schedule:"); ok {
		return t("schedule") + " " + id
	}
	return o
}

// postRaw sends r as the body of a POST (a backup being sent: no total timeout) and decodes the answer into out.
func (a *api) postRaw(path string, r io.Reader, size int64, out any) error {
	req, err := http.NewRequest(http.MethodPost, a.base+path, r)
	if err != nil {
		return err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	hc := *a.hc
	hc.Timeout = 0
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized && a.token == "" {
		return errSessionExpired
	}
	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		if json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return fmt.Errorf("%s: HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

func sizeText(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d KiB", (n+1023)/1024)
}

// backupBase is the API path of a server's backups.
func backupBase(s backupServer) string {
	if s.ID == 0 {
		return "/api/backups/panel"
	}
	return fmt.Sprintf("/api/agents/%d/backups", s.ID)
}

func (u *ui) backupPage() fyne.CanvasObject {
	var servers []backupServer
	var schedules []backupSchedule
	sel, schedSel := -1, -1
	serverPick := newSelect(nil, nil)
	count := newText("", mutedText, 12, false)
	last := newText("", mutedText, 12, false) // the last restoration, with its log
	var cur backupServer
	lastLog := newButton("", theme.DocumentIcon(), func() {
		if r := cur.Restore; r != nil {
			lines := append([]string{}, r.Log...)
			if r.Error != "" {
				lines = append(lines, t("Error")+": "+serverText(r.Error))
			}
			u.showInfo(r.Backup, strings.Join(lines, "\n"))
		}
	})
	lastRow := container.NewBorder(nil, nil, nil, lastLog, last)
	lastRow.Hide()
	tab := newTable([]column{{t("Date"), 150, false}, {t("Kind"), 230, false}, {t("Origin"), 200, false}, {t("Size"), 90, true}, {t("Status"), 110, true}},
		func() int { return len(cur.Backups) },
		func(r, c int) (string, color.Color) {
			b := cur.Backups[r]
			switch c {
			case 0:
				return b.Started.Local().Format(dateLayout()), strongText
			case 1:
				k := backupKindName(b.Kind)
				if b.Kind == "fileserver-data" {
					if b.Full {
						k += " · " + t("full")
					} else {
						k += " · " + t("incremental")
					}
				}
				return k, nil
			case 2:
				o := backupOrigin(b.Origin)
				if b.By != "" {
					o += " · " + b.By
				}
				return o, mutedText
			case 3:
				if b.Status != "ok" {
					return "—", mutedText
				}
				return sizeText(b.Size), nil
			}
			switch b.Status {
			case "ok":
				return strings.ToUpper(t("ok")), okText
			case "error":
				return strings.ToUpper(t("error")), errText
			}
			return strings.ToUpper(t("running")), warnText
		}, func(r int) { sel = r })
	schedTab := newTable([]column{{t("Name"), 140, false}, {t("Kind"), 220, false}, {t("When (cron)"), 120, false},
		{t("Keep chains"), 110, true}, {t("Keep days"), 100, true}, {t("On"), 60, true}},
		func() int { return len(schedules) },
		func(r, c int) (string, color.Color) {
			x := schedules[r]
			switch c {
			case 0:
				return x.ID, strongText
			case 1:
				return backupKindName(x.Kind), nil
			case 2:
				return x.Cron, nil
			case 3:
				return strconv.Itoa(x.Retention.Chains), nil
			case 4:
				return strconv.Itoa(x.Retention.Days), nil
			}
			if x.Enabled {
				return t("yes"), okText
			}
			return t("no"), mutedText
		}, func(r int) { schedSel = r })
	var reload func()
	var sched fyne.CanvasObject // the schedules card: agents only
	loadSchedules := func() {
		schedules, schedSel = nil, -1
		schedTab.clearSelection()
		schedTab.Refresh()
		if cur.ID == 0 || cur.Error != "" || len(cur.Kinds) == 0 {
			if sched != nil {
				sched.Hide()
			}
			return
		}
		if sched != nil {
			sched.Show()
		}
		var list []backupSchedule
		id := cur.ID
		u.bgDo(func() error {
			return u.panel.do(http.MethodGet, fmt.Sprintf("/api/agents/%d/backup-schedules", id), nil, &list)
		}, func() {
			schedules = list
			schedTab.Refresh()
		})
	}
	show := func() {
		sel = -1
		tab.clearSelection()
		tab.Refresh()
		if cur.Error != "" {
			count.Text = serverText(cur.Error)
		} else {
			count.Text = tf("%d backup(s)", len(cur.Backups))
		}
		count.Refresh()
		lastRow.Hide()
		if r := cur.Restore; r != nil {
			st, c := t("running"), warnText
			switch r.Status {
			case "ok":
				st, c = t("ok"), okText
			case "error":
				st, c = serverText(r.Error), errText
			}
			txt := t("Last restoration") + ": " + backupKindName(r.Kind) + " · " + r.Started.Local().Format(dateLayout())
			if r.Dest != "" {
				txt += " → " + r.Dest
			}
			last.Text, last.Color = txt+" · "+st, c
			last.Refresh()
			lastRow.Show()
		}
		loadSchedules()
	}
	name := func(s backupServer) string {
		if s.ID == 0 {
			return t("This panel")
		}
		if s.DC {
			return s.Name + " (DC)"
		}
		return s.Name
	}
	serverPick.OnChanged = func(n string) {
		for _, s := range servers {
			if name(s) == n {
				cur = s
				show()
			}
		}
	}
	reload = func() {
		var r []backupServer
		u.bgDo(func() error { return u.panel.do(http.MethodGet, "/api/backups", nil, &r) }, func() {
			servers = r
			var names []string
			for _, s := range r {
				names = append(names, name(s))
			}
			serverPick.SetOptions(names)
			pick := name(cur)
			if cur.Name == "" && len(r) > 0 {
				pick = names[0]
			}
			serverPick.SetSelected(pick) // OnChanged only when it changes
			for _, s := range r {
				if s.ID == cur.ID {
					cur = s
				}
			}
			show()
		})
	}
	selected := func() (backupManifest, bool) {
		if sel < 0 || sel >= len(cur.Backups) {
			return backupManifest{}, false
		}
		return cur.Backups[sel], true
	}
	now := newButton(t("Back up now"), theme.DocumentSaveIcon(), func() {
		s := cur
		if s.Error != "" || len(s.Kinds) == 0 {
			return
		}
		if s.ID == 0 {
			u.bgDo(func() error { return u.panel.do(http.MethodPost, "/api/backups/panel", nil, nil) },
				func() { u.setStatus(t("Backup done.")); reload() })
			return
		}
		var names []string
		for _, k := range s.Kinds {
			names = append(names, backupKindName(k))
		}
		kind := newSelect(names, nil)
		kind.SetSelected(names[0])
		folders := newMultiLineEntry()
		folders.SetPlaceHolder(strings.Join(s.Roots, "\n"))
		folders.SetMinRowsVisible(3)
		full := newCheck(t("Full backup (starts a new chain)"), nil)
		form := widget.NewForm(widget.NewFormItem(t("Kind"), kind),
			widget.NewFormItem(tf("Folders (one per line, inside %s)", strings.Join(s.Roots, ", ")), folders),
			widget.NewFormItem("", full))
		u.showModal(tf("Back up %s", s.Name), form, t("Back up now"), false, 620, func() bool {
			k := s.Kinds[kind.SelectedIndex()]
			var fs []string
			for _, f := range strings.Split(folders.Text, "\n") {
				if f = strings.TrimSpace(f); f != "" {
					fs = append(fs, f)
				}
			}
			if k == "fileserver-data" && len(fs) == 0 {
				return false
			}
			u.bgDo(func() error {
				return u.panel.do(http.MethodPost, backupBase(s), map[string]any{"kind": k, "folders": fs, "full": full.Checked && k == "fileserver-data"}, nil)
			}, func() { u.setStatus(tf("Backup queued on %s: it shows in the list when it ends.", s.Name)); reload() })
			return true
		})
	})
	now.Importance = widget.HighImportance
	download := newButton(t("Download"), theme.DownloadIcon(), func() {
		b, ok := selected()
		if !ok || b.Status != "ok" {
			return
		}
		path := backupBase(cur) + "/" + esc(b.ID) + "/download"
		save := dialog.NewFileSave(func(w fyne.URIWriteCloser, err error) {
			if err != nil || w == nil {
				return
			}
			go func() {
				defer w.Close()
				err := u.panel.getRaw(path, w)
				fyne.Do(func() {
					if err != nil {
						u.handleError(err)
						return
					}
					u.setStatus(tf("Saved to %s (SHA-256 %s)", w.URI().Path(), b.SHA256))
				})
			}()
		}, u.w)
		save.SetFileName(b.File)
		save.Show()
	})
	logBtn := newButton(t("Log"), theme.DocumentIcon(), func() {
		b, ok := selected()
		if !ok {
			return
		}
		lines := append([]string{}, b.Log...)
		if b.Error != "" {
			lines = append(lines, t("Error")+": "+serverText(b.Error))
		}
		if len(lines) == 0 {
			lines = []string{t("No log.")}
		}
		u.showInfo(b.ID, strings.Join(lines, "\n"))
	})
	del := newButton(t("Delete"), theme.DeleteIcon(), func() {
		b, ok := selected()
		if !ok {
			return
		}
		msg := tf("Delete the backup %s?", b.ID)
		if b.Full && b.Kind == "fileserver-data" {
			msg += "\n" + t("The incremental backups of this chain go too.")
		}
		s := cur
		u.confirmTyped(t("Delete backup"), msg, b.ID[len(b.ID)-8:], func() {
			u.bgDo(func() error { return u.panel.do(http.MethodDelete, backupBase(s)+"/"+esc(b.ID), nil, nil) },
				func() { u.setStatus(tf("%s deleted.", b.ID)); reload() })
		})
	})
	del.Importance = widget.DangerImportance
	restore := newButton(t("Restore"), theme.HistoryIcon(), func() {
		b, ok := selected()
		if !ok || b.Status != "ok" {
			return
		}
		s := cur
		if b.Kind == "dc" {
			u.showInfo(t("Restore"), t(`A domain controller backup is not restored over a DC in use: install ad-manager on a new server and, in the initial setup, choose "Restore a DC" with this backup (docs/backup.md).`))
			return
		}
		code := b.ID[len(b.ID)-8:]
		confirm := newEntry()
		confirmItem := widget.NewFormItem(tf("This replaces what is in use. Type %s to confirm", code), confirm)
		form := widget.NewForm()
		folders := newCheckList(b.Folders)
		folders.SetSelected(b.Folders)
		newDir := t("In a new folder: [share root]/_restored/[date]/ (nothing current is touched)")
		original := t("Over the original folders: they become a backup first, then go back to how this backup has them (newer files are removed)")
		target := newRadioList([]string{newDir, original})
		target.SetSelected(newDir)
		partNames := []string{t("Shares (registry)"), t("Permissions of the share roots"), t("smb.conf (checked with testparm; the current one is kept)")}
		partIDs := []string{"shares", "acls", "smbconf"}
		parts := newCheckList(partNames)
		parts.SetSelected(partNames[:2])
		note := func(text string) *widget.Label {
			l := widget.NewLabel(text)
			l.Wrapping = fyne.TextWrapWord
			return l
		}
		sub := newEntry()
		if len(b.Folders) > 0 {
			sub.SetPlaceHolder(b.Folders[0] + "/Financeiro")
		}
		switch b.Kind {
		case "fileserver-data":
			form.Append(t("Folders"), folders.Container)
			form.Append(t("Or only one folder inside them"), sub)
			form.Append(t("Where"), target.Container)
			target.OnChanged = func(v string) {
				form.Items = slices.DeleteFunc(form.Items, func(i *widget.FormItem) bool { return i == confirmItem })
				if v == original {
					form.AppendItem(confirmItem)
				}
				form.Refresh()
			}
		case "fileserver-config":
			form.Append(t("What to restore (the current configuration becomes a backup first)"), parts.Container)
			form.AppendItem(confirmItem)
		case "samba-files":
			form.Append("", note(t("Samba stops; /etc/samba, /var/lib/samba and krb5.conf are moved aside (…before-restore-[time]) and come back as this backup has them; Samba starts again. On a DC this undoes everything done to the domain since.")))
			form.AppendItem(confirmItem)
		case "panel":
			form.Append("", note(t("The panel's database (settings, registered servers, sources, links) goes back to this backup; the current one becomes a backup first. The panel restarts. The configuration file and the audit log are not restored.")))
			form.AppendItem(confirmItem)
		}
		u.showModal(tf("Restore %s", b.ID), form, t("Restore"), true, 680, func() bool {
			body := map[string]any{"confirm": strings.TrimSpace(confirm.Text)}
			if b.Kind == "fileserver-data" {
				body["folders"] = folders.Selected
				if f := strings.TrimSpace(sub.Text); f != "" {
					body["folders"] = []string{f}
				}
				body["target"] = "new"
				if target.Selected == original {
					body["target"] = "original"
				}
			}
			if b.Kind == "fileserver-config" {
				var ps []string
				for i, n := range partNames {
					if slices.Contains(parts.Selected, n) {
						ps = append(ps, partIDs[i])
					}
				}
				body["parts"] = ps
			}
			var out struct {
				Restart bool `json:"restart"`
			}
			u.bgDo(func() error {
				return u.panel.do(http.MethodPost, backupBase(s)+"/"+esc(b.ID)+"/restore", body, &out)
			}, func() {
				if out.Restart {
					u.showInfo(t("Restore"), t("The panel restarts with the restored database: sign in again in a few seconds."))
					return
				}
				u.setStatus(tf("Restoration queued on %s: follow it under Last restoration.", s.Name))
				time.AfterFunc(2*time.Second, func() { fyne.Do(reload) })
			})
			return true
		})
	})
	send := newButton(t("Send a backup"), theme.UploadIcon(), func() {
		s := cur
		if len(s.UploadKinds) == 0 {
			return
		}
		var names []string
		for _, k := range s.UploadKinds {
			names = append(names, backupKindName(k))
		}
		open := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
			if err != nil || r == nil {
				return
			}
			kind := newSelect(names, nil)
			kind.SetSelected(names[0])
			info := widget.NewLabel(tf("The server checks the content for the kind and lists it as sent; then it restores like the others. Up to %d MiB.", s.UploadMaxMiB))
			info.Wrapping = fyne.TextWrapWord
			form := widget.NewForm(widget.NewFormItem(t("File"), widget.NewLabel(r.URI().Name())), widget.NewFormItem(t("Kind"), kind),
				widget.NewFormItem("", info))
			u.showModal(t("Send a backup"), form, t("Send"), false, 620, func() bool {
				k := s.UploadKinds[kind.SelectedIndex()]
				var m backupManifest
				u.setStatus(t("Sending…"))
				u.bgDo(func() error {
					defer r.Close()
					var size int64 = -1 // unknown: sent chunked
					if fi, err := os.Stat(r.URI().Path()); err == nil {
						size = fi.Size()
					}
					return u.panel.postRaw(backupBase(s)+"/upload?kind="+esc(k), r, size, &m)
				}, func() { u.setStatus(tf("Backup sent: %s", m.ID)); reload() })
				return true
			})
		}, u.w)
		open.Show()
	})
	saveSchedules := func(list []backupSchedule) {
		id := cur.ID
		u.bgDo(func() error {
			return u.panel.do(http.MethodPut, fmt.Sprintf("/api/agents/%d/backup-schedules", id), list, nil)
		}, func() { u.setStatus(t("Schedules saved.")); loadSchedules() })
	}
	newSched := newButton(t("New schedule"), theme.ContentAddIcon(), func() {
		s := cur
		if s.ID == 0 || len(s.Kinds) == 0 {
			return
		}
		var names []string
		for _, k := range s.Kinds {
			names = append(names, backupKindName(k))
		}
		id := newEntry()
		id.SetText(fmt.Sprintf("backup-%d", len(schedules)+1))
		kind := newSelect(names, nil)
		kind.SetSelected(names[0])
		cron := newSelectEntry([]string{"0 2 * * *", "0 3 * * 0", "0 3 1 * *"})
		cron.SetText("0 2 * * *")
		chains, days, every := newEntry(), newEntry(), newEntry()
		chains.SetText("4")
		days.SetText("0")
		every.SetText("7")
		folders := newMultiLineEntry()
		folders.SetMinRowsVisible(2)
		form := widget.NewForm(widget.NewFormItem(t("Name"), id), widget.NewFormItem(t("Kind"), kind),
			widget.NewFormItem(t("When (cron)"), cron), widget.NewFormItem(t("Keep chains"), chains),
			widget.NewFormItem(t("Keep days"), days), widget.NewFormItem(t("New full every (days)"), every),
			widget.NewFormItem(tf("Folders (one per line, inside %s)", strings.Join(s.Roots, ", ")), folders))
		u.showModal(t("New schedule"), form, t("Save"), false, 620, func() bool {
			x := backupSchedule{ID: strings.TrimSpace(id.Text), Kind: s.Kinds[kind.SelectedIndex()], Cron: strings.TrimSpace(cron.Text), Enabled: true}
			x.Retention.Chains, _ = strconv.Atoi(chains.Text)
			x.Retention.Days, _ = strconv.Atoi(days.Text)
			x.FullEveryDays, _ = strconv.Atoi(every.Text)
			for _, f := range strings.Split(folders.Text, "\n") {
				if f = strings.TrimSpace(f); f != "" && x.Kind == "fileserver-data" {
					x.Folders = append(x.Folders, f)
				}
			}
			saveSchedules(append(append([]backupSchedule{}, schedules...), x))
			return true
		})
	})
	toggleSched := newButton(t("On/off"), theme.MediaPauseIcon(), func() {
		if schedSel < 0 || schedSel >= len(schedules) {
			return
		}
		list := append([]backupSchedule{}, schedules...)
		list[schedSel].Enabled = !list[schedSel].Enabled
		saveSchedules(list)
	})
	delSched := newButton(t("Remove"), theme.DeleteIcon(), func() {
		if schedSel < 0 || schedSel >= len(schedules) {
			return
		}
		x := schedules[schedSel]
		u.confirm(t("Remove schedule"), tf("Remove the schedule %s? Its backups stay.", x.ID), t("Remove"), true, func() {
			list := append(append([]backupSchedule{}, schedules[:schedSel]...), schedules[schedSel+1:]...)
			saveSchedules(list)
		})
	})
	reload()
	top := container.NewBorder(nil, nil, nil, container.NewHBox(container.NewCenter(count), newButton("", theme.ViewRefreshIcon(), reload)),
		fixedWidth(serverPick, 320))
	actions := container.NewHBox(now, send, download, restore, logBtn, del)
	list := card(t("Backup"), container.NewBorder(container.NewVBox(top, actions, lastRow), nil, nil, nil, tab.view()))
	sched = card(t("Schedules"), container.NewBorder(container.NewHBox(newSched, toggleSched, delSched),
		newText(t("Retention deletes whole chains only (a full backup and its incrementals), never the backup just made; 0 = no limit."), mutedText, 11, false),
		nil, nil, schedTab.view()))
	split := container.NewVSplit(list, sched)
	split.SetOffset(0.62)
	return container.NewPadded(split)
}
