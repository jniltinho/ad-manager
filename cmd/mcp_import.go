package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"time"

	"ad-manager/internal/ad"
	"ad-manager/internal/audit"
	"ad-manager/internal/sambadc"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// importPlans keeps the plans shown by ad_import_plan for ten minutes: ad_import_apply takes only one of them, and
// only while the recomputed plan is still the same (fingerprint of destination, operator and actions).
var importPlans = struct {
	sync.Mutex
	m map[string]storedPlan
}{m: map[string]storedPlan{}}

type storedPlan struct {
	opts        adImportOpts
	fingerprint string
	expires     time.Time
}

type importPlanIn struct {
	URL      string `json:"url" jsonschema:"source DC URL (ldaps://dc.source.lab; ldap:// only for 2008, with NTLM)"`
	Domain   string `json:"domain" jsonschema:"source domain"`
	User     string `json:"user" jsonschema:"user on the source; the password comes from the MCP server's AD_SOURCE_PASSWORD_FILE, never from the tool"`
	Insecure bool   `json:"insecure,omitempty" jsonschema:"accept a self-signed certificate on the source"`
	Objects  bool   `json:"objects,omitempty" jsonschema:"users, groups, OUs and computers (the default when dns is false too)"`
	DNS      bool   `json:"dns,omitempty" jsonschema:"static A, CNAME and PTR records (never of DCs, SOA, NS, SRV)"`
	Dynamic  bool   `json:"dynamic,omitempty" jsonschema:"DNS: also the clients' dynamic records"`
	Zones    bool   `json:"create_zones,omitempty" jsonschema:"DNS: create the zones missing on the DC (primary, AD-integrated), before the records"`
}

type importPlanOut struct {
	PlanID string         `json:"plan_id"`
	Plan   []ad.Action    `json:"plan"`
	Counts map[string]int `json:"counts"`
}

type importApplyIn struct {
	PlanID string `json:"plan_id" jsonschema:"the plan_id returned by ad_import_plan (valid for 10 minutes)"`
}

type importApplyOut struct {
	Plan          []ad.Action `json:"plan"`
	PasswordsFile string      `json:"passwords_file,omitempty"`
}

// registerImportTools adds ad_import_plan (read only) and, with write, ad_import_apply.
func registerImportTools(s *mcp.Server, write bool) {
	mcp.AddTool(s, &mcp.Tool{Name: "ad_import_plan", Description: "Import plan from another AD/Samba 4 (read-only on the source and the destination): " +
		"objects and/or DNS, the same plan as the panel. Returns a plan_id for ad_import_apply"},
		func(_ context.Context, _ *mcp.CallToolRequest, in importPlanIn) (*mcp.CallToolResult, importPlanOut, error) {
			var out importPlanOut
			o := adImportOpts{URL: in.URL, Domain: in.Domain, User: in.User, Insecure: in.Insecure, Objects: in.Objects || !in.DNS, DNS: in.DNS, Dynamic: in.Dynamic, CreateZones: in.Zones}
			pass, err := mcpSourcePassword()
			if err != nil {
				return nil, out, err
			}
			c, err := dial()
			if err != nil {
				return nil, out, err
			}
			defer c.Close()
			p, err := planADImport(c, o, pass)
			if err != nil {
				return nil, out, err
			}
			id := make([]byte, 16)
			_, _ = rand.Read(id)
			out = importPlanOut{PlanID: hex.EncodeToString(id), Plan: p.actions(), Counts: p.counts}
			importPlans.Lock()
			for k, v := range importPlans.m {
				if time.Now().After(v.expires) {
					delete(importPlans.m, k)
				}
			}
			importPlans.m[out.PlanID] = storedPlan{o, p.fingerprint(c.Me), time.Now().Add(10 * time.Minute)}
			importPlans.Unlock()
			return nil, out, nil
		})
	if !write {
		return
	}
	mcp.AddTool(s, &mcp.Tool{Name: "ad_import_apply", Description: "Apply a plan from ad_import_plan to the configured domain. Refuses when the plan " +
		"expired (10 min) or changed (source, destination or operator); generated passwords go to a 0600 file (path in the answer), never in the output"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in importApplyIn) (*mcp.CallToolResult, importApplyOut, error) {
			var out importApplyOut
			importPlans.Lock()
			sp, ok := importPlans.m[in.PlanID]
			delete(importPlans.m, in.PlanID) // one use
			importPlans.Unlock()
			if !ok || time.Now().After(sp.expires) {
				return nil, out, errors.New("unknown or expired plan_id: run ad_import_plan again")
			}
			pass, err := mcpSourcePassword()
			if err != nil {
				return nil, out, err
			}
			c, err := dial()
			if err != nil {
				return nil, out, err
			}
			defer c.Close()
			p, err := planADImport(c, sp.opts, pass)
			if err != nil {
				return nil, out, err
			}
			if p.fingerprint(c.Me) != sp.fingerprint {
				return nil, out, errors.New("the plan changed since ad_import_plan (source, destination or operator): nothing was written; run ad_import_plan again")
			}
			pw, applyErr := applyADImport(ctx, c, p, c.Me, "mcp")
			out.Plan = p.actions()
			if len(pw) > 0 { // saved even when a later step failed: the accounts exist already
				dir := os.Getenv("AD_IMPORT_DIR")
				if dir == "" {
					dir = "."
				}
				if out.PasswordsFile, err = writePasswords(dir, pw); err != nil {
					return nil, out, errors.Join(applyErr, err)
				}
			}
			if applyErr != nil {
				return nil, out, fmt.Errorf("%w (generated passwords: %s)", applyErr, out.PasswordsFile)
			}
			return nil, out, nil
		})
}

type pwMigrateIn struct {
	URL   string `json:"url" jsonschema:"source DC URL (ldaps://old-dc; ldap:// only for 2008)"`
	User  string `json:"user" jsonschema:"replication account on the source (Replicating Directory Changes + …All); the password comes from AD_SOURCE_PASSWORD_FILE, never from the tool"`
	Apply bool   `json:"apply,omitempty" jsonschema:"read the hashes and set them on this domain's users (otherwise preview only)"`
}

type pwMigrateOut struct {
	Users  []sambadc.PwUser `json:"users"`
	Counts map[string]int   `json:"counts"`
}

// registerPasswordTool adds ad_password_migrate (write mode only): the password-material migration through the DC
// agent. The source password comes from AD_SOURCE_PASSWORD_FILE and the agent from AGENT_URL/AGENT_TOKEN; the
// answer carries only outcomes, never a secret.
func registerPasswordTool(s *mcp.Server, write bool) {
	if !write {
		return
	}
	mcp.AddTool(s, &mcp.Tool{Name: "ad_password_migrate", Description: "Migrate the users' password hashes from a source domain to this one (new-domain cutover), through the DC " +
		"agent's dedicated write mode: sealed read-only DRSUAPI on the source, set on the matching users here. Never over LDAP, never plaintext. apply=false previews. Returns per-user outcomes only"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in pwMigrateIn) (*mcp.CallToolResult, pwMigrateOut, error) {
			var out pwMigrateOut
			agentURL, token := os.Getenv("AGENT_URL"), os.Getenv("AGENT_TOKEN")
			if agentURL == "" || token == "" {
				return nil, out, errors.New("set AGENT_URL and AGENT_TOKEN (this domain's DC agent) in the MCP server's environment")
			}
			u, err := url.Parse(in.URL)
			if err != nil || u.Hostname() == "" {
				return nil, out, errors.New("invalid source DC URL")
			}
			pass, err := mcpSourcePassword()
			if err != nil {
				return nil, out, err
			}
			// the destination's domain SID, read through our own authenticated connection (independent of the agent)
			dst, err := dial()
			if err != nil {
				return nil, out, err
			}
			destSID, err := dst.DomainSID()
			dst.Close()
			if err != nil {
				return nil, out, err
			}
			res, err := callAgentPasswords(ctx, agentURL, token, u.Hostname(), in.User, pass, destSID, os.Getenv("AGENT_INSECURE") == "1", in.Apply)
			if err != nil {
				audit.Log("mcp", "mcp", "password.migrate", u.Hostname(), err)
				return nil, out, err
			}
			audit.Log("mcp", "mcp", "password.migrate", fmt.Sprintf("%s → this domain (%v)", u.Hostname(), res.Counts), nil)
			out = pwMigrateOut{Users: res.Users, Counts: res.Counts}
			return nil, out, nil
		})
}

// mcpSourcePassword reads the source password for the MCP tools: only from AD_SOURCE_PASSWORD_FILE (0600), so it
// never goes through the AI agent's tool calls.
func mcpSourcePassword() (string, error) {
	if os.Getenv("AD_SOURCE_PASSWORD_FILE") == "" {
		return "", errors.New("set AD_SOURCE_PASSWORD_FILE (a 0600 file with the source password) in the MCP server's environment")
	}
	return sourcePassword("")
}
