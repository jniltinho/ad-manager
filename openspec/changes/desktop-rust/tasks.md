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
