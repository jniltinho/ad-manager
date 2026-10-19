<p align="center">
  <img src="docs/img/logo-wide.png" alt="AD Manager" width="70%">
</p>

<h3 align="center">Active Directory management for Windows Server and Samba 4 — web, desktop, CLI and AI</h3>

<p align="center">
  <a href="https://github.com/jniltinho/ad-manager/actions/workflows/ci.yml"><img src="https://github.com/jniltinho/ad-manager/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/jniltinho/ad-manager/releases/latest"><img src="https://img.shields.io/github/v/release/jniltinho/ad-manager?color=blue" alt="Release"></a>
  <a href="https://github.com/jniltinho/ad-manager/releases"><img src="https://img.shields.io/github/downloads/jniltinho/ad-manager/total?color=blue&label=downloads" alt="Downloads"></a>
  <img src="https://img.shields.io/badge/platforms-Linux%20%7C%20Windows%20%7C%20macOS-555" alt="Platforms">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue" alt="MIT license"></a>
</p>

<p align="center">
  <img src="docs/screenshots/03-users.png" alt="Users in the web panel" width="100%">
</p>

**AD Manager** manages Active Directory — **Windows Server** or **Samba 4** — from a web panel, a desktop app (Linux,
Windows, macOS), a CLI and an MCP server for AI agents, with an agent for Samba file servers: users, groups, OUs,
computers, shares with Windows permissions, backups, folder sync, a setup wizard and Windows-to-Samba migration.
No service account: everything runs with the signed-in administrator's credentials and is audited.

## Quick start

Try it with Docker (a demo Samba 4 domain, panel and agent in one container):

```bash
docker run -d --name ad-manager --hostname dc1 --cap-add SYS_ADMIN -p 8443:8443 jniltinho/ad-manager
```

```bash
docker logs ad-manager      # URL, user and the generated password
```

Install on a server or get the desktop app from [Releases](https://github.com/jniltinho/ad-manager/releases)
([installation guide](docs/installation.md)).

## Learn more

- [Overview](docs/overview.md) — features, screenshots and how the pieces fit together
- [Documentation](docs/README.md) — installation, panel, desktop, CLI, MCP, agent, backup, sync and migration
- [Screenshots](docs/screenshots/README.md) · [Changelog](CHANGELOG.md) · [Development](docs/development.md)

## License

[MIT](LICENSE).
