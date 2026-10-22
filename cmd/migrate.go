package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"

	"ad-manager/internal/ad"

	"github.com/spf13/cobra"
)

// migrateCmd groups the DC migration helpers; errFound signals a non-empty diff.
var (
	migrateCmd = &cobra.Command{Use: "migrate", Short: "DC migration helpers (read the source; shares --apply and passwords --apply write on the destination)"}
	invOut     string
	errFound   = errors.New("differences found")
)

func init() {
	preflight := &cobra.Command{Use: "preflight", Short: "Check the configured DC's ports, levels, schema and FSMO", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			u, err := url.Parse(cfg.AD.URL)
			if err != nil {
				return err
			}
			checks := ad.Ports(u.Hostname())
			c, err := dial()
			if err != nil {
				checks = append(checks, ad.Check{Item: "authentication", Status: ad.StatusBlocker, Detail: err.Error()})
			} else {
				defer c.Close()
				more, err := c.Preflight()
				if err != nil {
					return err
				}
				checks = append(checks, more...)
			}
			checks = append(checks, ad.Check{Item: "test origin", Status: ad.StatusWarning,
				Detail: "DC↔DC connectivity (RPC 135 + 49152-65535) is only proven by running this on the future DC, on the LAN"})
			blocked := false
			for _, ch := range checks {
				blocked = blocked || ch.Status == ad.StatusBlocker
			}
			err = show(cmd.OutOrStdout(), checks, "STATUS\tITEM\tDETAIL", func(t io.Writer) {
				for _, ch := range checks {
					fmt.Fprintf(t, "%s\t%s\t%s\n", tr(ch.Status), tr(ch.Item), tr(ch.Detail))
				}
			})
			if err == nil && blocked {
				err = errors.New("some items are blockers")
			}
			return err
		}}

	inventory := &cobra.Command{Use: "inventory", Short: "Export the inventory as JSON (no secrets)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run("", "", func(c *ad.Conn) error {
				inv, err := c.Inventory(cfg.AD.URL)
				if err != nil {
					return err
				}
				w := cmd.OutOrStdout()
				if invOut != "" {
					f, err := os.OpenFile(invOut, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
					if err != nil {
						return err
					}
					defer f.Close()
					w = f
				}
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				if err := enc.Encode(inv); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "inventory: %v\n", inv.Counts)
				return nil
			})
		}}
	inventory.Flags().StringVarP(&invOut, "output", "o", "", "output file (default stdout)")

	diff := &cobra.Command{Use: "diff <a.json> <b.json>", Short: "Compare two inventories (exit status 1 when they differ)", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var inv [2]ad.Inventory
			for i, p := range args {
				b, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				if err := json.Unmarshal(b, &inv[i]); err != nil {
					return fmt.Errorf("%s: %w", p, err)
				}
				if inv[i].Source == "" || inv[0].Source == inv[1].Source {
					inv[i].Source = p
				}
			}
			d := ad.Diff(&inv[0], &inv[1])
			for _, l := range d {
				fmt.Fprintln(cmd.OutOrStdout(), tr(l))
			}
			if len(d) > 0 {
				return fmt.Errorf("%w: %d", errFound, len(d))
			}
			fmt.Fprintln(cmd.OutOrStdout(), "equivalent inventories")
			return nil
		}}
	migrateCmd.AddCommand(preflight, inventory, diff)
}
