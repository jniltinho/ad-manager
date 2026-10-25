package main

import (
	"fmt"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// checkStep is one step of a connection test (POST …/syncs/browse with check).
type checkStep struct {
	Name, Detail, Hint string
	OK                 bool
	Items              []string
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// syncWizard creates (job nil) or edits a sync job in four steps, as the web wizard: source and access (with the
// connection test) → what to copy (the source's shares and folders, with sizes) → where to (this server's folders,
// checked as they are picked) → how and when, with the summary. done reloads the page.
func (u *ui) syncWizard(agent int64, data syncList, job *syncJob, done func()) {
	base := fmt.Sprintf("/api/agents/%d", agent)
	isNew := job == nil
	j := syncJob{Mode: "copy", MaxDelete: 100, MaxDeletePct: 20, KeepDays: 30, Transfers: 4, Verify: true, Reinherit: true,
		Exclude: []string{"Thumbs.db", "~$*", "*.tmp"}, Source: syncSource{Auth: "kerberos"}}
	if !data.Kerberos {
		j.Source.Auth = "ntlm"
	}
	if job != nil {
		j = *job
	}
	password := ""
	creds := func(src syncSource, extra map[string]any) map[string]any {
		b := map[string]any{"source": src, "password": password, "password_file": j.PasswordFile}
		if !isNew {
			b["job"] = j.ID
		}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	stepNames := []string{t("Source and access"), t("What to copy"), t("Where to"), t("How and when")}
	header := container.NewHBox()
	body := container.NewStack()
	var closeIt func()
	var show func(int)
	setHeader := func(n int) {
		header.Objects = nil
		for i, s := range stepNames {
			c := mutedText
			if i == n {
				c = strongText
			}
			header.Add(newText(fmt.Sprintf("%d. %s", i+1, s), c, 12, i == n))
			if i < len(stepNames)-1 {
				header.Add(newText("›", mutedText, 12, false))
			}
		}
		header.Refresh()
	}
	unc := func() string {
		s := `\\` + j.Source.Host
		if j.Source.Share != "" {
			s += `\` + j.Source.Share
		}
		if j.Source.Path != "" {
			s += `\` + strings.ReplaceAll(j.Source.Path, "/", `\`)
		}
		return s
	}
	var shares []string
	var srcSize *struct{ Files, Bytes int64 }

	// ── 1. source and access
	var tested string // the access the last good test was for
	step1 := func() fyne.CanvasObject {
		host := newEntry()
		host.SetText(j.Source.Host)
		host.SetPlaceHolder("old-fs.example.local")
		kerb, ntlm := t("This server's machine account (Kerberos)"), t("User and password")
		auth := widget.NewRadioGroup([]string{kerb, ntlm}, nil)
		if !data.Kerberos {
			auth.Options = []string{ntlm}
		}
		user, domain, pw := newEntry(), newEntry(), newPasswordEntry()
		user.SetText(j.Source.User)
		user.SetPlaceHolder(`DOMAIN\user`)
		domain.SetText(j.Source.Domain)
		pw.SetText(password)
		if !isNew && (j.PasswordSet || j.PasswordFile != "") {
			pw.SetPlaceHolder(t("kept (type to change)"))
		}
		ntlmBox := container.NewVBox(widget.NewForm(widget.NewFormItem(t("User"), user), widget.NewFormItem(t("Domain (or the server name)"), domain),
			widget.NewFormItem(t("Password"), pw)))
		machine := data.Machine
		if machine == "" {
			machine = "FS01$"
		}
		kerbHelp := newText(tf("Same domain, no password kept. Give %s (or Domain Computers) read permission on the share.", machine), mutedText, 11, false)
		warn := newText(t("Kerberos needs the full name (old-fs.example.local); with an IP use user and password."), warnText, 11, false)
		ipRe := regexp.MustCompile(`^\d{1,3}(\.\d{1,3}){3}$`)
		refresh := func() {
			ntlmBox.Hidden = auth.Selected != ntlm
			kerbHelp.Hidden = auth.Selected != kerb
			warn.Hidden = !(auth.Selected == kerb && ipRe.MatchString(strings.TrimSpace(host.Text)))
			ntlmBox.Refresh()
			kerbHelp.Refresh()
			warn.Refresh()
		}
		auth.OnChanged = func(string) { refresh() }
		host.OnChanged = func(string) { refresh() }
		if j.Source.Auth == "ntlm" {
			auth.SetSelected(ntlm)
		} else {
			auth.SetSelected(kerb)
		}
		result := container.NewVBox()
		read := func() {
			j.Source.Host = strings.TrimSpace(host.Text)
			j.Source.Auth = "kerberos"
			if auth.Selected == ntlm {
				j.Source.Auth = "ntlm"
				u := strings.TrimSpace(user.Text)
				if d, n, ok := strings.Cut(u, `\`); ok {
					domain.SetText(d)
					u = n
				} else if n, d, ok := strings.Cut(u, "@"); ok {
					domain.SetText(d)
					u = n
				}
				user.SetText(u)
				j.Source.User, j.Source.Domain = u, strings.TrimSpace(domain.Text)
			}
			password = pw.Text
		}
		key := func() string {
			return strings.Join([]string{j.Source.Host, j.Source.Auth, j.Source.User, j.Source.Domain, password}, "\x00")
		}
		names := map[string]string{"credentials": t("Credentials"), "resolve": t("Name resolved"), "port": t("Port 445 answers"), "signin": t("Signed in (signed SMB)")}
		test := func(next bool) {
			read()
			src := j.Source
			src.Share, src.Path = "", ""
			var out struct {
				Steps []checkStep `json:"steps"`
			}
			u.setStatus(t("Testing the connection…"))
			u.bgDo(func() error {
				return u.panel.withTimeout(2*time.Minute).do(http.MethodPost, base+"/syncs/browse", creds(src, map[string]any{"check": true}), &out)
			}, func() {
				result.Objects = []fyne.CanvasObject{newText(t("Connection test"), strongText, 12, true)}
				ok := len(out.Steps) > 0
				for _, st := range out.Steps {
					n := names[st.Name]
					if n == "" {
						n = st.Name
					}
					switch {
					case st.OK && st.Name == "signin":
						result.Add(newText("✓ "+n+" · "+tf("%d shares visible", len(st.Items)), okText, 12, false))
						shares = st.Items
					case st.OK:
						result.Add(newText("✓ "+n+" "+st.Detail, okText, 12, false))
					default:
						ok = false
						why := serverText(st.Detail)
						if st.Hint != "" {
							why = serverText(st.Hint)
						}
						l := widget.NewLabel("✗ " + n + ": " + why)
						l.Wrapping = fyne.TextWrapWord
						result.Add(l)
					}
				}
				ok = ok && out.Steps[len(out.Steps)-1].Name == "signin"
				result.Refresh()
				u.setStatus("")
				if ok {
					tested = key()
					if next {
						show(1)
					}
				}
			})
		}
		testBtn := newButton(t("Test connection"), theme.ConfirmIcon(), func() { test(false) })
		next := newButton(t("Next"), theme.NavigateNextIcon(), func() {
			read()
			if tested == key() {
				show(1)
				return
			}
			test(true)
		})
		next.Importance = widget.HighImportance
		refresh()
		return container.NewVBox(widget.NewForm(widget.NewFormItem(t("Source server"), host)), warn, auth, kerbHelp, ntlmBox, result,
			container.NewHBox(testBtn, next))
	}

	// ── 2. what to copy
	step2 := func() fyne.CanvasObject {
		var items []string
		where := newText("", strongText, 12, true)
		sizeLbl := newText("", mutedText, 11, false)
		list := widget.NewList(func() int { return len(items) }, func() fyne.CanvasObject { return widget.NewLabel("") },
			func(i widget.ListItemID, o fyne.CanvasObject) { o.(*widget.Label).SetText(items[i]) })
		var open func(share, p string)
		open = func(share, p string) {
			src := j.Source
			src.Share, src.Path = share, p
			if share == "" {
				j.Source.Share, j.Source.Path, items = "", "", shares
				where.Text = `\\` + j.Source.Host
				where.Refresh()
				list.UnselectAll()
				list.Refresh()
				return
			}
			var out struct {
				Dirs []string `json:"dirs"`
			}
			u.bgDo(func() error { return u.panel.do(http.MethodPost, base+"/syncs/browse", creds(src, nil), &out) }, func() {
				j.Source.Share, j.Source.Path, items = share, p, out.Dirs
				where.Text, sizeLbl.Text, srcSize = unc(), "", nil
				where.Refresh()
				sizeLbl.Refresh()
				list.UnselectAll()
				list.Refresh()
			})
		}
		list.OnSelected = func(i widget.ListItemID) {
			if j.Source.Share == "" {
				open(items[i], "")
			} else {
				open(j.Source.Share, strings.Trim(j.Source.Path+"/"+items[i], "/"))
			}
		}
		up := newButton(t("Up"), theme.NavigateBackIcon(), func() {
			switch {
			case j.Source.Path != "":
				p := path.Dir(j.Source.Path)
				if p == "." {
					p = ""
				}
				open(j.Source.Share, p)
			default:
				open("", "")
			}
		})
		size := newButton(t("Size"), theme.InfoIcon(), func() {
			if j.Source.Share == "" {
				return
			}
			var out struct{ Files, Bytes int64 }
			sizeLbl.Text = t("Counting…")
			sizeLbl.Refresh()
			u.bgDo(func() error {
				return u.panel.withTimeout(6*time.Minute).do(http.MethodPost, base+"/syncs/browse", creds(j.Source, map[string]any{"size": true}), &out)
			}, func() {
				srcSize = &out
				sizeLbl.Text = tf("%d files · %s", out.Files, sizeText(out.Bytes))
				sizeLbl.Refresh()
			})
		})
		use := newButton(t("Copy this folder"), theme.ConfirmIcon(), func() {
			if j.Source.Share == "" {
				return
			}
			if j.Dest == "" {
				root := "/srv/shares"
				if len(data.Roots) > 0 {
					root = data.Roots[0]
				}
				last := path.Base("/" + j.Source.Share + "/" + j.Source.Path)
				j.Dest = root + "/" + last
			}
			show(2)
		})
		use.Importance = widget.HighImportance
		back := newButton(t("Back"), theme.NavigateBackIcon(), func() { show(0) })
		open(j.Source.Share, j.Source.Path)
		help := newText(t("Open the share and the folder to copy; \"size\" counts what is inside (it can take a while on large folders)."), mutedText, 11, false)
		return container.NewBorder(container.NewVBox(help, container.NewHBox(up, where)), container.NewVBox(sizeLbl, container.NewHBox(back, size, use)), nil, nil, list)
	}

	// ── 3. where to
	var destOK bool
	var destFree *uint64
	step3 := func() fyne.CanvasObject {
		var dirs []string
		cur := ""
		where := newText("", strongText, 12, true)
		dest := newEntry()
		dest.SetText(j.Dest)
		status := widget.NewLabel("")
		status.Wrapping = fyne.TextWrapWord
		next := newButton(t("Next"), theme.NavigateNextIcon(), func() { j.Dest = strings.TrimSpace(dest.Text); show(3) })
		next.Importance = widget.HighImportance
		check := func(d string) {
			destOK = false
			next.Disable()
			var out struct {
				Dest struct {
					OK        bool    `json:"ok"`
					Error     string  `json:"error"`
					Exists    bool    `json:"exists"`
					Items     int     `json:"items"`
					FreeBytes *uint64 `json:"free_bytes"`
					Share     string  `json:"share"`
				} `json:"dest"`
			}
			u.bgDo(func() error {
				return u.panel.do(http.MethodPost, base+"/syncs/validate", map[string]any{"dest": d}, &out)
			}, func() {
				if strings.TrimSpace(dest.Text) != d {
					return
				}
				r := out.Dest
				if !r.OK {
					status.SetText("✗ " + serverText(r.Error))
					return
				}
				msg := t("The folder is made when the job runs")
				if r.Exists {
					msg = t("The folder exists")
				}
				if r.Share != "" {
					msg += " · " + tf("shared as %s", r.Share)
				}
				destFree = r.FreeBytes
				if r.FreeBytes != nil {
					msg += " · " + tf("%s free", sizeText(int64(*r.FreeBytes)))
					if srcSize != nil && srcSize.Bytes > int64(*r.FreeBytes) {
						msg += "\n✗ " + t("Not enough free space for the source.")
					}
				}
				if r.Items > 0 {
					msg += "\n⚠ " + tf("The folder already has %d items: files of the same name are replaced (the old ones go to the quarantine).", r.Items)
				}
				status.SetText("✓ " + msg)
				destOK = true
				next.Enable()
			})
		}
		dest.OnChanged = func(s string) { check(strings.TrimSpace(s)) }
		list := widget.NewList(func() int { return len(dirs) }, func() fyne.CanvasObject { return widget.NewLabel("") },
			func(i widget.ListItemID, o fyne.CanvasObject) { o.(*widget.Label).SetText(path.Base(dirs[i])) })
		var browse func(p string)
		browse = func(p string) {
			var out struct {
				Dirs []string `json:"dirs"`
			}
			u.bgDo(func() error { return u.panel.do(http.MethodGet, base+"/dirs?path="+esc(p), nil, &out) }, func() {
				cur, dirs = p, out.Dirs
				where.Text = p
				where.Refresh()
				list.UnselectAll()
				list.Refresh()
			})
		}
		list.OnSelected = func(i widget.ListItemID) { browse(dirs[i]) }
		inRoot := func(p string) bool {
			for _, r := range data.Roots {
				if p == r {
					return true
				}
			}
			return false
		}
		up := newButton(t("Up"), theme.NavigateBackIcon(), func() {
			if cur != "" && !inRoot(cur) {
				browse(path.Dir(cur))
			}
		})
		useHere := newButton(t("Use this folder"), theme.ConfirmIcon(), func() {
			if cur != "" && !inRoot(cur) {
				dest.SetText(cur)
			}
		})
		newName := newEntry()
		newName.SetPlaceHolder(t("name of the new folder"))
		newFolder := newButton(t("New folder…"), theme.FolderNewIcon(), func() {
			n := strings.Trim(strings.TrimSpace(newName.Text), `/\`)
			if n != "" && cur != "" {
				dest.SetText(cur + "/" + n)
			}
		})
		start := path.Dir(j.Dest)
		if j.Dest == "" || start == "." {
			start = "/srv/shares"
			if len(data.Roots) > 0 {
				start = data.Roots[0]
			}
		}
		browse(start)
		check(j.Dest)
		back := newButton(t("Back"), theme.NavigateBackIcon(), func() { show(1) })
		help := newText(t("Open a folder of this server and use it, or make a new one."), mutedText, 11, false)
		top := container.NewVBox(help, container.NewHBox(up, where))
		bottom := container.NewVBox(container.NewHBox(useHere, container.NewGridWrap(fyne.NewSize(220, newName.MinSize().Height), newName), newFolder),
			widget.NewForm(widget.NewFormItem(t("Destination folder"), dest)), status, container.NewHBox(back, next))
		return container.NewBorder(top, bottom, nil, nil, list)
	}

	// ── 4. how and when
	step4 := func() fyne.CanvasObject {
		copyM, mirrorM := t("Copy (recommended)"), t("Mirror")
		mode := widget.NewRadioGroup([]string{copyM, mirrorM}, nil)
		maxDel, maxPct := newEntry(), newEntry()
		maxDel.SetText(strconv.Itoa(j.MaxDelete))
		maxPct.SetText(strconv.Itoa(j.MaxDeletePct))
		keepDays, keepGiB := newEntry(), newEntry()
		if j.KeepDays == 0 { // 0 and missing mean the agent's default
			j.KeepDays = 30
		}
		keepDays.SetText(strconv.Itoa(j.KeepDays))
		keepGiB.SetText(strconv.Itoa(j.KeepGiB))
		root := "/srv/shares"
		for _, r := range data.Roots {
			if strings.HasPrefix(j.Dest, r+"/") {
				root = r
			}
		}
		keepBox := container.NewVBox(
			newText(tf("Deleted and replaced files go to %s (root only), where they can be given back.", root+"/_sync/"), mutedText, 11, false),
			widget.NewForm(widget.NewFormItem(t("Days kept in the quarantine"), keepDays), widget.NewFormItem(t("GiB kept in the quarantine (0: no limit)"), keepGiB)))
		mirrorBox := container.NewVBox(
			widget.NewForm(widget.NewFormItem(t("Most files deleted per run"), maxDel), widget.NewFormItem(t("Most deleted, % of the destination"), maxPct)),
			newText(t("A mirror runs by hand only after a preview of this version (saving makes one)."), mutedText, 11, false))
		presets := [][2]string{{"", t("Only by hand")}, {"0 1 * * *", t("Every day at 01:00")}, {"0 */4 * * *", t("Every 4 hours")}, {"0 2 * * 0", t("Sundays at 02:00")}}
		var labels []string
		for _, p := range presets {
			labels = append(labels, p[1])
		}
		labels = append(labels, t("Custom (cron)…"))
		when := newSelect(labels, nil)
		cron := newEntry()
		cron.SetPlaceHolder("30 22 * * 1-5")
		cron.SetText(j.Cron)
		nextRuns := newText("", mutedText, 11, false)
		cronOK := true
		checkCron := func(c string) {
			cronOK = true
			if c == "" {
				nextRuns.Text = ""
				nextRuns.Refresh()
				return
			}
			var out struct {
				Cron struct {
					OK   bool        `json:"ok"`
					Next []time.Time `json:"next"`
				} `json:"cron"`
			}
			u.bgDo(func() error {
				return u.panel.do(http.MethodPost, base+"/syncs/validate", map[string]any{"cron": c}, &out)
			}, func() {
				cronOK = out.Cron.OK
				if !cronOK {
					nextRuns.Text, nextRuns.Color = t("Not a valid schedule (min hour day month weekday)."), errText
				} else {
					var ts []string
					for _, n := range out.Cron.Next {
						ts = append(ts, n.Local().Format("Mon 02/01 15:04"))
					}
					nextRuns.Text, nextRuns.Color = tf("Next: %s", strings.Join(ts, " · ")), mutedText
				}
				nextRuns.Refresh()
			})
		}
		when.OnChanged = func(v string) {
			for _, p := range presets {
				if p[1] == v {
					cron.SetText(p[0])
					cron.Hide()
					checkCron(p[0])
					return
				}
			}
			cron.Show()
		}
		cron.OnChanged = func(c string) { checkCron(strings.TrimSpace(c)) }
		chosen := labels[len(labels)-1]
		for _, p := range presets {
			if p[0] == j.Cron {
				chosen = p[1]
			}
		}
		when.SetSelected(chosen)
		name := newEntry()
		if j.Name == "" {
			j.Name = path.Base("/"+j.Source.Share+"/"+j.Source.Path) + " · " + j.Source.Host
		}
		name.SetText(j.Name)
		id := newEntry()
		slug := func(s string) string {
			s = strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
			if len(s) > 32 {
				s = s[:32]
			}
			return s
		}
		if isNew {
			id.SetText(slug(j.Name))
			name.OnChanged = func(s string) { id.SetText(slug(s)) }
		} else {
			id.SetText(j.ID)
			id.Disable()
		}
		exclude := newMultiLineEntry()
		exclude.SetText(strings.Join(j.Exclude, "\n"))
		exclude.SetMinRowsVisible(2)
		bw, transfers := newEntry(), newEntry()
		bw.SetText(j.BwLimit)
		transfers.SetText(strconv.Itoa(j.Transfers))
		verify := newCheck(t("Compare the content after the run (reads the source again)"), nil)
		verify.SetChecked(j.Verify)
		reinherit := newCheck(t("Apply the destination folder's Windows permissions to what came"), nil)
		reinherit.SetChecked(j.Reinherit)
		advanced := widget.NewAccordion(widget.NewAccordionItem(t("Advanced"), widget.NewForm(
			widget.NewFormItem(t("Leave out (one pattern per line)"), exclude),
			widget.NewFormItem(t("Bandwidth limit (e.g. 10M, or 08:00,2M 19:00,off)"), bw),
			widget.NewFormItem(t("Simultaneous transfers"), transfers),
			widget.NewFormItem("", verify), widget.NewFormItem("", reinherit), widget.NewFormItem(t("Id"), id))))
		confirm := newEntry()
		confirmItem := container.NewVBox(newText(tf("A mirror deletes in the destination. Type %s to confirm", t("MIRROR")), warnText, 11, false), confirm)
		mode.OnChanged = func(v string) {
			mirrorBox.Hidden, confirmItem.Hidden = v != mirrorM, v != mirrorM
			mirrorBox.Refresh()
			confirmItem.Refresh()
		}
		if j.Mode == "mirror" {
			mode.SetSelected(mirrorM)
		} else {
			mode.SetSelected(copyM)
		}
		who := t("machine account")
		if j.Source.Auth == "ntlm" {
			who = j.Source.User
			if j.Source.Domain != "" {
				who += "@" + j.Source.Domain
			}
		}
		src := unc() + " · " + who
		if srcSize != nil {
			src += " · " + tf("%d files · %s", srcSize.Files, sizeText(srcSize.Bytes))
		}
		dst := j.Dest
		if destFree != nil {
			dst += " · " + tf("%s free", sizeText(int64(*destFree)))
		}
		summary := widget.NewForm(widget.NewFormItem(t("Source"), widget.NewLabel(src)), widget.NewFormItem(t("Destination"), widget.NewLabel(dst)))
		save := func(preview bool) {
			if !cronOK || (isNew && !destOK) {
				u.showInfo(t("Sync"), t("Not a valid schedule (min hour day month weekday)."))
				return
			}
			nj := j
			nj.Name, nj.ID, nj.Cron = strings.TrimSpace(name.Text), strings.TrimSpace(id.Text), strings.TrimSpace(cron.Text)
			nj.Enabled, nj.Mode = nj.Cron != "", "copy"
			if mode.Selected == mirrorM {
				nj.Mode = "mirror"
				if !strings.EqualFold(strings.TrimSpace(confirm.Text), t("MIRROR")) {
					u.showInfo(t("Mirror"), tf("A mirror deletes in the destination. Type %s to confirm", t("MIRROR")))
					return
				}
				nj.Confirm = nj.ID // the word was typed: the agent wants the job's id
			}
			nj.MaxDelete, _ = strconv.Atoi(maxDel.Text)
			nj.MaxDeletePct, _ = strconv.Atoi(maxPct.Text)
			nj.KeepDays, _ = strconv.Atoi(keepDays.Text)
			nj.KeepGiB, _ = strconv.Atoi(keepGiB.Text)
			nj.Transfers, _ = strconv.Atoi(transfers.Text)
			nj.BwLimit, nj.Verify, nj.Reinherit, nj.Exclude = strings.TrimSpace(bw.Text), verify.Checked, reinherit.Checked, nil
			for _, x := range strings.Split(exclude.Text, "\n") {
				if x = strings.TrimSpace(x); x != "" {
					nj.Exclude = append(nj.Exclude, x)
				}
			}
			nj.Password = ""
			if nj.Source.Auth == "ntlm" {
				nj.Password = password
			} else {
				nj.PasswordFile = ""
			}
			u.bgDo(func() error {
				if err := u.panel.do(http.MethodPut, base+"/syncs/"+esc(nj.ID), nj, nil); err != nil {
					return err
				}
				if preview {
					return u.panel.do(http.MethodPost, base+"/syncs/"+esc(nj.ID)+"/preview", nil, nil)
				}
				return nil
			}, func() {
				if preview {
					u.setStatus(t("Sync job saved; preview queued."))
				} else {
					u.setStatus(t("Sync job saved."))
				}
				closeIt()
				done()
			})
		}
		saveBtn := newButton(t("Save and preview"), theme.MediaPlayIcon(), func() { save(true) })
		saveBtn.Importance = widget.HighImportance
		onlySave := newButton(t("Only save"), theme.DocumentSaveIcon(), func() { save(false) })
		back := newButton(t("Back"), theme.NavigateBackIcon(), func() { show(2) })
		return container.NewVBox(mode, keepBox, mirrorBox, widget.NewForm(widget.NewFormItem(t("When"), when), widget.NewFormItem("", cron), widget.NewFormItem(t("Name"), name)),
			nextRuns, summary, advanced, confirmItem, container.NewHBox(back, saveBtn, onlySave))
	}

	show = func(n int) {
		setHeader(n)
		var c fyne.CanvasObject
		switch n {
		case 0:
			c = step1()
		case 1:
			c = step2()
		case 2:
			c = step3()
		default:
			c = step4()
		}
		body.Objects = []fyne.CanvasObject{container.NewVScroll(c)}
		body.Refresh()
	}
	first := 0
	if !isNew {
		first = 3
		destOK = true
	}
	show(first)
	title := t("New sync job")
	if !isNew {
		title = tf("Edit %s", j.Name)
	}
	box := container.NewBorder(header, nil, nil, nil, container.NewGridWrap(fyne.NewSize(760, 500), body))
	closeIt = u.showModal(title, box, t("Close"), false, 800, nil)
}
