package cmd

import (
	"os/signal"
	"syscall"

	"ad-manager/internal/server"

	"github.com/spf13/cobra"
)

// insecureDev allows plain HTTP on loopback (server and agent).
var insecureDev bool

// serverCmd starts the web panel.
var serverCmd = &cobra.Command{
	Use:     "server",
	Aliases: []string{"serve"},
	Short:   "Start the web panel (HTTPS)",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		return server.Run(ctx, cfg, insecureDev)
	},
}

func init() {
	serverCmd.Flags().BoolVar(&insecureDev, "insecure-dev", false, "allow plain HTTP without TLS (only when listening on loopback)")
}
