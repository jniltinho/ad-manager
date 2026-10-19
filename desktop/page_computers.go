package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ── Computers ───────────────────────────────────────────────────────────────

// computer is a computer account (GET /api/computers).
type computer struct {
	DN        string    `json:"dn"`
	Name      string    `json:"name"`
	Sam       string    `json:"sam"` // NAME$, used in the actions
	DNSName   string    `json:"dns_name"`
	OS        string    `json:"os"`
	OSVersion string    `json:"os_version"`
	Enabled   bool      `json:"enabled"`
	IsDC      bool      `json:"is_dc"`
	LastLogon time.Time `json:"last_logon"`
}

// legacyOS matches systems without support (NTLMv1/SMB1/Netlogon risks), as in the web panel.
var legacyOS = regexp.MustCompile(`XP|2003|Windows Server® 2008 Standard|Windows 7`)

// inactiveDays is the configured "no logon for N days" limit (90 when unset).
func (u *ui) inactiveDays() int {
	if u.me.Settings.InactiveDays > 0 {
		return u.me.Settings.InactiveDays
	}
	return 90
}

// bgDo runs work off the UI thread, then shows its error or runs done on the UI thread.
func (u *ui) bgDo(work func() error, done func()) {
	go func() {
		err := work()
		fyne.Do(func() {
			if err != nil {
				u.handleError(err)
				return
			}
			if done != nil {
				done()
			}
		})
	}()
}

func (u *ui) computersPage() fyne.CanvasObject {
	var all, visible []computer
	sel := -1
	allSystems := t("All systems")
	search := newEntry()
	search.SetPlaceHolder(t("Filter by name…"))
	search.ActionItem = widget.NewIcon(theme.SearchIcon())
	osFilter := newSelect([]string{allSystems}, nil)
	osFilter.SetSelected(allSystems)
	onlyInactive := newCheck(tf("no logon for %d+ days", u.inactiveDays()), nil)
	count := newText("", mutedText, 12, false)
	isInactive := func(c computer) bool {
		return time.Since(c.LastLogon) > time.Duration(u.inactiveDays())*24*time.Hour
	}
	osLabel := func(s string) string {
		if s == "" {
			return t("not reported")
		}
		return s
	}
	var toggle, move, del *volButton
	var updateButtons func()
	tab := newTable([]column{{t("Name"), 180, false}, {t("System"), 260, false}, {t("Version"), 120, false},
		{t("Status"), 110, true}, {"DC", 50, true}, {t("Last logon (approx.)"), 160, false}},
		func() int { return len(visible) },
		func(r, c int) (string, color.Color) {
			x := visible[r]
			switch c {
			case 0:
				return x.Name, strongText
			case 1:
				if legacyOS.MatchString(x.OS) {
					return x.OS, errText
				}
				return x.OS, nil
			case 2:
				return x.OSVersion, nil
			case 3:
				if !x.Enabled {
					return strings.ToUpper(t("deactivated")), mutedText
				}
				return strings.ToUpper(t("active")), okText
			case 4:
				if x.IsDC {
					return "DC", strongText
				}
				return "", nil
			}
			if x.LastLogon.IsZero() {
				return t("never"), warnText
			}
			if isInactive(x) {
				return x.LastLogon.Local().Format(dateLayout()), warnText
			}
			return x.LastLogon.Local().Format(dateLayout()), nil
		}, func(r int) { sel = r; updateButtons() })
	updateButtons = func() {
		if sel >= 0 && sel < len(visible) && !visible[sel].Enabled {
			toggle.SetText(t("Enable"))
		} else {
			toggle.SetText(t("Disable"))
		}
		for _, b := range []*volButton{toggle, move, del} {
			if sel < 0 || sel >= len(visible) || visible[sel].IsDC {
				b.Disable() // DCs are never disabled, moved or deleted here
			} else {
				b.Enable()
			}
		}
	}
	apply := func() {
		q := strings.ToLower(strings.TrimSpace(search.Text))
		visible = visible[:0]
		for _, x := range all {
			if (q == "" || strings.Contains(strings.ToLower(x.Name+" "+x.DNSName), q)) &&
				(osFilter.Selected == allSystems || osLabel(x.OS) == osFilter.Selected) &&
				(!onlyInactive.Checked || isInactive(x)) {
				visible = append(visible, x)
			}
		}
		sel = -1
		tab.clearSelection()
		tab.Refresh()
		updateButtons()
		count.Text = tf("%d of %d", len(visible), len(all))
		count.Refresh()
	}
	reload := func() {
		var r []computer
		u.bgDo(func() error { return u.panel.do(http.MethodGet, "/api/computers", nil, &r) }, func() {
			slices.SortFunc(r, func(a, b computer) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
			all = r
			systems := []string{allSystems}
			for _, x := range r {
				if l := osLabel(x.OS); !slices.Contains(systems, l) {
					systems = append(systems, l)
				}
			}
			slices.Sort(systems[1:])
			osFilter.SetOptions(systems)
			if !slices.Contains(systems, osFilter.Selected) {
				osFilter.SetSelected(allSystems) // runs apply
				return
			}
			apply()
		})
	}
	search.OnChanged = func(string) { apply() }
	osFilter.OnChanged = func(string) { apply() }
	onlyInactive.OnChanged = func(bool) { apply() }
	selected := func() (computer, bool) {
		if sel < 0 || sel >= len(visible) {
			return computer{}, false
		}
		return visible[sel], true
	}
	toggle = newButton(t("Disable"), theme.MediaPauseIcon(), func() {
		x, ok := selected()
		if !ok || x.IsDC {
			return
		}
		run := func(verb, msg string) {
			u.bgDo(func() error { return u.panel.do(http.MethodPost, "/api/computers/"+esc(x.Sam)+"/"+verb, nil, nil) },
				func() { u.setStatus(tf(msg, x.Name)); reload() })
		}
		if !x.Enabled {
			run("enable", "%s enabled.")
			return
		}
		u.confirm(t("Disable computer"), tf("Disable the computer %s? Users will not be able to log on to it with a domain account.", x.Name),
			t("Disable"), true, func() { run("disable", "%s disabled.") })
	})
	move = newButton(t("Move"), theme.MailForwardIcon(), func() {
		x, ok := selected()
		if !ok || x.IsDC {
			return
		}
		var ous []orgUnit
		u.bgDo(func() (err error) { ous, err = u.fetchOUs(); return err }, func() {
			pick, dn := ouSelect(ous, parentDN(x.DN))
			body := widget.NewForm(widget.NewFormItem(t("Computer"), widget.NewLabel(x.Name)), widget.NewFormItem(t("Move to"), pick))
			u.showModal(tf("Move %s", x.Name), body, t("Move"), false, 560, func() bool {
				if dn() == "" {
					return false
				}
				target := dn()
				u.bgDo(func() error {
					return u.panel.do(http.MethodPost, "/api/computers/"+esc(x.Sam)+"/move", map[string]string{"ou": target}, nil)
				}, func() { u.setStatus(tf("%s moved to %s.", x.Name, ouPath(target))); reload() })
				return true
			})
		})
	})
	del = newButton(t("Delete"), theme.DeleteIcon(), func() {
		x, ok := selected()
		if !ok || x.IsDC {
			return
		}
		u.confirmTyped(t("Delete computer"), tf("Delete the computer account %s? It leaves the domain and will have to rejoin.", x.Name), x.Name, func() {
			u.bgDo(func() error { return u.panel.do(http.MethodDelete, "/api/computers/"+esc(x.Sam), nil, nil) },
				func() { u.setStatus(tf("%s deleted.", x.Name)); reload() })
		})
	})
	del.Importance = widget.DangerImportance
	export := newButton(t("Export CSV"), theme.DownloadIcon(), func() { u.downloadExport("computers", t("computers.csv")) })
	updateButtons()
	reload()
	top := container.NewBorder(nil, nil, nil, container.NewHBox(fixedWidth(osFilter, 240), onlyInactive, container.NewCenter(count), export,
		newButton("", theme.ViewRefreshIcon(), reload)), search)
	// actions above the table, like Users
	actions := container.NewHBox(toggle, move, del)
	return container.NewPadded(card(t("Domain computers"), container.NewBorder(container.NewVBox(top, actions), nil, nil, nil, tab.view())))
}

// downloadExport saves GET /api/export/<file> (the CSV the CLI import reads)
// to a file the operator picks; name is the suggested file name.
func (u *ui) downloadExport(file, name string) {
	save := dialog.NewFileSave(func(w fyne.URIWriteCloser, err error) {
		if err != nil || w == nil {
			return
		}
		go func() {
			defer w.Close()
			err := u.panel.getRaw("/api/export/"+file, w)
			fyne.Do(func() {
				if err != nil {
					u.handleError(err)
					return
				}
				u.setStatus(tf("Exported to %s", w.URI().Path()))
			})
		}()
	}, u.w)
	save.SetFileName(name)
	save.Show()
}

// getRaw copies the body of GET path (not JSON, e.g. a CSV) to w.
func (a *api) getRaw(path string, w io.Writer) error {
	req, err := http.NewRequest(http.MethodGet, a.base+path, nil)
	if err != nil {
		return err
	}
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	hc := *a.hc // a download (a backup) can take longer than the API calls' total timeout
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
	_, err = io.Copy(w, resp.Body)
	return err
}
