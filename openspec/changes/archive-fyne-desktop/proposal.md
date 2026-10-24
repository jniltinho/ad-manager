## Why

The Rust desktop (`desktop-rust/`) now matches the web panel's pages, themes and rules, so keeping the Fyne desktop
(`desktop/`) built, tested and released doubles the work for one app. It is discontinued; its code stays to study.

## What Changes

- The Fyne app is archived: `make` no longer builds or tests it, `ci.yml` no longer tests or builds it (Linux and
  macOS), and `release.yml` no longer publishes it — the release carries the server and the `.deb`; the desktop app is
  released by `desktop-rust-release.yml` on `desktop-rust-v*` tags.
- `make desktop`, `make desktop-check`, `make desktop-test`, `make desktop-shots` and `make desktop-themes` at the
  root run the Rust app's recipes, all in Docker (the theme generator in the Go image): nothing to install.
- The Rust app no longer compares its `themes.json` with the Fyne app's palettes; `make desktop-check` and its CI
  check instead that `themes.json` is what the web's theme CSS gives now.
- Docs, screenshots and theme mosaics show the Rust app; the Fyne screenshots are removed.

## Impact

- `Makefile`, `desktop-rust/Makefile`, `.github/workflows/{ci,release,desktop-rust-ci}.yml`, docs, `AGENTS.md`,
  the `create-release` skill. No server or API change.
