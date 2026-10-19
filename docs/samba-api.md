# The Samba 4 API and the agent — study and proposal

A study of what Samba's own API (Python bindings and tools) allows the **agent** to do (`ad-manager
agent`, which runs as root on each file server) and, in the future, an agent installed on the DC. The **first
phase is implemented** (Windows permissions on folders, section [Implemented](#implemented)); the rest remains
a proposal. The spec is [`openspec/specs/agent-samba`](../openspec/specs/agent-samba/spec.md). GPOs have their own
study in [gpo.md](gpo.md).

## Sources

- Samba **4.23.6** source code (same version as the VMs and containers) in `samba-4.23.6/`, at the project root
  (ignored by git; download it again with `git clone --depth 1 --branch samba-4.23.6 https://git.samba.org/samba.git
  samba-4.23.6`).
- The Python package (`samba-4.23.6/python/`) is indexed in the codebase-memory MCP as project
  **`samba-4.23.6-python`** (≈11 thousand symbols): use `search_graph`/`get_code_snippet` to find classes and functions.
- Checked (read-only) in the test containers: Samba 4.23.6 on Ubuntu 26.04.1; the DC has every module; the
  file server image **did not have python3** (`python3-samba`, which also ships `samba-tool`, was only a
  *Recommends*).

## What the API offers

| Area | Module / tool | Where it works | Access |
|---|---|---|---|
| NTFS permissions (Windows ACL) | `samba.samba3.smbd.set_nt_acl` / `get_nt_acl` (through the `acl_xattr` module) | file server, DC | local root |
| Security descriptors, SIDs, SDDL | `samba.dcerpc.security`, `samba.security.access_check` | anywhere | — |
| Registry shares (the same as `net conf`) | `samba.samba3.smbconf.init_reg` (`create_set_share`, `get_config`, transactions) | file server, DC | local root |
| Sessions and open files | `smbstatus --json`; disconnect with `smbcontrol` | file server | local root |
| Domain join health | `net ads info --json`, `net ads testjoin`, `wbinfo -t` | file server | local root |
| Recycle bin, file auditing, previous versions, ABE | parameters and `vfs objects` (`recycle`, `full_audit`, `shadow_copy2`, `access based share enum`) | file server | smb.conf/registry |
| DC replication and health | `samba-tool drs showrepl --json`, `drs uptodateness --json` | DC (or remote) | machine account / operator |
| AD DNS | `samba.dcerpc.dnsserver` / `samba-tool dns` (MS-DNSP) | remote, on a DC | operator |
| GPO (backup, listing) | `samba-tool gpo backup`, LDAP `groupPolicyContainer` | DC / LDAP | operator |
| DC consistency and backup | `samba-tool dbcheck`, `ntacl sysvolcheck`, `domain backup offline` | DC | local root |
| Sites and subnets | `samba-tool sites ... --json`, LDAP | remote | operator |

Only a few commands have JSON output in 4.23: `smbstatus`, `drs showrepl/uptodateness`, `sites`, `net ads info`.
The rest of `samba-tool` (user, group, dns, fsmo, gpo list…) is **text only**, fragile to parse.

## Implemented

| | What | Where |
|---|---|---|
| Helper | `internal/sambabridge/bridge.py`, **embedded in the binary** (`go:embed`) and run with `/usr/bin/python3 -I -s -B -c` — there is no separate file to install or tamper with. It stays up (one for queries, another for long propagations) and serves one request at a time over a Unix socket, with folders passed as descriptors (`SCM_RIGHTS`); per-request deadline, restarted when it dies, stalls or is older than 1 hour | `internal/sambabridge` |
| Go reader | `security.NTACL` v4 read in the agent, without the helper, only when Samba would return the stored descriptor: SHA-256 hash of the POSIX ACL (`smb_acl_wrapper`) equal to the xattr's, `acl_xattr` in `[global]` with the default name and no share; SDDL equal to `as_sddl()`'s. Any other case (old version, `setfacl`/`chmod` outside Samba, unusual ACE) goes to the helper. Parity: `go test -tags parity` on the server (`ntacl_parity_test.go`) | `internal/sambabridge/ntacl*.go` |
| Operations | `capabilities` (python3-samba, `acl_xattr` in `[global]`, versions), `get_acl`, `set_folder_acl` | helper |
| Folders | `folder:create`/`share:create` write the Windows ACL when `capabilities` says it can; otherwise, the old POSIX path | `internal/shares/ntacl_linux.go` |
| Levels | **Full control** (`0x1F01FF`), **Modify** (`0x1301BF`), **Read and execute** (`0x1200A9`), `OI|CI` inheritance, protected DACL; owner Domain Admins; Administrators and SYSTEM always with full control | helper + agent |
| View ACL | `GET /api/acl?path=` on the agent, `GET /api/agents/{id}/acl` on the panel (share editing), `ad-manager agent acl`, MCP `ad_share_acl` | agent, panel, CLI, MCP |
| History | SDDL before and after each change in `data/acl-history.jsonl` (`0600`) | agent |
| Migration | `migrate shares` and the CSV carry full control (`full_control_groups`, `full_control_users`) | `internal/winshares`, `internal/shares` |
| Installation | `python3-samba` in `samba-setup.py` (role `agent`/`member --agent`), in `deploy/install.sh`, in the `.deb` (*Recommends*) and in the `docker/fileserver` image | |
| Diagnostics | `ad-manager agent check`; `/api/health` with `nt_acl` and `samba`; startup log with `nt_acl`, `samba`, `samba_api_error` | |

Validated in the lab (`fs9`, Samba 4.23.6, member of the `TESTE9` domain): folder created by the panel with Full
control for one group and read for another; the user of the first writes and changes the ACL (`smbcacls`), the user
of the second only reads (`NT_STATUS_ACCESS_DENIED` on write), a subfolder created over SMB inherits the ACL, and
editing writes the previous ACL to the history.

### How it works

1. The agent opens the folder with `O_NOFOLLOW|O_DIRECTORY`, checks that the real path is under the root and passes
   the **descriptor** to the helper as fd 3 (`ExtraFiles`); the helper checks that `/proc/self/fd/3` is that folder
   and uses that path — swapping the folder for a link midway does not help (no TOCTOU).
2. Names become SIDs in the agent (`wbinfo --name-to-sid`; on a server without a domain, `getent` → `S-1-22-2-gid`).
3. The helper builds the descriptor through the API (`security.descriptor`, `security.ace`), never from SDDL coming
   from outside, and writes it with `smbd.set_nt_acl(..., system_session_unix())` with the server's `smb.conf`
   (`param.default_path()`). `smbd` writes the `security.NTACL` xattr **and** the equivalent POSIX ACL.
4. It reads it back (`get_nt_acl`) and returns owner, group, SDDL and ACEs; the agent writes the history.

### Review findings (Codex) and decisions

- **`acl_xattr` must be in `[global]`**: the helper uses the global `vfs objects` (`service=None`); with the module
  only on one share, `capabilities` says it cannot and the agent stays in POSIX mode.
- **Fail closed**: a domain-qualified name (`EXAMPLE\x`) that winbind does not resolve is refused; turning it into a
  Unix SID would give access to another account as far as Windows is concerned.
- **Never `setfacl` over a Windows ACL**: without the helper, the agent refuses to touch a folder with
  `security.NTACL` and does not `chmod` parent folders that have a Windows ACL for traversal.
- **History before overwriting**: if the folder already had a Windows ACL, it is read and saved first.
- **Clean environment and trusted interpreter**: fixed `PATH`/`LANG`, `Dir=/`, root-owned `python3` not writable by
  group/others; limited output (4 MiB / 64 KiB) and timeout.
- **Approximate mapping**: `smbd` derives the POSIX ACL from the Windows one (Linux has no "change permissions"
  or "this folder only"); whoever accesses it from Linux sees an approximation. What counts for Windows is
  `security.NTACL`.
- **Second review (implementation)**: a path with a control character is refused (a line break would inject
  parameters into `smb.conf`); the history writes the previous ACL **before** the change; full control in the
  share list is recognized by SID and ignores inherit-only ACEs; only the complete mask (or `GENERIC_ALL`) counts as
  full control in the migration; a server without a domain uses a group SID as the descriptor's group; the MCP only
  talks HTTPS to the panel (HTTP only on loopback) and does not follow redirects.
- **Known limits**: Samba's `get_nt_acl` reopens the folder through the resolved path of `/proc/self/fd/3`
  (folders are already created with `mkdirat`/`openat` from the root, without following symbolic links).
- **`valid users` × ACL**: the share limits who gets in (`valid users`, `write list`); the ACL decides what each
  one does inside. Administrators/SYSTEM in the ACL do not grant share access to anyone not in `valid users`.

## What changes for the agent

**Before**, the agent created the folder with `chgrp` + `chmod 2770` + `setfacl` (POSIX ACL) and published the share
with `net conf import`. Windows **saw a synthesized ACL**: no "Modify" vs. "Full control", no "this folder only",
no deny, owner always root.

**With the Samba API**, the agent writes the Windows ACL through `smbd` itself (`set_nt_acl` with the `acl_xattr`
the server already uses): the result is the `security.NTACL` xattr **and** the equivalent POSIX ACL, so Explorer,
`icacls` and Linux show the same thing, and permissions migrated from Windows (`migrate shares`) arrive intact.

> Careful: `acl_xattr` stores a hash of the POSIX ACL. If someone later runs `setfacl` on a folder with a Windows
> ACL, Samba **silently discards the Windows ACL**. One model per tree: after migrating, never `setfacl` again.

## How to integrate in Go

| Option | Verdict |
|---|---|
| Run `samba-tool`/`net`/`smbstatus` | Only where there is JSON (`smbstatus`, `drs`, `sites`, `net ads info`) or for one-off maintenance (`dbcheck`, `gpo backup`, `testparm`) |
| **Embedded Python helper** (embedded in the binary, `python3 -I`, persistent, JSON over a Unix socket, closed list of operations) | **Chosen** for Windows ACLs and registry shares: uses Samba's own code, structured input and output, easy to test against the Docker Samba |
| cgo with the Samba libraries | **Rejected**: the registry init is private, the rest lives in private libraries without a stable ABI, and it would break the static build (`CGO_ENABLED=0`) and cross-builds |
| Pure Go | ACL backup/restore by reading the xattrs; LDAP (already used); in the future DNS, replication and sessions from the **panel** with `github.com/oiweiwei/go-msrpc` using the operator's credential |

Rules that still apply: Go remains the only daemon; directory writes still go over LDAP with the operator's
credential, from the panel; the agent never receives an operator password for file server operations.

## Proposed priorities

| | Feature | For the administrator | How | Where | Effort |
|---|---|---|---|---|---|
| ✅ **P1.1** | Create folders with a Windows ACL (instead of `setfacl`) | Permissions identical to Windows; faithful migration | helper: `set_nt_acl` with full SIDs (Modify for write, Read and execute for read, Full control for Admins/SYSTEM, inheritance) | files | M |
| ✅ **P1.2** | View a folder's ACL in the panel | Who has access, inherited or explicit | helper: `get_nt_acl` → SDDL → ACEs; names from LDAP | files | S |
| **P1.3** | ACL snapshot and restore before any change | "Undo" for permissions | ✅ history with the before/after SDDL; restore (`set_acl_sddl`) with propagation to subfolders and files (walk without following links) | files | M |
| **P1.4** | Sessions and open files, with disconnect | Support: who has the spreadsheet locked | `smbstatus --json`; `smbcontrol` | files | S |
| **P1.5** | Replication health (agent on the DC) | Safety at the migration cutover | `samba-tool drs showrepl --json` with the machine account, read-only | DC | S–M |
| P2 | Shares through the registry API; recycle bin and ABE; file auditing (`full_audit`); join health; DNS from the panel (`go-msrpc`); GPO inventory and backup ([gpo.md](gpo.md)); `dbcheck`/`sysvolcheck` report; share permissions (`sharesec`, read) | | | | |
| P3 | Per-folder quotas; previous versions (snapshots); propagate ACLs on existing trees; effective permission calculator; DFS links; scheduled DC backup; sites | | | | |

Effort: S ≤ 2 days, M ≤ 1 week, L > 1 week (with OpenSpec, tests and docs).

## Security

- **Password never on the command line or in the queue**: tasks stay in Redis as plain text for 7 days. When a
  tool needs a password, use `PASSWD_FD` (pipe) or stdin; prefer designs where the agent does not need a
  password (local root on the file server, machine account on the DC).
- **Hardened helper**: fixed path, root-owned, `python3 -I`, empty environment, no shell, timeout, limited
  output, strict JSON; a write that reached the helper and got no response (deadline, crash) comes back as an
  **uncertain result** ("the change may have been applied"): the agent kills the helper and does not retry on its
  own — reread the ACL and check the history; it revalidates everything again (allowed roots, no symbolic links,
  names, SIDs only in the `S-1-5-21-…` format or a few well-known ones); it builds the descriptor through the API,
  never by concatenating SDDL coming from the network.
- **Server-side protections** (rule 3 of `AGENTS.md`): never touch `sysvol`/`netlogon` through the folders API,
  refuse an ACL that removes Administrators/SYSTEM, refuse to delete DNS records of DCs.
- **Agent on the DC = higher risk**: read-only by default; directory writes still go through the panel (LDAP);
  domain backups contain every password hash. Test only against the Docker Samba and the lab — never against the
  production AD.
- **Audit**: every operation in `audit.log` with operator, server, operation, path, SIDs, result; for ACLs,
  the before and after SDDL (allows undoing).

## Packaging

- File server: `python3-samba` is installed with the agent (`samba-setup.py member --agent` and `agent`, which
  use `--no-install-recommends`; `deploy/install.sh` when `smbd` is present; in the `.deb`, `recommends`). The
  `docker/fileserver` test image has it too.
- DC: already has it (`samba-ad-dc` depends on `python3-samba`).
- The agent tests the helper at startup (`capabilities`) and reports it in `/api/health`; without it, folders keep
  POSIX ACLs and `/api/acl` answers 501, instead of failing the task.
