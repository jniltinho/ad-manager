# Changelog — AD Manager desktop (Rust)

The Rust/egui desktop of ad-manager (`desktop-rust/`), released on its own tags (`desktop-rust-vX.Y.Z`). The format
follows [Keep a Changelog](https://keepachangelog.com/); the release notes are the section of the tagged version.

## [Unreleased]

### Added

- The web panel's look and functions (OpenSpec `desktop-rust-web-parity`).
  - The web's shell: header with the global search, top tabs in the web's order, footer, the app box; zoom steps
    for QHD and 4K.
  - Every page and dialog laid out as the web's, with the web's control, table, badge and dialog sizes, its
    typography (the theme's font stacks, Inter embedded) and its depth (gradients, shadows, the page's pattern).
  - **Configure › Appearance**: any of the 18 themes, flat or with depth, on this computer only (`[appearance]` in
    desktop.toml).
  - **Settings** and **Import/export** as pages of the app (no tab opens the browser any more).
  - Computers' DNS and SMB sessions; OUs' GPOs; Shares' file servers, tasks, sessions, the share form's roots,
    suggestions and earlier permissions; Logs' HTTP tab and action filter; Sync's preview step, run details, log
    and quarantine as the web's; Backup schedules edited in place, refreshed while a backup runs.
  - Suggestions while typing (Home's quick box, group members, a user's groups, share lists).
  - Export CSV of the rows on screen (Users, Groups, Computers), as the web's.
  - The web's outline icons (Lucide, the same version, ISC license) in tabs, buttons, links and tiles instead of
    emoji; a subset font of the icons in use (`scripts/lucide-subset.sh`).

- Crash-safe graphics start, from rustXterm's design.
  - The app renders through wgpu, with the glow renderer only on macOS for the Metal → OpenGL fallback.
  - A `gpu-starting` marker is taken under an OS lock before the window opens. A start that died moves the next one
    down the ladder of its OS: Windows OpenGL → DX12 → Vulkan → WARP, Linux Vulkan → OpenGL → llvmpipe, macOS Metal →
    glow.
  - A working hardware fallback is saved as `[graphics] gpu_backend` in desktop.toml, which the Fyne app keeps. It is
    announced under the sign-in box and in the status line.
  - **Configure › Graphics › Rendering backend**, as rustXterm's, saves the choice at once without a panel test. It
    applies at the next start. The choices are Automatic, **wgpu (WebGPU)** (wgpu's own choice of backend and adapter)
    and the OS's backends.
  - Flags and variables: `--safe-gpu`, `--gpu-info` and `WGPU_BACKEND`.
  - The adapter choice prefers hardware OpenGL, and the output-owning WARP adapter on GPU-less Windows VMs (vendored
    `wgpu-hal` patch).
  - The window's size and position are restored.
  - A missing `libxkbcommon` is reported by name.
- CI: a native Windows job with the WARP readback test. The release no longer marks itself as the repository's
  latest release.

### Fixed

- The header's global search opened nothing; it now opens Users filtered by the term.
- A session that ended went back to sign-in only from some calls; any call that hears it now does.
- A click on the text of a side list item, a log file or a wizard card did not pick it.
- A confirmation over a dialog ran the dialog's own action (Restore of earlier permissions queued a share save).
- Run now after a preview could run another job than the one previewed; a second save could start meanwhile.
- Import previews failed on the server's empty password map; a computer's SMB sessions never loaded; the quarantine
  opened empty.
- Imports from another AD stopped after 20 seconds; the server allows them up to 33 minutes.
- A focused text field lost its border; truncated table cells were clipped instead of ending in an ellipsis; a
  scrolling dialog's title bar was narrower than the dialog.

- Card heads, list rows, dialog title bars and footers were taller than the web's; scroll bar handles were
  invisible; disabled buttons faded twice; dialog tabs lost their top edge and accent.
- A dark system theme (Windows' dark mode, or Wine) replaced the app's theme with egui's own visuals: the theme now
  applies to both of egui's light and dark styles.
- The Samba log showed one line per entry; it now shows each entry over its lines, laying out only the rows on
  screen.

### Security

- Downloads (DC backups, CSV exports with generated passwords) are created readable by their owner only.
- Passwords and tokens sent to the panel go straight into buffers wiped after use; the source AD's password and the
  agent token leave no text in egui's state after sign-out.
- A reply that arrives after sign-out no longer updates the next session (Settings, imports).

## [0.1.0] - 2026-10-23

### Added

- First version, at feature parity with the Fyne desktop (`desktop/`): Configure (real sign-in test, self-signed
  certificate pinned by its SHA-256 fingerprint after the operator trusts it, redirects never followed), the
  web-like sign-in with the panel's brand, Home (tiles, alerts, quick reset), Users (row links, Enter menu, compact
  icons at 1280 px, lean toolbar), new/copy/edit/move user, password and single-use link, password policy, Groups,
  OUs, Computers, Shares (tasks polled), Backup (restore guards per kind, schedules), Sync (wizard, runs,
  quarantine) and Logs (audit, Samba of each file server, loaded when opened).
- The same `desktop.toml` as the Fyne app (keys, legacy keys, lookup order, `0600`, agent tokens removed).
- English source strings with the pt-BR catalog; server messages translated with the panel's `/api/i18n`.
- The panel's 18 themes (color roles from `desktop/themes_gen.go`), drawn in their flat look.
- A modular workspace: `crates/core` (no GUI), `crates/ui` (egui), `crates/platform` with Linux, Windows and macOS
  implementations, the binary in `apps/`.
- Unit and UI acceptance tests (egui_kittest against an in-process mock panel), docs screenshots at 2×, and a
  benchmark against the Fyne app (`bench/`, report in `docs/benchmark.md`).
- Its own CI and release (`.github/workflows/desktop-rust-*.yml`), path-filtered to `desktop-rust/`.
