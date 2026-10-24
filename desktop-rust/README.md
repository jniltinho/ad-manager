# AD Manager desktop (Rust)

A second desktop client of the [ad-manager](../README.md) panel, written in Rust with
[egui/eframe](https://github.com/emilk/egui), at feature parity with the Fyne desktop ([`desktop/`](../desktop),
[docs/desktop.md](../docs/desktop.md)). It exists to measure whether an immediate-mode Rust GUI is faster: the
numbers are in [docs/benchmark.md](docs/benchmark.md). The Fyne app stays the shipped desktop; this one is released
on its own tags (`desktop-rust-vX.Y.Z`).

| Sign-in | Users |
|---|---|
| ![sign-in](../docs/screenshots/desktop-rs-01-login.png) | ![users](../docs/screenshots/desktop-rs-04-users.png) |

## What it does

The same as the Fyne app, against the same panel API, with the same rules (the server stays the authority):

- **Configure**: panel URL, user, password and language; **Test connection** reads the certificate, the health and
  signs in for real. A self-signed certificate is shown (fingerprint, subject, issuer, validity) and trusted only
  when the operator clicks **Trust this certificate**; from then on only that certificate is accepted (its
  handshake signed with its key). Redirects are never followed. The password is never saved.
- **Sign-in** like the web one, with the panel's theme, logo and domain; **Home**, **Users** (row links, Enter
  menu, icons at 1280 px), **Groups**, **OUs**, **Computers**, **Shares**, **Backup**, **Sync** and **Logs**, with
  the dialogs of the Fyne app (new/copy/edit/move user, password and single-use link, password policy, share form,
  restore, schedules, sync wizard and quarantine) and its guards (built-in and own accounts, DCs, typed
  confirmations).
- It talks **only to the panel**: file servers through the panel's proxy, never with an agent token.
- The same **`desktop.toml`** as the Fyne app (same keys and lookup: `--config`, `AD_MANAGER_DESKTOP`,
  `.config/desktop.toml` beside the executable, the user config folder), written `0600` and refused when looser.
- English interface with the pt-BR catalog (`crates/core/assets/i18n/pt-BR.json`); server messages translated with
  the panel's catalog (`/api/i18n`).
- The panel's 18 themes (`crates/core/assets/themes.json`, generated from `desktop/themes_gen.go`) in their flat
  look: the same colors, without drawn gradients and shadows.
- A **crash-safe graphics start** (rustXterm's design). The app renders through wgpu. Before the window opens it
  takes a `gpu-starting` marker next to desktop.toml, and a start that dies in the graphics driver makes the next
  start try a safer backend:
  - Windows: OpenGL → DirectX 12 → Vulkan → software (WARP);
  - Linux: Vulkan → OpenGL → software (llvmpipe);
  - macOS: Metal → OpenGL (glow).

  A working fallback is saved as `[graphics]` in desktop.toml (the Fyne app keeps it) and announced under the sign-in
  box. It can be changed in **Configure › Graphics › Rendering backend**, which applies at the next start. Flags and variables:
  - `--safe-gpu` forces the software path for one start;
  - `WGPU_BACKEND` forces wgpu's own choice;
  - `--gpu-info` prints the backend and adapter used.

  The window is 1280×800 (minimum 800×600) and eframe restores its size and position.

## Layout

A Cargo workspace, laid out as [rustXterm](https://github.com/jniltinho/rustXterm):

| Crate | Job |
|---|---|
| `apps/ad-manager-desktop-rs` | the binary: arguments, picks the platform, starts eframe |
| `crates/ui` (`adm-ui`) | egui: screens, pages, dialogs, the table, theme → visuals |
| `crates/core` (`adm-core`) | no GUI: config, panel client, TLS pinning, i18n, theme data, models, page rules, form payloads |
| `crates/platform` (`adm-platform`) | the `Platform` trait (config locations, private files) and the Unix helpers |
| `crates/platform-linux`, `-windows`, `-macos` | one implementation each |

Requests run on a tokio runtime off the UI thread; each ends by sending a closure back to the UI thread over a
channel (only the UI thread changes UI state; a result of a replaced session or of an older reload is dropped).
`unsafe` is forbidden and `unwrap`/`expect` are denied by the workspace lints; passwords are wiped from memory after
use (`zeroize`).

## Build and test

Everything runs in the pinned rustXterm toolchain image (Docker); the host needs no Rust:

```bash
make check     # fmt, clippy -D warnings, unit + UI acceptance tests, cargo deny
make release   # target/docker/release/ad-manager-desktop-rs
make dist      # Linux tar.gz and Windows zip in dist/
make shots     # the docs screenshots (2x, Classic blue, domain.local)
make mock      # a mock panel with 5,000 users (prints the URL and pin for desktop.toml)
make bench     # the benchmark against the Fyne app
```

macOS builds natively on a Mac (`rustup` reads `rust-toolchain.toml`): `cargo build --release` and
`packaging/macos/bundle.sh` for the universal `.app`. CI (`.github/workflows/desktop-rust-ci.yml`) runs only for
changes under `desktop-rust/`; the release (`desktop-rust-release.yml`) runs on `desktop-rust-v*` tags and publishes
Linux, Windows and macOS universal archives with `SHA256SUMS` and the notes of [CHANGELOG.md](CHANGELOG.md).

The OpenSpec change is [openspec/changes/desktop-rust](../openspec/changes/desktop-rust/) (parity matrix and
benchmark method in its `design.md`).
