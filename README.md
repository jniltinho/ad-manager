<p align="center">
  <img src="docs/img/logo-wide.png" alt="AD Manager" width="70%">
</p>

<h3 align="center">Active Directory management for Windows Server and Samba 4 — web, desktop, CLI and AI</h3>

<p align="center">
  <a href="https://github.com/jniltinho/ad-manager/actions/workflows/ci.yml"><img src="https://github.com/jniltinho/ad-manager/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/jniltinho/ad-manager/releases/latest"><img src="https://img.shields.io/github/v/release/jniltinho/ad-manager?color=blue" alt="Release"></a>
  <img src="https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/Vue-3-42b883?logo=vuedotjs&logoColor=white" alt="Vue 3">
  <img src="https://img.shields.io/badge/platforms-Linux%20%7C%20Windows%20%7C%20macOS-555" alt="Platforms">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue" alt="MIT license"></a>
</p>

<p align="center">
  <a href="#quick-start">Quick start</a> ·
  <a href="#features">Features</a> ·
  <a href="#screenshots">Screenshots</a> ·
  <a href="docs/README.md">Documentation</a> ·
  <a href="https://github.com/jniltinho/ad-manager/releases">Downloads</a>
</p>

<p align="center">
  <img src="docs/screenshots/03-users.png" alt="Users in the web panel" width="100%">
</p>

**AD Manager** is a single Go binary that gives your domain administrators a fast, audited and friendly way to run
Active Directory — whether your domain controllers are **Windows Server** or **Samba 4**. It ships a **web panel**, a
native **desktop app**, a **CLI**, an **MCP server** for AI agents and an **agent** that manages folders, shares and
Windows permissions on your Samba file servers.

- 🔐 **No service account.** Everything talks LDAPS with the credentials of the administrator who is signed in; only
  *Domain Admins* get in, and every change lands in the audit log.
- 🧩 **One binary, nothing to glue.** The SPA is embedded and SMB sync and Windows RPC are built in (no `rclone`,
  `rpcclient` or `net rpc`); on Samba servers the agent drives Samba's own code through a small embedded Python
  helper (`python3-samba`).
- 🐧 **Samba-first, Windows-friendly.** Built for Samba 4.23 on Ubuntu 26.04, it also manages Windows Server domains
  and helps you migrate from Windows to Samba.

<p align="center">
  <img src="docs/img/illustration.png" alt="AD Manager connects your administrators to domain controllers, file servers and AI agents" width="90%">
</p>

## Quick start

**Try it in one minute** — a demo Samba 4 domain with the panel and the agent in a single container
([docs/docker.md](docs/docker.md)):

```bash
docker run -d --name ad-manager --hostname dc1 --stop-timeout 60 --cap-add SYS_ADMIN -p 8443:8443 jniltinho/ad-manager
```

```bash
docker logs ad-manager      # URL, user and the generated password
```

**Install on a server** (Ubuntu 26.04) — download the `.deb` from
[Releases](https://github.com/jniltinho/ad-manager/releases):

```bash
sudo apt install --no-install-recommends ./ad-manager_X.Y.Z_amd64.deb
```

```bash
sudo systemctl enable --now ad-manager ad-manager-agent
```

```bash
sudo cat /opt/ad-manager/data/setup-token
```

Then open `https://<server IP>:8443` and enter the token: the **setup wizard** prepares the machine as a new domain
controller, an additional DC or a file server, or just connects the panel to an existing Active Directory
([docs/installation.md](docs/installation.md)). Configuring `config.toml` by hand instead is covered in
[docs/deployment.md](docs/deployment.md).

**Desktop app** — Linux, Windows and macOS (Apple Silicon, Intel, universal `.app`) builds are attached to every
release.

## Features

| | |
|---|---|
| 👤 **Directory** | Users, groups, OUs, computers and password policy: create, edit, move, enable/disable, delete with typed confirmation, password generator and **self-service password links** |
| 🗂️ **File servers** | Folders and shares with **Windows (NTFS) ACLs** per group and user, permission history and one-click restore, recycle bin, access-based enumeration and file auditing |
| 💾 **Backup** | Domain controller, file servers and the panel itself, with schedules, retention and restore |
| 🔄 **Folder sync** | Bring folders from another SMB server (Windows or Samba) with a wizard, preview, quarantine and verification |
| 🧭 **Setup wizard** | Turn a fresh Ubuntu server into a domain controller or a file server from the browser |
| 🚚 **Migration** | Windows → Samba: inventory, comparison, import from another AD (users, groups, DNS, shares) and DC join |
| 🧾 **Logs** | Audit trail of every change, HTTP access log and Samba logs per server |
| 🌐 **DNS, GPOs, sessions** | Domain DNS records, GPO links and backups, SMB sessions with disconnect |
| 🎨 **14 themes** | Light, dark and flat looks, identical in the web panel and the desktop app; English and Portuguese UI |
| 🤖 **MCP server** | Let AI agents query and manage the directory, with the same rules and audit |

## Screenshots

| Home | Shares with Windows permissions |
|---|---|
| ![Home](docs/screenshots/02-home.png) | ![Edit share](docs/screenshots/12-edit-share.png) |

| Sync wizard | Backup |
|---|---|
| ![Sync wizard](docs/screenshots/31-sync-wizard.png) | ![Backup](docs/screenshots/29-backup.png) |

| Setup wizard | Desktop app |
|---|---|
| ![Setup](docs/screenshots/setup-5-details.png) | ![Desktop](docs/screenshots/desktop-04-users.png) |

<p align="center">
  <img src="docs/screenshots/themes.jpg" alt="The 14 themes" width="80%">
</p>

More in [docs/screenshots](docs/screenshots/README.md).

## How it fits together

<p align="center">
  <img src="docs/img/architecture.png" alt="Architecture" width="100%">
</p>

| Component | What it does |
|---|---|
| `ad-manager server` | HTTPS web panel and REST API (Swagger at `/swagger`), LDAPS to the domain with the operator's credentials |
| `ad-manager agent` | Runs on each Samba file server (or DC): folders, shares, ACLs, backups and sync through a task queue (Redis/asynq) |
| `ad-manager` (CLI) | The same operations from a terminal, plus migration and import/export tools |
| `ad-manager mcp` | Model Context Protocol server for AI agents |
| `ad-manager-desktop` | Native app (Fyne) that talks to the panel's API and directly to the agents |

## Documentation

Start at **[docs/README.md](docs/README.md)**: installation, the setup wizard, the panel, the desktop app, the CLI,
MCP, the file-server agent, backups, sync, migration from Windows and development.

## Development

```bash
make check && make test          # gofmt, vet, frontend lint, Go/frontend/desktop tests
make samba-up                    # a test Samba domain in Docker (empresa.test)
make build                       # server and desktop app in dist/
```

See [docs/development.md](docs/development.md). Issues and pull requests are welcome.

## License

[MIT](LICENSE).
