package cmd

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// syncJobRow and syncRunRow mirror GET /api/agents/:id/syncs (only what the CLI shows).
type syncJobRow struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Mode   string `json:"mode"`
	Dest   string `json:"dest"`
	Cron   string `json:"cron"`
	Source struct {
		Host  string `json:"host"`
		Share string `json:"share"`
		Path  string `json:"path"`
		Auth  string `json:"auth"`
	} `json:"source"`
}

type syncRunRow struct {
	ID       string    `json:"id"`
	Job      string    `json:"job"`
	Preview  bool      `json:"preview"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
	Status   string    `json:"status"`
	Error    string    `json:"error"`
	Result   *struct {
		Plan      struct{ New, Changed, Delete, Same int } `json:"plan"`
		Transfers int64                                    `json:"transfers"`
		Deleted   int64                                    `json:"deleted"`
	} `json:"result"`
}

// syncAgent finds a registered server by name and returns its API path.
func syncAgent(call func(method, path string, body, out any) error, name string) (string, error) {
	var agents []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := call(http.MethodGet, "/api/agents", nil, &agents); err != nil {
		return "", err
	}
	for _, a := range agents {
		if strings.EqualFold(a.Name, name) {
			return fmt.Sprintf("/api/agents/%d", a.ID), nil
		}
	}
	return "", fmt.Errorf("server %q is not in the panel", name)
}

// runFinished is when a run ended (with the date when not the day it started) and how long it took; "—" while it
// goes on.
func runFinished(r syncRunRow) string {
	if r.Finished.IsZero() || r.Status == "running" || r.Status == "queued" {
		return "—"
	}
	s, f := r.Started.Local(), r.Finished.Local()
	at := f.Format("15:04")
	if s.Year() != f.Year() || s.YearDay() != f.YearDay() {
		at = f.Format("2006-01-02 15:04")
	}
	if r.Started.IsZero() {
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

var syncCmd = &cobra.Command{Use: "sync", Short: "Folder sync (through the panel: AD_PANEL_URL, AD_USER, AD_PASSWORD)"}

func init() {
	session := func() (func(method, path string, body, out any) error, func(), error) {
		user, pass, err := credentials()
		if err != nil {
			return nil, nil, err
		}
		_, call, logout, err := panelSessionAs(user, pass)
		return call, logout, err
	}
	list := &cobra.Command{Use: "list <server>", Short: "A server's jobs and latest runs", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call, logout, err := session()
			if err != nil {
				return err
			}
			defer logout()
			base, err := syncAgent(call, args[0])
			if err != nil {
				return err
			}
			var d struct {
				Jobs []syncJobRow `json:"jobs"`
				Runs []syncRunRow `json:"runs"`
			}
			if err := call(http.MethodGet, base+"/syncs", nil, &d); err != nil {
				return err
			}
			if err := show(cmd.OutOrStdout(), d.Jobs, "ID\tMODE\tSOURCE\tDESTINATION\tSCHEDULE", func(t io.Writer) {
				for _, j := range d.Jobs {
					src := `\\` + j.Source.Host + `\` + j.Source.Share + strings.ReplaceAll("/"+j.Source.Path, "/", `\`)
					fmt.Fprintf(t, "%s\t%s\t%s (%s)\t%s\t%s\n", j.ID, j.Mode, strings.TrimSuffix(src, `\`), j.Source.Auth, j.Dest, j.Cron)
				}
			}); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout())
			return show(cmd.OutOrStdout(), d.Runs, "RUN\tJOB\tKIND\tSTATUS\tFINISHED\tNEW/CHANGED/DELETE\tTRANSFERRED", func(t io.Writer) {
				for _, r := range d.Runs[:min(len(d.Runs), 20)] {
					kind, plan, done := "run", "—", "—"
					if r.Preview {
						kind = "preview"
					}
					if r.Result != nil {
						plan = fmt.Sprintf("%d/%d/%d", r.Result.Plan.New, r.Result.Plan.Changed, r.Result.Plan.Delete)
						done = fmt.Sprint(r.Result.Transfers)
					}
					st := r.Status
					if r.Error != "" {
						st += ": " + tr(r.Error)
					}
					fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.ID, r.Job, kind, st, runFinished(r), plan, done)
				}
			})
		}}
	start := func(preview bool) *cobra.Command {
		use, short := "run <server> <job>", "Run a job now (a mirror needs a preview of its current revision)"
		if preview {
			use, short = "preview <server> <job>", "Preview: the full plan, writing nothing"
		}
		return &cobra.Command{Use: use, Short: short, Args: cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				call, logout, err := session()
				if err != nil {
					return err
				}
				defer logout()
				base, err := syncAgent(call, args[0])
				if err != nil {
					return err
				}
				what := "run"
				if preview {
					what = "preview"
				}
				var r syncRunRow
				if err := call(http.MethodPost, base+"/syncs/"+url.PathEscape(args[1])+"/"+what, nil, &r); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s queued: %s (follow it with ad-manager sync list %s)\n", what, r.ID, args[0])
				return nil
			}}
	}
	syncCmd.AddCommand(list, start(true), start(false))
	rootCmd.AddCommand(syncCmd)
}
