package cmd

import (
	"ad-manager/internal/store"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"ad-manager/internal/agent"
	"ad-manager/internal/audit"
	"ad-manager/internal/sambabridge"
	"ad-manager/internal/sambadc"
	"ad-manager/internal/shares"

	"github.com/spf13/cobra"
)

// agentCmd runs the file server agent daemon.
var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "File server daemon: HTTPS API with a token + asynq queue (folders and shares)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		return agent.Run(ctx, cfg, insecureDev)
	},
}

// agentTokenCmd generates a new agent token.
var agentTokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Generate a new agent token (shown once; only its hash is saved in agent.token_file)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		tok, err := agent.NewToken(cfg.Agent.TokenFile)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s\n", tok)
		fmt.Fprintf(cmd.ErrOrStderr(), "hash saved in %s — register this token in the panel (Shares → servers); it will not be shown again.\n", cfg.Agent.TokenFile)
		return nil
	},
}

// agentRegisterLocalCmd registers this server's own agent in this server's panel (a DC that also serves files,
// set up by samba-setup.py): the token on standard input, the panel's database from the configuration. Run as the
// panel's user (its database files stay its own).
var agentRegisterLocalCmd = &cobra.Command{
	Use:    "register-local",
	Short:  "Used by setup: register this server's agent in this server's panel (token on standard input)",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		name, _ := cmd.Flags().GetString("name")
		tok, err := io.ReadAll(io.LimitReader(os.Stdin, 4096))
		if err != nil {
			return err
		}
		token := strings.TrimSpace(string(tok))
		if token == "" || name == "" || cfg.DB.Path == "" {
			return errors.New("register-local: --name, the token on stdin and [db] path are needed")
		}
		st, err := store.Open(cfg.DB.Path)
		if err != nil {
			return err
		}
		defer st.Close()
		list, err := st.Agents()
		if err != nil {
			return err
		}
		a := &store.Agent{Name: name, URL: "https://127.0.0.1:9443", Token: token, Insecure: true} // loopback, self-signed
		for _, x := range list {
			if x.URL == a.URL {
				a.ID = x.ID // set up again: a new token for the same registration
			}
		}
		if err := st.SaveAgent(a); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "agent %q registered in the panel (%s)\n", name, a.URL)
		return nil
	},
}

// agentCheckCmd shows what the Samba Python API offers on this server.
var agentCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Show whether this server writes Windows permissions (python3-samba + acl_xattr)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		c := (&sambabridge.Bridge{}).Capabilities(cmd.Context())
		mode := "POSIX (setfacl)"
		if c.Usable() {
			mode = "Windows (ACL NTFS via Samba)"
		}
		return show(cmd.OutOrStdout(), c, "ITEM\tVALUE", func(t io.Writer) {
			fmt.Fprintf(t, "Samba Python API\t%s\n", yn(c.NTACL))
			fmt.Fprintf(t, "vfs objects = acl_xattr\t%s\n", yn(c.ACLXattr))
			fmt.Fprintf(t, "Samba\t%s\n", c.Samba)
			fmt.Fprintf(t, "Folder permissions\t%s\n", mode)
			if c.Error != "" {
				fmt.Fprintf(t, "Error\t%s\n", tr(c.Error))
			}
		})
	},
}

// localManager is the shares manager of this server, for the commands run on
// the file server itself (as root).
func localManager(cmd *cobra.Command) *shares.Manager {
	b := &sambabridge.Bridge{}
	c := b.Capabilities(cmd.Context())
	b.GoRead = c.GoReadable()
	return agent.NewManager(cfg, b, c)
}

// agentACLCmd shows a folder's Windows permissions.
var agentACLCmd = &cobra.Command{
	Use:   "acl <folder>",
	Short: "Show a folder's Windows permissions (run on the file server, as root)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		acl, err := localManager(cmd).ReadACL(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		label := map[string]string{"full": "full control", "rw": "modify", "ro": "read & execute", "custom": "special"}
		return show(cmd.OutOrStdout(), acl, "PRINCIPAL\tTYPE\tACCESS\tINHERITED", func(t io.Writer) {
			for _, a := range acl.ACEs {
				kind := a.Type
				fmt.Fprintf(t, "%s\t%s\t%s\t%s\n", orDefault(a.Name, a.SID), kind, label[a.Access], yn(a.Inherited))
			}
			fmt.Fprintf(t, "\nOwner: %s · Group: %s · Protected: %s\nSDDL: %s\n", orDefault(acl.OwnerName, acl.Owner),
				orDefault(acl.GroupName, acl.Group), yn(acl.Protected), acl.SDDL)
		})
	},
}

// aclSetOpts are the flags of `agent acl set`.
var aclSetOpts struct{ full, rw, ro, fullUsers, rwUsers, roUsers []string }

// agentACLSetCmd applies permissions to a folder like the panel does.
var agentACLSetCmd = &cobra.Command{
	Use:   "set <folder>",
	Short: "Apply permissions to a folder (created if missing), as the panel does (run on the file server, as root)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		o := aclSetOpts
		r := shares.FolderReq{Path: args[0], Full: o.full, RW: o.rw, RO: o.ro, FullUsers: o.fullUsers, RWUsers: o.rwUsers, ROUsers: o.roUsers}
		if len(r.Full) > 0 {
			r.Owner = r.Full[0]
		} else if len(r.RW) > 0 {
			r.Owner = r.RW[0]
		}
		f, err := audit.Open(cfg.Server.AuditLog)
		if err != nil {
			return fmt.Errorf("audit log unavailable (%s): %w", cfg.Server.AuditLog, err)
		}
		defer f.Close()
		m := localManager(cmd)
		if err := m.ValidateFolder(&r); err != nil {
			return err
		}
		err = m.CreateFolder(cmd.Context(), r)
		audit.Log(currentUser(), "cli", "folder.acl", r.Path, err)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "permissions applied to %s\n", r.Path)
		return nil
	},
}

// agentACLHistoryCmd lists a folder's recorded ACL changes.
var agentACLHistoryCmd = &cobra.Command{
	Use:   "history <folder>",
	Short: "List a folder's recorded permission changes (newest first)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		list, err := localManager(cmd).ACLHistory(args[0])
		if err != nil {
			return err
		}
		return show(cmd.OutOrStdout(), list, "TIME\tWHICH\tSDDL", func(t io.Writer) {
			for _, c := range list {
				what, sddl := "before", c.Before
				if sddl == "" {
					what, sddl = "after", c.After
				}
				fmt.Fprintf(t, "%s\t%s\t%s\n", c.Time, what, sddl)
			}
		})
	},
}

// agentACLRestoreCmd puts back the ACL a folder had before a recorded change.
var agentACLRestoreCmd = &cobra.Command{
	Use:   "restore <folder> <time>",
	Short: "Put back the permissions the folder had before the change recorded at that time (see acl history), also on the content that inherits them",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		f, err := audit.Open(cfg.Server.AuditLog)
		if err != nil {
			return fmt.Errorf("audit log unavailable (%s): %w", cfg.Server.AuditLog, err)
		}
		defer f.Close()
		acl, err := localManager(cmd).RestoreACL(cmd.Context(), shares.RestoreReq{Path: args[0], Time: args[1]})
		audit.Log(currentUser(), "cli", "acl:restore", args[0], err)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "permissions restored on %s (%d items updated, %d skipped)\n", args[0], acl.Propagated, acl.Skipped)
		return nil
	},
}

// agentDCCmd shows the health of this domain controller, read only.
var agentDCCmd = &cobra.Command{
	Use:   "dc",
	Short: "Show this domain controller's replication and database check (dbcheck), read-only (run on the DC, as root)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		run := sambadc.Runner(shares.Exec)
		// each part on its own: the database check is useful precisely when replication cannot be read
		r, rerr := sambadc.ReadReplication(cmd.Context(), run)
		c := sambadc.RunChecks(cmd.Context(), run)
		out := map[string]any{"replication": r, "checks": c}
		if rerr != nil {
			out["replication_error"] = rerr.Error()
		}
		return show(cmd.OutOrStdout(), out, "DIRECTION\tPARTITION\tPARTNER DC\tFAILURES\tLAST SUCCESS", func(t io.Writer) {
			if rerr != nil {
				fmt.Fprintf(t, "\nReplication: could not read it (%v)\n", rerr)
			} else {
				for _, l := range r.Links {
					fmt.Fprintf(t, "%s\t%s\t%s\t%d\t%s\n", l.Direction, l.NC, l.DSA, l.Failures, l.LastSuccess)
				}
				fmt.Fprintf(t, "\nReplication: %s (%d links)\n", map[bool]string{true: "healthy", false: "failing"}[r.OK], len(r.Links))
			}
			for _, k := range c.Checks {
				fmt.Fprintf(t, "%s: %s", k.Name, map[bool]string{true: "ok", false: "problems"}[k.OK])
				if k.Name == "dbcheck" {
					fmt.Fprintf(t, " (%d objects, %d errors)", k.Objects, k.Errors)
				}
				fmt.Fprintln(t)
				if !k.OK {
					fmt.Fprintln(t, k.Output)
				}
			}
		})
	},
}

func init() {
	agentCmd.Flags().BoolVar(&insecureDev, "insecure-dev", false, "allow plain HTTP without TLS (only when listening on loopback)")
	agentACLSetCmd.Flags().StringSliceVar(&aclSetOpts.full, "full", nil, "groups with full control (DOMAIN\\group)")
	agentACLSetCmd.Flags().StringSliceVar(&aclSetOpts.rw, "rw", nil, "groups with write (modify)")
	agentACLSetCmd.Flags().StringSliceVar(&aclSetOpts.ro, "ro", nil, "read-only groups")
	agentACLSetCmd.Flags().StringSliceVar(&aclSetOpts.fullUsers, "full-user", nil, "users with full control")
	agentACLSetCmd.Flags().StringSliceVar(&aclSetOpts.rwUsers, "rw-user", nil, "users with write")
	agentACLSetCmd.Flags().StringSliceVar(&aclSetOpts.roUsers, "ro-user", nil, "read-only users")
	agentACLCmd.AddCommand(agentACLSetCmd, agentACLHistoryCmd, agentACLRestoreCmd)
	agentRegisterLocalCmd.Flags().String("name", "", "server name in the panel")
	agentCmd.AddCommand(agentTokenCmd, agentCheckCmd, agentACLCmd, agentDCCmd, agentRegisterLocalCmd)
	rootCmd.AddCommand(agentCmd)
}

// currentUser is who ran a local command (the sudo user when present).
func currentUser() string {
	for _, k := range []string{"SUDO_USER", "USER"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return "root"
}
