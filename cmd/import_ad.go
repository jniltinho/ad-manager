package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"

	"ad-manager/internal/ad"
	"ad-manager/internal/audit"
	"ad-manager/internal/importad"
	"ad-manager/internal/store"
	"ad-manager/internal/winrpc"

	"github.com/spf13/cobra"
)

// adImportOpts are what an import from another AD brings and from where.
type adImportOpts struct {
	URL, Domain, User string
	Insecure          bool
	Objects, DNS      bool
	Dynamic           bool
	CreateZones       bool // DNS: create the zones missing on this DC
}

// adImport is a computed plan: the objects' actions (with the data Apply needs) and the DNS part.
type adImport struct {
	opts    adImportOpts
	data    ad.ImportData
	objects []ad.Action
	meta    []importad.MetaEntry // non-secret metadata carried after the objects (last-logon, OS)
	dns     importad.DNSPlan
	counts  map[string]int
}

// actions is the whole plan, objects first.
func (p *adImport) actions() []ad.Action {
	return append(append([]ad.Action{}, p.objects...), p.dns.Actions...)
}

// fingerprint covers the actions and everything the apply writes (the source rows, the DNS records with TTL).
func (p *adImport) fingerprint(operator string) string {
	return importad.Fingerprint(cfg.AD.URL, operator, p.actions(), struct {
		Data ad.ImportData
		DNS  map[int]importad.DNSChange
	}{p.data, p.dns.Changes})
}

// planADImport reads the source (only for search) and plans it on the destination c.
func planADImport(c *ad.Conn, o adImportOpts, password string) (*adImport, error) {
	src, err := importad.Open(o.URL, o.Domain, o.User, password, o.Insecure)
	if err != nil {
		return nil, fmt.Errorf("source AD: %w", err)
	}
	defer src.Close()
	p := &adImport{opts: o, counts: map[string]int{}}
	if o.Objects {
		set, err := src.Export()
		if err != nil {
			return nil, err
		}
		p.data = ad.SetToData(set)
		p.counts["users"], p.counts["groups"], p.counts["ous"], p.counts["computers"] = len(set.Users), len(set.Groups), len(set.OUs), len(set.Computers)
		if p.objects, err = c.Plan(p.data); err != nil {
			return nil, err
		}
		if p.meta, err = src.Metadata(); err != nil {
			return nil, err
		}
	}
	if o.DNS {
		recs, bad, err := src.DNSRecords()
		if err != nil {
			return nil, err
		}
		dcs, err := src.DCNames()
		if err != nil {
			return nil, err
		}
		here, _, err := c.DNSRecords()
		if err != nil {
			return nil, err
		}
		cs, err := c.Computers("")
		if err != nil {
			return nil, err
		}
		for _, k := range cs {
			if k.IsDC {
				dcs = append(dcs, k.DNSName)
			}
		}
		if h, err := c.DCHostName(); err == nil {
			dcs = append(dcs, h)
		}
		p.dns = importad.PlanDNS(recs, here, dcs, o.Dynamic, o.CreateZones)
		p.counts["dns"], p.counts["dns_unreadable"] = len(recs), bad
		for k, v := range p.dns.Counts {
			p.counts[k] = v
		}
	}
	return p, nil
}

// applyADImport writes the plan on c (objects, then DNS through the RPC helper process), audits each action as
// actor from ip and returns the generated passwords.
func applyADImport(ctx context.Context, c *ad.Conn, p *adImport, actor, ip string) (map[string]string, error) {
	var pw map[string]string
	if p.opts.Objects {
		pw = c.Apply(p.objects, genPassword)
		for _, a := range p.objects {
			if a.Kind != "skip" {
				audit.Log(actor, ip, "import."+a.Kind, a.Target, okErr(a))
			}
		}
		// carry the non-secret metadata (last-logon, OS) to the objects just imported
		for _, m := range p.meta {
			if err := c.SetImportMetadata(m.Sam, m.LastLogon, m.OS, m.OSVersion); err != nil {
				audit.Log(actor, ip, "import.metadata", m.Sam, err)
			}
		}
	}
	if len(p.dns.Changes) == 0 && len(p.dns.Zones) == 0 {
		return pw, nil
	}
	user, pass, err := credentials()
	if err != nil {
		return pw, err
	}
	u, err := url.Parse(cfg.AD.URL)
	if err != nil {
		return pw, err
	}
	host, err := c.DCHostName()
	if err != nil {
		return pw, err
	}
	exe, err := os.Executable()
	if err != nil {
		return pw, err
	}
	rpc := winrpc.Isolated{Exe: exe}
	t := winrpc.Target{Addr: u.Hostname(), Host: host}
	cred := winrpc.Cred{Realm: strings.ToUpper(cfg.AD.Domain), KDC: net.JoinHostPort(u.Hostname(), "88"), User: user, Password: pass}
	importad.ApplyDNS(p.dns, func(zone string) error { return rpc.CreateZone(ctx, t, zone, cred) },
		func(ch importad.DNSChange) error {
			return rpc.UpdateRecord(ctx, t, ch.Zone, winrpc.Record{Name: ch.Name, Type: ch.Type, TTL: ch.TTL, Data: ch.Data}, false, cred)
		}, func(kind, what string, err error) { audit.Log(actor, ip, kind, what, err) })
	return pw, nil
}

// exitCode ends the CLI with that status without printing an error (the output already says why).
type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("exit %d", int(e)) }

var importAD struct {
	adImportOpts
	profile, only, dir string
	json, apply        bool
}

var importADCmd = &cobra.Command{
	Use:   "ad",
	Short: "Import from another AD/Samba 4 (objects and DNS) into the configured domain (preview; writes only with --apply)",
	Long: `Reads the source for search only and shows the same plan as the panel ("From another AD"):
  objects  users, groups, OUs and computers (only creates and updates; generated passwords in
           generated-passwords-<date>.csv, 0600, in --dir)
  dns      static A, CNAME and PTR records (--dynamic: the clients' too); never those of
           DCs, SOA, NS, SRV, _msdcs or the zone apex; conflicts and missing zones only in the plan
           (--create-zones: creates the missing zones, primary and AD-integrated, before the records)

Source password: AD_SOURCE_PASSWORD_FILE (a 0600 file) or the terminal — never an argument.
Destination: AD_USER/AD_PASSWORD, like the other commands.
Exit status: 0 nothing to do (or all written), 1 there are changes to apply, 2 some action failed.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		o := importAD.adImportOpts
		if importAD.profile != "" {
			if err := useProfile(&o, importAD.profile); err != nil {
				return err
			}
		}
		if o.URL == "" || o.Domain == "" || o.User == "" {
			return errors.New("give --from, --domain and --user (or --profile)")
		}
		for _, w := range strings.Split(importAD.only, ",") {
			switch strings.TrimSpace(w) {
			case "objects", "objetos", "users", "groups", "ous", "computers":
				o.Objects = true
			case "dns":
				o.DNS = true
			case "":
			default:
				return fmt.Errorf("--only: objects and/or dns, not %q", w)
			}
		}
		pass, err := sourcePassword(o.User)
		if err != nil {
			return err
		}
		var p *adImport
		var pw map[string]string
		var applyErr error // after the apply started: the passwords are saved and the plan shown anyway
		err = run("", "", func(c *ad.Conn) error {
			if p, err = planADImport(c, o, pass); err != nil {
				return err
			}
			if importAD.apply {
				f, err := audit.Open(cfg.Server.AuditLog)
				if err != nil {
					return fmt.Errorf("audit log unavailable (%s): %w", cfg.Server.AuditLog, err)
				}
				defer f.Close()
				pw, applyErr = applyADImport(cmd.Context(), c, p, c.Me, "cli")
			}
			return nil
		})
		if err != nil {
			return err
		}
		if len(pw) > 0 {
			path, err := writePasswords(importAD.dir, pw)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "generated passwords in", path)
		}
		if applyErr != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), tr("error")+":", tr(applyErr.Error()))
		}
		acts := p.actions()
		if importAD.json {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			if err := enc.Encode(map[string]any{"plan": acts, "counts": p.counts}); err != nil {
				return err
			}
		} else if err := show(cmd.OutOrStdout(), acts, "ACTION\tTARGET\tDETAIL\tRESULT", func(t io.Writer) {
			for _, a := range acts {
				fmt.Fprintf(t, "%s\t%s\t%s\t%s\n", a.Kind, a.Target, tr(a.Detail), tr(a.Result))
			}
			if !importAD.apply {
				fmt.Fprintf(t, "\n%d actions — nothing was written. Run again with --apply.\n", len(acts))
			}
		}); err != nil {
			return err
		}
		pending, failed := 0, 0
		for _, a := range acts {
			if a.Kind == "skip" || a.Kind == "zone.missing" || a.Kind == "dns.conflict" {
				continue
			}
			pending++
			if importAD.apply && !a.OK {
				failed++
			}
		}
		switch {
		case failed > 0 || applyErr != nil:
			return exitCode(2)
		case !importAD.apply && pending > 0:
			return exitCode(1)
		}
		return nil
	},
}

// useProfile fills o from a profile saved by the panel (URL, domain, user, certificate; never the password).
func useProfile(o *adImportOpts, name string) error {
	st, err := store.Open(cfg.DB.Path)
	if err != nil {
		return fmt.Errorf("profiles: %w", err)
	}
	defer st.Close()
	list, err := st.Sources()
	if err != nil {
		return err
	}
	for _, s := range list {
		if strings.EqualFold(s.Name, name) {
			o.URL, o.Domain, o.User, o.Insecure = s.URL, s.Domain, s.User, s.Insecure
			return nil
		}
	}
	return fmt.Errorf("profile %q does not exist (save it in the panel: Import/export → From another AD)", name)
}

// sourcePassword reads the source password from AD_SOURCE_PASSWORD_FILE (0600) or the terminal.
func sourcePassword(user string) (string, error) {
	if f := os.Getenv("AD_SOURCE_PASSWORD_FILE"); f != "" {
		fi, err := os.Stat(f)
		if err != nil {
			return "", err
		}
		if fi.Mode().Perm()&0o077 != 0 {
			return "", fmt.Errorf("%s must have mode 0600", f)
		}
		b, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		return strings.TrimRight(string(b), "\r\n"), nil
	}
	return readSecret("Password for " + user + " on the source: ")
}

func init() {
	f := importADCmd.Flags()
	f.StringVar(&importAD.URL, "from", "", "source DC URL (ldaps://dc.source.lab, or ldap:// for 2008)")
	f.StringVar(&importAD.Domain, "domain", "", "source domain")
	f.StringVar(&importAD.User, "user", "", "user on the source")
	f.BoolVar(&importAD.Insecure, "insecure", false, "accept a self-signed certificate on the source")
	f.StringVar(&importAD.profile, "profile", "", "profile saved in the panel (URL, domain, user)")
	f.StringVar(&importAD.only, "only", "objects", "what to import: objects, dns (comma-separated)")
	f.BoolVar(&importAD.Dynamic, "dynamic", false, "DNS: also the clients' dynamic records")
	f.BoolVar(&importAD.CreateZones, "create-zones", false, "DNS: create the zones missing on the DC (primary, AD-integrated; never _msdcs)")
	f.StringVarP(&importAD.dir, "dir", "d", ".", "where to write the generated passwords CSV")
	f.BoolVar(&importAD.json, "json", false, "JSON output")
	f.BoolVar(&importAD.apply, "apply", false, "write the changes to the configured domain")
	importCmd.AddCommand(importADCmd)
}
