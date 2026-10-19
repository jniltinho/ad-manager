package cmd

import (
	"fmt"
	"io"
	"strings"

	"ad-manager/internal/ad"

	"github.com/spf13/cobra"
)

// userCmd groups the user subcommands.
var userCmd = &cobra.Command{Use: "user", Short: "Domain users"}

// Flags shared by the user subcommands.
var (
	userIn     ad.UserInput
	userOU     string
	mustChange bool
)

func init() {
	list := &cobra.Command{
		Use: "list [search]", Short: "List users", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return run("", "", func(c *ad.Conn) error {
				us, err := c.Users(strings.Join(args, ""))
				if err != nil {
					return err
				}
				return show(cmd.OutOrStdout(), us, "LOGIN\tNAME\tE-MAIL\tENABLED\tLOCKED\tLAST LOGON", func(t io.Writer) {
					for _, u := range us {
						fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%s\t%s\n", u.Sam, u.Name, u.Mail, yn(u.Enabled), yn(u.Locked), day(u.LastLogon))
					}
				})
			})
		},
	}
	showCmd := &cobra.Command{
		Use: "show <login>", Short: "Show a user", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return run("", "", func(c *ad.Conn) error {
				u, err := c.User(args[0])
				if err != nil {
					return err
				}
				return show(cmd.OutOrStdout(), u, "FIELD\tVALUE", func(t io.Writer) {
					for _, kv := range [][2]string{{"DN", u.DN}, {"Login", u.Sam}, {"UPN", u.UPN}, {"Name", u.Name},
						{"E-mail", u.Mail}, {"Phone", u.Phone}, {"Title", u.Title}, {"Department", u.Department},
						{"Description", u.Description}, {"Enabled", yn(u.Enabled)}, {"Locked", yn(u.Locked)},
						{"Password never expires", yn(u.NoExpire)}, {"Last logon", day(u.LastLogon)},
						{"Password changed", day(u.PwdLastSet)}, {"Groups", strings.Join(u.Groups, ", ")}} {
						fmt.Fprintf(t, "%s\t%s\n", kv[0], kv[1])
					}
				})
			})
		},
	}
	create := &cobra.Command{
		Use: "create <login>", Short: "Create a user (password read from the terminal or stdin)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			userIn.Sam = args[0]
			pass, err := readSecret("Initial password: ")
			if err != nil {
				return err
			}
			return run("user.create", args[0], func(c *ad.Conn) error {
				u, err := c.CreateUser(userIn, pass, userOU, mustChange)
				if err == nil {
					fmt.Fprintln(cmd.OutOrStdout(), "created:", u.DN)
				}
				return err
			})
		},
	}
	update := &cobra.Command{
		Use: "set <login>", Short: "Change a user's data (only the flags given; an empty value clears the field)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return run("user.update", args[0], func(c *ad.Conn) error {
				cur, err := c.User(args[0])
				if err != nil {
					return err
				}
				in := ad.UserInput{Name: cur.Name, GivenName: cur.GivenName, Surname: cur.Surname, Mail: cur.Mail,
					Phone: cur.Phone, Title: cur.Title, Department: cur.Department, Description: cur.Description}
				f := cmd.Flags()
				for flag, dst := range map[string]*string{"name": &in.Name, "given-name": &in.GivenName, "surname": &in.Surname,
					"mail": &in.Mail, "phone": &in.Phone, "title": &in.Title, "department": &in.Department, "description": &in.Description} {
					if f.Changed(flag) {
						*dst, _ = f.GetString(flag)
					}
				}
				_, err = c.UpdateUser(args[0], in)
				return err
			})
		},
	}
	passwd := &cobra.Command{
		Use: "passwd <login>", Short: "Reset the password (read from the terminal or stdin)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pass, err := readSecret("New password: ")
			if err != nil {
				return err
			}
			return run("password.reset", args[0], func(c *ad.Conn) error { return c.ResetPassword(args[0], pass, mustChange) })
		},
	}
	move := &cobra.Command{
		Use: "move <login> <ou-dn>", Short: "Move to another OU", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return run("object.move", args[0], func(c *ad.Conn) error { return c.Move(args[0], args[1]) })
		},
	}
	simple := func(use, short, action string, fn func(c *ad.Conn, sam string) error) *cobra.Command {
		return &cobra.Command{Use: use + " <login>", Short: short, Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return run(action, args[0], func(c *ad.Conn) error { return fn(c, args[0]) })
			}}
	}
	for _, c := range []*cobra.Command{create, update} {
		c.Flags().StringVar(&userIn.Name, "name", "", "display name")
		c.Flags().StringVar(&userIn.GivenName, "given-name", "", "given name")
		c.Flags().StringVar(&userIn.Surname, "surname", "", "surname")
		c.Flags().StringVar(&userIn.Mail, "mail", "", "e-mail")
		c.Flags().StringVar(&userIn.Phone, "phone", "", "phone")
		c.Flags().StringVar(&userIn.Title, "title", "", "job title")
		c.Flags().StringVar(&userIn.Department, "department", "", "department")
		c.Flags().StringVar(&userIn.Description, "description", "", "description")
	}
	create.Flags().StringVar(&userOU, "ou", "", "OU DN (default CN=Users)")
	for _, c := range []*cobra.Command{create, passwd} {
		c.Flags().BoolVar(&mustChange, "must-change", false, "require a password change at next logon")
	}
	userCmd.AddCommand(list, showCmd, create, update, passwd, move,
		simple("enable", "Enable", "user.enable", func(c *ad.Conn, s string) error { return c.SetEnabled(s, true) }),
		simple("disable", "Disable", "user.disable", func(c *ad.Conn, s string) error { return c.SetEnabled(s, false) }),
		simple("unlock", "Unlock", "user.unlock", (*ad.Conn).Unlock),
		simple("delete", "Delete", "user.delete", (*ad.Conn).Delete),
	)
}
