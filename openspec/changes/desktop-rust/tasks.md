## 1. Proposal

- [x] 1.1 Proposal, design (parity matrix, benchmark method), spec; `openspec validate desktop-rust --strict`.
- [x] 1.2 Codex review of the proposal (10 should-fix: restore guards, form payloads, action targets, legacy TLS,
      CI isolation, theme drift, accessibility metadata, catalog timing, benchmark readiness, UI tests); fixes applied; second pass
      (3 should-fix: workspace scan paths, equal benchmark interval, selection contrast roles) applied.

## 2. Scaffold (rustXterm style)

- [x] 2.1 `desktop-rust/` workspace: `apps/ad-manager-desktop-rs`, `crates/{core,ui,platform,platform-linux,
      platform-windows,platform-macos}`; workspace lints, `rust-toolchain.toml`, `rustfmt.toml`, `clippy.toml`,
      `deny.toml`, `scripts/cargo-docker.sh`, `Makefile`, `README.md`, `CHANGELOG.md`, `.gitignore`.
- [x] 2.2 Assets: logos, icon; `scripts/gen-catalog.py` → `assets/i18n/pt-BR.json`; `scripts/gen-themes.py` →
      `assets/themes.json`.

## 3. Core

- [x] 3.1 `config.rs` (lookup, legacy keys, 0600, agent tokens, atomic save) + tests.
- [x] 3.2 `tls.rs` (system, pinned with signature check, probe) + tests.
- [x] 3.3 `api.rs` (client, no redirects, 401, errors, timeouts, via agent, raw get/post) + tests against a local
      server.
- [x] 3.4 `i18n.rs` (t, tf!, dates, server catalog algorithm) + tests (catalog coverage scan).
- [x] 3.5 `theme.rs` (roles → egui visuals, unknown → Default) + test against `../desktop/themes_gen.go`.

## 4. UI

- [x] 4.1 Shell, modal stack, table widget with links, toolbar with ⋯.
- [x] 4.2 Configure and sign-in (brand, logo, messages).
- [x] 4.3 Home, Users (+ dialogs), Password policy.
- [x] 4.4 Groups, OUs, Computers.
- [x] 4.5 Shares, Backup.
- [x] 4.6 Sync (+ wizard, quarantine), Logs.
- [x] 4.7 `egui_kittest` acceptance tests with an in-process mock panel (row targeting, typed confirmations,
      Configure races, lazy Samba tabs, payloads of share/sync/restore/new user).
- [x] 4.8 Pure logic tests (filters, editable, links per row kind, password generator, ouPath, parentDN, sizes,
      durations, labels).

## 5. CI and release

- [x] 5.1 `.github/workflows/desktop-rust-ci.yml` with path filters.
- [x] 5.2 `.github/workflows/desktop-rust-release.yml` on `desktop-rust-v*` (Linux, Windows, macOS universal).

## 6. Validation and docs

- [x] 6.1 fmt, clippy `-D warnings`, tests green, cargo deny; run against the mock panel (tests, screenshots,
      benchmark) and against the all-in-one image with `domain.local` (sign-in, every page with real data).
- [x] 6.2 Codex review of the code (6 blockers: HTTPS-only client, a new session per sign-in, unique Configure
      generations, dialog instance ids, password field history, file picker bound to its session; 11 should-fix:
      share/sync session guards and polling, password copies, Unicode own-login check, exclusive write probe,
      catalog on every session, "Panel default", per-address TLS dial, selection ink, accessible link names and
      labelled fields, an auditable benchmark); fixes applied with regression tests.
- [x] 6.3 Benchmark (`bench/`), report `desktop-rust/docs/benchmark.md`.
- [x] 6.4 Docs (`desktop-rust/README.md`, `docs/desktop.md`), CHANGELOGs, screenshots (2×, classic-blue, English,
      Example Company Ltd, `domain.local`).
- [ ] 6.5 PR.

## 7. Crash-safe graphics start (rustXterm design)

- [x] 7.0 Proposal, design (D12) and spec; Codex review (1 blocker: marker race — ownership before truncation; 7
      should-fix: eframe-owned window placement, `WGPU_BACKEND` forced launches, qualified software/adapter
      guarantees, Graphics in Configure without a panel test, `graphics.toml` instead of desktop.toml, a manual
      GPU-less WARP presentation gate, an isolated and recorded benchmark); applied.
- [x] 7.1 `apps/ad-manager-desktop-rs/src/gpu.rs` from rustXterm with the corrections of D12: `Backend`, `Os`,
      `ladder`, `next`, `plan` (forced, `to_save`, `notice`), `Marker` (lock before truncate, clear under the lock),
      `configuration`, `pick_adapter`; unit tests (ladders, plan, forced launches, what is saved, adapter choice incl.
      no software adapter) and the two-process marker test.
- [x] 7.2 eframe: `wgpu` on Linux and Windows, `glow` only on macOS (Metal → glow), `persistence`; rustXterm's
      vendored `wgpu-hal` patch (output-owning WARP) with its `vendor/README.md` note.
- [x] 7.3 `main.rs`: plan before the window, `--safe-gpu`, `--gpu-info`, `WGPU_BACKEND`, `persist_window`,
      1280×800 / 800×600, the `run_native` error message, the missing `libxkbcommon` error.
- [x] 7.4 App: `GPU_START_FRAMES` = 3 then clear the marker and save a working hardware fallback to `graphics.toml`
      (write errors as notices), the notice under the sign-in box and in the status line, Configure › Graphics saved
      without a panel test ("Applies at the next start."), tests that desktop.toml is never written by it.
- [x] 7.5 CI: a `windows-latest` job in `desktop-rust-ci.yml` (native build + `tests/warp.rs` readback); the release
      keeps the cross-built Windows zip; the GPU-less Windows presentation gate written in `docs/gates/windows.md`.
- [ ] 7.5a Run the GPU-less Windows presentation gate and fill its receipt (needs a Windows VM without 3D).
- [x] 7.6 Benchmark: `--gpu-info` recorded per run, `WGPU_BACKEND=gl` + llvmpipe for the wgpu build, a fresh
      `HOME`/`XDG_*` per run; rerun and update `docs/benchmark.md` with glow and wgpu numbers (wgpu: still faster to
      start and to Users and lighter, but +26% scroll CPU vs Fyne on llvmpipe and +5 MiB binary vs glow).
- [x] 7.7 Codex review of the implementation (2 blockers: frames counted as UI passes — now egui's completed frames
      with a repaint until then; the marker and window beside desktop.toml — now a private `desktop-rs/` state folder
      refused when a link or open to others. 3 should-fix: a stable `gpu-starting.lock` separate from the removable
      marker, the protection-off warning shown at once and on the sign-in screen, the two-process test reaping its
      child); applied, with tests.
