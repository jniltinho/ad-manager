package cmd

import (
	"fmt"
	"io"
	"strings"

	"ad-manager/internal/ad"

	"github.com/spf13/cobra"
)

// Parent commands for groups, OUs, computers and policy, and their flags.
var (
	groupCmd    = &cobra.Command{Use: "group", Short: "Groups and members"}
	ouCmd       = &cobra.Command{Use: "ou", Short: "Organizational units"}
	computerCmd = &cobra.Command{Use: "computer", Short: "Computer accounts"}
	policyCmd   = &cobra.Command{Use: "policy", Short: "Domain password policy"}

	grpDesc, grpType, grpScope, grpOU string
	ouParent, ouDesc                  string
	pol                               ad.Policy
)

// act builds a write subcommand that takes n positional arguments.
func act(use, short, action string, n int, fn func(c *ad.Conn, a []string) error) *cobra.Command {
	return &cobra.Command{Use: use, Short: short, Args: cobra.ExactArgs(n),
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(action, strings.Join(args, " "), func(c *ad.Conn) error { return fn(c, args) })
		}}
}

func init() {
	// ── groups
	gcreate := act("create <name>", "Create a group", "group.create", 1, func(c *ad.Conn, a []string) error {
		_, err := c.CreateGroup(a[0], grpDesc, grpType, grpScope, grpOU)
		return err
	})
	gcreate.Flags().StringVar(&grpDesc, "description", "", "description")
	gcreate.Flags().StringVar(&grpType, "type", "security", "security | distribution")
	gcreate.Flags().StringVar(&grpScope, "scope", "global", "global | domainlocal | universal")
	gcreate.Flags().StringVar(&grpOU, "ou", "", "OU DN (default CN=Users)")
	groupCmd.AddCommand(
		&cobra.Command{Use: "list [search]", Short: "List groups", Args: cobra.MaximumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return run("", "", func(c *ad.Conn) error {
					gs, err := c.Groups(strings.Join(args, ""))
					if err != nil {
						return err
					}
					return show(cmd.OutOrStdout(), gs, "GROUP\tTYPE\tSCOPE\tMEMBERS\tDESCRIPTION", func(t io.Writer) {
						for _, g := range gs {
							fmt.Fprintf(t, "%s\t%s\t%s\t%d\t%s\n", g.Name, g.Type, g.Scope, g.MemberCount, g.Description)
						}
					})
				})
			}},
		&cobra.Command{Use: "show <name>", Short: "Show the group and its members", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return run("", "", func(c *ad.Conn) error {
					g, err := c.Group(args[0])
					if err != nil {
						return err
					}
					return show(cmd.OutOrStdout(), g, "MEMBERS OF "+g.Name, func(t io.Writer) {
						for _, m := range g.Members {
							fmt.Fprintln(t, m)
						}
					})
				})
			}},
		gcreate,
		act("delete <name>", "Delete a group", "group.delete", 1, func(c *ad.Conn, a []string) error { return c.Delete(a[0]) }),
		act("describe <name> <description>", "Change the description", "group.update", 2, func(c *ad.Conn, a []string) error { return c.SetDescription(a[0], a[1]) }),
		act("add <group> <member>", "Add a member (login; a computer ends with $)", "group.add", 2, func(c *ad.Conn, a []string) error { return c.AddMember(a[0], a[1]) }),
		act("remove <group> <member>", "Remove a member", "group.remove", 2, func(c *ad.Conn, a []string) error { return c.RemoveMember(a[0], a[1]) }),
	)

	// ── OUs
	ocreate := act("create <name>", "Create an OU", "ou.create", 1, func(c *ad.Conn, a []string) error {
		_, err := c.CreateOU(a[0], ouDesc, ouParent)
		return err
	})
	ocreate.Flags().StringVar(&ouParent, "parent", "", "parent DN (default: the domain root)")
	ocreate.Flags().StringVar(&ouDesc, "description", "", "description")
	ouCmd.AddCommand(
		&cobra.Command{Use: "list", Short: "List OUs", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return run("", "", func(c *ad.Conn) error {
					os, err := c.OUs()
					if err != nil {
						return err
					}
					return show(cmd.OutOrStdout(), os, "NAME\tDN", func(t io.Writer) {
						for _, o := range os {
							fmt.Fprintf(t, "%s\t%s\n", o.Name, o.DN)
						}
					})
				})
			}},
		ocreate,
		act("delete <dn>", "Delete an empty OU", "ou.delete", 1, func(c *ad.Conn, a []string) error { return c.DeleteOU(a[0]) }),
	)

	// ── computers (login = NAME$; pass the sAMAccountName if it differs from the name)
	sam := func(n string) string { return strings.TrimSuffix(n, "$") + "$" }
	computerCmd.AddCommand(
		&cobra.Command{Use: "list [search]", Short: "List computers", Args: cobra.MaximumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return run("", "", func(c *ad.Conn) error {
					cs, err := c.Computers(strings.Join(args, ""))
					if err != nil {
						return err
					}
					return show(cmd.OutOrStdout(), cs, "NAME\tOS\tENABLED\tDC\tLAST LOGON", func(t io.Writer) {
						for _, m := range cs {
							fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%s\n", m.Name, m.OS, yn(m.Enabled), yn(m.IsDC), day(m.LastLogon))
						}
					})
				})
			}},
		act("enable <name>", "Enable", "computer.enable", 1, func(c *ad.Conn, a []string) error { return c.SetEnabled(sam(a[0]), true) }),
		act("disable <name>", "Disable", "computer.disable", 1, func(c *ad.Conn, a []string) error { return c.SetEnabled(sam(a[0]), false) }),
		act("move <name> <ou-dn>", "Move to another OU", "object.move", 2, func(c *ad.Conn, a []string) error { return c.Move(sam(a[0]), a[1]) }),
		act("delete <name>", "Delete", "computer.delete", 1, func(c *ad.Conn, a []string) error { return c.Delete(sam(a[0])) }),
	)

	// ── policy
	pset := &cobra.Command{Use: "set", Short: "Change the policy (only the flags given)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run("policy.update", "domain", func(c *ad.Conn) error {
				cur, err := c.Policy()
				if err != nil {
					return err
				}
				f := cmd.Flags()
				if f.Changed("complexity") {
					cur.Complexity = pol.Complexity
				}
				for flag, p := range map[string][2]*int{"min-length": {&cur.MinLength, &pol.MinLength}, "history": {&cur.History, &pol.History},
					"min-age": {&cur.MinAgeDays, &pol.MinAgeDays}, "max-age": {&cur.MaxAgeDays, &pol.MaxAgeDays},
					"lockout-threshold": {&cur.LockoutThreshold, &pol.LockoutThreshold}, "lockout-minutes": {&cur.LockoutMinutes, &pol.LockoutMinutes},
					"lockout-window": {&cur.ObservationMinute, &pol.ObservationMinute}} {
					if f.Changed(flag) {
						*p[0] = *p[1]
					}
				}
				return c.SetPolicy(*cur)
			})
		}}
	pset.Flags().BoolVar(&pol.Complexity, "complexity", false, "require complexity")
	pset.Flags().IntVar(&pol.MinLength, "min-length", 0, "minimum length")
	pset.Flags().IntVar(&pol.History, "history", 0, "passwords remembered (0-24)")
	pset.Flags().IntVar(&pol.MinAgeDays, "min-age", 0, "minimum age (days)")
	pset.Flags().IntVar(&pol.MaxAgeDays, "max-age", 0, "maximum age (days; 0 = never expires)")
	pset.Flags().IntVar(&pol.LockoutThreshold, "lockout-threshold", 0, "attempts before lockout (0 = never)")
	pset.Flags().IntVar(&pol.LockoutMinutes, "lockout-minutes", 0, "lockout duration (min)")
	pset.Flags().IntVar(&pol.ObservationMinute, "lockout-window", 0, "observation window (min)")
	policyCmd.AddCommand(
		&cobra.Command{Use: "show", Short: "Show the policy", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return run("", "", func(c *ad.Conn) error {
					p, err := c.Policy()
					if err != nil {
						return err
					}
					return show(cmd.OutOrStdout(), p, "ITEM\tVALUE", func(t io.Writer) {
						fmt.Fprintf(t, "Complexity\t%s\nMinimum length\t%d\nHistory\t%d\nMinimum age (days)\t%d\nMaximum age (days)\t%d\nLockout after\t%d\nLockout duration (min)\t%d\nWindow (min)\t%d\n",
							yn(p.Complexity), p.MinLength, p.History, p.MinAgeDays, p.MaxAgeDays, p.LockoutThreshold, p.LockoutMinutes, p.ObservationMinute)
					})
				})
			}},
		pset,
	)
}
