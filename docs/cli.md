# CLI

The `ad-manager` binary runs the panel, the agent and the MCP server, and it is also a full CLI (Cobra) for everything
the panel does, plus migration and export/import. Useful for scripts, bulk loads and for anyone who prefers the terminal.

## Credentials and connection

There is no service account: the CLI uses your own domain account.

```bash
export AD_URL=ldaps://dc01.example.local          # or [ad] url in config.toml
export AD_USER=administrator@example.local        # also: user or EXAMPLE\user
# AD_PASSWORD is optional: without it, the password is prompted in the terminal (no echo)
```

- Credentials **only through the environment** — never in a flag, so they do not show up in `ps` or the shell history.
- User passwords (`user create`, `user passwd`) are read from the terminal or from the first line of stdin.
- Writes require TLS (`ldaps://` or `start_tls`); `ldap://` without TLS is read-only (NTLM bind, useful on AD 2008).
- Every change goes to `audit.log` with the origin `cli`.

Configuration: `--config file` or `./config.toml` or `/opt/ad-manager/config.toml`; `AD_*` variables override it
(`AD_URL`, `AD_DOMAIN`, `AD_BASE_DN`, `AD_CA_FILE`, `AD_INSECURE_SKIP_VERIFY`, `AD_ADMIN_GROUP`…).

## Global options

| Flag | Effect |
|---|---|
| `--json` | JSON output (for `jq` and scripts) instead of a table |
| `--config file` | another `config.toml` |
| `-h`, `--help` | command help |

## Commands

| Command | What it does |
|---|---|
| `server` | starts the web panel (HTTPS; `--insecure-dev` only on loopback) |
| `user …` | domain users |
| `group …` | groups and members |
| `ou …` | organizational units |
| `computer …` | computer accounts |
| `policy …` | domain password policy |
| `export` / `import` | CSV of users, groups, OUs and computers |
| `migrate …` | preflight, inventory, diff and share migration |
| `agent`, `agent token` | file server daemon and token generation ([agent.md](agent.md)) |
| `agent check` | what the server supports: Samba Python API, `acl_xattr`, permissions mode |
| `agent acl <folder>` / `agent acl set <folder>` | show / write the Windows ACL of a folder (`--full`, `--rw`, `--ro`, `--full-user`, `--rw-user`, `--ro-user`), on the file server itself, as root |
| `mcp` | MCP server for AI agents ([mcp.md](mcp.md)) |
| `version` | build version, commit and date |
| `completion bash\|zsh\|fish` | shell completion |

### Users

```bash
ad-manager user list [search]                    # login, name, email, enabled, locked, last logon
ad-manager user show john.smith                  # details and groups
ad-manager user create john.smith --name "John Smith" --given-name John --surname Smith \
  --mail john@example.com --ou "OU=Sales,DC=example,DC=local" --must-change
ad-manager user set john.smith --title "Analyst" --department Finance   # an empty field removes the value
ad-manager user passwd john.smith --must-change  # new password from the terminal or stdin
ad-manager user enable|disable|unlock|delete john.smith
ad-manager user move john.smith "OU=HQ,DC=example,DC=local"
```

| `user create` | |
|---|---|
| `--name` | display name |
| `--given-name`, `--surname` | first name and last name |
| `--mail`, `--phone` | email and phone |
| `--title`, `--department`, `--description` | job title, department and description |
| `--ou` | OU DN (default `CN=Users`) |
| `--must-change` | require a password change at next logon |

### Groups

```bash
ad-manager group list [search]
ad-manager group show Finance                    # with its members
ad-manager group create Finance --type security --scope global --description "Team" --ou "OU=Groups,DC=example,DC=local"
ad-manager group add Finance john.smith          # computer: NAME$
ad-manager group remove Finance john.smith
ad-manager group describe Finance "Finance team"
ad-manager group delete Finance
```

### OUs and computers

```bash
ad-manager ou list
ad-manager ou create Sales --parent ""           # "" = domain root; --description is optional
ad-manager ou delete "OU=Old,DC=example,DC=local"   # only when empty

ad-manager computer list [search]
ad-manager computer enable|disable|delete PC-01
ad-manager computer move PC-01 "OU=Workstations,DC=example,DC=local"
```

### Password policy

```bash
ad-manager policy show
ad-manager policy set --min-length 8 --complexity --history 12 \
  --lockout-threshold 10 --lockout-minutes 30 --lockout-window 30 --max-age 0
```

`policy set` changes only the flags given (`--min-age` and `--max-age` in days; `--max-age 0` = never expires).

## CSV export and import

Brazilian Excel format (`;` + UTF-8 BOM; `,` is also accepted when reading). OUs are paths relative to the domain root
(`OU=Sales`, `CN=Users`), so an export from one domain imports into another.

| File | Columns |
|---|---|
| `ous.csv` | `ou;description` |
| `groups.csv` | `group;type;scope;description;ou;members` (members separated by `\|`) |
| `users.csv` | `login;name;given_name;surname;email;phone;title;department;description;enabled;ou;groups` |
| `computers.csv` | `computer;dns;os;ou;enabled` (pre-created accounts; DCs are left out) |

Files exported by older versions still import: the Portuguese file names (`grupos.csv`, `usuarios.csv`,
`computadores.csv`), column names (`nome`, `ativo`, `controle_total_grupos`, …) and yes/no values (`sim`, `não`) are
read as the current ones.

```bash
# from an old DC without a certificate: ldap:// with NTLM bind, read-only
AD_URL=ldap://10.0.0.5 ad-manager export -d old-export

# on the new domain: first the preview, then write
ad-manager import -d old-export
ad-manager import -d old-export --apply   # generated passwords in old-export/generated-passwords-<date>.csv (0600)
```

The import **only creates and updates** — it never deletes objects or removes members — and it is idempotent (running it
again repeats nothing). Built-in accounts (Administrator, krbtgt, Guest) are ignored. New accounts get a generated
password and must change it at first logon. The same flow exists in the panel under **Import/export**, including
directly from another AD.

### Directly from another AD or Samba4 (`import ad`)

The same plan as the panel's "From another AD" tab, without going through CSV. The source is opened for searches only;
its password comes from `AD_SOURCE_PASSWORD_FILE` (a 0600 file) or the terminal, never from an argument.

```bash
# objects (default) and DNS; --profile uses a profile saved in the panel (URL, domain, user)
ad-manager import ad --from ldaps://dc01.source.lab --domain source.lab --user administrator --only objects,dns
ad-manager import ad --profile legacy-2008 --only dns --json      # plan as JSON
ad-manager import ad --profile legacy-2008 --only objects,dns --apply -d .   # writes; passwords in ./generated-passwords-<date>.csv
```

- **DNS**: only static A, CNAME and PTR records (`--dynamic` also brings the workstations' ones), read over LDAP
  (`DomainDnsZones`, `ForestDnsZones` and the old 2000/2003 container); never DC records (of either domain), SOA, NS,
  SRV, `_msdcs` or the zone root. A record already identical on the target stays as is; a name with different data
  becomes `dns.conflict` and a missing zone becomes `zone.missing` — both only show up in the plan. With
  `--create-zones`, the missing zone becomes `zone.create`: it is created before its records, as an AD-integrated
  primary zone (domain partition, secure updates only, like `samba-tool dns zonecreate`; never `_msdcs`). If the
  creation fails, the zone's records are not attempted. Writes go over MS-DNSP with the target credential
  (`AD_USER`), sealed by Kerberos.
- **Exit code**: 0 nothing to do (or everything written), 1 there are changes to apply (without `--apply`), 2 some
  action failed — usable in lab scripts.

> In a migration by joining a DC ([migration-runbook.md](migration-runbook.md)) the import is not needed: Samba
> replicates everything, with passwords and SIDs. The CSV is for new domains, labs and bulk loads.

## Migration

```bash
ad-manager migrate preflight                       # ports, functional levels, schema, FSMO, DCs
ad-manager migrate inventory -o before.json        # JSON without secrets
ad-manager migrate diff before.json after.json     # exits with 1 if there is a difference (ignores DC accounts)
```

### Shares from a Windows server

Reads the shares (read-only, `rpcclient`/`smbcacls` from `samba-common-bin`), computes the effective access of each
group and user (share permission ∩ root NTFS) and recreates **only the structure and permissions** on Samba — without
copying files.

```bash
AD_USER=administrator@example.local ad-manager migrate shares --host 10.0.0.5 \
  --inventory before.json --map 'E:\=/srv/shares' --csv shares.csv          # preview + CSV for the panel
AGENT_TOKEN=… ad-manager migrate shares --host 10.0.0.5 --inventory before.json \
  --apply https://fs01.example.local:9443 --insecure                        # creates directly through the agent
```

| Flag | |
|---|---|
| `--host` | source Windows server |
| `--inventory` | inventory (`migrate inventory`) to know who is a group and who is a member |
| `--map` | Windows → Linux prefix replacement (default `E:\=/srv/shares`) |
| `--csv` | writes a shares CSV to import in the panel (**Import/export → Folders**) |
| `--apply URL` | creates through the agent (token in `AGENT_TOKEN`); `--insecure` accepts a self-signed certificate |

Deleted accounts and deny ACEs are not migrated and show up as warnings in the preview. Details in [agent.md](agent.md).

### Passwords from a source domain (new-domain cutover)

Preserves the users' production passwords after a *From another AD* import, so LDAP/Kerberos apps (Gitea, etc.) keep
working without everyone changing their password. The DC agent, in its dedicated password-migration write mode
(`password_migration_user`), reads the source's hashes over a **sealed, read-only DRSUAPI** and sets them on this
domain's matching users — never over LDAP, never as plaintext, never creating a user, leaving the source unchanged.
Only for the new-domain cutover; a `dc-join` already replicates the hashes. See [migration-runbook.md](migration-runbook.md).
In the panel this is the **Passwords** option of *Import/export → From another AD* (beside Objects/GPOs/DNS/Shares),
which reuses the form's source login and password; the CLI below is for automation.

```bash
ad-manager migrate passwords --from ldap://10.0.0.10 --user 'OLD\administrator' \
  --agent https://dc:9443 --insecure                                         # preview (no secret read)
AD_SOURCE_PASSWORD_FILE=/root/src.pass AGENT_TOKEN=… ad-manager migrate passwords \
  --from ldap://10.0.0.10 --user 'OLD\administrator' --agent https://dc:9443 --insecure --apply
```

| Flag | |
|---|---|
| `--from` | source DC URL (`ldaps://old-dc`, or `ldap://` for 2008 with NTLM) |
| `--user` | replication account on the source (*Replicating Directory Changes* **and** *…All*) |
| `--agent URL` | this domain's DC agent (token in `AGENT_TOKEN`); `--insecure` accepts a self-signed certificate |
| `--apply` | reads the hashes and sets them (otherwise preview only) |

Source password: `AD_SOURCE_PASSWORD_FILE` (a 0600 file) or the terminal — never an argument. Per-user outcome:
`would_set` (preview), `set`, `ntlm_only` (Kerberos salt did not transfer), `same`, `missing` (import the objects
first), `builtin` (RID 500/501/502, trust/machine), `rejected` (`Primary:CLEARTEXT`). The audit (`password.migrate`)
records counts only, never a secret.

## Script examples

```bash
# disabled users as JSON
ad-manager --json user list | jq -r '.[] | select(.enabled == false) | .sam'

# add a list of logins to a group
while read -r login; do ad-manager group add Finance "$login"; done < logins.txt

# initial password from a file (stdin), with a mandatory change
ad-manager user passwd john.smith --must-change < password.txt
```
