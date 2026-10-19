# MCP — AI agents

`ad-manager mcp` is an [MCP](https://modelcontextprotocol.io) server (stdio) that exposes Active Directory as tools
for AI agents (Claude Code, Codex, Cursor…). The agent queries and, if you allow it, changes the directory with
**your** credential — the same protections as the panel and the CLI apply here.

There are **26 tools**: 10 read tools (always available) and 16 write tools (only with `--allow-write`).

## Environment variables

| Variable | Purpose |
|---|---|
| `AD_URL`, `AD_DOMAIN` | DC and domain (`ldaps://dc01.example.local`, `example.local`); or `[ad]` in `config.toml` |
| `AD_USER`, `AD_PASSWORD` | operator credential (the MCP server cannot prompt for the password: set it in the client) |
| `AD_CA_FILE` / `AD_INSECURE_SKIP_VERIFY` | CA of the DC certificate (Samba: `/var/lib/samba/private/tls/ca.pem`) / lab only |
| `AD_PANEL_URL` | panel, for `ad_user_password_link` and `ad_share_acl` (e.g. `https://dc01.example.local:8443`) |
| `AD_PANEL_INSECURE` | `true` accepts the panel's self-signed certificate |
| `AD_SOURCE_PASSWORD_FILE` | 0600 file with the source AD password, for `ad_import_plan`/`ad_import_apply` (the password never goes through the agent's calls) |
| `AD_IMPORT_DIR` | where `ad_import_apply` writes the CSV of generated passwords (default: current directory) |

## Connecting to the agent

```bash
# Claude Code — read-only
claude mcp add ad-manager \
  -e AD_URL=ldaps://dc01.example.local -e AD_DOMAIN=example.local \
  -e AD_USER=administrator@example.local -e AD_PASSWORD='…' \
  -- ad-manager mcp

# Claude Code — read and write, with a password change link through the panel
claude mcp add ad-manager-rw \
  -e AD_URL=ldaps://dc01.example.local -e AD_DOMAIN=example.local \
  -e AD_USER=administrator@example.local -e AD_PASSWORD='…' \
  -e AD_PANEL_URL=https://dc01.example.local:8443 -e AD_PANEL_INSECURE=true \
  -- ad-manager mcp --allow-write
```

Codex (`~/.codex/config.toml`):

```toml
[mcp_servers.ad-manager]
command = "ad-manager"
args = ["mcp", "--allow-write"]
env = { AD_URL = "ldaps://dc01.example.local", AD_DOMAIN = "example.local", AD_USER = "administrator@example.local", AD_PASSWORD = "…", AD_PANEL_URL = "https://dc01.example.local:8443" }
```

Other clients (Cursor, Claude Desktop…): the server is the `ad-manager mcp` command (stdio) with the same variables:

```json
{
  "mcpServers": {
    "ad-manager": {
      "command": "/usr/bin/ad-manager",
      "args": ["mcp"],
      "env": { "AD_URL": "ldaps://dc01.example.local", "AD_DOMAIN": "example.local",
               "AD_USER": "administrator@example.local", "AD_PASSWORD": "…" }
    }
  }
}
```

- Prefer a dedicated account for the agent, with only the rights it needs (`admin_group` in `config.toml`).
- **Read-only by default.** `--allow-write` enables the tools that change the directory.
- Every change goes to `audit.log` with the origin `mcp` (it shows up on the panel's **Logs** page).
- `ad_user_password_link` generates the link through the **panel**: it signs in with the same credential (must be an
  administrator), requests the link and signs out. The link is the same as on the page (single use, only in the first
  browser that opens it, valid from 1 h to 7 days) and the panel also records `password.link` in its own audit log.
- `ad_share_acl` also goes through the panel (same credential, administrator): it finds the file server by its
  registered name and reads the ACL through the agent. Read-only.
- `ad_audit_log` and `ad_samba_logs` read the files **of the machine where the MCP server runs** (`server.audit_log`
  and `[samba] log_dir` in `config.toml`) — useful when the MCP server runs on the panel server itself or on a DC.

## Read tools (12)

| Tool | Parameters | Returns |
|---|---|---|
| `ad_users_list` | `query` (part of the login, name or email) | users: login, name, email, enabled, locked, last logon |
| `ad_user_show` | `sam` | user with all fields and groups |
| `ad_groups_list` | `query` | groups: type, scope, member count |
| `ad_group_show` | `name` | group with its member list |
| `ad_computers_list` | `query` | computers: OS, enabled, DC, last logon |
| `ad_ous_list` | — | OUs and default containers |
| `ad_policy_show` | — | domain password policy |
| `ad_preflight` | — | migration check: functional levels, schema, FSMO, DCs |
| `ad_audit_log` | `query`, `limit` (default 200) | latest audited changes (panel, CLI, MCP, agents) |
| `ad_share_acl` | `server` (name in the panel), `path` | Windows permissions of the folder through the panel and the agent: owner, group, each ACE (full control, modify, read & execute, deny) |
| `ad_samba_logs` | `file` (empty lists the files), `query`, `lines` | Samba logs: files or newest entries |
| `ad_import_plan` | `url`, `domain`, `user`, `insecure`, `objects`, `dns`, `dynamic`, `create_zones` | import plan from another AD/Samba4 (the same as the panel's) and a `plan_id` valid for 10 minutes |

## Write tools (17, with `--allow-write`)

| Tool | Parameters | Does |
|---|---|---|
| `ad_user_create` | `sam`, `name`, `given_name`, `surname`, `mail`, `phone`, `title`, `department`, `description`, `password`, `ou`, `must_change` | creates an enabled user with an initial password |
| `ad_user_update` | `sam` and the fields to change | changes user data |
| `ad_user_reset_password` | `sam`, `password`, `must_change` | resets the password |
| `ad_user_password_link` | `sam`, `hours` (1–168, default 24) | single-use link for the user to choose the new password (through the panel); the current password stops working |
| `ad_user_enable` / `ad_user_disable` | `sam` (computer: `NAME$`) | enables / disables the account |
| `ad_user_unlock` | `sam` | unlocks an account locked by failed attempts |
| `ad_object_move` | `sam`, `ou` (DN) | moves a user or computer to another OU |
| `ad_object_delete` | `sam` (group: name; computer: `NAME$`) | deletes a user, group or computer |
| `ad_group_create` | `name`, `description`, `type` (security/distribution), `scope` (global/domainlocal/universal), `ou` | creates a group |
| `ad_group_add_member` / `ad_group_remove_member` | `group`, `member` | adds / removes a member |
| `ad_set_description` | `name`, `description` | changes the description of a group, user or computer |
| `ad_ou_create` | `name`, `description`, `parent` (DN; empty = root) | creates an OU |
| `ad_ou_delete` | `dn` | deletes an empty OU |
| `ad_policy_set` | all fields of `ad_policy_show` | writes the password policy (read it first, change it and send everything) |
| `ad_import_apply` | `plan_id` | applies a plan from `ad_import_plan` (single use); refuses if it expired or if the recomputed plan changed (source, target or operator); generated passwords go to a 0600 file in `AD_IMPORT_DIR`, never in the response |

## Protections

The same as the server's, enforced in the `internal/ad` package — the agent cannot bypass them:

- built-in accounts and groups (RID < 1000), domain controllers and your own account are not deleted, disabled or
  moved (error "protected system object");
- writes only over TLS; without `--allow-write` the write tools are not even shown to the agent;
- the password change link goes through the panel's rules (refuses protected accounts) and the agent never sees the
  new password or the temporary one;
- every call that changes something is audited with operator, target and result (including refused ones).

## Example requests to the agent

- "Who is locked out right now, and when was their last logon?"
- "List the enabled accounts with no logon for more than 90 days and disable the ones of people who left the company."
- "Create the user maria.souza in the Sales OU, in the Commercial group, with a password change at first logon."
- "Generate a 2-hour password change link for ana.souza." (uses `ad_user_password_link`)
- "Who has full control on the /srv/shares/Finance folder of fs01?" (uses `ad_share_acl`)
- "What changed in AD today?" (uses `ad_audit_log`)
- "Are there replication or DNS errors in the Samba log?" (uses `ad_samba_logs` on the DC)

## Testing

```bash
make samba-up        # test Samba in Docker
AD_URL=ldaps://127.0.0.1:6636 AD_DOMAIN=empresa.test AD_INSECURE_SKIP_VERIFY=true \
  AD_USER=Administrator AD_PASSWORD=Teste-ad-manager-2026 npx @modelcontextprotocol/inspector dist/ad-manager mcp --allow-write
```

For the password change link, also start the local panel (`dist/ad-manager server --insecure-dev`, see
[development.md](development.md)) and add `AD_PANEL_URL=http://127.0.0.1:8080`.

`go test ./cmd` checks which tools exist with and without `--allow-write` and the `ad_user_password_link`
conversation with a fake panel (login, link, forwarded error).
