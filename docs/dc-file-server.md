# DC that also serves files

Samba 4 can be **a DC and a file server on the same machine** — common in companies that had both roles on one Windows
Server. The agent always runs on the Samba machine (it is the one that touches `smb.conf`, the ACLs and the folders); on
a DC it only changes files with the mode below turned on.

## When a DC's agent touches files

All of these conditions, checked when the agent starts (`health` shows `files` and, if false, `files_why`):

- `[agent] dc_file_server = true` in `config.toml`;
- Windows ACLs through Samba (`python3-samba`, `vfs objects = acl_xattr`, as on a member);
- `registry shares = yes` in the `[global]` section of `smb.conf` (the panel's shares live in the registry);
- `share_roots` outside the Samba folders (`/etc/samba`, `/var/lib/samba`, private, state, lock, cache, binddns,
  `sysvol`, `netlogon`).

Without them the DC stays read-only on the file pages (Shares shows the warning, Sync and data restores refuse).

## Protections on a DC

- Fixed `smb.conf` sections (`sysvol`, `netlogon` and any other), and names ending in `$`, are reserved: the panel does
  not create, change or delete shares with those names.
- The default owner of the folders is **Domain Admins** (RID 512, from `wbinfo -D`); on a DC there is no fallback to
  `root`.
- Closing an SMB session is refused (it would drop the DC's clients).
- Restoring the file server configuration: only the ACL part (the DC's `smb.conf` is not overwritten by a backup).
- Backup types: `dc` + `fileserver-config` + `fileserver-data` + full.

## Setup

In **Setup** (new-dc or dc-join) check **This DC also serves files (shares)** and enter the root (default
`/srv/shares`). On the command line:

```bash
sudo python3 scripts/samba-setup.py new-dc ... --panel --files --share-root /srv/shares
```

The script creates the root (owner Domain Admins, ACL `DA/SYSTEM` full control, authenticated users read), turns on
`registry shares = yes`, writes the `[agent]` section with `dc_file_server = true`, generates the token and registers
the agent in the local panel (`https://127.0.0.1:9443`). Only the panel reaches the agent, so on this DC the agent
can listen on the loopback only (`listen = "127.0.0.1:9443"` in `[agent]`, then `sudo systemctl restart
ad-manager-agent`): nothing on the network needs port 9443.

## DC already installed

```bash
sudo sed -i '/^\[global\]/a registry shares = yes' /etc/samba/smb.conf
sudo smbcontrol all reload-config
```

Add the `[agent]` section to `/etc/ad-manager/config.toml` (as in `scripts/samba-setup.py`, `DC_AGENT_CONF`), restart
`ad-manager-agent` and register the agent in the panel (File servers).

## Migrating the shares

1. **Import/export → From another AD**, source = old file server, target = the DC's agent, mapping the Windows path
   to the root (`C:\srv\shares=/srv/shares`): creates folders, shares and ACLs.
2. **Sync**: one job per folder copies the data from the old server to the DC (`docs/sync.md`).
