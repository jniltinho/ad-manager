# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

—

## [0.28.0] — 2026-10-28

**The password migration runs in Go (no Python helper), a preflight warning for RC4-only accounts, migration fixes
found by the Windows Server 2008 R2 lab, and the Linux desktop app as one AppImage.**

### Fixed
- Folder sync: a run is no longer refused with "… is writable to the sync worker" when a folder outside the
  destination is mounted twice on the same point (the covered mount is unreachable); seen after a reboot of a
  Vagrant file server.
- Share migration: a Windows share without a security descriptor (Everyone: Full Control) keeps the root's NTFS
  access (e.g. `Domain Users` write) instead of none.
- Password migration: a source user without a password is `no_password` (it keeps the generated one) instead of an
  `error` that made `migrate passwords` exit 2.
- `import --apply --dir X` creates `X` (0700) instead of losing the generated passwords of the users just created.
- Desktop app: the footer shows the app's own version (a v0.27.1 app showed its panel's v0.27.0); when the panel
  runs another one, both are labeled: "desktop v0.28.0 · panel v0.27.0".

### Changed
- The password migration runs in Go in the DC agent (no Python helper): read over a Kerberos-sealed DRSUAPI bound
  to the DC's replication SPN, each account checked in its sealed reply, written with Samba's `ldbmodify`; the same
  outcomes and stored material as before on Samba 4.23/4.22 and Windows Server 2008 R2, 2019 and 2022 sources.
- Every OpenSpec spec passes strict validation; Windows labs run in dockur/windows containers (AGENTS.md) and a lab
  client of a recreated domain is repaired in place.
- Desktop app for Linux: one AppImage (`ad-manager-desktop_X.Y.Z_linux_x86_64.AppImage`, GTK4 and WebKitGTK 6.0
  inside) for any x86_64 desktop with glibc 2.39+, instead of the `.tar.gz`, `.deb` and `.rpm`. It keeps
  `desktop.toml` in `~/.config/ad-manager/`; WebKit's sandbox needs `bubblewrap` and `xdg-dbus-proxy`, which GNOME
  and KDE have. A `.deb` or `.rpm` install can be removed: its `desktop.toml` stays.

### Added
- `migrate preflight` warns about `RC4-only accounts`: enabled users whose password predates the domain's AES
  Kerberos keys (Ubuntu's Kerberos refuses RC4); reset them before the cutover.
- `internal/gosamba`: Go replacements for the python3-samba calls of the Entra sync — DRSUAPI hash read (sealed,
  Kerberos only, CRC-checked) and value formats; tested on Samba 4.23/4.22 and Windows Server 2022, 2019 and 2008 R2.
- `internal/entrasync`: the start of the AzureADConnect_Samba4 port (`nbfx` binary XML codec, AAD password hash);
  OpenSpec change `entra-sync-go`.
- go-msrpc DRSUAPI decryption checks the payload's CRC32 (`third_party/go-msrpc/PATCHED.md`).

## [0.27.1] — 2026-10-27

**The desktop header is the web's: Configure moves to the tab bar.**

### Changed
- Desktop app: Configure moves from the header to the tab bar, next to Queues: the header is the web's.
- Desktop app: Queues shows disabled (the queue monitor needs a browser session to the panel).
- The links at the right of the tab bar (API, Queues, Configure) change only their text color on hover.

## [0.27.0] — 2026-10-27

**The desktop app is the web panel in its own window: Go + Wails v3 replaces the Rust app.**

### Changed
- The desktop app (`desktop/`) runs the web panel's own pages in the system's webview (Go, Wails v3).
  - Every page, dialog, theme and translation is the web's.
  - A Go backend is its only way to the panel: https only, the pinned certificate, no redirects, the session kept
    out of the page.
  - Files go straight to disk, private.
  - It reads the same `desktop.toml`.
- Configure (first run, sign-in box, header): test, trust the certificate and save. This computer's language, theme,
  look and safe rendering save without a test.
- The window keeps its place, size and maximised state and has the app's icon; one window per configuration file. It
  fills the window at Full HD, QHD and 4K, and fits a smaller screen. The page gets no microphone, camera, location,
  notifications or clipboard reading. The Windows `.exe` carries the icon, its version and a manifest (per-monitor
  DPI, no administrator). On macOS the app has an icon in the macOS shape, says why it asks for the local network
  and where to allow it when refused, and keeps the window when quit with Cmd+Q.
- Linux packages: `.deb` and `.rpm` (they bring GTK4 and WebKitGTK 6.0). Linux needs Ubuntu 24.04+, Debian 13+,
  Fedora 40+ or RHEL 10+. Windows needs WebView2.
- The Rust app moves to `archive/desktop-rust/` (code only, as `archive/desktop-go/`).

### Fixed (web panel)
- An answer, error, 401 or sign-out of an earlier session no longer reaches the next one.
- A sign-out closes open confirmations.
- The Shares task follower stops with its page.
- Sync no longer shows a server's late jobs under another, and actions confirmed after a server switch send nothing.
- A finished backup upload closes only its own dialog.
- The page fills the window at QHD/4K in Safari and Firefox too.

### Known limits
- No native navigation guard on Windows and macOS yet.
- The Users table renders every row, which is slow with thousands of users in the desktop app.

See `openspec/changes/desktop-wails` and `desktop-switch`.

## [0.26.1] — 2026-10-25

**The queue monitor moves to jniltinho/asynqmon v0.8.1.**

### Changed
- The queue monitor is [jniltinho/asynqmon](https://github.com/jniltinho/asynqmon) v0.8.1: the Redis INFO in a tab per
  section, pages that fit the window (only lists, tables and charts scroll), card shadows and smoother charts.
- Every release also publishes the all-in-one image to Docker Hub (`jniltinho/ad-manager:X.Y.Z` and `:latest`).

## [0.26.0] — 2026-10-25

**Grafana and RabbitMQ themes, a thin app outline in every theme, and the queue monitor in the panel's theme.**

### Added
- Two themes, on the web and the desktop: **Grafana (dark)**, the Grafana 11 palette, and **RabbitMQ**, the RabbitMQ
  management UI palette, both with the panel's folder tabs, header band, page dots and drop shadows.
- The queue monitor (`/asynqmon`) is the new Vue frontend of
  [jniltinho/asynqmon](https://github.com/jniltinho/asynqmon) v0.8.0, opened in the panel's theme and flat look.

### Changed
- A thin outline in the theme's line color marks the app's edge in every theme, also in the flat look.

## [0.25.0] — 2026-10-25

**Classic blue (XP) looks like Windows XP Luna, with volume and WCAG AAA contrast, on the web and the desktop.**

### Changed
- Classic blue (XP) looks like Windows XP Luna, with volume, on the web and the desktop: vertical title-bar gradients
  in the header and dialogs, a taskbar footer, Start-green glossy buttons, XP push buttons, task-pane card heads,
  listview table heads, a beige tab strip with XP tabs and orange tops, soft drop shadows and an XP window frame on
  the login; only colors, gradients and shadows changed. The theme now meets WCAG AAA (normal text 7:1).
- The flat look keeps the page's dots in every theme (a theme's own pattern gives way to them).

### Fixed
- Desktop: the New user dialog's rows are 12 px apart, as on the web.

## [0.24.0] — 2026-10-25

**The server and the desktop app in one release, with one version; the desktop binary is `ad-manager-desktop`.**

### Changed
- One CI and one release for the server and the desktop app: `ci.yml` runs the desktop's checks too (Linux, Windows
  cross and native, macOS), and a tag `vX.Y.Z` publishes the server, the `.deb` and the desktop app (Linux, Windows,
  macOS universal) in one release with one `checksums.txt`. The desktop app carries the server's version (the release
  refuses a tag that differs from `desktop/Cargo.toml`), and `desktop-rust-v*` tags are no longer used.
- The desktop app lives in `desktop/` (it was `desktop-rust/`) and its binary is `ad-manager-desktop` (it was
  `ad-manager-desktop-rs`; the macOS app is `AD Manager.app`); the archived Fyne app moved to `archive/desktop-go/`.
  Its `desktop.toml` is the same: an existing configuration keeps working.
- CI: the GitHub actions moved to their Node 24 versions, pinned by commit SHA, and the Ubuntu runner is pinned to
  24.04.

## [0.23.1] — 2026-10-25

**Sync of every share reads a share that denies its account through the disk's administrative share.**

### Fixed
- Sync of every share: a share whose own permission leaves the job's account out ("permission denied" at its root)
  is read through its disk's administrative share (`E$\ADM`), read only, with a note in the run; a share at a
  disk's root still fails, and a saved job can never name an administrative share.

## [0.23.0] — 2026-10-24

**Sync of every share of a server, share sizes measured in the background, thin theme shadows, and the Rust desktop
as the desktop app (the Fyne one archived).**

### Added
- Sync: a job of kind **every share of the server** copies each non-administrative share of a source server into the
  share of the same name on this server, one at a time, with a note per share (copy only; system, unpublished and
  nested shares skipped with the reason). A running run shows its share (n of total) and the files compared.
- The desktop app in Rust (egui), `desktop-rust/`, matching the web panel's pages, themes and rules: the same panel
  API, `desktop.toml`, protections, pt-BR catalog and themes; its own CI and release (tags `desktop-rust-v*`), a
  benchmark against the Fyne app (`desktop-rust/docs/benchmark.md`), and `make desktop`, `make desktop-check`,
  `make desktop-shots` and `make desktop-themes`, all in Docker (nothing to install).

### Removed
- The Fyne desktop app (`desktop/`) is archived: kept only as code to study, it is no longer built by `make`, tested by
  CI or published by the release (which now carries the server and the `.deb`); its docs and screenshots now show the
  Rust app.

### Changed
- Desktop app (Fyne, before its archiving): saving Configure keeps the `[graphics]` section of desktop.toml, where the
  Rust desktop stores its rendering backend.
- Themes: every theme's shadows are thin and soft (cards, login box, dialogs, window), light and dark; Matrix, Winamp
  and High contrast keep theirs, Classic gray its bevel.
- Sync: stopping a running run asks first (web and desktop); a job of every share shows the server's share folders
  (`/srv/shares`) as its destination; counts follow the interface language; Save settings shows a turning loader.
- Shares: folder sizes are measured by the agent in the background (every 4 h with jitter, one share at a time at
  idle CPU and I/O priority, 30 min per share, right after a share is created or changed from the panel) and cached
  in memory and in `share-sizes.json` in its state folder, so the page shows them at once with the time of the last
  scan. A share whose measure fails keeps its last complete value, shown muted with the reason; a partial measure
  shows "≥ N"; nested shares count once in the total; Refresh starts a scan without blocking the page, which follows
  it ("measuring N/M…"). Older web or desktop builds against a new agent get the cached values at once, but their
  Refresh shows the new values only after a reload.

### Security
- The import write modes keep no password on the DC: `import_modes = true` in the agent and the administrator
  credential encrypted in the panel's database (*Settings → Import from an existing AD*, checked as a Domain Admin),
  sent to the agent with each GPO task. The setup's **Prepare the import of GPOs and passwords** no longer writes
  `/opt/ad-manager/import.pass` (and removes an old one). A *From another AD* profile keeps its password encrypted,
  bound to that profile's server and user. The former `gpo_restore_*`/`password_migration_*` keys keep working.

## [0.22.0] — 2026-10-23

**The desktop's user edit dialog shows the group count, and Echo v5.4.2.**

### Changed
- Desktop app: the user edit dialog's Groups tab shows how many groups the user is in (`Groups (5)`), as the web
  panel, and follows each add and remove.
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
