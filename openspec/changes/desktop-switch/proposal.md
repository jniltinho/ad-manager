## Why

The Wails desktop (`desktop-wails/`, change `desktop-wails`) runs the web panel's own SPA and keeps the desktop's
protections in Go. It passes its gate G1 on Linux, Windows and macOS. The operator decided on 2026-10-27 to make it
**the** desktop app now, before gates G2–G5. The Rust app's redraw of every web page (~25,000 lines) stops being
maintained.

The open items are accepted in writing in `openspec/changes/desktop-wails/design.md` (Gates → Operator decision):
- G2: no native navigation guard on Windows and macOS;
- G4: the web's Users table renders every row;
- G3 and G5: platform runs and the preview period, not done.

## What Changes

- **The Rust app moves to `archive/desktop-rust/`**: code only, not built, tested or released, as
  `archive/desktop-go/`.
- **The Wails app moves to `desktop/`**:
  - binary `ad-manager-desktop` and Go module `ad-manager/desktop`;
  - the `.desktop` entry, icon name and program name `ad-manager-desktop`;
  - the macOS bundle `AD Manager.app`;
  - its private state folder beside `desktop.toml` becomes `desktop-state/`.
- **`make desktop`, `desktop-check`, `desktop-shots`** build, check and photograph the new app.
  - `make build` puts its Linux and Windows archives, `.deb`, `.rpm` and Linux binary in `dist/desktop/`.
  - `make desktop-themes` goes away: the app draws the web's own CSS.
- **CI**: `desktop-ci.yml` is the new app's checks, called by `ci.yml`.
  - It runs on every pull request and push, as before.
  - It uploads diagnostics only on failure, so a release, which publishes every artifact of its run, never
    carries them.
  - `desktop-wails-ci.yml` goes.
- **Release**: the desktop jobs build the new app:
  - Linux and Windows in the toolchain image, with the `.deb` and `.rpm`;
  - macOS universal on the runner.

  Its version is the tag's, given at build time, so the `desktop/Cargo.toml` check goes. The archive names stay
  `ad-manager-desktop_X.Y.Z_{linux_x86_64.tar.gz,windows_x86_64.zip,macos_universal.zip}`.
- **Docs and screenshots**: `docs/desktop.md`, `desktop/README.md`, the install guide, overview, themes, README,
  AGENTS.md and the release skill describe the new app. The desktop screenshots (`desktop-NN-*.png`) are retaken at
  2×, in Classic blue, with Example Company Ltd and `domain.local`, from a demo panel in Go
  (`desktop/tools/demopanel`). The Rust ones (`desktop-rs-*.png`) go.

## Capabilities

### Modified Capabilities

- `app-desktop`: the desktop app the project builds, tests and releases is the Wails one.
- `release`: the desktop's version is the tag's, and the release also carries its `.deb` and `.rpm`.

## Impact

- Moved: `desktop/` → `archive/desktop-rust/`, `desktop-wails/` → `desktop/`.
- Changed:
  - `Makefile`;
  - `.github/workflows/{ci,desktop-ci,release}.yml`;
  - `frontend/vite.config.ts` (the desktop's out dir);
  - the docs listed above and `.claude/skills/create-release/SKILL.md`.
- Removed: `.github/workflows/desktop-wails-ci.yml`, `docs/screenshots/desktop-rs-*.png`.
- Users: `desktop.toml` is the same file. A Linux user needs GTK4 and WebKitGTK 6.0 (Ubuntu 24.04+, Debian 13+,
  Fedora 40+, RHEL 10+). A Windows user needs WebView2.
