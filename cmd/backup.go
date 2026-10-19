package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

// backupServer and backupItem mirror GET /api/backups.
type backupServer struct {
	ID      int64        `json:"id"`
	Name    string       `json:"name"`
	DC      bool         `json:"dc"`
	Kinds   []string     `json:"kinds"`
	Backups []backupItem `json:"backups"`
	Error   string       `json:"error,omitempty"`
}

type backupItem struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Origin  string    `json:"origin"`
	By      string    `json:"by,omitempty"`
	Full    bool      `json:"full"`
	Size    int64     `json:"size"`
	Started time.Time `json:"started"`
	Status  string    `json:"status"`
	Error   string    `json:"error,omitempty"`
}

// backupsList reads the backups of every server through the panel.
func backupsList(call func(method, path string, body, out any) error) ([]backupServer, error) {
	var list []backupServer
	return list, call(http.MethodGet, "/api/backups", nil, &list)
}

// backupTarget finds a server by name ("panel" for the panel) and returns its API path of backups.
func backupTarget(list []backupServer, name string) (backupServer, string, error) {
	for _, s := range list {
		if strings.EqualFold(s.Name, name) || (name == "panel" && s.ID == 0) {
			if s.ID == 0 {
				return s, "/api/backups/panel", nil
			}
			return s, fmt.Sprintf("/api/agents/%d/backups", s.ID), nil
		}
	}
	return backupServer{}, "", fmt.Errorf("server %q is not in the panel (see ad-manager backup list)", name)
}

// backupRun asks the panel for a backup of server (kind: its first kind when empty).
func backupRun(call func(method, path string, body, out any) error, server, kind string, folders []string, full bool) (string, error) {
	list, err := backupsList(call)
	if err != nil {
		return "", err
	}
	s, path, err := backupTarget(list, server)
	if err != nil {
		return "", err
	}
	if len(s.Kinds) == 0 {
		return "", fmt.Errorf("%s has no backups", server)
	}
	if kind == "" {
		kind = s.Kinds[0]
	}
	if s.ID == 0 {
		var m backupItem
		if err := call(http.MethodPost, path, nil, &m); err != nil {
			return "", err
		}
		return "backup " + m.ID + " done", nil
	}
	var ti struct{ ID string }
	if err := call(http.MethodPost, path, map[string]any{"kind": kind, "folders": folders, "full": full}, &ti); err != nil {
		return "", err
	}
	return "backup " + kind + " queued on " + s.Name + " (task " + ti.ID + ")", nil
}

var backupCmd = &cobra.Command{Use: "backup", Short: "Samba and panel backups (through the panel: AD_PANEL_URL, AD_USER, AD_PASSWORD)"}

var backupOpts struct {
	full    bool
	kind    string
	folders []string
	target  string
	parts   []string
	confirm string
}

// backupRestore asks the panel to restore backup id of server; confirm is the end of the id (required when the
// restoration replaces what is in use).
func backupRestore(call func(method, path string, body, out any) error, server, id, target string, folders, parts []string, confirm string) (string, error) {
	list, err := backupsList(call)
	if err != nil {
		return "", err
	}
	s, path, err := backupTarget(list, server)
	if err != nil {
		return "", err
	}
	kind := ""
	for _, b := range s.Backups {
		if b.ID == id {
			kind = b.Kind
		}
	}
	if kind == "" {
		return "", fmt.Errorf("backup %s is not on %s (see ad-manager backup list)", id, server)
	}
	if (kind != "fileserver-data" || target == "original") && confirm == "" {
		return "", fmt.Errorf("this restore replaces what is in use: confirm with --confirm %s", id[max(0, len(id)-8):])
	}
	var out struct {
		ID      string `json:"id"`
		Restart bool   `json:"restart"`
	}
	body := map[string]any{"target": target, "folders": folders, "parts": parts, "confirm": confirm}
	if err := call(http.MethodPost, path+"/"+url.PathEscape(id)+"/restore", body, &out); err != nil {
		return "", err
	}
	if out.Restart {
		return "the panel restarts with the restored database", nil
	}
	return "restore queued on " + s.Name + " (task " + out.ID + "); follow it with ad-manager backup list", nil
}

func init() {
	session := func() (func(method, path string, body, out any) error, func(), error) {
		user, pass, err := credentials() // AD_USER, AD_PASSWORD or the terminal
		if err != nil {
			return nil, nil, err
		}
		_, call, logout, err := panelSessionAs(user, pass)
		return call, logout, err
	}
	list := &cobra.Command{Use: "list", Short: "List the backups of each server", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			call, logout, err := session()
			if err != nil {
				return err
			}
			defer logout()
			servers, err := backupsList(call)
			if err != nil {
				return err
			}
			var rows []backupItem
			names := map[string]string{}
			for _, s := range servers {
				name := s.Name
				if s.ID == 0 {
					name = "panel"
				}
				for _, b := range s.Backups {
					rows = append(rows, b)
					names[b.ID] = name
				}
				if s.Error != "" {
					fmt.Fprintf(cmd.ErrOrStderr(), "%s: %s\n", name, tr(s.Error))
				}
			}
			return show(cmd.OutOrStdout(), servers, "SERVER\tID\tKIND\tORIGIN\tSIZE\tSTATUS", func(t io.Writer) {
				for _, b := range rows {
					size := "—"
					if b.Status == "ok" {
						size = fmt.Sprintf("%.1f MiB", float64(b.Size)/(1<<20))
					}
					fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%s\t%s\n", names[b.ID], b.ID, b.Kind, b.Origin, size, b.Status)
				}
			})
		}}
	run := &cobra.Command{Use: "run <server>", Short: "Back up now (server as named in the panel; panel for the panel itself)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call, logout, err := session()
			if err != nil {
				return err
			}
			defer logout()
			msg, err := backupRun(call, args[0], backupOpts.kind, backupOpts.folders, backupOpts.full)
			if err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), msg)
			}
			return err
		}}
	run.Flags().StringVar(&backupOpts.kind, "kind", "", "all (configuration + every root, full), dc, fileserver-config or fileserver-data (default: the server's first kind)")
	run.Flags().BoolVar(&backupOpts.full, "full", false, "fileserver-data: full backup now (new chain)")
	run.Flags().StringSliceVar(&backupOpts.folders, "folder", nil, "fileserver-data: folder inside the share roots (repeatable)")
	rm := &cobra.Command{Use: "rm <server> <id>", Short: "Delete a backup (a full one takes its chain with it)", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			call, logout, err := session()
			if err != nil {
				return err
			}
			defer logout()
			servers, err := backupsList(call)
			if err != nil {
				return err
			}
			_, path, err := backupTarget(servers, args[0])
			if err != nil {
				return err
			}
			var out struct{ Deleted []string }
			if err := call(http.MethodDelete, path+"/"+url.PathEscape(args[1]), nil, &out); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "deleted:", strings.Join(out.Deleted, ", "))
			return nil
		}}
	restore := &cobra.Command{Use: "restore <server> <id>", Short: "Restore a backup (data into a new folder by default; anything else needs --confirm)", Args: cobra.ExactArgs(2),
		Long: `Restores a backup through the panel, audited.
  fileserver-data: --target new (default: <root>/_restored/<date>/) or original (the current folders are backed up first);
                   --folder limits it to the chosen folders of the backup
  fileserver-config: --part shares,acls,smbconf (the current configuration is backed up first)
  samba-files, panel: go back to the backup's state (the current one is kept)
  dc: never over a DC in use — use the initial setup on a new server
--confirm takes the last 8 characters of the id when the restore replaces what is in use.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			call, logout, err := session()
			if err != nil {
				return err
			}
			defer logout()
			msg, err := backupRestore(call, args[0], args[1], backupOpts.target, backupOpts.folders, backupOpts.parts, backupOpts.confirm)
			if err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), msg)
			}
			return err
		}}
	restore.Flags().StringVar(&backupOpts.target, "target", "new", "fileserver-data: new or original")
	restore.Flags().StringSliceVar(&backupOpts.folders, "folder", nil, "fileserver-data: only these folders of the backup (repeatable)")
	restore.Flags().StringSliceVar(&backupOpts.parts, "part", []string{"shares", "acls"}, "fileserver-config: shares, acls, smbconf")
	restore.Flags().StringVar(&backupOpts.confirm, "confirm", "", "the last 8 characters of the backup id")
	upload := &cobra.Command{Use: "upload <server> <file.tar.gz>", Short: "Send a backup to the server (it checks it and lists it as uploaded)", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if backupOpts.kind == "" {
				return errors.New("give --kind (fileserver-config, fileserver-data, samba-files or panel)")
			}
			f, err := os.Open(args[1])
			if err != nil {
				return err
			}
			defer f.Close()
			call, logout, err := session()
			if err != nil {
				return err
			}
			defer logout()
			servers, err := backupsList(call)
			if err != nil {
				return err
			}
			_, path, err := backupTarget(servers, args[0])
			if err != nil {
				return err
			}
			var m backupItem
			if err := call(http.MethodPost, path+"/upload?kind="+url.QueryEscape(backupOpts.kind), f, &m); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "backup uploaded: %s (%.1f MiB)\n", m.ID, float64(m.Size)/(1<<20))
			return nil
		}}
	upload.Flags().StringVar(&backupOpts.kind, "kind", "", "fileserver-config, fileserver-data, samba-files or panel")
	backupCmd.AddCommand(list, run, rm, restore, upload)
	rootCmd.AddCommand(backupCmd)
}

// registerBackupTools adds ad_backup_list and, with write, ad_backup_run (both through the panel).
func registerBackupTools(s *mcp.Server, write bool) {
	type listOut struct {
		Servers []backupServer `json:"servers"`
	}
	mcp.AddTool(s, &mcp.Tool{Name: "ad_backup_list", Description: "Backups of each server (panel and agents): kind, origin, size, status. Uses the panel (AD_PANEL_URL)"},
		func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, listOut, error) {
			_, call, logout, err := panelSession()
			if err != nil {
				return nil, listOut{}, err
			}
			defer logout()
			list, err := backupsList(call)
			return nil, listOut{list}, err
		})
	if !write {
		return
	}
	type runIn struct {
		Server  string   `json:"server" jsonschema:"server name in the panel; panel for the panel itself"`
		Kind    string   `json:"kind,omitempty" jsonschema:"all (configuration + every root, full), dc, fileserver-config or fileserver-data (default: the server's first kind)"`
		Folders []string `json:"folders,omitempty" jsonschema:"fileserver-data: folders inside the share roots"`
	}
	type runOut struct {
		Result string `json:"result"`
	}
	mcp.AddTool(s, &mcp.Tool{Name: "ad_backup_run", Description: "Back up a server now (through the panel, audited). Deleting a backup is not offered here: only in the panel or the CLI"},
		func(_ context.Context, _ *mcp.CallToolRequest, in runIn) (*mcp.CallToolResult, runOut, error) {
			if in.Server == "" {
				return nil, runOut{}, errors.New("give server")
			}
			_, call, logout, err := panelSession()
			if err != nil {
				return nil, runOut{}, err
			}
			defer logout()
			msg, err := backupRun(call, in.Server, in.Kind, in.Folders, false)
			return nil, runOut{msg}, err
		})
}
