package cmd

import (
	"os"

	"ad-manager/internal/winrpc"

	"github.com/spf13/cobra"
)

// winrpcCmd is the panel's RPC helper process: one request on stdin, one answer on stdout (internal).
var winrpcCmd = &cobra.Command{
	Use:    "winrpc",
	Short:  "Panel helper process for DNS and sessions over RPC (internal use)",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return winrpc.Serve(cmd.Context(), os.Stdin, os.Stdout)
	},
}

func init() { rootCmd.AddCommand(winrpcCmd) }
