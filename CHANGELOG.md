# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

**Import from an existing AD during setup, and an installation guide.**

### Added
- The setup wizard's **New domain** ends with **Import from an existing AD**, which opens *Import/export → From
  another AD* after the Domain Admin signs in (the same import, options and source credential as in the panel). An
  optional **Prepare the import of GPOs and passwords** (with "This DC also serves files"; `samba-setup.py new-dc
  --panel --files --import-ready`) turns on this DC agent's GPO-import and password-migration write modes with the
  administrator password in `/opt/ad-manager/import.pass` (0600 root); `sudo rm` it after the import to turn both
  off, and `apt purge` removes it. The setup-mode API (`/api/setup/*`) is documented in `openapi.yaml`.
- **Installation guide** (`docs/install-guide.md`): the panel and the agent from the `.deb` or the tarball, and the
  desktop app on Linux, Windows and macOS — prerequisites, config keys, TLS, tokens, write-mode gates, verification,
  upgrade and uninstall. `config.toml.example` lists every key the binary reads, and a test keeps it so.

### Changed
- The DC roles of `samba-setup.py` start the panel last, so the wizard no longer reports success before the DC agent
  part has run (a failure there now shows in the wizard); the config-backup notice no longer counts as a wizard step.

## [0.16.0] — 2026-10-22

**Password migration for a new-domain cutover.**

### Added
- **Password-material migration** (`docs/password-migration.md`): on a new-domain cutover (the "From another AD"
  import, not a `dc-join`), preserve the users' production passwords. The DC agent, in a dedicated default-off
  `password_migration` write mode, reads the source's password hashes over a sealed, **read-only** DRSUAPI
  (`GetNCChanges`, `REPL_OBJ`; never `UpdateRefs`/`DRS_ADD_REF`/join — the source is left unchanged) and sets them on
  this domain's matching users by `sAMAccountName`: the NT hash always, the Kerberos keys when their salt transfers
  (otherwise NTLM-only, rebuilt on the next password change). Never over LDAP, never as plaintext, never creating a
  user; `Primary:CLEARTEXT` sources are rejected and built-ins (RID 500/501/502), trust and machine accounts are
  skipped. Per-user outcome `set`/`ntlm_only`/`same`/`missing`/`builtin`/`rejected`/`error` with a sanitized reason
  (shown as a tooltip in the panel; never a secret). Available as the **Passwords** option of *Import/export → From
  another AD* (reusing the form's source login and password), the CLI `migrate passwords` and the MCP
  `ad_password_migrate`; the source credential is taken from the form, `AD_SOURCE_PASSWORD_FILE` (0600) or stdin,
  never an argument. `POST /api/import/ad/passwords` and the `passwords` flag of `POST /api/import/ad/plan|apply`.
- The "From another AD" import also carries the `lastLogonTimestamp` of users and computers and the
  `operatingSystem` of computers (never moving a last-logon backwards, never clearing a value it did not receive).
- Import/export: a per-profile trash button (with a confirmation) deletes a saved source profile, and a log/status
  modal shows each import's progress and result.

## [0.15.1] — 2026-10-19

**A raised logo.**

### Changed
- The AD Manager logo has a soft relief (light from the top left and a soft shadow, the mark's shield and org chart
  included) in the web panel, the desktop app and the README banner, with the underline closer to the wordmark. The
  apps show it at the same size, from transparent PNGs at 2× (`scripts/logo-relief.py`).
- README: the banner fills the image; the three screenshots under "Why AD Manager" share one height.
- Screenshots and theme mosaics retaken with the new logo.

## [0.15.0] — 2026-10-19

**Password policy moves into Users, and sharper screenshots.**

### Changed
- Web panel: the password policy is a dialog opened from the **Password policy** button on the Users toolbar, no
  longer a tab of its own. `/password-policy` (and the legacy `/politica`) open it, as does Home's weak policy alert.
- Docs screenshots (web panel, setup wizard, sync wizard and desktop app) are captured at 2× with the Classic blue
  (XP) theme; the theme mosaics are larger and sharper.
- Docs: the browser setup wizard is the recommended installation (`docs/installation.md`); the desktop's agent token
  can be generated in the web panel (Settings → File server agent token).

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

[Unreleased]: https://github.com/jniltinho/ad-manager/compare/v0.15.1...HEAD
[0.15.1]: https://github.com/jniltinho/ad-manager/compare/v0.15.0...v0.15.1
[0.15.0]: https://github.com/jniltinho/ad-manager/compare/v0.14.0...v0.15.0
[0.14.0]: https://github.com/jniltinho/ad-manager/releases/tag/v0.14.0
