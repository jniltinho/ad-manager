# Installation

> **Easiest way:** install the `.deb`, enable the services and let the **setup wizard** in the browser configure
> the server automatically — Samba, the domain, the panel and the agent, or just the connection to an existing AD.
> See [installation.md](installation.md). This page covers installing and configuring the panel **by hand**; the
> [installation guide](install-guide.md) has every component (panel, agent, desktop) from the `.deb` and from the
> binary, with upgrade and removal.

ad-manager works for **any company** with Active Directory, whether **Windows Server** or
**Samba 4**: nothing in the binary is tied to a domain. Domain, DC and certificates come from
`config.toml`; users and permissions come from AD itself (only *Domain Admins* can log in). In the
examples below, replace `example.local`/`EXAMPLE` with the company's domain.

![typical deployment](img/deployment.png)

## Requirements

| Role | Machine | Needs |
|---|---|---|
| Panel (`ad-manager`) | any Linux amd64 (tested on Ubuntu 26.04), can be the DC itself | LDAPS (636) and Kerberos (TCP 88 and 464, password change links) to a DC; browser → panel port (8443) |
| Agent (`ad-manager-agent`) | each Samba file server that is a domain member | `samba`, `winbind`, `acl`, `python3-samba` (Windows permissions; `vfs objects = acl_xattr`); reachable Redis; panel → port 9443 |
| Queue | a Redis (can be on the panel server) | only if you use the agent / Queues screen |
| Desktop | the administrator's Linux, Windows or macOS | HTTPS access to the panel or the agent |

New domain or file server without Samba yet: see [samba-install.md](samba-install.md).

## 1. Install the package

```bash
sudo apt install --no-install-recommends ./ad-manager_X.Y.Z_amd64.deb
# file server: also redis-server acl python3-samba (with --no-install-recommends they are not pulled in)
```

The package creates the `ad-manager` system user, generates a self-signed certificate and **does not enable any
service**: whoever installs it decides whether the machine is a panel, an agent or both.

Everything of the product lives in **`/opt/ad-manager`** (`/usr/bin/ad-manager` is just a link):

| Path | Contents | Permissions |
|---|---|---|
| `ad-manager` | binary (panel, CLI, MCP, agent) | `0755 root` |
| `config.toml` | configuration (conffile: upgrades do not overwrite it) | `0640 root:ad-manager` |
| `tls/` | `tls.crt`/`tls.key` (self-signed if missing), AD CA | `0750 root:ad-manager` |
| `data/` | SQLite (file servers, AD profiles) and `self_signed` certificate | `0750 ad-manager` |
| `logs/` | `audit.log` (panel **Logs** screen) | `0750 ad-manager` |
| `agent.token`, `env` | agent token hash; `AD_*` variables read by the services | `0600 root` |
| `scripts/`, `docs/` | Samba installer and this documentation | `0755 root` |

## 2. Configure the domain

`sudoedit /opt/ad-manager/config.toml`:

```toml
[ad]
url = "ldaps://dc01.example.local"     # a DC (Windows or Samba)
domain = "example.local"
ca_file = "/opt/ad-manager/tls/ad-ca.pem"   # CA that signed the DC's LDAPS certificate

[server]
listen = "0.0.0.0:8443"
cert_file = "/opt/ad-manager/tls/tls.crt"
key_file = "/opt/ad-manager/tls/tls.key"
audit_log = "/opt/ad-manager/logs/audit.log"

[db]
path = "/opt/ad-manager/data/ad-manager.db"

[redis]                                 # optional: agent and Queues screen
addr = "127.0.0.1:6379"
```

LDAPS CA:

- **Samba**: copy `/var/lib/samba/private/tls/ca.pem` from the DC to `/opt/ad-manager/tls/ad-ca.pem` (`0644`).
- **Windows**: export the AD CS root certificate (or the DC certificate) in Base-64 to the same path.
  DC without an LDAPS certificate: install AD CS or a certificate on the DC first — without TLS the panel is
  read-only.

Administrators group other than *Domain Admins*: `admin_group = "CN=…"` in `[ad]`.

## 3. Services (systemd)

The package installs two units in **`/usr/lib/systemd/system/`** (in the repository: `deploy/`):

| Unit | Purpose | Runs as |
|---|---|---|
| `ad-manager.service` | web panel + API (`ad-manager server`) | user `ad-manager`, `ProtectSystem=strict`, only `data/` and `logs/` writable |
| `ad-manager-agent.service` | folders and shares agent (`ad-manager agent`) | root (needs `chown`, Windows ACLs via Samba or `setfacl`, and `net conf`) |

```bash
sudo systemctl enable --now ad-manager            # panel
sudo ad-manager agent token                       # file server only: shows the token once
sudo systemctl enable --now ad-manager-agent      # agent

systemctl status ad-manager                       # status
journalctl -u ad-manager -f                       # service log (the audit trail is in logs/audit.log)
sudo systemctl restart ad-manager                 # after changing config.toml
sudo systemctl edit ad-manager                    # local override (goes to /etc/systemd/system/ad-manager.service.d/)
```

Environment variables for both services (e.g. `AD_REDIS_PASSWORD`) go in `/opt/ad-manager/env`
(`0600 root`, `NAME=value` format).

Open `https://<server>:8443` and log in with a *Domain Admins* account
(`user`, `user@example.local` or `EXAMPLE\user`).

## 4. Configuration in the panel

After the first login, the **Settings** screen adjusts the following without a restart (stored in the local
database `data/ad-manager.db`):

| Setting | Default | Range |
|---|---|---|
| Default interface language | English | English or Português (each operator can still switch EN/PT in the header) |
| Idle session timeout | `session_minutes` from `config.toml` (30) | 5 to 720 min |
| Account inactive after | 90 days without logon | 7 to 3650 |
| Generated password length | 14 | 10 to 64 |
| Default OU for new users | `CN=Users` | any OU in the domain |
| Company name, support link, logo | AD Manager branding | PNG, JPEG or SVG logo up to 512 KiB |

Every change goes to the audit log (`settings.update`, `settings.logo`). What comes from `config.toml` (DC, TLS,
Redis, database, audit) is shown read-only on the same screen.

## 5. File servers (optional)

On each file server that is a domain member: install the same package, set `[agent]` and `[redis]` in
`config.toml`, generate the token and enable `ad-manager-agent` (above). In the panel, register the server under
**Shares → File servers** with the URL `https://fs01.example.local:9443` and the token. Details in
[agent.md](agent.md).

## Upgrade, remove and install without the package

Upgrading (`.deb` or binary), removing, migrating from an installation up to 0.2.x and installing from the release
tarball (with `deploy/install.sh` and the units from the same tag) are in the
[installation guide](install-guide.md#upgrade), which covers the panel, the agent and the desktop app.

## Self-signed certificate

Without `cert_file`, `self_signed = true` (or `AD_SELF_SIGNED=true`) makes the panel and the agent generate an
ECDSA certificate with the machine's name and IPs in `/opt/ad-manager/data/selfsigned.{crt,key}` (key `0600`),
reused on later starts and renewed 30 days before it expires. The fingerprint appears in the log
(`sha256=…`) so you can verify it on first access. In the panel, register the agent with "Accept self-signed
certificate"; in the desktop app, `insecure = true`.

## Behind a TLS proxy (Nginx/Caddy)

```toml
[server]
listen = "127.0.0.1:8443"
trust_proxy = true
```

Without `cert_file` the panel only accepts listening on loopback, and only with `trust_proxy = true` (or
`--insecure-dev`).

## Docker

```bash
make docker-build
docker run -d --name ad-manager -p 8443:8443 \
  -v /opt/ad-manager:/opt/ad-manager:ro -v /opt/ad-manager/logs:/opt/ad-manager/logs \
  ad-manager:latest server --config /opt/ad-manager/config.toml
```
