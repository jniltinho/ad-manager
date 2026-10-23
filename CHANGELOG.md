# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Changed
- Updated the Echo web framework to v5.4.2.

## [0.21.0] — 2026-10-23

**The desktop app reaches only the panel and acts on each user row in place, as the web panel.**

### Changed
- Desktop app: the **Password policy** is a dialog opened from the button of the same name on the **Users** toolbar,
  as in the web panel, no longer an item of the side menu; Home's policy alert opens it over Users.
- Desktop app: the Users table acts on each row in place, as the web panel — the login opens the edit form, the
  status enables or disables the account, an **unlock** link sits beside a locked one, and one column per action
  (**password**, **copy**, **move**, **delete**; move and delete not on built-in accounts nor your own). The toolbar
  keeps only the search, filter, Password policy, Export CSV, New user and refresh, on one line. On a 1280 px window
  the action links are icons; Enter on the selected row opens a menu of its actions.
- Desktop app: the Users toolbar takes as few lines as fit — on a wide window the search, filter, actions, Password
  policy and Export CSV share one line (the search is at most 360 px and gives way first, with room between the
  actions and the extras); narrower, the extras go behind a ⋯ menu, and only then do the actions move to a second
  line.
- The desktop app reaches **only the panel**: the Shares page and the file servers' Samba logs list the servers
  registered in the panel and go through its proxy with your session, as Sync and Backup already did. The desktop
  no longer asks for, stores or sends a file-server agent token (that token is root on the file server), its
  actions there are audited under your name, and the workstation needs only the panel's port. A configuration file
  of an older version that still lists `[[agents]]` with tokens is rewritten without them on start.
- The desktop's **Configure** asks for the panel URL, user, password and language; **Test connection** signs in for
  real and shows the panel's reason when it cannot (wrong password, not a Domain Admin); **Save** writes the file
  (never the password) and opens the app signed in. A self-signed panel certificate is shown with its fingerprint and
  trusted only when you accept it (pinned), instead of turning certificate checks off; the app never follows a
  redirect from the panel.
- `samba-setup.py` (and the setup wizard's "This DC also serves files") makes the DC's agent listen on
  `127.0.0.1:9443` only: the panel beside it is the only one that reaches it, so its port is no longer open to the
  network. A separate file server's agent keeps listening for its panel; an existing `[agent]` is left as it is.
- Docs and diagrams: only the panel reaches an agent's port 9443 (the desktop goes through the panel); on the
  panel's own server the agent can listen on `127.0.0.1:9443` only, and a separate file server allows 9443 from the
  panel's IP alone.

### Fixed
- Desktop app: the Home page could crash when the panel answered its totals before the page was built.
- Measuring the shares' folder sizes through the panel (web and desktop) no longer fails after 60 seconds on large
  folders: the panel waits for the agent's answer for up to 2 minutes. An agent answer larger than the panel passes
  on is an error instead of a cut answer.

## [0.20.0] — 2026-10-23

**The sync run's end and duration on the desktop app and the CLI too, Portuguese for the inheritance notes, a leaner
set of release packages, and Go 1.27.3.**

### Added
- The desktop app's **Sync** page and `ad-manager sync list` show when each run finished and how long it took, as
  the web panel does (`08:41 AM · 4m38s`; `—` while the run goes on).

### Changed
- Releases publish the server only for Linux (the `.tar.gz` and the `.deb`) and the macOS desktop app only as the
  universal `.app` (Apple Silicon and Intel); on Windows only the desktop app is published.
- Built with Go 1.27.3.

### Fixed
- Three notes of a sync's Windows-inheritance pass (the incremental pass falling back to the whole folder, a pending
  inheritance being repaired, a pending mark that could not be written) showed in English on a Portuguese panel.

## [0.19.0] — 2026-10-23

**Windows inheritance after a sync in about a minute instead of a 30-minute timeout, repaired when a run leaves it
pending; the `.deb` restarts what is running on upgrade; and each sync run shows when it finished.**

### Added
- The **Sync** page's Runs table has a **Finished** column: when each run ended and how long it took
  (`12:14 AM · 4m38s`, with the date when it ended on another day), on one line; empty while the run goes on.

### Changed
- Reapplying a folder's **Windows inheritance** is much faster on a domain controller. Every item is still read
  through Samba, but Samba's write (~180 ms per item on a DC, where each SID goes through idmap) runs once per
  distinct case — the ACL to set and the item's owner, mode and current ACL — and the other items get the result it
  left, each one checked byte for byte (or written through Samba when the copy does not match). On a real 16k-file
  share with three distinct ACLs, the pass that ran past the 30-minute limit (and stopped at ~55%) now takes about a
  minute for its 16,567 items.
  Only with the `acl_xattr` VFS stack Samba's own configuration uses; otherwise every item goes through Samba as
  before. Used by a sync's ACL phase, a restore from quarantine and a folder ACL change applied to its contents.
- The agent's Samba (python3) helper stops after 5 minutes without a request and starts again on the next one.
- Upgrading the `.deb` restarts the panel and the agent that are running, so they run the new version right away
  (before, they kept the old binary until restarted by hand). A fresh install still starts nothing, and an upgrade
  never starts or enables a stopped service.

### Fixed
- A sync whose Windows-inheritance pass failed, timed out or was cancelled — or that was cancelled or died after
  copying files — left those files without their Windows ACL for good: the next run transferred nothing and did no
  ACL work. The job now stays "inheritance pending" until a run applies it, and its next run reapplies the whole
  folder even when nothing came in.

## [0.18.1] — 2026-10-23

### Fixed
- The **Sync** page's Runs table no longer wraps the **Files** and **Size** values onto two lines — those cells stay
  on one line.

## [0.18.0] — 2026-10-22

**Faster, clearer folder sync: incremental ACL re-inheritance, run phases, and a scrollable run-log.**

### Changed
- After a folder sync, the agent now reapplies the destination's **Windows inheritance only to the files it
  transferred** (and the directories on the way to them) instead of walking the whole tree on every run — an
  incremental run over a large share no longer spends many minutes reapplying ACLs it already set, and the serial
  sync queue is not held behind it. The whole tree is still reapplied on a first run or when the transferred list is
  too large to carry, and the incremental pass falls back to the whole tree if it fails. The ACL phase can be
  cancelled.
- A sync run now reports its **current phase** on the panel — preparing, copying (files *n* of *total*), applying
  Windows permissions, finalizing — instead of an opaque "running" that could look hung for many minutes. The phase
  shows in the job's "Last run" cell and the Runs table.

### Fixed
- A sync run's **Log** now opens a dedicated scrollable modal instead of the generic alert: a long run log (a dry-run
  can print a skip line per object, thousands of lines) scrolls inside the dialog's monospace body — capped to the
  viewport — while the run's summary (job, run id, status, file/size counts) and the Copy/Close footer stay in view,
  so the page no longer grows past the bottom. The dialog stays on screen on small viewports.

## [0.17.0] — 2026-10-22

**Four new panel themes, and import from an existing AD during setup with an installation guide.**

### Added
- **Four new themes**, identical in the web panel and the desktop app: **Matrix** (green phosphor on a black-green
  terminal, monospace, a static "digital rain" on the page and a soft green glow), **Winamp** (the Winamp 2 skin:
  brushed dark-steel chrome with a 1 px bevel, the LCD readout in fields and the current tab, amber buttons, the
  playlist's blue selected row), **Pink Panther** (blush page, plum title bars with a panther-pink stripe, deep rose
  actions) and **High contrast** (black and white with a single yellow accent, 3 px focus ring, no depth, WCAG AAA).
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
- Updated the Echo web framework to v5.4.1.

### Fixed
- The pt-BR names of the Breeze, Violet, Crystal and Graphite dark themes in Settings → Theme (stale catalog keys).
- The lists (Users, Groups, Computers, logs) lost their left and right lines once a scrollbar showed: the frame is
  now on the scrolling box, not on the collapsed table border, in every theme.

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

[Unreleased]: https://github.com/jniltinho/ad-manager/compare/v0.20.0...HEAD
[0.20.0]: https://github.com/jniltinho/ad-manager/compare/v0.19.0...v0.20.0
[0.19.0]: https://github.com/jniltinho/ad-manager/compare/v0.18.1...v0.19.0
[0.15.1]: https://github.com/jniltinho/ad-manager/compare/v0.15.0...v0.15.1
[0.15.0]: https://github.com/jniltinho/ad-manager/compare/v0.14.0...v0.15.0
[0.14.0]: https://github.com/jniltinho/ad-manager/releases/tag/v0.14.0
