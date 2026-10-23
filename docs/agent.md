# File server agent

`ad-manager agent` runs on the Samba file server (a domain member — `scripts/samba-setup.py member --agent`, which
also installs and configures the agent; see [samba-install.md](samba-install.md))
and creates folders and shares at the panel's request. The panel never runs remote commands: it calls the agent's
HTTPS API with a **token**; the agent validates the request and puts the task on an **asynq/Redis queue**; the
worker in the same process runs it (with retries) and the result can be queried by id.

![creating or editing a share](img/share-flow.png)

```
panel ──HTTPS + Bearer──▶ agent (API) ──▶ Redis (queue fs-<host>) ──▶ worker ──▶ mkdir + Windows ACL (Samba), net conf
                                                   ▲
panel /asynqmon (logged-in admin only) ────────────┘   (the agent exposes no monitor: Redis may be shared)
```

## Install

```bash
sudo apt install ./ad-manager_X.Y.Z_amd64.deb redis-server acl python3-samba   # binary, upgrade, removal: install-guide.md
sudo ad-manager agent check           # Samba Python API, acl_xattr, permissions mode
sudo ad-manager agent token           # shows the token ONCE; stores only the SHA-256
sudo systemctl enable --now ad-manager-agent
```

The token can also be generated in the panel, under **Settings → File server agent token**: the panel shows the
token (to register under **Shares → + New**) and the command that writes only the hash on the file server
(`/opt/ad-manager/agent.token`) and restarts the agent.

`/opt/ad-manager/config.toml` (agent section):

```toml
[redis]
addr = "127.0.0.1:6379"        # password via AD_REDIS_PASSWORD; between hosts use tls = true
[agent]
listen = "0.0.0.0:9443"
cert_file = "/opt/ad-manager/tls/tls.crt"
key_file = "/opt/ad-manager/tls/tls.key"
token_file = "/opt/ad-manager/agent.token"
queue = "fs-fs01"              # default: fs-<hostname>; one queue per server
share_roots = ["/srv/shares"]  # only creates inside these roots
```

In the panel: **Shares → + New** with the name, the URL `https://server:9443` and the token.

**Who reaches port 9443: only the panel.** The desktop app goes through the panel, never to an agent. When the agent
runs on the panel's own server (a DC with the panel, `dc_file_server = true`), make it listen on the loopback only —
`listen = "127.0.0.1:9443"`, with the agent registered in the panel as `https://127.0.0.1:9443`; the CLI's
`--agent`/`--apply` on that server then use `https://127.0.0.1:9443` too. On a separate file server keep the address
the panel reaches and allow 9443 only from the panel's IP in the firewall.

## API (Bearer)

| Method | Route | What |
|---|---|---|
| GET | `/api/health` | no token: queue, version, `nt_acl` (Windows permissions active) and Samba version |
| GET | `/api/shares` | shares in the registry (`net conf list`) |
| GET | `/api/dirs?path=` | roots or subfolders |
| GET | `/api/acl?path=` | the folder's Windows ACL: owner, group, SDDL and each ACE with its name (501 if the server only has POSIX ACLs) |
| GET | `/api/sizes[?refresh=1]` | total size (`du -sb`) of each share's folder; 5-min cache |
| POST | `/api/tasks` | `{"type": "...", "payload": {...}}` → 202 with the id |
| GET | `/api/tasks/:id` | state (`pending`, `active`, `retry`, `completed`, `archived`), error and result |

Tasks:

| Type | Payload |
|---|---|
| `folder:create` | `path`, `owner_group`, `full_groups[]`, `rw_groups[]`, `ro_groups[]`, `full_users[]`, `rw_users[]`, `ro_users[]` |
| `share:create` | `name` (a trailing `$` = hidden), `path`, `comment`, `full_groups[]`, `rw_groups[]`, `ro_groups[]`, `full_users[]`, `rw_users[]`, `ro_users[]`, `hidden`, `create_path` |
| `share:delete` | `name` (unpublishes; the folder and the files remain) |

Groups in winbind format: `EXAMPLE\Finance`. The share gets `valid users`, `read only = yes` and
`write list` (write and full control). **Domain Admins** are always in both lists, just as administrators
are in every share on Windows. This way the folders' owner can manage them, back them up and migrate
over SMB (the "From another AD" import and Sync use the administrator account). The group does not appear in the
list on screen, and the folder's Windows ACL still decides what each one can reach. The folder permissions depend
on the server (see the section below).

## Windows permissions (NTFS ACL)

With `python3-samba` installed and `vfs objects = acl_xattr` in `[global]` (`samba-setup.py member` already sets it),
the agent writes the **Windows ACL** through Samba's own API (`smbd.set_nt_acl`, a Python helper embedded in the
binary, [samba-api.md](samba-api.md)). Explorer, `icacls` and Linux show the same thing:

| Level in the panel/CSV | On Windows | Mask |
|---|---|---|
| Full control (`full_*`, CSV `full_control_*`) | Full control: writes, changes permissions and takes ownership | `0x1F01FF` |
| Write (`rw_*`, CSV `write_*`) | Modify | `0x1301BF` |
| Read (`ro_*`, CSV `read_*`) | Read & execute | `0x1200A9` |

- Owner **DOMAIN\Domain Admins** (on a server without a domain, root); the descriptor's group = the folder's owner
  group. **Administrators** and **SYSTEM** always have full control (they are never left out).
- Inheritance to subfolders and files (`OI|CI`); the folder's ACL is protected (does not inherit from the parent).
- Names are turned into SIDs by winbind (`wbinfo --name-to-sid`); a domain-qualified name that winbind cannot
  resolve is refused (a local `S-1-22-…` SID would be a different account to Windows).
- Each change records the **before** SDDL (written and synced to disk before touching anything; if that fails,
  nothing changes) and the **after** SDDL in `/opt/ad-manager/data/acl-history.jsonl` (`0600`): that is all it
  takes to put the permissions back.
- When editing a share, the panel shows the folder's real permissions (`/api/acl`), including inherited and deny
  entries.
- Full control appears in the panel by reading the ACL; `smb.conf` only knows "write".

Without `python3-samba` or without `acl_xattr` the agent keeps the old mode: `root:<owner group>`, `2770` (setgid)
and POSIX ACLs with inheritance (`rwx` for write and full control, `r-x` for read). It **refuses** to apply
`setfacl` to a folder that already has a Windows ACL (Samba would silently discard the ACL): install
`python3-samba`.

```bash
sudo ad-manager agent check                               # what this server supports
sudo ad-manager agent acl /srv/shares/Finance             # ACEs, owner, group and SDDL
sudo ad-manager agent acl set /srv/shares/Finance --rw 'EXAMPLE\Finance' --ro 'EXAMPLE\Management'
```

Intermediate folders the agent needs to create (e.g. `/srv/shares/Users` for `Users/Public`) and the folders
above a nested share only get traversal for others (`o+x`, no listing) — the equivalent of Windows'
"bypass traverse checking". Inside a share, the intermediate folder inherits the parent's ACL.

## Migrate shares from a Windows server

Structure and permissions only: empty folders, no files are copied.

```bash
# 1. read (read-only on Windows; needs rpcclient/smbcacls from samba-common-bin)
AD_USER=administrator@example.local ad-manager migrate shares --host 10.0.0.5 \
  --inventory inventory.json --map 'E:\=/srv/shares' --csv shares.csv
```

The access of each group/user is the **effective** one: share permission ∩ NTFS ACL of the folder root, in
three levels: **full control** (Full control, or write + change permissions + take ownership), **write**
(Modify) and **read**. Domain Admins come in as full control. Deleted accounts (SID or RID in hexadecimal) and deny
ACEs are not migrated and show up as warnings in the preview.

2. In the panel, **Import/export → Folders**: choose the destination server, upload `shares.csv`,
   check the preview (create/update/unchanged) and submit. Or directly from the CLI with `--apply https://fs01:9443`
   (token in `AGENT_TOKEN`). The same CSV comes out of a Samba server under **Export → Folders**.

## Security

- The agent runs as root (chown, ACL, `net conf`): paths only inside `share_roots`, no `..` and no
  components that are symbolic links; reserved names (`sysvol`, `netlogon`, `IPC$`…) refused; groups
  validated; commands always with separate arguments (no shell). Validation in the API **and** again in the worker.
- 256-bit token, stored only as SHA-256 (`0600`), compared in constant time; the panel stores the token
  in SQLite (`0600`) and never returns it through the API.
- HTTP without TLS only with `--insecure-dev` on loopback.
- Folders are created component by component from a descriptor of the root (`mkdirat`/`openat` with `O_NOFOLLOW`):
  a component swapped for a symbolic link is refused and nothing is created outside the root; owner, mode and ACL
  are applied through the descriptor and only if the real path is still inside the root (the roots must be real
  paths, without symbolic links).
- A share is published in one go by `net conf import` (it is never left without `valid users`); tasks run
  one at a time; removing something already removed is a success (queue redelivery).
- Samba helper: `/usr/bin/python3 -I -s -B` (only if owned by root and not writable by others), clean environment,
  timeout, limited output, strict JSON; the folder is passed as an already opened and checked descriptor (fd 3),
  never by path alone; the helper revalidates the root, system paths and SIDs.
- The panel's proxy does not follow redirects (the token does not leak to another origin).
- Every task requested by the panel goes to the audit log (`share:create fs01:Finance /srv/shares/finance`).

## Test locally

```bash
make build && docker compose -f docker/compose.yml up -d --build redis fileserver
curl -sk -H 'Authorization: Bearer agent-test-token' https://127.0.0.1:9443/api/shares
```

## Future

Restoring ACLs from the history, sessions and open files, shares through the registry API and GPOs: proposal
in [samba-api.md](samba-api.md) and [gpo.md](gpo.md).
