package main

import (
	"bytes"
	"fmt"
	"image/color"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ── Sync ────────────────────────────────────────────────────────────────────

type syncSource struct {
	Host   string `json:"host"`
	Port   int    `json:"port,omitempty"`
	Share  string `json:"share"`
	Path   string `json:"path,omitempty"`
	Auth   string `json:"auth"`
	User   string `json:"user,omitempty"`
	Domain string `json:"domain,omitempty"`
}

// syncJob mirrors the agent's sync job (GET /api/agents/:id/syncs).
type syncJob struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Rev          int        `json:"rev"`
	Source       syncSource `json:"source"`
	PasswordFile string     `json:"password_file,omitempty"`
	Dest         string     `json:"dest"`
	Mode         string     `json:"mode"`
	Exclude      []string   `json:"exclude,omitempty"`
	BwLimit      string     `json:"bwlimit,omitempty"`
	Transfers    int        `json:"transfers,omitempty"`
	MaxDelete    int        `json:"max_delete"`
	MaxDeletePct int        `json:"max_delete_pct"`
	Verify       bool       `json:"verify,omitempty"`
	Reinherit    bool       `json:"reinherit"`
	KeepDays     int        `json:"keep_days,omitempty"`
	KeepGiB      int        `json:"keep_gib,omitempty"`
	Cron         string     `json:"cron,omitempty"`
	Enabled      bool       `json:"enabled"`
	Approved     int        `json:"approved_rev,omitempty"`
	PasswordSet  bool       `json:"password_set,omitempty"`
	Password     string     `json:"password,omitempty"` // typed in the form: kept encrypted by the panel and the agent
	Confirm      string     `json:"confirm,omitempty"`
}

type syncPlan struct {
	New, Changed, Delete, Same, Errors  int
	NewBytes, ChangedBytes, DeleteBytes int64
	Sample                              map[string][]string `json:"sample"`
}

type syncRun struct {
	ID       string    `json:"id"`
	Job      string    `json:"job"`
	Preview  bool      `json:"preview"`
	Origin   string    `json:"origin"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
	Status   string    `json:"status"`
	Error    string    `json:"error"`
	Notes    []string  `json:"notes"`
	Quarant  string    `json:"quarantine"`
	Progress struct {
		Transfers int64 `json:"transfers"`
		Bytes     int64 `json:"bytes"`
	} `json:"progress"`
	Result *struct {
		Plan            syncPlan `json:"plan"`
		Transfers       int64    `json:"transfers"`
		Bytes           int64    `json:"bytes"`
		Deleted         int64    `json:"deleted"`
		Checked         bool     `json:"checked"`
		ContentVerified bool     `json:"content_verified"`
		Seconds         float64  `json:"seconds"`
	} `json:"result"`
}

type syncList struct {
	Jobs       []syncJob `json:"jobs"`
	Runs       []syncRun `json:"runs"`
	Roots      []string  `json:"roots"`
	FileServer bool      `json:"file_server"`
	Kerberos   bool      `json:"kerberos"`
	Machine    string    `json:"machine"`
}

func syncStatusText(s string) (string, color.Color) {
	switch s {
	case "ok":
		return t("ok"), okText
	case "error":
		return t("error"), errText
	case "cancelled":
		return t("cancelled"), mutedText
	case "queued":
		return t("queued"), warnText
	}
	return t("running"), warnText
}

func (u *ui) syncPage() fyne.CanvasObject {
	type agent struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	// label tells apart servers with look-alike names: where each agent answers
	label := func(a agent) string {
		h := a.URL
		if p, err := url.Parse(a.URL); err == nil {
			h = p.Hostname()
		}
		if h == "127.0.0.1" || h == "localhost" || h == "::1" {
			h = t("This server")
		}
		return a.Name + " · " + h
	}
	var agents []agent
	var cur agent
	var data syncList
	jobSel, runSel := -1, -1
	serverPick := newSelect(nil, nil)
	info := newText("", mutedText, 12, false)
	jobName := func(id string) string {
		for _, j := range data.Jobs {
			if j.ID == id && j.Name != "" {
				return j.Name
			}
		}
		return id
	}
	jobs := newTable([]column{{t("Name"), 200, false}, {t("Source"), 300, false}, {t("Destination"), 220, false}, {t("Mode"), 140, false}, {t("When (cron)"), 110, false}},
		func() int { return len(data.Jobs) },
		func(r, c int) (string, color.Color) {
			j := data.Jobs[r]
			switch c {
			case 0:
				return jobName(j.ID), strongText
			case 1:
				auth := "Kerberos"
				if j.Source.Auth == "ntlm" {
					auth = "NTLM"
				}
				return `\\` + j.Source.Host + `\` + j.Source.Share + strings.ReplaceAll("/"+j.Source.Path, "/", `\`) + " · " + auth, nil
			case 2:
				return j.Dest, nil
			case 3:
				if j.Mode == "mirror" {
					if j.Approved != j.Rev {
						return t("mirror") + " · " + t("preview needed"), warnText
					}
					return t("mirror"), nil
				}
				return t("copy"), nil
			}
			if j.Enabled && j.Cron != "" {
				return j.Cron, nil
			}
			return "—", mutedText
		}, func(r int) { jobSel = r })
	runs := newTable([]column{{t("Date"), 165, false}, {t("Finished"), 220, false}, {t("Job"), 160, false}, {t("Kind"), 90, false}, {t("Status"), 200, false}, {t("Files"), 80, true}, {t("Deleted"), 80, true}},
		func() int { return len(data.Runs) },
		func(r, c int) (string, color.Color) {
			x := data.Runs[r]
			switch c {
			case 0:
				return x.Started.Local().Format(dateLayout()), strongText
			case 1:
				if f := syncFinished(x); f != "—" {
					return f, nil
				}
				return "—", mutedText
			case 2:
				return jobName(x.Job), nil
			case 3:
				if x.Preview {
					return t("preview"), mutedText
				}
				return t("run"), mutedText
			case 4:
				s, cl := syncStatusText(x.Status)
				if x.Error != "" {
					s += " · " + serverText(x.Error)
				}
				return s, cl
			case 5:
				if x.Result == nil {
					if x.Status == "running" {
						return strconv.FormatInt(x.Progress.Transfers, 10), nil
					}
					return "—", mutedText
				}
				if x.Preview {
					return strconv.Itoa(x.Result.Plan.New + x.Result.Plan.Changed), nil
				}
				return strconv.FormatInt(x.Result.Transfers, 10), nil
			}
			if x.Result == nil {
				return "—", mutedText
			}
			if x.Preview {
				return strconv.Itoa(x.Result.Plan.Delete), nil
			}
			return strconv.FormatInt(x.Result.Deleted, 10), nil
		}, func(r int) { runSel = r })
	base := func() string { return fmt.Sprintf("/api/agents/%d", cur.ID) }
	var reload func()
	reload = func() {
		if cur.ID == 0 {
			return
		}
		var d syncList
		id := cur.ID
		u.bgDo(func() error { return u.panel.do(http.MethodGet, fmt.Sprintf("/api/agents/%d/syncs", id), nil, &d) }, func() {
			data, jobSel, runSel = d, -1, -1
			jobs.clearSelection()
			runs.clearSelection()
			jobs.Refresh()
			runs.Refresh()
			info.Text = ""
			if !d.FileServer {
				info.Text = t("Folders are synced into a file server.")
			}
			info.Refresh()
			for _, r := range d.Runs {
				if r.Status == "queued" || r.Status == "running" {
					time.AfterFunc(3*time.Second, func() { fyne.Do(reload) })
					break
				}
			}
		})
	}
	serverPick.OnChanged = func(n string) {
		for _, a := range agents {
			if label(a) == n {
				cur = a
				reload()
			}
		}
	}
	u.bgDo(func() error { return u.panel.do(http.MethodGet, "/api/agents", nil, &agents) }, func() {
		var names []string
		for _, a := range agents {
			names = append(names, label(a))
		}
		serverPick.SetOptions(names)
		if len(names) > 0 {
			serverPick.SetSelected(names[0])
		}
	})
	selectedJob := func() (syncJob, bool) {
		if jobSel < 0 || jobSel >= len(data.Jobs) {
			return syncJob{}, false
		}
		return data.Jobs[jobSel], true
	}
	newBtn := newButton(t("New job"), theme.ContentAddIcon(), func() {
		if cur.ID == 0 {
			return
		}
		root := "/srv/shares"
		if len(data.Roots) > 0 {
			root = data.Roots[0]
		}
		_ = root
		u.syncWizard(cur.ID, data, nil, reload)
	})
	newBtn.Importance = widget.HighImportance
	editBtn := newButton(t("Edit"), theme.DocumentCreateIcon(), func() {
		if j, ok := selectedJob(); ok {
			u.syncWizard(cur.ID, data, &j, reload)
		}
	})
	start := func(preview bool) func() {
		return func() {
			j, ok := selectedJob()
			if !ok {
				return
			}
			what := "run"
			if preview {
				what = "preview"
			}
			u.bgDo(func() error { return u.panel.do(http.MethodPost, base()+"/syncs/"+esc(j.ID)+"/"+what, nil, nil) }, func() {
				if preview {
					u.setStatus(t("Preview queued."))
				} else {
					u.setStatus(t("Sync queued."))
				}
				time.AfterFunc(1500*time.Millisecond, func() { fyne.Do(reload) })
			})
		}
	}
	previewBtn := newButton(t("Preview"), theme.VisibilityIcon(), start(true))
	runBtn := newButton(t("Run now"), theme.MediaPlayIcon(), start(false))
	delBtn := newButton(t("Delete"), theme.DeleteIcon(), func() {
		j, ok := selectedJob()
		if !ok {
			return
		}
		u.confirmTyped(t("Delete"), tf("Delete the sync job %s? The files already brought stay.", jobName(j.ID)), j.ID, func() {
			u.bgDo(func() error { return u.panel.do(http.MethodDelete, base()+"/syncs/"+esc(j.ID), nil, nil) }, reload)
		})
	})
	delBtn.Importance = widget.DangerImportance
	quarantineBtn := newButton(t("Quarantine"), theme.HistoryIcon(), func() {
		if j, ok := selectedJob(); ok {
			u.syncQuarantine(cur.ID, j)
		}
	})
	selectedRun := func() (syncRun, bool) {
		if runSel < 0 || runSel >= len(data.Runs) {
			return syncRun{}, false
		}
		return data.Runs[runSel], true
	}
	detailsBtn := newButton(t("Details"), theme.InfoIcon(), func() {
		r, ok := selectedRun()
		if !ok || r.Result == nil {
			return
		}
		p := r.Result.Plan
		lines := []string{
			fmt.Sprintf("%s: %d (%s)", t("New"), p.New, sizeText(p.NewBytes)),
			fmt.Sprintf("%s: %d (%s)", t("Changed"), p.Changed, sizeText(p.ChangedBytes)),
			fmt.Sprintf("%s: %d (%s)", t("To delete"), p.Delete, sizeText(p.DeleteBytes)),
			fmt.Sprintf("%s: %d", t("Unchanged"), p.Same),
		}
		if !r.Preview {
			v := t("not compared")
			if r.Result.ContentVerified {
				v = t("content compared")
			} else if r.Result.Checked {
				v = t("sizes and dates compared")
			}
			lines = append(lines, fmt.Sprintf("%s: %d (%s) · %s", t("Transferred"), r.Result.Transfers, sizeText(r.Result.Bytes), v))
		}
		if r.Quarant != "" {
			lines = append(lines, t("Quarantine")+": "+r.Quarant)
		}
		for _, g := range [][2]string{{"new", "New"}, {"changed", "Changed"}, {"delete", "To delete"}, {"error", "Errors"}} {
			if s := p.Sample[g[0]]; len(s) > 0 {
				lines = append(lines, "", t(g[1])+":", strings.Join(s, "\n"))
			}
		}
		for _, n := range r.Notes {
			lines = append(lines, "· "+serverText(n))
		}
		u.showInfo(r.ID, strings.Join(lines, "\n"))
	})
	logBtn := newButton(t("Log"), theme.DocumentIcon(), func() {
		r, ok := selectedRun()
		if !ok {
			return
		}
		var b bytes.Buffer
		path := base() + "/sync-runs/" + esc(r.ID) + "/log"
		u.bgDo(func() error { return u.panel.getRaw(path, &b) }, func() {
			txt := b.String()
			if txt == "" {
				txt = t("No log.")
			}
			u.showInfo(r.ID, txt)
		})
	})
	cancelBtn := newButton(t("Cancel"), theme.MediaStopIcon(), func() {
		r, ok := selectedRun()
		if !ok || r.Status != "running" {
			return
		}
		u.bgDo(func() error { return u.panel.do(http.MethodPost, base()+"/sync-runs/"+esc(r.ID)+"/cancel", nil, nil) }, reload)
	})
	top := container.NewBorder(nil, nil, nil, container.NewHBox(container.NewCenter(info), newButton("", theme.ViewRefreshIcon(), reload)), fixedWidth(serverPick, 320))
	jobsCard := card(t("Sync jobs"), container.NewBorder(container.NewVBox(top, container.NewHBox(newBtn, editBtn, previewBtn, runBtn, quarantineBtn, delBtn)), nil, nil, nil, jobs.view()))
	runsCard := card(t("Runs"), container.NewBorder(container.NewHBox(detailsBtn, logBtn, cancelBtn), nil, nil, nil, runs.view()))
	split := container.NewVSplit(jobsCard, runsCard)
	split.SetOffset(0.45)
	return container.NewPadded(split)
}

// pickFrom shows items to choose one from.
func (u *ui) pickFrom(title string, items []string, pick func(string)) {
	if len(items) == 0 {
		u.showInfo(title, t("No folders inside."))
		return
	}
	sel := -1
	list := widget.NewList(func() int { return len(items) }, func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) { o.(*widget.Label).SetText(items[i]) })
	list.OnSelected = func(i widget.ListItemID) { sel = i }
	box := container.NewGridWrap(fyne.NewSize(520, 320), list)
	u.showModal(title, box, t("choose"), false, 560, func() bool {
		if sel < 0 {
			return false
		}
		pick(items[sel])
		return true
	})
}

// syncFinished is when a run ended (with the date when not the day it started) and how long it took, as the panel
// shows it; "—" while it goes on.
func syncFinished(x syncRun) string {
	if x.Finished.IsZero() || x.Status == "running" || x.Status == "queued" {
		return "—"
	}
	s, f := x.Started.Local(), x.Finished.Local()
	at := f.Format("15:04")
	if lang == "en" {
		at = f.Format("03:04 PM")
	}
	if s.Year() != f.Year() || s.YearDay() != f.YearDay() {
		at = f.Format(dateLayout())
	}
	if x.Started.IsZero() {
		return at
	}
	return at + " · " + shortDuration(f.Sub(s))
}

// shortDuration is a run's length, short: 38s, 4m38s, 1h02m.
func shortDuration(d time.Duration) string {
	n := max(0, int(d.Round(time.Second)/time.Second))
	h, m := n/3600, n%3600/60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh%02dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm%02ds", m, n%60)
	}
	return fmt.Sprintf("%ds", n)
}
