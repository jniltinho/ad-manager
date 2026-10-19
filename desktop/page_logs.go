package main

import (
	"fmt"
	"image/color"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ── Logs: audit trail and Samba logs ───────────────────────────────────────

// auditEvent is one line of the panel's audit.log (GET /api/audit).
type auditEvent struct {
	Time   string `json:"time"`
	Actor  string `json:"actor"`
	IP     string `json:"ip"`
	Action string `json:"action"`
	Target string `json:"target"`
	Result string `json:"result"`
}

// sambaFile and sambaEntry mirror internal/sambalog (GET …/samba/logs[/:name]).
type (
	sambaFile struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	}
	sambaEntry struct {
		Time    string `json:"time"`
		Level   int    `json:"level"`
		Source  string `json:"source"`
		Message string `json:"message"`
	}
)

func (u *ui) logsPage() fyne.CanvasObject {
	tabs := newTabs(tabItem{t("Audit"), theme.DocumentIcon(), u.auditView()})
	loaders := map[int]func(){}
	addSamba := func(label string, a *api) {
		view, load := u.sambaLogsView(a)
		loaders[len(tabs.items)] = load
		tabs.Append(tabItem{label, theme.StorageIcon(), view})
	}
	addSamba(t("Samba — this server"), u.panel)
	for _, ag := range u.cfg.Agents {
		addSamba(tf("Samba — %s", ag.Name), newAPI(strings.TrimRight(ag.URL, "/"), ag.Token, ag.Insecure))
	}
	// Samba tabs load the first time they are opened (no request to every agent at login).
	tabs.OnSelected = func(i int) {
		if load, ok := loaders[i]; ok {
			delete(loaders, i)
			load()
		}
	}
	return container.NewPadded(tabs.View())
}

// auditView lists the audit trail: every change made through the panel, CLI or agent.
func (u *ui) auditView() fyne.CanvasObject {
	var all, visible []auditEvent
	search := newEntry()
	search.SetPlaceHolder(t("Filter by operator, target, IP…"))
	search.ActionItem = widget.NewIcon(theme.SearchIcon())
	failures := newCheck(t("failures only"), nil)
	count := newText("", mutedText, 12, false)
	tab := newTable([]column{{t("Date"), 140, false}, {t("Operator"), 140, false}, {t("Source"), 120, false},
		{t("Action"), 150, false}, {t("Target"), 240, false}, {t("Result"), 110, true}},
		func() int { return len(visible) },
		func(r, c int) (string, color.Color) {
			e := visible[r]
			switch c {
			case 0:
				if tm, err := time.Parse(time.RFC3339, e.Time); err == nil {
					return tm.Local().Format(dateLayout()), nil
				}
				return e.Time, nil
			case 1:
				return e.Actor, strongText
			case 2:
				return e.IP, nil
			case 3:
				return e.Action, nil
			case 4:
				return e.Target, nil
			}
			if e.Result == "ok" {
				return "ok", okText
			}
			return e.Result, errText
		}, func(r int) {
			if e := visible[r]; e.Result != "ok" { // the error is usually longer than the column
				u.showInfo(e.Action+" — "+e.Target, e.Result)
			}
		})
	apply := func() {
		q := strings.ToLower(strings.TrimSpace(search.Text))
		visible = visible[:0]
		for _, e := range all {
			if (q == "" || strings.Contains(strings.ToLower(strings.Join([]string{e.Actor, e.IP, e.Target, e.Result, e.Action}, " ")), q)) &&
				(!failures.Checked || e.Result != "ok") {
				visible = append(visible, e)
			}
		}
		tab.clearSelection()
		tab.Refresh()
		count.Text = tf("%d of %d (newest first)", len(visible), len(all))
		count.Refresh()
	}
	reload := func() {
		var r []auditEvent
		u.bgDo(func() error { return u.panel.do(http.MethodGet, "/api/audit?limit=2000", nil, &r) }, func() { all = r; apply() })
	}
	search.OnChanged = func(string) { apply() }
	failures.OnChanged = func(bool) { apply() }
	reload()
	top := container.NewBorder(nil, nil, nil, container.NewHBox(failures, container.NewCenter(count),
		newButton(t("Refresh"), theme.ViewRefreshIcon(), reload)), search)
	return card(t("Audit"), container.NewBorder(top, nil, nil, nil, tab.view()))
}

// sambaLogsView is the Samba log viewer of one host (the panel's own or an
// agent): files on the left, the newest entries of the chosen file on the
// right. load fetches the file list; nothing is read before it is called.
func (u *ui) sambaLogsView(a *api) (view fyne.CanvasObject, load func()) {
	var files []sambaFile
	var entries []sambaEntry
	current := ""
	dir := newText("", mutedText, 11, false)
	listErr := wrappedLabel("")
	listErr.Importance = widget.DangerImportance
	listErr.Hide()
	search := newEntry()
	search.SetPlaceHolder(t("Search in the messages…"))
	search.ActionItem = widget.NewIcon(theme.SearchIcon())
	sizes := []string{tf("last %d", 200), tf("last %d", 500), tf("last %d", 2000)}
	lines := newSelect(sizes, nil)
	lines.SetSelected(sizes[1])
	count := newText("", mutedText, 12, false)
	tab := newTable([]column{{t("Date"), 190, false}, {t("Level"), 60, true}, {t("Message"), 400, false}},
		func() int { return len(entries) },
		func(r, c int) (string, color.Color) {
			e := entries[r]
			switch c {
			case 0:
				if e.Time == "" {
					return "—", mutedText
				}
				return e.Time, nil
			case 1:
				switch {
				case e.Level < 0:
					return "—", mutedText
				case e.Level == 0:
					return "0", errText
				case e.Level <= 2:
					return strconv.Itoa(e.Level), warnText
				}
				return strconv.Itoa(e.Level), strongText
			}
			return strings.ReplaceAll(e.Message, "\n", " ⏎ "), nil
		}, func(r int) { // full message (cells show one truncated line)
			e := entries[r]
			u.showModal(e.Time, container.NewVBox(newText(e.Source, mutedText, 11, false), wrappedLabel(e.Message)), "OK", false, 720, nil)
		})
	loadEntries := func() {
		entries = nil
		tab.clearSelection()
		tab.Refresh()
		if current == "" {
			count.Text = t("Choose a file.")
			count.Refresh()
			return
		}
		n := []int{200, 500, 2000}[max(lines.SelectedIndex(), 0)]
		q := url.Values{"lines": {strconv.Itoa(n)}, "q": {strings.TrimSpace(search.Text)}}
		name := current
		var r []sambaEntry
		count.Text = t("Loading…")
		count.Refresh()
		u.bgDo(func() error { return a.do(http.MethodGet, "/api/samba/logs/"+esc(name)+"?"+q.Encode(), nil, &r) }, func() {
			if name != current {
				return // another file was chosen meanwhile
			}
			entries = r
			tab.Refresh()
			count.Text = tf("%d entries (newest first)", len(r))
			count.Refresh()
		})
	}
	list := widget.NewList(func() int { return len(files) },
		func() fyne.CanvasObject {
			return container.NewBorder(nil, nil, nil, newText("", mutedText, 10, false), newText("", strongText, 12, false))
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			c := o.(*fyne.Container)
			name, size := c.Objects[0].(*canvas.Text), c.Objects[1].(*canvas.Text)
			f := files[id]
			name.Text, size.Text = f.Name, humanBytes(f.Size)
			name.Color = strongText
			if f.Size == 0 {
				name.Color = mutedText
			}
			name.Refresh()
			size.Refresh()
		})
	list.OnSelected = func(id widget.ListItemID) {
		current = files[id].Name
		loadEntries()
	}
	load = func() {
		var r struct {
			Dir   string      `json:"dir"`
			Files []sambaFile `json:"files"`
			Error string      `json:"error"`
		}
		u.bgDo(func() error { return a.do(http.MethodGet, "/api/samba/logs", nil, &r) }, func() {
			files = r.Files
			dir.Text = r.Dir
			dir.Refresh()
			listErr.SetText(r.Error)
			if r.Error != "" {
				listErr.Show()
			} else {
				listErr.Hide()
			}
			list.UnselectAll()
			list.Refresh()
			pick := -1
			for i, f := range files {
				if f.Name == current || (pick < 0 && current == "" && f.Size > 0) {
					pick = i
				}
			}
			if pick < 0 {
				current = ""
				loadEntries()
				return
			}
			list.Select(pick) // loads the entries
		})
	}
	search.OnSubmitted = func(string) { loadEntries() }
	lines.OnChanged = func(string) { loadEntries() }
	refresh := newButton(t("Refresh"), theme.ViewRefreshIcon(), func() { load() })
	left := card(t("Files"), container.NewBorder(container.NewVBox(container.NewPadded(dir), listErr), nil, nil, nil, list))
	top := container.NewBorder(nil, nil, nil, container.NewHBox(fixedWidth(lines, 130), container.NewCenter(count), refresh), search)
	// wrapping label: a canvas.Text this long would force the window wider
	note := wrappedLabel(t("Times as written by Samba, in the server time zone. Read-only, never changed here."))
	note.Importance = widget.LowImportance
	split := container.NewHSplit(left, card(t("Entries"), container.NewBorder(top, note, nil, nil, tab.view())))
	split.Offset = 0.22
	return split, load
}

// humanBytes formats a size in B/KB/MB/GB/TB with one decimal (decimal comma in Portuguese).
func humanBytes(n int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	v, i := float64(n), 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	s := fmt.Sprintf("%.0f", v)
	if i > 0 {
		s = strings.TrimSuffix(fmt.Sprintf("%.1f", v), ".0")
	}
	if lang != "en" {
		s = strings.ReplaceAll(s, ".", ",")
	}
	return s + " " + units[i]
}
