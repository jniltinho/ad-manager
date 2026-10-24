# Changelog — AD Manager desktop (Rust)

The Rust/egui desktop of ad-manager (`desktop-rust/`), released on its own tags (`desktop-rust-vX.Y.Z`). The format
follows [Keep a Changelog](https://keepachangelog.com/); the release notes are the section of the tagged version.

## [Unreleased]

### Added

- Crash-safe graphics start, from rustXterm's design.
  - The app renders through wgpu, with the glow renderer only on macOS for the Metal → OpenGL fallback.
  - A `gpu-starting` marker is taken under an OS lock before the window opens. A start that died moves the next one
    down the ladder of its OS: Windows OpenGL → DX12 → Vulkan → WARP, Linux Vulkan → OpenGL → llvmpipe, macOS Metal →
    glow.
  - A working hardware fallback is saved as `[graphics] gpu_backend` in desktop.toml, which the Fyne app keeps. It is
    announced under the sign-in box and in the status line.
  - **Configure › Graphics › Rendering backend**, as rustXterm's, saves the choice at once without a panel test. It
    applies at the next start.
  - Flags and variables: `--safe-gpu`, `--gpu-info` and `WGPU_BACKEND`.
  - The adapter choice prefers hardware OpenGL, and the output-owning WARP adapter on GPU-less Windows VMs (vendored
    `wgpu-hal` patch).
  - The window's size and position are restored.
  - A missing `libxkbcommon` is reported by name.
- CI: a native Windows job with the WARP readback test. The release no longer marks itself as the repository's
  latest release.

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
