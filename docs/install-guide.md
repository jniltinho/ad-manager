# Installation guide — panel, agent and desktop app

How to install, configure, check, upgrade and remove each AD Manager component, from the **`.deb` package** or from the
**release binary**. The shortest path for a new server is the `.deb` plus the browser **setup wizard**
([installation.md](installation.md)); this page is the full reference behind it. Configuring the panel against an
existing AD by hand (the `[ad]` section, the DC's CA, Settings) is detailed in [deployment.md](deployment.md).

In the examples, replace `X.Y.Z` with the version and `example.local` with your domain. The `.deb` is named
`ad-manager_X.Y.Z_amd64.deb`; the tarballs and zips carry the tag, `vX.Y.Z`.

## Components and release files

Everything is on the [Releases page](https://github.com/jniltinho/ad-manager/releases), with `checksums.txt`.

| Component | Where it runs | `.deb` | Binary |
|---|---|---|---|
| **Web panel** (`ad-manager server`) | Linux amd64 server, can be the DC itself | `ad-manager_X.Y.Z_amd64.deb` | `ad-manager_vX.Y.Z_linux_amd64.tar.gz` |
| **Agent** (`ad-manager agent`) | each Samba file server, or the DC that also serves files | the same `.deb` | the same `.tar.gz` |
| **Desktop app** (`ad-manager-desktop`) | the operator's Linux, Windows or macOS | — | `ad-manager-desktop_X.Y.Z_linux_x86_64.AppImage`, `…_windows_x86_64.zip`, `…_macos_universal.zip` (the same release) |
| CLI and MCP (`ad-manager …`) | any machine with LDAPS to a DC | the same `.deb` | the same `.tar.gz` (Linux; on Windows, build it from source) |

The panel, the agent, the CLI and MCP are **one binary** (`ad-manager`); what a machine runs depends on which systemd
unit you enable. The server tarball holds only the binary, a `config.toml` (a copy of
[`config.toml.example`](../config.toml.example)) and the README: the systemd units and `deploy/install.sh` are taken
from the repository at the same tag (below). The panel's units, the setup wizard and the agent's Samba work are Linux
only.

Check what you downloaded (the release's `checksums.txt` lists every package, server and desktop):

```bash
sha256sum -c checksums.txt --ignore-missing
```

## Prerequisites

| Component | Needs |
|---|---|
| Panel | Linux amd64 (tested on **Ubuntu 26.04**); LDAPS (636) and Kerberos (TCP 88 and 464, for password links) to a DC; the browsers reach the panel port (8443); optional Redis (queues, the agent) |
| Agent on a file server | a Samba domain member (`samba`, `winbind`, `acl`, `python3-samba`, `vfs objects = acl_xattr`); a Redis (`redis-server`, usually local); the panel reaches its port 9443 |
| Agent on a DC | the Samba DC itself, with the panel ([dc-file-server.md](dc-file-server.md)); `tdb-tools` for DC backups |
| Desktop app | a desktop session: Linux (X11 or Wayland) with GTK4 and WebKitGTK 6.0 (Ubuntu 24.04+, Debian 13+, Fedora 40+, RHEL 10+); Windows 10+ with WebView2 (Windows Server 2016–2022: install it first); macOS 12+; HTTPS to the panel only |
| Setup wizard | a fresh Ubuntu 26.04 server, the `.deb`, `redis-server`, a synchronized clock and 2 GiB free (10 GiB for backups) |

A domain that does not exist yet (a new domain, an additional DC or a file server) is created by the setup wizard
([installation.md](installation.md)) or by `scripts/samba-setup.py` ([samba-install.md](samba-install.md)).

## Layout on the server

Both install methods use the same layout, so upgrades and support steps are the same:

| Path | What | Permissions |
|---|---|---|
| `/opt/ad-manager/ad-manager` | the binary (`/usr/bin/ad-manager` links to it) | `0755 root` |
| `/opt/ad-manager/config.toml` | the configuration (a `.deb` conffile: upgrades never overwrite it) | `0640 root:ad-manager` |
| `/opt/ad-manager/env` | `AD_*` variables of both services (`NAME=value`) | `0600 root` |
| `/opt/ad-manager/tls/` | `tls.crt`/`tls.key`, the AD's CA | `0750 root:ad-manager` |
| `/opt/ad-manager/data/` | SQLite, setup token, self-signed certificate | `0750 ad-manager` |
| `/opt/ad-manager/logs/` | `audit.log`, `access.log` | `0750 ad-manager` |
| `/opt/ad-manager/agent.token` | SHA-256 of the agent token (agent only) | `0600 root` |
| `ad-manager.service`, `ad-manager-agent.service` | the units: `/usr/lib/systemd/system/` (`.deb`) or `/etc/systemd/system/` (binary) | `0644 root` |

The panel runs as the `ad-manager` user with only `data/` and `logs/` writable; the agent runs as root (it changes
owners, Windows ACLs and `net conf`), both hardened by systemd.

## 1. Web panel

### From the `.deb`

```bash
sudo apt install --no-install-recommends ./ad-manager_X.Y.Z_amd64.deb redis-server
```

The package creates the `ad-manager` user and the layout above, makes a self-signed certificate in `tls/`, and
**starts nothing**. On a fresh install its `config.toml` has `setup = true`, which opens the setup wizard:

```bash
sudo systemctl enable --now ad-manager ad-manager-agent
sudo cat /opt/ad-manager/data/setup-token
```

Open `https://<server IP>:8443`, enter the token and follow [installation.md](installation.md) (new domain, additional
DC, restored DC, file server, or **Connect only** for an AD that already runs elsewhere). The wizard writes the final
`config.toml` and restarts the panel out of setup mode.

To configure by hand instead (an existing Windows or Samba AD): replace `/opt/ad-manager/config.toml` with your own
(start from `/opt/ad-manager/config.toml.example`, details in [deployment.md](deployment.md#2-configure-the-domain)),
then `sudo systemctl enable --now ad-manager`.

### From the binary

```bash
V=X.Y.Z
tar xzf ad-manager_v${V}_linux_amd64.tar.gz                # ad-manager, config.toml, README.md
for f in install.sh ad-manager.service ad-manager-agent.service; do
  curl -fsSLO "https://raw.githubusercontent.com/jniltinho/ad-manager/v${V}/deploy/$f"
done
chmod +x install.sh
id ad-manager >/dev/null 2>&1 || sudo useradd --system --no-create-home --shell /usr/sbin/nologin ad-manager
sudo install -d -m 0755 /opt/ad-manager
sudo install -m 0640 -g ad-manager config.toml /opt/ad-manager/config.toml   # edit [ad] and [server] first (deployment.md)
sudo ./install.sh ./ad-manager panel.example.local
```

`install.sh` creates the `ad-manager` user, the same `/opt/ad-manager` layout, a self-signed certificate for the name
given (when `tls/` has none), links `/usr/bin/ad-manager`, installs `ad-manager.service` in `/etc/systemd/system/` and
starts it. A binary install has no setup wizard: `config.toml` must describe an AD that already exists.

### Configuration (`config.toml`)

The complete, commented list of keys is [`config.toml.example`](../config.toml.example) (a test keeps it complete).
The panel reads:

| Section | Keys | Notes |
|---|---|---|
| `[ad]` | `url`, `domain`, `base_dn`, `start_tls`, `ca_file`, `insecure_skip_verify`, `admin_group` | `ldaps://` to a DC; `ca_file` is the CA of the DC's certificate; `insecure_skip_verify` only in a lab |
| `[server]` | `listen`, `cert_file`, `key_file`, `self_signed`, `session_minutes`, `audit_log`, `access_log`, `trust_proxy` | HTTPS is required outside loopback |
| `[db]` | `path` | SQLite: file servers, AD profiles, Settings |
| `[redis]` | `addr`, `password`, `db`, `tls` | optional: queues and `/asynqmon`; the password goes in `env` |
| `[samba]` | `log_dir` | the Logs page on a DC |
| (top) | `setup` | `true` only on a fresh `.deb` install: the wizard |

`AD_*` variables override the file (`AD_URL`, `AD_DOMAIN`, `AD_BASE_DN`, `AD_CA_FILE`, `AD_INSECURE_SKIP_VERIFY`,
`AD_ADMIN_GROUP`, `AD_LISTEN`, `AD_SELF_SIGNED`, `AD_DB`, `AD_REDIS_ADDR`, `AD_REDIS_PASSWORD`, `AD_SAMBA_LOG_DIR`);
put them in `/opt/ad-manager/env`. **No credential goes in `config.toml`**: operators sign in with their own domain
account (Domain Admins only), and the CLI reads `AD_USER`/`AD_PASSWORD`.

What changes without a restart (language, session timeout, inactivity, password length, default OU, branding, theme,
Swagger) is on the **Settings** screen ([deployment.md](deployment.md#4-configuration-in-the-panel)).

### TLS

- **Panel certificate.** `cert_file`/`key_file` (the `.deb` and `install.sh` make a self-signed one in `tls/`;
  replace it with your own in production), or `self_signed = true` without `cert_file`: an ECDSA certificate in
  `data/`, renewed 30 days before it expires, its SHA-256 fingerprint in the journal.
- **Behind Nginx/Caddy.** `listen = "127.0.0.1:8443"` and `trust_proxy = true` (the real IP comes from
  `X-Forwarded-For`). Without TLS the panel only listens on loopback.
- **To the DC.** `ldaps://` with `ca_file`: on Samba copy `/var/lib/samba/private/tls/ca.pem` (its self-generated
  certificate has a negative serial number, accepted on purpose); on Windows export the AD CS root certificate in
  Base-64. Without TLS every password operation is refused.

### Verify

```bash
ad-manager version
systemctl status ad-manager
curl -sk https://127.0.0.1:8443/api/health          # {"ldap":"up","version":…}; "setup":true while the wizard is open
journalctl -u ad-manager -n 50                       # service log (the audit trail is logs/audit.log)
```

Then sign in at `https://<server>:8443` with a Domain Admins account (`user`, `user@example.local` or
`EXAMPLE\user`).

## 2. Agent (file server or DC)

The agent creates folders and shares, Windows ACLs and backups on its server, at the panel's request over HTTPS with a
token ([agent.md](agent.md)). A DC created by the setup wizard with **This DC also serves files** already has its agent
installed and registered; this section is for file servers and for manual installs.

### From the `.deb`

On the file server (already a domain member — `samba-setup.py member --agent` does both, [samba-install.md](samba-install.md)):

```bash
sudo apt install --no-install-recommends ./ad-manager_X.Y.Z_amd64.deb redis-server acl python3-samba
```

Write the agent's `config.toml` (replace the package's `setup = true` one):

```toml
[ad]
url = "ldaps://dc01.example.local"     # not used by the agent; the domain is
domain = "example.local"

[redis]
addr = "127.0.0.1:6379"                # password: AD_REDIS_PASSWORD in /opt/ad-manager/env; between hosts tls = true

[agent]
listen = "0.0.0.0:9443"
cert_file = "/opt/ad-manager/tls/tls.crt"
key_file = "/opt/ad-manager/tls/tls.key"
token_file = "/opt/ad-manager/agent.token"
share_roots = ["/srv/shares"]          # it only creates inside these roots
# queue = "fs-fs01"                    # default fs-<hostname>

[samba]
log_dir = "/var/log/samba"
```

**Who reaches port 9443: only the panel.** The desktop app goes through the panel, never to an agent. When the agent
runs on the panel's own server (a DC with the panel, `dc_file_server = true`), make it listen on the loopback only —
`listen = "127.0.0.1:9443"`, with the agent registered in the panel as `https://127.0.0.1:9443`; the CLI's
`--agent`/`--apply` on that server then use `https://127.0.0.1:9443` too. On a separate file server keep the address
the panel reaches and allow 9443 only from the panel's IP in the firewall.

Then the share root, Samba's registry shares (the agent publishes shares there), the token and the service:

```bash
sudo install -d -m 0755 /srv/shares            # each share_roots entry must exist
sudoedit /etc/samba/smb.conf                   # [global]: registry shares = yes (samba-setup.py member sets it)
sudo smbcontrol all reload-config
sudo ad-manager agent check                    # Samba Python API, acl_xattr, permission mode
sudo ad-manager agent token                    # prints the token ONCE; keeps only its SHA-256
sudo systemctl enable --now ad-manager-agent
```

### From the binary

```bash
V=X.Y.Z
tar xzf ad-manager_v${V}_linux_amd64.tar.gz
curl -fsSLO "https://raw.githubusercontent.com/jniltinho/ad-manager/v${V}/deploy/ad-manager-agent.service"
sudo install -d -m 0755 /opt/ad-manager /srv/shares                 # /srv/shares: each share_roots entry
sudo install -d -m 0750 /opt/ad-manager/tls /opt/ad-manager/data     # data/: ACL history, self-signed certificate
sudo install -m 0755 ad-manager /opt/ad-manager/ad-manager && sudo ln -sf /opt/ad-manager/ad-manager /usr/bin/ad-manager
sudo install -m 0600 config.toml /opt/ad-manager/config.toml     # edit it as above ([agent] and [redis])
sudo install -m 0644 ad-manager-agent.service /etc/systemd/system/
sudo apt install -y redis-server acl python3-samba                # or your distribution's packages
sudo systemctl daemon-reload
sudo ad-manager agent check && sudo ad-manager agent token        # registry shares = yes as in the .deb steps
sudo systemctl enable --now ad-manager-agent
```

Without `cert_file`, set `self_signed = true` in `[agent]` (a certificate in `/opt/ad-manager/data/`) or bring your
own certificate to `tls/`.

### Tokens

- **Agent token.** `sudo ad-manager agent token` prints a new token and writes only its hash to `token_file`; or, in
  the panel, **Settings → File server agent token → Generate token** shows the token and the command that writes the
  hash on the server. The agent reads the hash at start: `sudo systemctl restart ad-manager-agent` after a new one.
  One token per agent: a new one replaces the old one in the panel (the desktop app never holds it).
- **Setup token.** Only on a fresh `.deb` install: `/opt/ad-manager/data/setup-token` (root only), valid 24 h, 5
  tries every 15 min, spent on the first good sign-in. For a new one (expired, lost or used): `sudo rm -f
  /opt/ad-manager/data/setup-token /opt/ad-manager/data/setup-token.used` and `sudo systemctl restart ad-manager`.
- **Register the agent** in the panel: **Shares → File servers → + New**, URL `https://fs01.example.local:9443` and the
  token (tick "Accept self-signed certificate" for a self-signed one).

### Write modes (gates)

An agent on a **DC** is read-only unless one of its explicit modes is on in its `[agent]` section — each off by
default and reported by `/api/health` (`files`, `gpo_restore`, `password_migration`):

| Key(s) | Turns on | Reference |
|---|---|---|
| `dc_file_server = true` | the DC changes files and shares (its own share roots) | [dc-file-server.md](dc-file-server.md) |
| `import_modes = true` | both import modes below, with no secret on the DC (the GPO credential comes from the panel) | [installation.md](installation.md#import-from-an-existing-ad) |
| `gpo_restore_user`, `gpo_restore_password_file` | restoring and importing GPOs (always as new GPOs) | [gpo.md](gpo.md) |
| `password_migration_user`, `password_migration_password_file` | the password-material migration of a new-domain cutover | [password-migration.md](password-migration.md) |

Not a gate, but also in `[agent]`: `sync_unconfined = true` lets a sync run without its systemd confinement (a container
without systemd only, [sync.md](sync.md)).

The password files are regular `0600 root` files with only the password, never in `config.toml`. A new domain created
by the setup wizard with **Prepare the import of GPOs and passwords** has `import_modes = true` instead, with the
administrator credential encrypted in the panel's database (*Settings → Import from an existing AD*); remove it there and
delete the `import_modes` line to turn both off on that DC ([installation.md](installation.md#import-from-an-existing-ad)).

### Verify

```bash
sudo ad-manager agent check
systemctl status ad-manager-agent
curl -sk https://127.0.0.1:9443/api/health    # queue, version, nt_acl, dc, gpo_restore, password_migration, files
```

In the panel, the server appears under **Shares → File servers** with its version, and its folders load.

## 3. Desktop app

The desktop app is a client of the panel's API only (the file servers through the panel): nothing is installed on a server
([desktop.md](desktop.md)).

| System | Install |
|---|---|
| Linux | put `ad-manager-desktop_X.Y.Z_linux_x86_64.AppImage` in a folder of your own (e.g. `~/Apps`), `chmod +x` it and run it: it carries GTK4 and WebKitGTK 6.0, for any x86_64 desktop with glibc 2.39+ (Ubuntu 24.04+, Debian 13+, Fedora 40+); WebKit's sandbox needs `bubblewrap` and `xdg-dbus-proxy`, which GNOME and KDE have ([desktop.md](desktop.md#requirements)) |
| Windows | unzip `ad-manager-desktop_X.Y.Z_windows_x86_64.zip` into a folder of your own (not `Program Files`) and run `ad-manager-desktop.exe` (it needs the WebView2 runtime: part of Windows 10/11; on Windows Server 2016–2022 install it from Microsoft) |
| macOS | unzip `…_macos_universal.zip` and move `AD Manager.app` to Applications (the first time: right-click → **Open**, it is ad-hoc signed); it runs on Apple Silicon and Intel |

On the first run the **Configure** screen asks for the panel URL, the user and the password (language, theme and
look are this computer's preferences); **Test
connection** signs in for real (a self-signed certificate is shown with its fingerprint, to trust once), and **Save**
writes the file and opens the app signed in. The password is never saved. It saves `.config/desktop.toml` beside the
executable; when that folder is not writable (and always for the Linux AppImage and the macOS `.app`), the user's
configuration folder:
`~/.config/ad-manager/desktop.toml` on Linux, `~/Library/Application Support/ad-manager/` on macOS,
`%AppData%\ad-manager\` on Windows. On Linux and macOS the file is `0600` and the app refuses a looser one; on
Windows its protection is the folder's, so keep the app in a folder of your own. `--config` or `AD_MANAGER_DESKTOP`
point to another file.

**Verify:** sign in with a Domain Admins account; Home shows the totals, and Shares lists the shares of each file
server registered in the panel. The panel's audit log records the desktop's actions with the machine's IP.

## Upgrade

Read the [CHANGELOG](../CHANGELOG.md) first. `config.toml`, `env`, `tls/`, `data/` and `logs/` are kept by every
method. Take a panel backup before a major upgrade (**Backup → panel**, [backup.md](backup.md)).

| Installed from | Upgrade |
|---|---|
| `.deb` (panel and/or agent) | `sudo apt install --no-install-recommends ./ad-manager_NEW_amd64.deb` (the package restarts the panel and the agent that are running; it never starts or enables one that is not) |
| Binary (panel) | download the new tarball and its `deploy/` files at the new tag, then `sudo ./install.sh ./ad-manager` (replaces the binary and the unit, keeps the rest, restarts the panel) |
| Binary (agent) | `sudo install -m 0755 ad-manager /opt/ad-manager/ad-manager`, refresh `/etc/systemd/system/ad-manager-agent.service` from the new tag, `sudo systemctl daemon-reload && sudo systemctl restart ad-manager-agent` |
| Desktop app | replace the AppImage, the executable or the `.app` in its folder; `desktop.toml` stays (the Rust app's is used as is) |

After it: `ad-manager version` and the health checks above (`/api/health` reports `version`). Keep the agents and the
panel on the same version.

### From an `ad-manager` installation up to 0.2.x

Older versions used `/etc/ad-manager`, `/var/lib/ad-manager` and `/var/log/ad-manager`:

```bash
sudo apt install --no-install-recommends ./ad-manager_X.Y.Z_amd64.deb
sudo systemctl disable --now ad-manager
O=/opt/ad-manager
sudo install -m 0640 -o root -g ad-manager /etc/ad-manager/tls.key $O/tls/
sudo install -m 0644 /etc/ad-manager/tls.crt /etc/ad-manager/samba-ca.pem $O/tls/
sudo sed -e "s|/etc/ad-manager/|$O/tls/|; s|/var/log/ad-manager/|$O/logs/|; s|/var/lib/ad-manager/ad-manager.db|$O/data/ad-manager.db|" \
  /etc/ad-manager/config.toml | sudo tee $O/config.toml >/dev/null
sudo install -m 0600 -o ad-manager -g ad-manager /var/lib/ad-manager/ad-manager.db $O/data/ad-manager.db
sudo install -m 0600 -o ad-manager -g ad-manager /var/log/ad-manager/audit.log $O/logs/audit.log
sudo systemctl enable --now ad-manager
```

On the agent, set `token_file` to `/opt/ad-manager/agent.token` and copy `/etc/ad-manager/agent.token` there.

## Uninstall

**`.deb`:**

```bash
sudo apt remove ad-manager     # stops and disables both services; config, tls/, data/ and logs/ stay
sudo apt purge ad-manager      # also config.toml, tls/, env, agent.token and import.pass; data/ and logs/ stay for auditing
```

**Binary:**

```bash
sudo systemctl disable --now ad-manager ad-manager-agent
sudo rm -f /etc/systemd/system/ad-manager.service /etc/systemd/system/ad-manager-agent.service /usr/bin/ad-manager
sudo systemctl daemon-reload
sudo rm -rf /opt/ad-manager/ad-manager /opt/ad-manager/config.toml /opt/ad-manager/env /opt/ad-manager/tls \
  /opt/ad-manager/agent.token /opt/ad-manager/import.pass
# keep /opt/ad-manager/data and /opt/ad-manager/logs for auditing, or remove them and the user:
# sudo rm -rf /opt/ad-manager && sudo userdel ad-manager
```

**Desktop app:** delete its folder (with its `.config/`) or the `.app`, and the user configuration folder if used:
`~/.config/ad-manager/` (Linux), `~/Library/Application Support/ad-manager/` (macOS), `%AppData%\ad-manager` (Windows).

Uninstalling never touches Samba, the domain, the shares or the backups in `/var/backups/ad-manager`; remove an agent
from the panel under **Shares → File servers**.
