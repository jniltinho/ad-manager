package cmd

import (
	"context"
	"os"

	"ad-manager/internal/ad"
	"ad-manager/internal/audit"
	"ad-manager/internal/buildinfo"
	"ad-manager/internal/sambabridge"
	"ad-manager/internal/sambalog"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

// allowWrite enables the MCP tools that change the directory.
var allowWrite bool

// mcpCmd runs the MCP server over stdio.
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "MCP server (stdio) for AI agents to query/manage the AD",
	Long: `Exposes the directory as MCP tools over stdio, with the AD_USER/AD_PASSWORD credential.
Read-only by default; --allow-write enables changes (all audited with ip "mcp").

Example (Claude Code):
  claude mcp add ad-manager -e AD_USER=administrator@example.local -e AD_PASSWORD=... -- ad-manager mcp`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		f, err := audit.Open(cfg.Server.AuditLog)
		if err != nil {
			return err
		}
		defer f.Close()
		s := mcp.NewServer(&mcp.Implementation{Name: "ad-manager", Version: buildinfo.Version}, nil)
		registerTools(s, allowWrite)
		return s.Run(cmd.Context(), &mcp.StdioTransport{})
	},
}

func init() {
	mcpCmd.Flags().BoolVar(&allowWrite, "allow-write", false, "enable the tools that change the directory")
	rootCmd.AddCommand(mcpCmd)
}

// Tool inputs and outputs.
type (
	qIn struct {
		Query string `json:"query,omitempty" jsonschema:"filter: part of the login, name or e-mail"`
	}
	samIn struct {
		Sam string `json:"sam" jsonschema:"login (sAMAccountName); a computer ends with $"`
	}
	nameIn struct {
		Name string `json:"name" jsonschema:"group name"`
	}
	memberIn struct {
		Group  string `json:"group" jsonschema:"group name"`
		Member string `json:"member" jsonschema:"member login"`
	}
	resetIn struct {
		Sam        string `json:"sam" jsonschema:"user login"`
		Password   string `json:"password" jsonschema:"new password"`
		MustChange bool   `json:"must_change" jsonschema:"require a password change at next logon"`
	}
	createIn struct {
		ad.UserInput
		Password   string `json:"password" jsonschema:"initial password"`
		OU         string `json:"ou,omitempty" jsonschema:"OU DN (default CN=Users)"`
		MustChange bool   `json:"must_change"`
	}
	updateIn struct {
		ad.UserInput
	}
	moveIn struct {
		Sam string `json:"sam" jsonschema:"login (sAMAccountName); a computer ends with $"`
		OU  string `json:"ou" jsonschema:"destination OU DN"`
	}
	groupCreateIn struct {
		Name        string `json:"name" jsonschema:"group name"`
		Description string `json:"description,omitempty"`
		Type        string `json:"type,omitempty" jsonschema:"security (default) or distribution"`
		Scope       string `json:"scope,omitempty" jsonschema:"global (default), domainlocal or universal"`
		OU          string `json:"ou,omitempty" jsonschema:"OU DN (default CN=Users)"`
	}
	describeIn struct {
		Name        string `json:"name" jsonschema:"group name, login or computer ($)"`
		Description string `json:"description"`
	}
	ouCreateIn struct {
		Name        string `json:"name" jsonschema:"OU name"`
		Description string `json:"description,omitempty"`
		Parent      string `json:"parent,omitempty" jsonschema:"parent OU DN (empty = the domain root)"`
	}
	dnIn struct {
		DN string `json:"dn" jsonschema:"OU DN"`
	}
	auditIn struct {
		Query string `json:"query,omitempty" jsonschema:"filter: operator, action, target, IP or result"`
		Limit int    `json:"limit,omitempty" jsonschema:"maximum events (default 200, up to 5000)"`
	}
	sambaIn struct {
		File  string `json:"file,omitempty" jsonschema:"file (e.g. log.samba); empty lists the files"`
		Query string `json:"query,omitempty" jsonschema:"text filter on the messages"`
		Lines int    `json:"lines,omitempty" jsonschema:"maximum entries (default 200, up to 5000)"`
	}
	sambaOut struct {
		Files   []sambalog.File  `json:"files,omitempty"`
		Entries []sambalog.Entry `json:"entries,omitempty"`
	}
	auditOut struct {
		Events []audit.Event `json:"events"`
	}
	okOut struct {
		OK bool `json:"ok"`
	}
)

// tool registers a tool that opens the AD connection and, when action is not
// empty, audits the result.
func tool[In, Out any](s *mcp.Server, name, desc, action string, target func(In) string, fn func(*ad.Conn, In) (Out, error)) {
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: desc},
		func(_ context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
			var out Out
			c, err := dial()
			if err != nil {
				return nil, out, err
			}
			defer c.Close()
			out, err = fn(c, in)
			if action != "" {
				audit.Log(c.Me, "mcp", action, target(in), err)
			}
			return nil, out, err
		})
}

// registerTools registers the read-only tools and, with write, the ones that
// change the directory.
func registerTools(s *mcp.Server, write bool) {
	tool(s, "ad_users_list", "List domain users (login, name, e-mail, enabled, locked, last logon)", "", nil,
		func(c *ad.Conn, in qIn) (struct {
			Users []ad.User `json:"users"`
		}, error) {
			us, err := c.Users(in.Query)
			return struct {
				Users []ad.User `json:"users"`
			}{us}, err
		})
	tool(s, "ad_user_show", "A user's details, including groups", "", nil,
		func(c *ad.Conn, in samIn) (*ad.User, error) { return c.User(in.Sam) })
	tool(s, "ad_groups_list", "List groups (type, scope, member count)", "", nil,
		func(c *ad.Conn, in qIn) (struct {
			Groups []ad.Group `json:"groups"`
		}, error) {
			gs, err := c.Groups(in.Query)
			return struct {
				Groups []ad.Group `json:"groups"`
			}{gs}, err
		})
	tool(s, "ad_group_show", "A group with its member list", "", nil,
		func(c *ad.Conn, in nameIn) (*ad.Group, error) { return c.Group(in.Name) })
	tool(s, "ad_computers_list", "List computer accounts (OS, enabled, DC, last logon)", "", nil,
		func(c *ad.Conn, in qIn) (struct {
			Computers []ad.Computer `json:"computers"`
		}, error) {
			cs, err := c.Computers(in.Query)
			return struct {
				Computers []ad.Computer `json:"computers"`
			}{cs}, err
		})
	tool(s, "ad_ous_list", "List OUs and the default containers", "", nil,
		func(c *ad.Conn, _ struct{}) (struct {
			OUs []ad.OU `json:"ous"`
		}, error) {
			os, err := c.OUs()
			return struct {
				OUs []ad.OU `json:"ous"`
			}{os}, err
		})
	tool(s, "ad_policy_show", "Domain password policy", "", nil,
		func(c *ad.Conn, _ struct{}) (*ad.Policy, error) { return c.Policy() })
	mcp.AddTool(s, &mcp.Tool{Name: "ad_share_acl", Description: "Show the Windows permissions (NTFS ACL) of a folder on a file server " +
		"registered in the panel (AD_PANEL_URL): owner, group and each permission (full control, modify, read & execute, deny)"},
		func(_ context.Context, _ *mcp.CallToolRequest, in aclIn) (*mcp.CallToolResult, *sambabridge.ACL, error) {
			acl, err := panelACL(in)
			return nil, acl, err
		})
	tool(s, "ad_audit_log", "Latest audited changes (panel, CLI, MCP and agents), newest first", "", nil,
		func(_ *ad.Conn, in auditIn) (auditOut, error) {
			ev, err := audit.Read(cfg.Server.AuditLog, limit(in.Limit, 200), in.Query)
			return auditOut{ev}, err
		})
	tool(s, "ad_samba_logs", "Samba logs of this machine: without file it lists the files; with file, the newest entries", "", nil,
		func(_ *ad.Conn, in sambaIn) (out sambaOut, err error) {
			if in.File == "" {
				out.Files, err = sambalog.List(cfg.Samba.LogDir)
			} else {
				out.Entries, err = sambalog.Read(cfg.Samba.LogDir, in.File, limit(in.Lines, 200), in.Query)
			}
			return out, err
		})
	tool(s, "ad_preflight", "Migration check: functional levels, schema, FSMO, DCs", "", nil,
		func(c *ad.Conn, _ struct{}) (struct {
			Checks []ad.Check `json:"checks"`
		}, error) {
			cs, err := c.Preflight()
			return struct {
				Checks []ad.Check `json:"checks"`
			}{cs}, err
		})
	registerImportTools(s, write)
	registerPasswordTool(s, write)
	registerBackupTools(s, write)
	if !write {
		return
	}
	bySam := func(in samIn) string { return in.Sam }
	tool(s, "ad_user_enable", "Enable a user or computer", "user.enable", bySam,
		func(c *ad.Conn, in samIn) (okOut, error) { return okOut{true}, c.SetEnabled(in.Sam, true) })
	tool(s, "ad_user_disable", "Disable a user or computer (refuses protected accounts)", "user.disable", bySam,
		func(c *ad.Conn, in samIn) (okOut, error) { return okOut{true}, c.SetEnabled(in.Sam, false) })
	tool(s, "ad_user_unlock", "Unlock an account locked out by failed attempts", "user.unlock", bySam,
		func(c *ad.Conn, in samIn) (okOut, error) { return okOut{true}, c.Unlock(in.Sam) })
	tool(s, "ad_user_reset_password", "Reset a user's password", "password.reset", func(in resetIn) string { return in.Sam },
		func(c *ad.Conn, in resetIn) (okOut, error) {
			return okOut{true}, c.ResetPassword(in.Sam, in.Password, in.MustChange)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "ad_user_password_link", Description: "Generate a single-use link for the user to choose a new password " +
		"(through the panel at AD_PANEL_URL): the current password stops working and the account is unlocked; give the link to the user over a trusted channel"},
		func(_ context.Context, _ *mcp.CallToolRequest, in linkIn) (*mcp.CallToolResult, linkOut, error) {
			out, err := panelLink(in)
			audit.Log(os.Getenv("AD_USER"), "mcp", "password.link", in.Sam, err)
			return nil, out, err
		})
	tool(s, "ad_user_create", "Create an enabled user with an initial password", "user.create", func(in createIn) string { return in.Sam },
		func(c *ad.Conn, in createIn) (*ad.User, error) {
			return c.CreateUser(in.UserInput, in.Password, in.OU, in.MustChange)
		})
	byMember := func(in memberIn) string { return in.Group + " " + in.Member }
	tool(s, "ad_group_add_member", "Add a member to a group", "group.add", byMember,
		func(c *ad.Conn, in memberIn) (okOut, error) { return okOut{true}, c.AddMember(in.Group, in.Member) })
	tool(s, "ad_group_remove_member", "Remove a member from a group", "group.remove", byMember,
		func(c *ad.Conn, in memberIn) (okOut, error) { return okOut{true}, c.RemoveMember(in.Group, in.Member) })

	// users and computers: edit, move, delete
	tool(s, "ad_user_update", "Change a user's data (name, e-mail, phone, title, department, description)", "user.update",
		func(in updateIn) string { return in.Sam },
		func(c *ad.Conn, in updateIn) (*ad.User, error) { return c.UpdateUser(in.Sam, in.UserInput) })
	tool(s, "ad_object_move", "Move a user or computer ($) to another OU (refuses protected accounts and DCs)", "object.move",
		func(in moveIn) string { return in.Sam + " → " + in.OU },
		func(c *ad.Conn, in moveIn) (okOut, error) { return okOut{true}, c.Move(in.Sam, in.OU) })
	tool(s, "ad_object_delete", "Delete a user, group or computer ($); refuses built-in accounts and groups, DCs and your own account", "object.delete", bySam,
		func(c *ad.Conn, in samIn) (okOut, error) { return okOut{true}, c.Delete(in.Sam) })

	// groups and descriptions
	tool(s, "ad_group_create", "Create a group (type security/distribution, scope global/domainlocal/universal)", "group.create",
		func(in groupCreateIn) string { return in.Name },
		func(c *ad.Conn, in groupCreateIn) (*ad.Group, error) {
			return c.CreateGroup(in.Name, in.Description, orDefault(in.Type, "security"), orDefault(in.Scope, "global"), in.OU)
		})
	tool(s, "ad_set_description", "Change the description of a group, user or computer", "object.describe",
		func(in describeIn) string { return in.Name },
		func(c *ad.Conn, in describeIn) (okOut, error) {
			return okOut{true}, c.SetDescription(in.Name, in.Description)
		})

	// OUs
	tool(s, "ad_ou_create", "Create an OU (empty parent = the domain root)", "ou.create", func(in ouCreateIn) string { return in.Name },
		func(c *ad.Conn, in ouCreateIn) (*ad.OU, error) { return c.CreateOU(in.Name, in.Description, in.Parent) })
	tool(s, "ad_ou_delete", "Delete an empty OU", "ou.delete", func(in dnIn) string { return in.DN },
		func(c *ad.Conn, in dnIn) (okOut, error) { return okOut{true}, c.DeleteOU(in.DN) })

	// password policy (full replacement: read it with ad_policy_show first)
	tool(s, "ad_policy_set", "Write the domain password policy (send every field; read it first with ad_policy_show)", "policy.set",
		func(ad.Policy) string { return "domain" },
		func(c *ad.Conn, in ad.Policy) (okOut, error) { return okOut{true}, c.SetPolicy(in) })
}

// limit applies a default and the 5000 ceiling to a requested count.
func limit(n, def int) int {
	if n <= 0 {
		return def
	}
	return min(n, 5000)
}

// orDefault returns s, or def when s is empty.
func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
