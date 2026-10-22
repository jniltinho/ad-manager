# GPOs (Group Policy)

A study of what AD Manager can do with GPOs using LDAP (which the panel already uses) and the Samba 4.23.6 Python API
(which the agent already calls for the Windows permissions, [samba-api.md](samba-api.md)). **Implemented**: phase 1
(inventory and links, read-only, "GPOs" button on the OUs page), the phase 4 backup (through the agent installed on a
DC, `.tar.gz`) and restoring a backup as a new GPO ([below](#restoring-a-backup)). The rest is still a study.

Source: `samba-4.23.6/` at the project root (outside git), Python package indexed in the codebase-memory MCP as
`samba-4.23.6-python`. The paths below are relative to `samba-4.23.6/`.

## How a GPO is stored

A GPO has **two halves**, and both must stay in step:

| Half | Where | What |
|---|---|---|
| Container (GPC) | LDAP, `CN={GUID},CN=Policies,CN=System,<domain>`, class `groupPolicyContainer` | `displayName`, `versionNumber`, `flags`, `gPCFileSysPath`, `gPCMachineExtensionNames`, `gPCUserExtensionNames`, `nTSecurityDescriptor` |
| Template (GPT) | SYSVOL, `\\domain\SYSVOL\domain\Policies\{GUID}\` | `GPT.INI` (version), `Machine\Registry.pol`, `User\Registry.pol`, `Machine\Microsoft\Windows NT\SecEdit\GptTmpl.inf` (password, Kerberos), `User\Scripts\scripts.ini`, `User\Preferences\Drives\Drives.xml`… |
| Link | LDAP, on OUs, the domain and sites | `gPLink` = `[LDAP://cn={GUID},…;options]…` (option 1 = disabled, 2 = enforced); `gPOptions` = 1 blocks inheritance |

- GPO `flags`: bit 1 disables the user part, bit 2 the computer part (`source4/lib/policy/policy.h:27-28`).
- `versionNumber`: low 16 bits = computer, high 16 = user. It has to be **the same** in LDAP and in `GPT.INI`; if only
  the file changes, Windows and `samba-gpupdate` do not reapply it.
- The *extension names* tell Windows which extensions (CSEs) to process: without the right GUID (e.g. Registry
  `{35378EAC-683F-11D2-A89A-00C04FBBCFA2}`, Security `{827D319E-6EAC-11D2-A4EA-00C04F79F83A}`), the setting is
  ignored.

## What the Samba API offers

| Module | What |
|---|---|
| `python/samba/netcmd/gpo.py` (`samba-tool gpo`) | `parse_gplink`/`encode_gplink` (112/130), `get_gpo_info` (150) and the commands below |
| `python/samba/policies.py` → `RegistryGroupPolicies` (49) | edits `Registry.pol` through JSON (`merge_s` 299, `remove_s` 257), `increment_gpt_ini` (161: GPT.INI **and** LDAP), registers *extension names* |
| `python/samba/gp_parse/` | `Registry.pol`, `GptTmpl.inf`, `GPT.INI`, `scripts.ini`, `.aas`, `audit.csv` ↔ readable XML (backup, diff) |
| `python/samba/gp/` + `samba-gpupdate` | Linux **client** side (applying GPOs on a Samba member): sudoers, scripts, smb.conf, firewall, browsers, drive mapping… |
| `python/samba/netcmd/ntacl.py` | `sysvolreset` / `sysvolcheck`: SYSVOL ACL derived from the LDAP one (`ntacls.dsacl2fsacl`) |

`samba-tool gpo` commands (text only, no `--json` in 4.23):

| Command | Does | Needs |
|---|---|---|
| `listall`, `getlink`, `listcontainers`, `getinheritance` | inventory of GPOs and links | LDAP only |
| `list <account>` | effective GPOs of a user/computer (inheritance, enforcement, security filtering) | LDAP only |
| `setlink`, `dellink`, `setinheritance` | link, unlink, enforce, disable, block inheritance | LDAP only |
| `show`, `fetch`, `backup` | GPO content; XML backup (`--generalize`) | LDAP + SYSVOL |
| `load`, `remove` | writes/removes `Registry.pol` keys (JSON) | LDAP + SYSVOL |
| `create`, `del`, `restore` | creates (GPC + folders + ACL), deletes, restores from a backup | LDAP + SYSVOL |
| `aclcheck` | LDAP ACL × SYSVOL ACL | LDAP + SYSVOL |
| `manage security\|scripts startup\|sudoers\|smb_conf\|files\|symlink\|openssh\|motd\|issue\|access` | ready-made settings (the Linux ones only apply to `samba-gpupdate` clients) | LDAP + SYSVOL |
| `admxload` | copies ADMX/ADML to `PolicyDefinitions` | SYSVOL |

## What fits in each part of AD Manager

- **Panel (Go, go-ldap)**: everything that is LDAP only — inventory, links, inheritance, disabling a GPO, effective
  GPOs. It means porting `parse_gplink`/`encode_gplink` and the `cmd_list` logic; no new dependency.
- **Agent on the DC (Python helper)**: everything that touches SYSVOL — content, create/delete, backup/restore. Use
  `RegistryGroupPolicies` and the `cmd_*` classes instead of rewriting binary formats; local paths on the DC itself.
- **Never** through the file server agent: it does not have SYSVOL.

## Risks

- **SYSVOL does not replicate on Samba** (there is no DFS-R): LDAP replicates, the files do not. Every write goes to
  **one DC that owns SYSVOL** (the PDC; `create` already uses the PDC, the other commands can pick another DC), then
  sync it (rsync through the agent) and run `samba-tool ntacl sysvolreset` on the others. See
  [migration-runbook.md](migration-runbook.md) (step 4: GPO editing frozen until the cutover).
- **Version**: always `increment_gpt_ini` (file + LDAP together), never reuse an old version
  (`restore --restore-metadata` brings the backup's version).
- **SYSVOL ACL** comes from the GPO's ACL in LDAP: changing delegation/filtering only in LDAP leaves SYSVOL out of step;
  `aclcheck` and `sysvolreset` afterwards.
- **No atomicity**: `create`/`del` mix LDAP and SMB; a failure midway leaves orphans (`restore` undoes it with `del`).
- **`manage security set`** writes `GptTmpl.inf` in UTF-8 (Windows uses UTF-16LE) and does not register the security
  CSE: fix this before using it for password policy.
- **GPOs made on Windows** (ADMX, Preferences with `uid`/`changed`): edit only known keys, with *merge* (never
  *replace*), keep `**delvals.` and whatever is not understood.
- **Reach**: a wrong GPO linked to the domain root applies to everyone; on a DC, `gp_access_ext` changes the domain
  password policy. Explicit confirmation, auditing and testing only on the Docker Samba and the lab — never on the
  production AD (rule 1 of `AGENTS.md`).

## Restoring a backup

In the "GPOs" dialog (OUs), "Restore a backup as a new GPO": the `.tar.gz` of a backup from the panel and the name of
the new GPO, confirmed by typing the name. Always a **new GPO** (new GUID, no link, version reset); a name in use is
refused (409). Audited as `gpo.restore`, refusals included.

The agent of a DC is read-only; restoring is the only **explicit write mode**, turned on in the `config.toml` of that
DC's agent:

```toml
[agent]
gpo_restore_user = "svc-gporestore"                          # account that creates GPOs
gpo_restore_password_file = "/opt/ad-manager/gpo-restore.pass" # only the password, mode 0600 (root)
```

- A new domain created by the setup wizard with **Prepare the import of GPOs and passwords** has this mode on for its
  administrator (`/opt/ad-manager/import.pass`, shared with the password-migration mode), for the "From another AD"
  import; `sudo rm /opt/ad-manager/import.pass` turns both off ([installation.md](installation.md#import-from-an-existing-ad)).
- The account must be able to create GPOs in LDAP and write the folder and the ACL in SYSVOL. On Samba that means
  **Domain Admins** (the "Group Policy Creator Owners" group is not enough: Samba's `CN=Policies` does not delegate to
  it and the copy to SYSVOL writes the ACL). On a DC the agent's root is already equivalent to a domain administrator;
  the credential does not extend that.
- The password goes to `samba-tool` through `PASSWD_FILE` (never as an argument, which shows up in `ps`). Without both
  parameters, or with the file readable by group/others, the agent's `/api/health` reports `gpo_restore: false` and
  the panel responds 503.
- The file is validated before `samba-tool`: only `policy/{GUID}/` (one GPO), regular folders and files, no `..`,
  absolute path or link, up to 4 MiB compressed and 16 MiB of files.
- **SYSVOL**: Samba does not replicate it. The GPO is born in the SYSVOL of the agent's DC (install the agent in write
  mode on the DC that owns SYSVOL, the PDC); the response lists the other DCs, and the `Policies/{GUID}` folder must be
  copied to them (rsync) with `samba-tool ntacl sysvolreset` before linking the GPO.
- A failure midway (LDAP written, SMB refused) may leave a container without a name in `CN=Policies` — it shows up in
  the list by its GUID; delete it with `samba-tool gpo del {GUID}`.

## Proposed phases

| Phase | What | How | Effort |
|---|---|---|---|
| **1. Read** | GPOs page: name, state, computer/user version, CSEs with friendly names, where it is linked (enforced, disabled, inheritance blocked); "effective GPOs" of a user or computer; MCP tool `ad_gpo_list` | panel, LDAP | M |
| 1b | Health check: LDAP version × `GPT.INI` on each DC (SYSVOL behind), `aclcheck`, `Registry.pol` content as JSON (`show`) | agent on the DC, read-only | M |
| **2. Links** | Link/unlink, reorder, enforce, disable, block inheritance; disable a GPO (`flags`) | panel, LDAP (re-read before writing), audit | S–M |
| **3. Settings** | Create/delete GPOs; `Registry.pol` keys; password policy through `GptTmpl.inf` (UTF-16 + CSE); logon scripts (`scripts.ini`); drive mapping (`Drives.xml`) | agent on the DC that owns SYSVOL; always `increment_gpt_ini` and *extension names*; sync SYSVOL + `sysvolreset` | L |
| **4. Backup** | Scheduled XML backup (`backup --generalize`), diff between versions, always restore as a new GPO with approval | agent on the DC | M |

Effort: S ≤ 2 days, M ≤ 1 week, L > 1 week (with OpenSpec, tests and docs).

For the Windows 2008 migration, phase 1 already helps: inventory of GPOs and links before and after the cutover, and an
alert for out-of-date SYSVOL between the Samba DCs.
