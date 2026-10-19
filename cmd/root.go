// Package cmd defines the ad-manager Cobra command tree: directory management
// (user, group, ou, computer, policy), CSV export/import, migration
// (migrate), the web panel (server), the file server agent (agent), the MCP
// server (mcp) and version.
package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"ad-manager/internal/ad"
	"ad-manager/internal/audit"
	"ad-manager/internal/config"
	"ad-manager/internal/i18n"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// Global flags and the configuration loaded by PersistentPreRunE.
var (
	cfgFile string
	asJSON  bool
	cfg     *config.Config
)

// rootCmd is the ad-manager command.
var rootCmd = &cobra.Command{
	Use:           "ad-manager",
	Short:         "Active Directory management (Samba/Windows) — CLI and web panel",
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == versionCmd.Name() || cmd.Name() == winrpcCmd.Name() { // no configuration needed
			return nil
		}
		c, err := config.Load(cfgFile)
		if err != nil {
			return err
		}
		cfg = c
		return nil
	},
}

// Execute runs the CLI and exits with status 1 on error.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		var code exitCode
		if errors.As(err, &code) { // the output already says why
			os.Exit(int(code))
		}
		fmt.Fprintln(os.Stderr, tr("error")+":", tr(err.Error()))
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config.toml (default: ./config.toml, /opt/ad-manager/config.toml)")
	rootCmd.PersistentFlags().BoolVar(&asJSON, "json", false, "JSON output")
	rootCmd.AddCommand(versionCmd, serverCmd, userCmd, groupCmd, ouCmd, computerCmd, policyCmd, migrateCmd)
}

// tr translates a message (English) through the internal/i18n catalog when the
// environment's locale is Portuguese; otherwise it returns s unchanged.
func tr(s string) string { return i18n.T(locale(), s) }

// locale is the environment's message locale, as POSIX picks it: LC_ALL, then
// LC_MESSAGES, then LANG ("pt_BR.UTF-8" translates; anything else is English).
func locale() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return "en"
}

// dial connects with AD_USER/AD_PASSWORD (the password is prompted for on the
// terminal if missing).
func dial() (*ad.Conn, error) {
	user, pass, err := credentials()
	if err != nil {
		return nil, err
	}
	return ad.Dial(&cfg.AD, user, pass)
}

// credentials is the destination account (AD_USER, AD_PASSWORD or asked once on the terminal).
func credentials() (user, pass string, err error) {
	credOnce.Do(func() {
		credUser = os.Getenv("AD_USER")
		if credUser == "" {
			credErr = errors.New("set AD_USER (e.g. administrator@example.local)")
			return
		}
		if credPass = os.Getenv("AD_PASSWORD"); credPass == "" {
			credPass, credErr = readSecret("Password for " + credUser + ": ")
		}
	})
	return credUser, credPass, credErr
}

var (
	credOnce           sync.Once
	credUser, credPass string
	credErr            error
)

// run opens the connection, runs fn and writes an audit record when action is
// not empty. A change that cannot be audited is not made: the log opens first.
func run(action, target string, fn func(c *ad.Conn) error) error {
	if action != "" {
		f, err := audit.Open(cfg.Server.AuditLog)
		if err != nil {
			return fmt.Errorf("audit log unavailable (%s): %w", cfg.Server.AuditLog, err)
		}
		defer f.Close()
	}
	c, err := dial()
	if err != nil {
		return err
	}
	defer c.Close()
	err = fn(c)
	if action != "" {
		audit.Log(c.Me, "cli", action, target, err)
	}
	return err
}

// readSecret reads a password without echo on the terminal, or the first line
// of stdin.
func readSecret(prompt string) (string, error) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, prompt)
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// show prints v as JSON (--json) or as a table through rows.
func show(w io.Writer, v any, header string, rows func(t io.Writer)) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	t := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(t, header)
	rows(t)
	return t.Flush()
}

// yn formats a boolean for table output.
func yn(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// day formats a table date ("-" means never).
func day(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04")
}
