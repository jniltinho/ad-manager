package cmd

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"ad-manager/internal/ad"
	"ad-manager/internal/agent"
	"ad-manager/internal/audit"
	"ad-manager/internal/shares"
	"ad-manager/internal/winshares"

	"github.com/spf13/cobra"
)

// sharesOpts holds the flags of `migrate shares`.
var sharesOpts struct {
	host, inv, driveMap, apply, csv string
	insecure                        bool
}

func init() {
	c := &cobra.Command{Use: "shares", Short: "Read the Windows server's shares and recreate them on Samba through the agent",
		Long: `Reads (read-only, rpcclient/smbcacls) the Windows server's data shares, computes the
effective access (share permission ∩ NTFS of the root) and shows the preview.
With --apply it sends each one to the Samba server's agent (AGENT_TOKEN in the environment); the
folder is created empty, no files are copied. Windows credential: AD_USER/AD_PASSWORD.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if sharesOpts.host == "" || sharesOpts.inv == "" {
				return errors.New("give --host and --inventory")
			}
			d, err := loadDirectory(sharesOpts.inv)
			if err != nil {
				return err
			}
			auth, err := writeAuthFile()
			if err != nil {
				return err
			}
			defer os.Remove(auth)
			ws, err := winshares.Collect(cmd.Context(), winshares.Exec, sharesOpts.host, auth)
			if err != nil {
				return err
			}
			pre, dst, ok := strings.Cut(sharesOpts.driveMap, "=")
			if !ok {
				return errors.New(`--map in the form 'E:\=/srv/shares'`)
			}
			plans := winshares.Plan(ws, d, map[string]string{pre: dst})
			if err := show(cmd.OutOrStdout(), plans, "SHARE\tPATH\tFULL CONTROL\tWRITE\tREAD\tWARNINGS", func(t io.Writer) {
				for _, p := range plans {
					fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%s\t%s\n", p.Name, p.Path, strings.Join(append(p.Full, p.FullUser...), ", "),
						strings.Join(append(p.RW, p.RWUsers...), ", "),
						strings.Join(append(p.RO, p.ROUsers...), ", "), tr(strings.Join(p.Warnings, "; ")))
				}
			}); err != nil {
				return err
			}
			if sharesOpts.csv != "" {
				if err := writeSharesCSV(sharesOpts.csv, plans); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "CSV written to %s (panel: Import/export → Folders).\n", sharesOpts.csv)
			}
			if sharesOpts.apply == "" {
				fmt.Fprintln(os.Stderr, "preview only; use --apply https://server:9443 to create them, or --csv file.csv to import in the panel.")
				return nil
			}
			return applyShares(cmd.Context(), plans)
		}}
	f := c.Flags()
	f.StringVar(&sharesOpts.host, "host", "", "source Windows server (e.g. 10.0.0.10)")
	f.StringVar(&sharesOpts.inv, "inventory", "", "inventory JSON (migrate inventory) for groups and members")
	f.StringVar(&sharesOpts.driveMap, "map", `E:\=/srv/shares`, "path prefix map Windows=Linux")
	f.StringVar(&sharesOpts.apply, "apply", "", "URL of the agent that creates the shares")
	f.StringVar(&sharesOpts.csv, "csv", "", "write the folders and permissions to a CSV to import in the panel")
	f.BoolVar(&sharesOpts.insecure, "insecure", false, "accept the agent's self-signed certificate")
	migrateCmd.AddCommand(c)
}

// loadDirectory builds the group/user directory from an inventory JSON file.
func loadDirectory(path string) (winshares.Directory, error) {
	var inv ad.Inventory
	b, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(b, &inv)
	}
	if err != nil {
		return winshares.Directory{}, fmt.Errorf("inventory: %w", err)
	}
	d := winshares.Directory{NetBIOS: strings.ToUpper(strings.Split(inv.Domain, ".")[0]), Groups: inv.Members, Users: map[string]bool{}}
	if v := os.Getenv("AD_NETBIOS"); v != "" {
		d.NetBIOS = v
	}
	for _, u := range inv.Users {
		d.Users[strings.ToLower(u.Sam)] = true
	}
	return d, nil
}

// writeAuthFile writes the credentials to a temporary 0600 file (kept out of
// argv) and returns its path.
func writeAuthFile() (string, error) {
	user := os.Getenv("AD_USER")
	if user == "" {
		return "", errors.New("set AD_USER (e.g. administrator@example.local)")
	}
	pass := os.Getenv("AD_PASSWORD")
	if pass == "" {
		var err error
		if pass, err = readSecret("Password for " + user + ": "); err != nil {
			return "", err
		}
	}
	return winshares.AuthFile("", user, pass, "")
}

// applyShares sends one share:create per share, in order (parents before
// children), and waits for each task to finish before the next one.
func applyShares(ctx context.Context, plans []winshares.SharePlan) error {
	tok := os.Getenv("AGENT_TOKEN")
	if tok == "" {
		return errors.New("set AGENT_TOKEN")
	}
	af, err := audit.Open(cfg.Server.AuditLog)
	if err != nil {
		return fmt.Errorf("audit log unavailable (%s): %w", cfg.Server.AuditLog, err)
	}
	defer af.Close()
	cl := &http.Client{Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport:     &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: sharesOpts.insecure, MinVersion: tls.VersionTLS12}}} //nolint:gosec // opt-in (--insecure)
	base := strings.TrimRight(sharesOpts.apply, "/")
	call := func(method, path string, body, out any) error {
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, err := http.NewRequestWithContext(ctx, method, base+path, rd)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		resp, err := cl.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode >= 300 {
			return fmt.Errorf("agent: %s: %s", resp.Status, strings.TrimSpace(string(b)))
		}
		return json.Unmarshal(b, out)
	}
	var failed int
	for _, p := range plans {
		req := shares.ShareReq{Name: p.Name, Path: p.Path, Comment: p.Comment, RW: p.RW, RO: p.RO,
			RWUsers: p.RWUsers, ROUsers: p.ROUsers, Full: p.Full, FullUsers: p.FullUser, Hidden: strings.HasSuffix(p.Name, "$"), CreatePath: true}
		payload, _ := json.Marshal(req)
		var ti agent.TaskInfo
		err := call(http.MethodPost, "/api/tasks", agent.TaskReq{Type: agent.TypeShare, Payload: payload}, &ti)
		for i := 0; err == nil && i < 120 && ti.State != "completed" && ti.State != "archived"; i++ {
			time.Sleep(time.Second)
			err = call(http.MethodGet, "/api/tasks/"+ti.ID, nil, &ti)
		}
		if err == nil && ti.State != "completed" {
			err = fmt.Errorf("task %s: %s %s", ti.ID, ti.State, ti.LastErr)
		}
		audit.Log(os.Getenv("AD_USER"), "cli", "migrate.shares", p.Name, err)
		if err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "✗ %s: %v\n", p.Name, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "✓ %s → %s\n", p.Name, p.Path)
	}
	if failed > 0 {
		return fmt.Errorf("%d share(s) failed", failed)
	}
	return nil
}

// writeSharesCSV writes the plans as a folders-and-permissions CSV for the
// panel import.
func writeSharesCSV(path string, plans []winshares.SharePlan) error {
	rows := winshares.CSVRows(plans)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if err := ad.WriteCSV(f, shares.CSVHeader, rows); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
