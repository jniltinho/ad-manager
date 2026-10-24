package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"strings"

	"ad-manager/internal/server"
	"ad-manager/internal/store"

	"github.com/spf13/cobra"
)

var importCredOpts struct {
	user  string
	clear bool
}

// importCredentialCmd keeps the import credential (the target domain's administrator the DC agent's GPO import and
// restore use) sealed in the panel's database, as Settings → Import from an existing AD does: the password is read
// from the first line of stdin, never from an argument. Run it as the panel's service user.
var importCredentialCmd = &cobra.Command{
	Use:   "import-credential",
	Short: "Keep the import credential (target administrator) sealed in the panel's database; password on stdin",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		st, err := store.Open(cfg.DB.Path)
		if err != nil {
			return fmt.Errorf("panel database: %w", err)
		}
		defer st.Close()
		if importCredOpts.clear {
			if err := server.StoreImportCredential(cfg, st, "", ""); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "import credential removed")
			return nil
		}
		if importCredOpts.user == "" {
			return errors.New("--user is required (or --clear)")
		}
		line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		pw := strings.TrimRight(line, "\r\n")
		if pw == "" {
			if err != nil {
				return fmt.Errorf("password on stdin: %w", err)
			}
			return errors.New("password on stdin: empty")
		}
		if err := server.StoreImportCredential(cfg, st, importCredOpts.user, pw); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "import credential kept for %s (sealed)\n", importCredOpts.user)
		return nil
	},
}

func init() {
	importCredentialCmd.Flags().StringVar(&importCredOpts.user, "user", "", "the target domain's administrator (e.g. Administrator)")
	importCredentialCmd.Flags().BoolVar(&importCredOpts.clear, "clear", false, "remove the kept credential")
	rootCmd.AddCommand(importCredentialCmd)
}
