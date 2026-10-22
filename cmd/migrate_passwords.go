package cmd

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"ad-manager/internal/ad"
	"ad-manager/internal/audit"
	"ad-manager/internal/sambadc"

	"github.com/spf13/cobra"
)

// pwMigrateOpts holds the flags of `migrate passwords`.
var pwMigrateOpts struct {
	from, user, agent string
	insecure, apply   bool
}

// callAgentPasswords posts a password-migration request to the DC agent and returns its outcomes. The source
// password reaches the agent only in the TLS body (never an argument); destSID pins the destination domain. The
// answer carries only counts and per-user outcomes, never a secret. It is shared by the CLI and the MCP tool.
func callAgentPasswords(ctx context.Context, agentURL, token, host, user, srcPassword, destSID string, insecure, apply bool) (*sambadc.PwResult, error) {
	u, err := url.Parse(agentURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, errors.New("the agent URL must be https:// (credentials must not travel in clear text)")
	}
	body, _ := json.Marshal(map[string]any{"source": map[string]string{"host": host, "user": user},
		"password": srcPassword, "apply": apply, "dest_sid": destSID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(agentURL, "/")+"/api/dc/passwords/migrate", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	cl := &http.Client{Timeout: 35 * time.Minute,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport:     &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure, MinVersion: tls.VersionTLS12}}} //nolint:gosec // opt-in (--insecure)
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		var e struct{ Error string }
		_ = json.Unmarshal(out, &e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return nil, errors.New(e.Error)
	}
	var res sambadc.PwResult
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, fmt.Errorf("invalid answer: %w", err)
	}
	return &res, nil
}

func init() {
	c := &cobra.Command{
		Use:   "passwords",
		Short: "Migrate the users' password hashes from the source domain to this one (preview; writes only with --apply)",
		Long: `Preserves the production passwords on a NEW-DOMAIN cutover (not needed with a dc-join): the DC agent, in
its dedicated password-migration write mode, reads the source's password hashes over a sealed, read-only DRSUAPI
and sets them on this domain's matching users (by sAMAccountName). Never over LDAP, never as plaintext, never
creating a user; the source is left unchanged.

  plan (default)  authenticated connectivity and non-secret metadata only (would-set / missing / built-in)
  --apply         reads the hashes and sets them per user (NTLM, and Kerberos when the salt transfers)

Source replication account: AD_SOURCE_PASSWORD_FILE (a 0600 file) or the terminal — never an argument. It needs
Replicating Directory Changes and …All on the source. The DC agent's token: AGENT_TOKEN in the environment.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if pwMigrateOpts.from == "" || pwMigrateOpts.user == "" || pwMigrateOpts.agent == "" {
				return errors.New("give --from, --user and --agent")
			}
			u, err := url.Parse(pwMigrateOpts.from)
			if err != nil || u.Hostname() == "" {
				return errors.New("invalid --from URL")
			}
			token := os.Getenv("AGENT_TOKEN")
			if token == "" {
				return errors.New("set AGENT_TOKEN (the DC agent's token)")
			}
			pass, err := sourcePassword(pwMigrateOpts.user)
			if err != nil {
				return err
			}
			// the destination's domain SID, read through our own authenticated connection to this domain (independent
			// of the agent), pins the target so the agent cannot write to a wrong same-realm domain
			var destSID string
			if err := run("", "", func(c *ad.Conn) error {
				var e error
				destSID, e = c.DomainSID()
				return e
			}); err != nil {
				return err
			}
			af, err := audit.Open(cfg.Server.AuditLog)
			if err != nil {
				return fmt.Errorf("audit log unavailable (%s): %w", cfg.Server.AuditLog, err)
			}
			defer af.Close()
			res, err := callAgentPasswords(cmd.Context(), pwMigrateOpts.agent, token, u.Hostname(), pwMigrateOpts.user, pass,
				destSID, pwMigrateOpts.insecure, pwMigrateOpts.apply)
			target := u.Hostname()
			if err == nil {
				target = fmt.Sprintf("%s → this domain (%v)", u.Hostname(), res.Counts)
			}
			audit.Log(os.Getenv("AD_USER"), "cli", "password.migrate", target, err)
			if err != nil {
				return err
			}
			if err := show(cmd.OutOrStdout(), res.Users, "USER\tOUTCOME\tREASON", func(t io.Writer) {
				for _, usr := range res.Users {
					fmt.Fprintf(t, "%s\t%s\t%s\n", usr.Sam, tr(usr.Outcome), tr(usr.Reason))
				}
				if !pwMigrateOpts.apply {
					fmt.Fprintf(t, "\n%d user(s) — nothing was written. Run again with --apply.\n", res.Counts["would_set"])
				}
			}); err != nil {
				return err
			}
			// a non-zero exit when any user failed (e.g. missing replication rights produce error rows), so
			// automation does not read a failed migration as success
			if res.Counts["error"] > 0 {
				return exitCode(2)
			}
			return nil
		}}
	f := c.Flags()
	f.StringVar(&pwMigrateOpts.from, "from", "", "source DC URL (ldaps://old-dc or ldap://10.0.0.10 for 2008)")
	f.StringVar(&pwMigrateOpts.user, "user", "", "replication account on the source (Replicating Directory Changes + …All)")
	f.StringVar(&pwMigrateOpts.agent, "agent", "", "URL of this domain's DC agent (https://dc:9443)")
	f.BoolVar(&pwMigrateOpts.insecure, "insecure", false, "accept the agent's self-signed certificate")
	f.BoolVar(&pwMigrateOpts.apply, "apply", false, "read the hashes and set them (otherwise preview only)")
	migrateCmd.AddCommand(c)
}
