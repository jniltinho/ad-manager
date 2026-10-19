package cmd

import (
	"fmt"

	"ad-manager/internal/buildinfo"

	"github.com/spf13/cobra"
)

// versionCmd prints the build metadata.
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show the version, commit and build time",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintf(cmd.OutOrStdout(), "ad-manager %s (commit %s, built %s)\n",
			buildinfo.Version, buildinfo.Commit, buildinfo.BuildTime)
		return nil
	},
}
