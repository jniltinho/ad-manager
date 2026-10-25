# Overview

**AD Manager** is a single Go binary that gives domain administrators a fast, audited and friendly way to run Active
Directory — whether the domain controllers are **Windows Server** or **Samba 4**. It ships a **web panel**, a native
**desktop app**, a **CLI**, an **MCP server** for AI agents and an **agent** that manages folders, shares and Windows
permissions on Samba file servers.

- 🔐 **No service account.** Everything talks LDAPS with the credentials of the administrator who is signed in; only
  *Domain Admins* get in, and every change lands in the audit log.
- 🧩 **One binary, nothing to glue.** The SPA is embedded and SMB sync and Windows RPC are built in (no `rclone`,
  `rpcclient` or `net rpc`); on Samba servers the agent drives Samba's own code through a small embedded Python
  helper (`python3-samba`).
- 🐧 **Samba-first, Windows-friendly.** Built for Samba 4.23 on Ubuntu 26.04, it also manages Windows Server domains
  and helps you migrate from Windows to Samba.

<p align="center">
  <img src="img/illustration.png" alt="AD Manager connects administrators to domain controllers, file servers and AI agents" width="90%">
</p>

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
| 🎨 **20 themes** | Light, dark and flat looks, identical in the web panel and the desktop app; English and Portuguese UI |
| 🤖 **MCP server** | Let AI agents query and manage the directory, with the same rules and audit |

## Screenshots

| Home | Shares with Windows permissions |
|---|---|
| ![Home](screenshots/02-home.png) | ![Edit share](screenshots/12-edit-share.png) |

| Sync wizard | Backup |
|---|---|
| ![Sync wizard](screenshots/31-sync-wizard.png) | ![Backup](screenshots/29-backup.png) |

| Setup wizard | Desktop app |
|---|---|
| ![Setup](screenshots/setup-5-details.png) | ![Desktop](screenshots/desktop-rs-04-users.png) |

<p align="center">
  <img src="screenshots/themes.jpg" alt="The 20 themes" width="80%">
</p>

More in [screenshots/README.md](screenshots/README.md).

## How it fits together

<p align="center">
  <img src="img/architecture.png" alt="Architecture" width="100%">
</p>

| Component | What it does |
|---|---|
| `ad-manager server` | HTTPS web panel and REST API (Swagger at `/swagger`), LDAPS to the domain with the operator's credentials |
| `ad-manager agent` | Runs on each Samba file server (or DC): folders, shares, ACLs, backups and sync through a task queue (Redis/asynq) |
| `ad-manager` (CLI) | The same operations from a terminal, plus migration and import/export tools |
| `ad-manager mcp` | Model Context Protocol server for AI agents |
| `ad-manager-desktop` | Native app (Rust/egui) that talks only to the panel's API (the file servers through it) |

## Development

```bash
make check && make test          # gofmt, vet, frontend lint, Go and frontend tests
make desktop-check               # the desktop app (Rust), in Docker
make samba-up                    # a test Samba domain in Docker (empresa.test)
make build                       # server and desktop app (Rust) in dist/
```

See [development.md](development.md). Issues and pull requests are welcome.
