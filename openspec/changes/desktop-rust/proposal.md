## Why

The desktop app (`desktop/`, Go + Fyne) is the manager's day-to-day tool. Fyne draws everything on the CPU side of
its own canvas and rebuilds widget trees on every refresh; on a domain with thousands of users the Users table,
startup and memory are the parts operators notice. We want **numbers**, not opinions: a second implementation of the
same app in Rust with egui/eframe (immediate mode, one GPU draw per frame), built to **feature parity**, and a
benchmark that compares both on the same machine against the same panel. The Fyne app stays the shipped desktop;
the Rust one is an experiment that ships separately until the numbers decide.

## What Changes

- **New app `desktop-rust/`** (binary `ad-manager-desktop-rs`), a Cargo workspace on its own (stable toolchain, its
  own `Cargo.lock`) laid out as github.com/jniltinho/rustXterm: `crates/core` (no GUI), `crates/ui` (egui),
  `crates/platform` (the OS trait) with `platform-linux`, `platform-windows` and `platform-macos`, and the binary in
  `apps/`; pinned toolchain (`rust-toolchain.toml`, the same toolchain image), workspace lints (`unsafe_code`
  forbidden, `unwrap`/`expect` denied), `rustfmt.toml`, `clippy.toml`, `deny.toml` (cargo-deny),
  `scripts/cargo-docker.sh`, `Makefile`, CHANGELOG-driven release notes. Passwords are zeroized after use.
- **Feature parity with the Fyne app** (matrix in `design.md`): the Configure screen with the real sign-in test and
  certificate pinning; the web-like sign-in; the shell (header, sidebar, status bar, footer); Home, Users (row links,
  Enter menu, compact icons at 1280 px, lean toolbar with the ⋯ menu), Groups, OUs, Computers, Shares, Backup, Sync
  (with the 4-step wizard and the quarantine), Logs (audit and Samba of each file server); the password policy,
  password and single-use link, new/copy/edit/move user dialogs; typed confirmations; CSV export and file
  download/upload.
- **Same protections, client side, as the Fyne app** (the server stays the authority, `internal/ad`): talks **only to
  the panel** (file servers through `/api/agents/{id}/…`, never an agent token); only `https://`; **no redirect is
  ever followed**; a self-signed panel certificate is accepted only after the operator trusts its SHA-256 fingerprint
  in Configure and then **only that certificate** (rustls with a pinning verifier that still checks the handshake
  signature); the password is never written anywhere; `desktop.toml` is `0600` and refused when looser; agent tokens
  of old config files are removed on start; built-in/own accounts get no toggle/move/delete; DCs are not
  disabled/moved/deleted; groups/OUs that the server protects get no delete; destructive actions ask to type the name.
- **Same config file** (`desktop.toml`, same keys, legacy Portuguese keys read, same lookup order with `--config` and
  `AD_MANAGER_DESKTOP`), so either app opens the other's configuration.
- **Same i18n**: English source strings through `t()`/`tf!()`, pt-BR in `desktop-rust/assets/i18n/pt-BR.json`
  (seeded from `desktop/i18n_pt_*.go`); server messages translated with the panel's public catalog (`/api/i18n`,
  messages and patterns, the same algorithm as `internal/i18n`). The language follows `lang` in the file, then the
  panel default, then English.
- **Same themes**: the 18 panel themes' color roles (`desktop-rust/assets/themes.json`, generated from
  `desktop/themes_gen.go`), the session's theme applied at sign-in and the public one on the sign-in screen; Classic
  blue (XP) is the screenshot theme.
- **Separate CI and release**: `.github/workflows/desktop-rust-ci.yml` (pull requests, and pushes to `main`, that
  touch `desktop-rust/**`, the two workflow files or the Fyne sources its asset checks read — `desktop/themes_gen.go`
  and `desktop/i18n_pt_*.go`: fmt, clippy `-D warnings`, tests, cargo-deny, a Linux release build) and
  `.github/workflows/desktop-rust-release.yml` (tags `desktop-rust-v*`: Linux x86_64 tar.gz, Windows x86_64 zip,
  macOS universal `.app` zip, SHA256SUMS, notes from `desktop-rust/CHANGELOG.md`). `ci.yml` and `release.yml` are
  not changed and are never called by the Rust workflows; `ci.yml` keeps running on every pull request as today
  (path filters of new workflows cannot and do not suppress it), and the `desktop-rust-v*` tags do not match
  `release.yml`'s `v*` filter. Path filters do not apply to tag pushes, so the release workflow is selected by its tag
  pattern alone.
- **Crash-safe graphics start**, the design of rustXterm (`apps/rustxterm/src/gpu.rs`): a graphics driver that
  crashes while the window's device is created kills the process where no Rust error handling runs, so every launch
  would die the same way. Before the window exists the app plans its backend: `--safe-gpu` forces software for one
  launch; a `gpu-starting` marker in the config folder, holding the backend being tried under an OS file lock and
  emptied after 3 painted frames, tells a later launch that a start died (a marker nobody holds) and moves it to the
  next safer backend of the platform's ladder (Windows OpenGL → DX12 → Vulkan → software/WARP; Linux Vulkan → OpenGL →
  software/llvmpipe; macOS Metal → the glow renderer); otherwise the saved `gpu_backend` choice (`[graphics]` in desktop.toml,
  set in Configure › Graphics: auto, gl, dx12, vulkan, cpu) applies; `WGPU_BACKEND` forces wgpu's
  choice and then nothing is promoted or saved. A hardware fallback that worked is saved and said before sign-in;
  software is never saved silently. The marker is locked before it is written (fixing a race of the original). Linux and Windows render through
  wgpu (glow is compiled in only on macOS, as rustXterm), with the adapter picked among the surface-compatible ones
  (hardware OpenGL first, then discrete > integrated > virtual > other > CPU; in software, the output-owning WARP
  entry, which needs rustXterm's vendored `wgpu-hal` patch). The window starts at 1280×800 and eframe restores it where it
  was (`persist_window`, its own monitor clamp); a start that fails says the next one tries a safer backend, and a
  missing `libxkbcommon` on Linux is a clear startup error.
- **Benchmark**: `desktop-rust/bench/` — a mock panel (HTTPS, self-signed, `domain.local`, N synthetic users) and a
  script that runs both apps under the same X server and measures binary size, cold start to the sign-in window,
  idle memory, memory and time with 5,000 users on the Users page, and CPU for a scripted scroll of that table. The
  report goes to `desktop-rust/docs/benchmark.md`.

## Out of scope

- Replacing or changing the Fyne app (`desktop/` is not touched), the panel or its API.
- **Drawn depth.** Fyne draws gradients, bevels and soft shadows per theme; egui draws every theme in its **flat
  look** (the same roles and colors, solid fills, the 1 px frames the flat look uses). The flat look is a valid look
  of every theme in the panel and the Fyne app; this is a visual difference, not a functional one.
- An installer (`.msi`, `.deb`, `.dmg`): archives only, as the Fyne app.
- Code signing beyond the macOS ad-hoc signature.

## Capabilities

### New Capabilities

- `app-desktop-rust`: the Rust/egui desktop client of the panel, at parity with `app-desktop`, with its own build,
  CI, release and benchmark.

## Impact

New `desktop-rust/` (code, assets, tests, docs, bench), two new workflows, `docs/desktop.md` (a section pointing to
the Rust app), the root `CHANGELOG.md` (Unreleased), `docs/screenshots/` (Rust app screenshots, 2×, classic-blue,
English, Example Company Ltd, `domain.local`). No server or API change, so no `openapi.yaml` change.
