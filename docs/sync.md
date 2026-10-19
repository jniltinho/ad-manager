# Folder sync

**Backup → Sync** page (web panel), **Sync** page of the desktop app and `ad-manager sync` CLI. The agent of a file
server **pulls folders from an SMB share** (Windows, Samba from another AD or from the same domain) into a folder
inside its roots, with the **rclone engine embedded** in `ad-manager` (`third_party/rclone`, v1.75.2): nothing
external, no CIFS mount.

## Job

| Field | What it is |
|---|---|
| Source | server (name or IP), share and folder inside it. Never `C$`, `ADMIN$`, `IPC$`, `SYSVOL`, `NETLOGON`. |
| Authentication | **Kerberos** with the server's machine account (default; no stored password: the agent gets a ticket with `net ads kerberos kinit -P`) or **user and password** (NTLM: another domain, standalone Windows). |
| Destination | a real folder inside a root (not the root, not `_restored`/`_sync` nor their earlier names `_restaurado`/`_sincronizacao`, no link in the path, no mount point inside). |
| Mode | **copy** (adds and updates; never deletes) or **mirror** (also deletes what disappeared from the source). |
| Mirror limits | maximum number of deleted files and % of the destination; an empty source never deletes anything. |
| Exclude | rclone patterns, one per line (`*.tmp`, `Thumbs.db`, `/Old/**`). |
| Bandwidth, transfers | `10M`, or by time of day: `08:00,2M 19:00,off`; up to 16 transfers. |
| Schedule | cron (`0 1 * * *`), runs on the agent with the panel turned off. |
| Verify content | reads the source again and compares (SMB has no hash); without it, size and date. |
| Windows inheritance | after the run, the destination folder's ACL is reapplied to what arrived (rclone writes outside Samba). |

**Always signed SMB**: rclone's SMB backend was adjusted to require signing (`third_party/rclone/PATCHED.md`); a
server that does not sign, a guest session or an anonymous one are refused. SMB3 encryption when the server requires
it.

### Wizard

**+ New** opens a five-step wizard:

1. **Source and access** — server and how to sign in (machine account through Kerberos, or user and password;
   `DOM\user` and `user@dom` are split automatically). **Test connection** shows each stage — credentials, resolved
   name, port 445, sign-in with signed SMB (and how many shares the account sees) — and, on the one that failed, the
   likely reason (DNS, firewall, server without signing, KDC, name unknown to Kerberos, password, access denied).
   "Next" tests first, if you have not tested yet.
2. **What to copy** — the source's shares and folders; "size" counts the files and bytes of a folder.
3. **Where to** — this server's folders (or "New folder…", or type another one), checked right away: whether it can
   be a destination, whether it already exists and has items, the share and the free space compared with the size of
   the source.
4. **How and when** — copy or mirror (with the limits and the quarantine explained), when (presets or cron, with the
   next runs in the server's time zone), the summary, and "Advanced" (exclusions, bandwidth, transfers, verify
   content, inheritance). **Save and preview** saves the job and starts the preview.
5. **Preview of the copy** — follows the preview right there (what is new, changed or would be deleted, with sizes and
   samples); from there, "Run now".

Schedules (backup and sync) follow the server's time zone.

### Password (user and password)

Typed in the wizard, the password is stored **encrypted** (AES-256-GCM): in the panel database (`sync_secrets`, key in
`data/secret.key`, 0600) and on the agent (`backup_dir/sync/secrets/<job>`, key `backup_dir/sync/secret.key`, 0600
root), so that schedules run with the panel turned off. It never comes back through the API (the page shows "kept"),
never goes to a log, queue or audit. Deleting the job removes both copies. If you prefer, through the API you can give
`password_file` (a 0600 root file on the server) instead of the password.

### Kerberos

The server's machine account needs read access on the share and on the source folders (for example,
`Domain Computers` or the `FS01$` account itself). Source server name by FQDN (the SPN is `cifs/<name>`); the KDCs are
found through DNS.

## Preview, run and mirror

- **Preview**: the full plan (new, changed, to delete, identical, errors), with the first names of each group, without
  writing anything.
- **Run**: the plan comes first; with a listing or read error, nothing is changed. Deleted or replaced files go to the
  **quarantine** `<root>/_sync/<job>-<run>/` (root only), they never just disappear (see
  [Quarantine](#quarantine)).
- **Mirror**: saving asks for the id to be typed; running by hand requires a clean preview of the current version
  (editing the job invalidates it). Above the limits or with an empty source, it stops before deleting any file.
  Scheduled, the plan and the limits apply to each run.
- Each preview or run executes in a child process (`ad-manager agent sync-worker`) in a transient systemd unit that can
  only write the destination and the quarantine (opened without following links and handed to systemd pinned), with no
  devices, with umask 077. Without systemd (a container) the sync does not run, unless `sync_unconfined = true` is set
  in `[agent]`. The quarantine must be on the same file system as the destination.
- The mirror copies first and then deletes **only the files in the plan** that the source still does not have (each
  one checked again): a source that changed midway never increases what gets deleted. Links and junctions in the
  source are skipped (never followed). Its own queue (`<queue>-sync`), one at a time: it does not hold up backups or
  folders. Two syncs never write the same tree.
- Audit: `sync.save`, `sync.delete`, `sync.preview`, `sync.run`, `sync.cancel`, `sync.quarantine.restore`,
  `sync.quarantine.download`, `sync.quarantine.empty`. History of the last 200 runs on the agent
  (`backup_dir/sync/`), with the rclone log.

## Quarantine

Each run keeps what it deleted or replaced in `<root>/_sync/<job>-<run>/`, with the same paths as in the
destination (quarantines kept in `_sincronizacao/` by earlier versions are still listed and given back). The agent records in its own manifest (`backup_dir/sync/quarantine/`) the job, its **generation** and the
**destination of that run**. A job deleted and recreated with the same id neither sees nor deletes the previous one's
quarantines. They stay on disk until someone removes them by hand.

- **Retention** (step 4 of the wizard): days (default 30) and, optionally, GiB. After each run, the agent deletes the
  job's oldest quarantines, never the one just created. First those past the deadline go; then the oldest ones, until
  it fits the GiB limit. The removal goes to the agent log (`sync.quarantine.prune`) and to the run notes. A run that
  moved nothing leaves no folder.
- **Page**: the jobs' *Quarantine* column shows the space used and opens the list per run. Each run shows its files,
  with **Give back** (selected or all) and **Download**; **Empty the quarantine** asks for the job id. On the desktop,
  use the *Quarantine* button with the job selected.
- **Give back** moves the file back to its path in the destination of that run (not the job's current destination).
  Folders are opened one by one, without following links, and missing ones are created. If a file already exists at
  that path, it shows up as a conflict. With *replace*, the two swap places: the current one goes into the quarantine,
  and nothing is lost. If the job reapplies the Windows permissions, they are reapplied afterwards. It does not run
  while a run is writing that destination.
- **Free space**: with less than 10% or 5 GiB free on a root's disk, the page and the run notes warn about it.

## CLI

```bash
ad-manager sync list fs01
ad-manager sync preview fs01 old-finance
ad-manager sync run fs01 old-finance
```

## Limits of this phase

Only **pull** (SMB → this server). Push, local→local and copying translated ACLs are left for phase 2
(`openspec/specs/folder-sync`). To migrate a Samba server from the same domain with the ACLs as they are,
`rsync -aAX` by hand is still the way.
