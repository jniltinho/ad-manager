# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

—

## [0.14.0] — 2026-10-19

**First public release.** Manage Active Directory (Windows Server or Samba 4) from a web panel, a desktop app, a CLI
and an MCP server, with an agent for Samba file servers.

### Added
- Web panel (Go + embedded Vue 3): users, groups, OUs, computers, password policy, self-service password links,
  GPOs, domain DNS, SMB sessions, CSV import/export, audit, HTTP and Samba logs, Settings (company, logo, theme,
  language) and Swagger.
- File-server agent: folders and shares in Samba's registry with Windows (NTFS) ACLs, permission history and
  restore, recycle bin, access-based enumeration and file auditing; a persistent Python helper and Go-side ACL reads
  when it is safe (`docs/samba-bridge.md`).
- Backups of the domain controller, file servers and the panel, with restore and schedules; folder sync from another
  SMB server with a wizard, preview and quarantine.
- Browser-based setup wizard that prepares a new server as a domain controller or a file server.
- Windows-to-Samba migration: inventory, comparison, import from another AD and share migration.
- 14 themes (light, dark and a flat look), identical in the web panel and the desktop app; English UI with a
  Portuguese (pt-BR) translation.
- Desktop app (Fyne) for Linux, Windows and macOS; CLI; MCP server; all-in-one Docker demo image.
- CI and releases on GitHub Actions: `.deb`, server binaries and the desktop app for Linux, Windows and macOS.

[Unreleased]: https://github.com/jniltinho/ad-manager/compare/v0.14.0...HEAD
[0.14.0]: https://github.com/jniltinho/ad-manager/releases/tag/v0.14.0
