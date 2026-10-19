package main

import (
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type quarantine struct {
	Name    string    `json:"name"`
	Job     string    `json:"job"`
	Run     string    `json:"run"`
	Dest    string    `json:"dest"`
	Created time.Time `json:"created"`
	Files   int       `json:"files"`
	Size    int64     `json:"size"`
}

type diskSpace struct {
	Root  string `json:"root"`
	Free  int64  `json:"free"`
	Total int64  `json:"total"`
	Low   bool   `json:"low"`
}

type quarantineList struct {
	Quarantines []quarantine `json:"quarantines"`
	Space       []diskSpace  `json:"space"`
}

type quarantineFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	InDest bool   `json:"in_dest"`
}

// syncQuarantine shows the quarantines of a job (what its runs deleted or replaced): files go back to the run's
// destination (a file there already swaps places only with "replace"), one downloads, and the job's whole
// quarantine empties with a typed confirmation.
func (u *ui) syncQuarantine(agent int64, j syncJob) {
	base := fmt.Sprintf("/api/agents/%d/syncs", agent)
	var all quarantineList
	u.bgDo(func() error { return u.panel.do(http.MethodGet, base+"/quarantines", nil, &all) }, func() {
		var qs []quarantine
		for _, q := range all.Quarantines {
			if q.Job == j.ID {
				qs = append(qs, q)
			}
		}
		var warn []string
		for _, s := range all.Space {
			if s.Low {
				warn = append(warn, tf("Little free space on %s: %s of %s.", s.Root, sizeText(s.Free), sizeText(s.Total)))
			}
		}
		u.showQuarantine(base, j, qs, warn)
	})
}

func (u *ui) showQuarantine(base string, j syncJob, qs []quarantine, warn []string) {
	name := j.Name
	if name == "" {
		name = j.ID
	}
	days := j.KeepDays
	if days == 0 {
		days = 30
	}
	head := container.NewVBox(newText(tf("What the runs deleted or replaced, kept %d days. Files go back to where they were in the destination of that run.", days), mutedText, 11, false))
	for _, w := range warn {
		head.Add(newText(w, warnText, 11, true))
	}
	if len(qs) == 0 {
		head.Add(newText(t("The quarantine is empty."), mutedText, 12, false))
		u.showModal(tf("Quarantine of %s", name), head, "", false, 760, nil)
		return
	}
	var cur quarantine
	var files []quarantineFile
	sel := -1
	list := widget.NewList(func() int { return len(files) }, func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			f := files[i]
			txt := f.Path + "  ·  " + sizeText(f.Size)
			if f.InDest {
				txt += "  ·  " + t("in the destination")
			}
			o.(*widget.Label).SetText(txt)
		})
	list.OnSelected = func(i widget.ListItemID) { sel = i }
	asked := 0 // the newest list asked for: an older answer that arrives late is dropped
	load := func(q quarantine) {
		asked++
		n := asked
		cur, sel, files = q, -1, nil // nothing of the one before stays actionable while this loads
		list.UnselectAll()
		list.Refresh()
		var out struct {
			Files     []quarantineFile `json:"files"`
			Truncated bool             `json:"truncated"`
		}
		u.bgDo(func() error { return u.panel.do(http.MethodGet, base+"/quarantines/"+esc(q.Name)+"/files", nil, &out) }, func() {
			if n != asked {
				return
			}
			files = out.Files
			list.UnselectAll()
			list.Refresh()
		})
	}
	var labels []string
	for _, q := range qs {
		labels = append(labels, fmt.Sprintf("%s  ·  %d %s  ·  %s", q.Created.Local().Format("02/01/2006 15:04"), q.Files, t("files"), sizeText(q.Size)))
	}
	pick := newSelect(labels, func(l string) {
		for i, x := range labels {
			if x == l {
				load(qs[i])
			}
		}
	})
	replace := newCheck(t("Replace files in the destination (the current ones go into the quarantine in their place)"), nil)
	var closeIt func()
	giveBack := func(all bool) {
		paths := []string{}
		if !all {
			if sel < 0 || sel >= len(files) {
				return
			}
			paths = []string{files[sel].Path}
		}
		var r struct {
			Restored  int      `json:"restored"`
			Swapped   int      `json:"swapped"`
			Conflicts []string `json:"conflicts"`
			Conflict  int      `json:"conflict_count"`
			Errors    []string `json:"errors"`
			Failed    int      `json:"error_count"`
		}
		q := cur // the answer belongs to this one, whatever is picked meanwhile
		u.bgDo(func() error {
			return u.panel.withTimeout(30*time.Minute).do(http.MethodPost, base+"/quarantines/"+esc(q.Name)+"/restore", map[string]any{"paths": paths, "replace": replace.Checked}, &r)
		}, func() {
			lines := []string{tf("%d files given back.", r.Restored+r.Swapped)}
			if r.Swapped > 0 {
				lines = append(lines, tf("%d files of the destination went into the quarantine in their place.", r.Swapped))
			}
			if r.Conflict > 0 {
				lines = append(lines, tf("%d files are in the destination already (check \"replace\" to swap them):", r.Conflict))
				lines = append(lines, r.Conflicts[:min(len(r.Conflicts), 20)]...)
			}
			for _, e := range r.Errors[:min(len(r.Errors), 20)] {
				lines = append(lines, serverText(e))
			}
			u.showInfo(t("Give back"), strings.Join(lines, "\n"))
			if cur.Name == q.Name {
				load(q)
			}
		})
	}
	backOne := newButton(t("Give back selected"), theme.MailReplyIcon(), func() { giveBack(false) })
	backAll := newButton(t("Give back all"), theme.MailReplyAllIcon(), func() { giveBack(true) })
	backAll.Importance = widget.HighImportance
	download := newButton(t("Download"), theme.DownloadIcon(), func() {
		if sel < 0 || sel >= len(files) {
			return
		}
		f := files[sel]
		p := base + "/quarantines/" + esc(cur.Name) + "/file?path=" + url.QueryEscape(f.Path)
		save := dialog.NewFileSave(func(w fyne.URIWriteCloser, err error) {
			if err != nil || w == nil {
				return
			}
			go func() {
				defer w.Close()
				err := u.panel.getRaw(p, w)
				fyne.Do(func() {
					if err != nil {
						u.handleError(err)
						return
					}
					u.setStatus(tf("Saved to %s", w.URI().Path()))
				})
			}()
		}, u.w)
		save.SetFileName(path.Base(f.Path))
		save.Show()
	})
	empty := newButton(t("Empty the quarantine"), theme.DeleteIcon(), func() {
		var total int64
		for _, q := range qs {
			total += q.Size
		}
		u.confirmTyped(t("Empty the quarantine"), tf("Remove every quarantine of %s (%s)? Nothing there can be given back afterwards.", name, sizeText(total)), j.ID, func() {
			var r struct {
				Removed int   `json:"removed"`
				Freed   int64 `json:"freed"`
			}
			u.bgDo(func() error {
				return u.panel.withTimeout(30*time.Minute).do(http.MethodPost, base+"/"+esc(j.ID)+"/quarantine/empty", map[string]any{"confirm": j.ID}, &r)
			}, func() {
				u.setStatus(tf("%d quarantines removed (%s).", r.Removed, sizeText(r.Freed)))
				if closeIt != nil {
					closeIt()
				}
			})
		})
	})
	empty.Importance = widget.DangerImportance
	body := container.NewBorder(container.NewVBox(head, pick),
		container.NewVBox(replace, container.NewHBox(backOne, backAll, download, layout.NewSpacer(), empty)), nil, nil,
		container.NewGridWrap(fyne.NewSize(720, 300), list))
	closeIt = u.showModal(tf("Quarantine of %s", name), body, "", false, 760, nil)
	pick.SetSelectedIndex(0)
}
