package cmd

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"ad-manager/internal/syncer"

	"github.com/spf13/cobra"
)

// agentSyncWorkerCmd is the child the agent starts for each sync preview or run (rclone keeps process-wide state:
// one process per run). The spec comes on standard input (with the NTLM password, never on the command line);
// progress and the result go to standard output as JSON lines, rclone's log to standard error.
var agentSyncWorkerCmd = &cobra.Command{
	Use:    "sync-worker",
	Short:  "Internal use by the agent: one sync run",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		restrictUmask() // what it creates is root's alone until the destination's Windows ACL is applied again
		var sp syncer.Spec
		if err := json.NewDecoder(io.LimitReader(os.Stdin, 1<<20)).Decode(&sp); err != nil {
			return err
		}
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
		defer stop()
		if sp.Confined {
			if err := syncer.CheckConfinement(sp); err != nil {
				return json.NewEncoder(os.Stdout).Encode(map[string]any{"error": err.Error()})
			}
		}
		if sp.Browse && sp.Check {
			return json.NewEncoder(os.Stdout).Encode(map[string]any{"steps": syncer.Check(ctx, sp)})
		}
		if sp.Browse && sp.Size {
			files, size, err := syncer.Size(ctx, sp)
			out := map[string]any{"files": files, "bytes": size}
			if err != nil {
				out["error"] = err.Error()
			}
			return json.NewEncoder(os.Stdout).Encode(out)
		}
		if sp.Browse {
			dirs, err := syncer.Browse(ctx, sp)
			out := map[string]any{"dirs": dirs}
			if err != nil {
				out["error"] = err.Error()
			}
			return json.NewEncoder(os.Stdout).Encode(out)
		}
		var mu sync.Mutex
		enc := json.NewEncoder(os.Stdout)
		emit := func(v any) {
			mu.Lock()
			defer mu.Unlock()
			_ = enc.Encode(v)
		}
		res, err := syncer.Run(ctx, sp, func(p map[string]any) { emit(map[string]any{"progress": p}) })
		out := map[string]any{"result": res}
		if err != nil {
			out["error"] = err.Error()
		}
		emit(out)
		return nil
	},
}

func init() { agentCmd.AddCommand(agentSyncWorkerCmd) }
