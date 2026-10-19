package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"ad-manager/internal/ad"
	"ad-manager/internal/audit"

	"github.com/spf13/cobra"
)

// Flags of export and import.
var (
	csvDir   string
	applyNow bool
)

// exportCmd writes the domain to CSV files.
var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export users, groups, OUs and computers to CSV (read-only; works on a 2008 AD over ldap:// + NTLM)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return run("", "", func(c *ad.Conn) error {
			set, err := c.Export()
			if err != nil {
				return err
			}
			if err := os.MkdirAll(csvDir, 0o700); err != nil {
				return err
			}
			for _, f := range []struct {
				name   string
				header []string
				rows   [][]string
			}{{"ous.csv", ad.OUsHeader, set.OUs}, {"groups.csv", ad.GroupsHeader, set.Groups}, {"users.csv", ad.UsersHeader, set.Users}, {"computers.csv", ad.ComputersHeader, set.Computers}} {
				p := filepath.Join(csvDir, f.name)
				w, err := os.OpenFile(p, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
				if err != nil {
					return err
				}
				err = ad.WriteCSV(w, f.header, f.rows)
				w.Close()
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s: %d rows\n", p, len(f.rows))
			}
			return nil
		})
	},
}

// importCmd plans (and with --apply runs) a CSV import.
var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import the exported CSVs into the configured domain (preview; writes only with --apply)",
	Long: `Reads ous.csv, groups.csv, users.csv and computers.csv from the directory and compares them with the
domain (the older Portuguese names grupos.csv, usuarios.csv and computadores.csv are read too).
It only creates and updates — it never deletes objects nor removes members. New accounts get a
generated password and must change it at first logon; the passwords go to
generated-passwords-<date>.csv (0600, a new file per run) in the same directory.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := readImport(csvDir)
		if err != nil {
			return err
		}
		return run("", "", func(c *ad.Conn) error {
			plan, err := c.Plan(data)
			if err != nil {
				return err
			}
			if applyNow {
				f, err := audit.Open(cfg.Server.AuditLog)
				if err != nil {
					return err
				}
				defer f.Close()
				pw := c.Apply(plan, genPassword)
				for _, a := range plan {
					if a.Kind != "skip" {
						audit.Log(c.Me, "cli", "import."+a.Kind, a.Target, okErr(a))
					}
				}
				if len(pw) > 0 {
					p, err := writePasswords(csvDir, pw)
					if err != nil {
						return err
					}
					fmt.Fprintln(cmd.ErrOrStderr(), "generated passwords in", p)
				}
			}
			err = show(cmd.OutOrStdout(), plan, "ACTION\tTARGET\tDETAIL\tRESULT", func(t io.Writer) {
				for _, a := range plan {
					fmt.Fprintf(t, "%s\t%s\t%s\t%s\n", a.Kind, a.Target, tr(a.Detail), tr(a.Result))
				}
				if !applyNow {
					fmt.Fprintf(t, "\n%d actions — nothing was written. Run again with --apply.\n", len(plan))
				}
			})
			if err != nil || !applyNow {
				return err
			}
			failed := 0
			for _, a := range plan {
				if a.Kind != "skip" && !a.OK {
					failed++
				}
			}
			if failed > 0 {
				return fmt.Errorf("%d action(s) failed", failed)
			}
			return nil
		})
	},
}

func init() {
	exportCmd.Flags().StringVarP(&csvDir, "dir", "d", "export", "output directory")
	importCmd.Flags().StringVarP(&csvDir, "dir", "d", "export", "directory with the CSV files")
	importCmd.Flags().BoolVar(&applyNow, "apply", false, "write the changes to the domain")
	rootCmd.AddCommand(exportCmd, importCmd)
}

// readImport reads the CSV files present in dir (English names, or the older
// Portuguese ones).
func readImport(dir string) (ad.ImportData, error) {
	var d ad.ImportData
	for _, f := range []struct {
		name, old string
		header    []string
		dst       *[]map[string]string
	}{{"ous.csv", "", ad.OUsHeader, &d.OUs}, {"groups.csv", "grupos.csv", ad.GroupsHeader, &d.Groups},
		{"users.csv", "usuarios.csv", ad.UsersHeader, &d.Users}, {"computers.csv", "computadores.csv", ad.ComputersHeader, &d.Computers}} {
		r, err := os.Open(filepath.Join(dir, f.name))
		if os.IsNotExist(err) && f.old != "" {
			f.name = f.old
			r, err = os.Open(filepath.Join(dir, f.name))
		}
		if os.IsNotExist(err) {
			continue // optional file
		}
		if err != nil {
			return d, err
		}
		*f.dst, err = ad.ReadCSV(r, f.header)
		r.Close()
		if err != nil {
			return d, fmt.Errorf("%s: %w", f.name, err)
		}
	}
	return d, nil
}

// okErr turns an action's result into an error (nil when it succeeded).
func okErr(a ad.Action) error {
	if a.OK {
		return nil
	}
	return fmt.Errorf("%s", tr(a.Result))
}

// writePasswords writes a NEW file per run (O_EXCL, 0600, does not follow an
// existing symlink): a re-run never overwrites passwords from a previous run.
func writePasswords(dir string, pw map[string]string) (string, error) {
	rows := make([][]string, 0, len(pw))
	for u, p := range pw {
		rows = append(rows, []string{u, p})
	}
	path := filepath.Join(dir, "generated-passwords-"+time.Now().Format("20060102-150405")+".csv")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return path, ad.WriteCSV(f, []string{"login", "initial_password"}, rows)
}

// genPassword generates initial passwords (replaceable in tests).
var genPassword = ad.GenPassword
