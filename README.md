<p align="center">
  <img src="docs/img/logo-wide.png" alt="AD Manager" width="70%">
</p>

<h3 align="center">The friendly way to run Active Directory — on Windows Server or Samba 4</h3>

<p align="center">
  Web panel · Desktop app · CLI · MCP for AI agents · File-server agent — one open-source Go binary.
</p>

<p align="center">
  <a href="https://github.com/jniltinho/ad-manager/actions/workflows/ci.yml"><img src="https://github.com/jniltinho/ad-manager/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/jniltinho/ad-manager/releases/latest"><img src="https://img.shields.io/github/v/release/jniltinho/ad-manager?color=blue" alt="Release"></a>
  <a href="https://github.com/jniltinho/ad-manager/releases"><img src="https://img.shields.io/github/downloads/jniltinho/ad-manager/total?color=blue&label=downloads" alt="Downloads"></a>
  <a href="https://github.com/jniltinho/ad-manager/stargazers"><img src="https://img.shields.io/github/stars/jniltinho/ad-manager?style=flat&color=yellow" alt="Stars"></a>
  <img src="https://img.shields.io/badge/platforms-Linux%20%7C%20Windows%20%7C%20macOS-555" alt="Platforms">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue" alt="MIT license"></a>
</p>

<p align="center">
  <a href="#-quick-start"><b>Quick start</b></a> ·
  <a href="docs/overview.md"><b>Features</b></a> ·
  <a href="docs/screenshots/README.md"><b>Screenshots</b></a> ·
  <a href="docs/README.md"><b>Docs</b></a> ·
  <a href="https://github.com/jniltinho/ad-manager/releases/latest"><b>Download</b></a>
</p>

<p align="center">
  <img src="docs/screenshots/03-users.png" alt="Users in the AD Manager web panel" width="100%">
</p>

## ✨ Why AD Manager

Stop juggling ADUC, RSAT, `samba-tool` and shell scripts. AD Manager puts your whole domain — users, groups, file
shares with Windows permissions, backups and migrations — behind one fast, audited interface that runs anywhere.

<table>
  <tr>
    <td width="33%" valign="top"><b>👤 Directory, done right</b><br>Users, groups, OUs, computers and password policy. Password generator and <b>self-service reset links</b>.</td>
    <td width="33%" valign="top"><b>🗂️ Shares with real NTFS ACLs</b><br>Folders and shares on Samba with Windows permissions, history and one-click restore.</td>
    <td width="33%" valign="top"><b>💾 Backup &amp; sync</b><br>Back up DCs, file servers and the panel on a schedule; pull folders from any SMB server.</td>
  </tr>
  <tr>
    <td valign="top"><b>🧭 Setup wizard</b><br>Turn a fresh Ubuntu server into a domain controller or file server — from the browser.</td>
    <td valign="top"><b>🚚 Windows → Samba</b><br>Inventory, compare, import users, groups, DNS and shares, and join a Samba DC.</td>
    <td valign="top"><b>🔐 Secure by design</b><br>No service account: LDAPS with the signed-in admin's credentials, <i>Domain Admins</i> only, every change audited.</td>
  </tr>
  <tr>
    <td valign="top"><b>🖥️ Native desktop app</b><br>Linux, Windows and macOS (Apple Silicon, Intel, universal <code>.app</code>), same themes as the web.</td>
    <td valign="top"><b>🤖 AI-ready</b><br>A built-in MCP server lets AI agents query and manage the directory under the same rules and audit.</td>
    <td valign="top"><b>🎨 14 themes</b><br>Light, dark and flat looks, English and Portuguese, sharp on QHD and 4K screens.</td>
  </tr>
</table>

<p align="center">
  <img src="docs/screenshots/12-edit-share.png" alt="Shares with Windows permissions" width="32%">
  <img src="docs/screenshots/31-sync-wizard.png" alt="Sync wizard" width="32%">
  <img src="docs/screenshots/desktop-04-users.png" alt="Desktop app" width="32%">
</p>

## 🚀 Quick start

Spin up a demo Samba 4 domain with the panel and the agent in one container:

```bash
docker run -d --name ad-manager --hostname dc1 --cap-add SYS_ADMIN -p 8443:8443 jniltinho/ad-manager
```

```bash
docker logs ad-manager      # URL, user and the generated password
```

Open `https://localhost:8443` and sign in. More in the [Docker guide](docs/docker.md).

**On a real server** (Ubuntu 26.04), install the `.deb` from
[Releases](https://github.com/jniltinho/ad-manager/releases/latest), start the services and finish in the browser:

```bash
sudo apt install --no-install-recommends ./ad-manager_X.Y.Z_amd64.deb
```

```bash
sudo systemctl enable --now ad-manager ad-manager-agent && sudo cat /opt/ad-manager/data/setup-token
```

Then open `https://<server IP>:8443` and enter the token: the **setup wizard** does the rest automatically — it
installs and configures Samba as a new domain controller, an additional DC or a file server, or connects the panel
to an existing Active Directory, writing the configuration for you ([installation guide](docs/installation.md)).

## 🧩 Under the hood

<p align="center">
  <img src="docs/img/architecture.png" alt="AD Manager architecture" width="100%">
</p>

| Component | What it does |
|---|---|
| `ad-manager server` | HTTPS web panel and REST API (Swagger at `/swagger`), LDAPS to the domain with the operator's credentials |
| `ad-manager agent` | Runs on each Samba file server or DC: folders, shares, ACLs, backups and sync through a task queue |
| `ad-manager` (CLI) | The same operations from a terminal, plus migration and import/export tools |
| `ad-manager mcp` | Model Context Protocol server for AI agents |
| `ad-manager-desktop` | Native app (Fyne) that talks to the panel's API and directly to the agents |

## 📚 Learn more

- **[Overview](docs/overview.md)** — every feature, more screenshots and the 14 themes
- **[Documentation](docs/README.md)** — installation, panel, desktop, CLI, MCP, agent, backup, sync and migration
- **[Changelog](CHANGELOG.md)** · **[Development](docs/development.md)** — contributions welcome!

If AD Manager saves you time, a ⭐ helps others find it.

## License

[MIT](LICENSE).
