# Changelog — AD Manager desktop (Rust)

The Rust/egui desktop of ad-manager (`desktop-rust/`), released on its own tags (`desktop-rust-vX.Y.Z`). The format
follows [Keep a Changelog](https://keepachangelog.com/); the release notes are the section of the tagged version.

## [Unreleased]

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
