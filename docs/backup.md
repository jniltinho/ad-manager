# Samba and panel backup

**Backup** page (web panel and desktop app), `ad-manager backup` CLI and MCP (`ad_backup_list`, `ad_backup_run`).
The backups are made and kept by the **agent** of each server; the panel lists, requests, downloads, deletes and audits
them (`backup.run`, `backup.download`, `backup.delete`, `backup.schedule`).

## Types

| Type | Where | What it keeps | How |
|---|---|---|---|
| `dc` | agent on a DC | the whole Samba database: `sam.ldb` and the `sam.ldb.d/` partitions (`.ldb`, or `.mdb` with the LMDB backend), `secrets`, `idmap`, `privilege`, the state `.tdb` files, SYSVOL with ACLs and `smb.conf` | `samba-tool domain backup offline` (needs the `tdb-tools` package) |
| `fileserver-config` | agent on a file server | `smb.conf`, `testparm -s`, `net conf list`, SDDL and POSIX ACL of each root, ACL history and the member's Samba databases (`secrets.tdb` with the machine account password, `passdb.tdb`, `registry.tdb`, `winbindd_idmap.tdb`…) | Go + `tar`; databases copied with `tdbbackup` (consistent copy, never the file in use) |
| `samba-files` | agent (setup) | `/etc/samba`, `/var/lib/samba` and `/etc/krb5.conf` as they are, with ACLs and attributes — taken before the setup touches anything | GNU `tar --acls --xattrs` |
| `fileserver-data` | agent on a file server | chosen folders (inside `share_roots`), with `security.NTACL` and ACLs | GNU `tar --listed-incremental` |
| Full (`all`) | agent on a file server | `fileserver-config` + **full** `fileserver-data` of all roots (`share_roots`), in a single action or schedule (retention counts each type's chains separately) | both of the above |
| `panel` | the panel itself | SQLite database (`VACUUM INTO`), `config.toml` without secrets, audit | Go |

The agent of a DC only does the `dc` type (it only reads the database); the other types only run on a file server. A
server with `server role = auto` follows the `security` setting of `smb.conf` (`user` = standalone, `ads`/`domain` =
member).

**A DC backup contains the domain secrets** (password hashes, Kerberos keys); the file server one carries the machine
account password (`secrets.tdb`); the panel one carries the SQLite database with the **agent tokens**. Treat these
files as secrets: only signed-in administrators download them, and every download is audited.

In "Data folders", **Full backup** forces a full backup now (new chain), without waiting for "New full every N days".
Data backups never include `_restored/` (the restored copies; nor `_restaurado/`, its name in earlier versions).

Deleting an incremental closes the chain (the next backup is a full one again), and nothing is deleted while a backup
is running on the server.

## Where they are kept

`backup_dir` in the `[agent]` section of `config.toml` (default `/var/backups/ad-manager`, 0700 root): each backup is
`<id>.tar.bz2|.tar.gz` (0600) with a `<id>.json` manifest (type, origin, who requested it, size, SHA-256, status,
log). The panel keeps its own in `<state folder>/backups`. A backup does not start with less free space than twice the
last full backup of the same type, and only one backup runs at a time on each server.

## Chains and retention

`fileserver-data` forms **chains**: the first backup is full; the following ones are incremental (only what changed),
until the full one is `full_every_days` old (default 7) — then another chain starts. A schedule's retention (`chains`:
how many chains to keep; `days`: maximum age) **only deletes whole chains** and never the backup just taken. Manually
deleting a full backup takes its incrementals with it; deleting an incremental takes the ones that come after it.

## Schedules

They live in the agent (`backup_dir/schedules.json`) and run with the panel turned off (asynq scheduler in the agent
itself). On the page: name, type, `cron` (minute hour day month weekday, or `@daily`), retention, new full every N days
and the folders (data).

## Restoring

From the Backup page (restore icon on the row), from the desktop app or from the CLI (`ad-manager backup restore`).
Before anything else the server checks the file's SHA-256 (and that of each file in the chain) and the names inside it
(nothing absolute, nothing with `..`, nothing outside the backup's folders, no special files). Anything that replaces
what is in use asks for the **last 8 characters of the id** to be typed and first takes a backup of the current state
(origin "before a restoration"). Everything goes to the audit log (`backup.restore`); the last restoration shows up on
the page with target, status and log (`backup_dir/restore.json` on the agent).

| Type | What happens |
|---|---|
| Data | The chain (the full backup and the incrementals up to the chosen one, in order, as GNU tar reapplies them) goes to **a new folder**: `<root>/_restored/<date>/<original path>`, with the Windows permissions (`security.NTACL`) and the POSIX ACLs. Choose "Over the original folders" to bring the folders back to the backup's state: they are backed up first and files created afterwards disappear. You can restore only some of the backup's folders, or a folder inside them (e.g. `/srv/shares/Finance` from a full backup). |
| Configuration | Parts of your choice: registry shares (`net conf import`, tested first with `-T`), permissions of the roots that still exist (`samba-tool ntacl set` + `setfacl --restore`) and `smb.conf` (checked with `testparm`; the current one is kept in `smb.conf.before-restore`). The member's databases (`samba/`) are not restored from the page: with Samba stopped, put them back in `/var/lib/samba/`. |
| Samba files (before the setup) | Samba stops; `/etc/samba`, `/var/lib/samba` and `/etc/krb5.conf` are moved aside (`…before-restore-<time>`) and come back as the backup has them; Samba starts again. On a DC this undoes the domain created afterwards. |
| Panel | The backup's database is checked and placed next to the current one, the current one becomes a backup, the panel exits and systemd starts it again with the restored database (the previous one is kept in `ad-manager.db.before-restore`). `config.toml` and the audit log are not restored. |
| DC | **Never over a DC in use.** On a new server (Ubuntu 26.04 with the `.deb`): download the backup (type `dc`), copy it to the server and choose **Restore a DC** in the initial setup, or run `sudo python3 scripts/samba-setup.py restore-dc --realm <domain> --ip <IP> --backup /root/<file>.tar.bz2 --panel`. The domain comes back with this server as the only DC (`samba-tool domain backup restore --newservername`, in the normal `/var/lib/samba` paths). The FSMO roles, users, passwords and SYSVOL come from the backup, and the self-generated TLS certificate is recreated for the new name. The old DC must stay turned off. |

### Sending a backup

"Send a backup" uploads a `.tar.gz` downloaded from another Backup page (another server, or one kept elsewhere) to the
chosen agent, without going through the panel's disk. The agent checks the content for the type (`smb.conf` in a
configuration backup; the folders of a data backup; only Samba paths in "Samba files"; an `ad-manager.db` SQLite
database in the panel one), computes the SHA-256 and lists the backup with origin "sent"; from there it restores like
the others (an uploaded data backup is a single-file chain). Limit: `backup_upload_max_mib` in `[agent]` (default
20 GiB); 4 GiB on the panel.

### From the CLI

```bash
ad-manager backup list
ad-manager backup restore fs1 20261018T020000Z-fileserver-data-1a2b3c4d                     # new folder
ad-manager backup restore fs1 20261018T020000Z-fileserver-data-1a2b3c4d --target original --confirm 1a2b3c4d
ad-manager backup restore fs1 20261018T021500Z-fileserver-config-5e6f7a8b --part shares,acls --confirm 5e6f7a8b
ad-manager backup upload fs1 backup-config.tar.gz --kind fileserver-config
```

### By hand

- **Data**: `tar --extract --listed-incremental=/dev/null --acls --xattrs --xattrs-include='security.NTACL' --xattrs-include='user.*' --file=<each file> -C /`
  in order (without the `--xattrs-include` options, GNU tar only restores `user.*` and the Windows permissions are lost).
- **Panel**: stop the service, replace `ad-manager.db` with the backup's one and restart.
