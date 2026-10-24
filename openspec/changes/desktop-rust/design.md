## Context

`desktop/` is a Fyne app of ~12k lines (Go) that talks only to the panel API. This change adds a second client in
Rust with egui/eframe to measure whether it is faster, so behavior must match before numbers mean anything. The
base project for structure and tooling is github.com/jniltinho/rustXterm (pinned toolchain image, workspace lints,
CHANGELOG-driven releases).

## Goals / Non-Goals

**Goals:** functional parity (every page, dialog, link, protection and message), the same config file, i18n and
theme roles; a fair, repeatable benchmark against the Fyne app; CI and release isolated to `desktop-rust/`.

**Non-Goals:** drawn depth (gradients, bevels, soft shadows: the Rust app draws every theme flat); changing the
panel, its API or the Fyne app; installers.

## Decisions

### D1. A modular workspace (rustXterm layout)
`desktop-rust/` is a Cargo workspace, layered so each crate has one job and the OS-specific parts are isolated:

| Crate | Job | Depends on |
|---|---|---|
| `apps/ad-manager-desktop-rs` | the binary: arguments, picks the platform by `cfg(target_os)`, starts eframe | everything below |
| `crates/ui` (`adm-ui`) | egui: shell, pages, dialogs, table, toolbar, theme → `Visuals` | core, platform |
| `crates/core` (`adm-core`) | config, panel API client, TLS, i18n, theme data, models and the pure page logic (filters, guards, labels, password generator) — no GUI | platform |
| `crates/platform` (`adm-platform`) | the `Platform` trait: config locations, private files and dirs; shared Unix mode-bit helpers | — |
| `crates/platform-linux`, `-windows`, `-macos` | one `Platform` implementation each (XDG / `%AppData%` + DACL / `Application Support` + no write inside a `.app`) | platform |

The core receives the platform as `&dyn Platform` (dependency injection; tests pass a `TestPlatform`), so no crate
but the platform ones tests `cfg(target_os)`. Lints as rustXterm: `unsafe_code = "forbid"`, clippy
`unwrap_used`/`expect_used`/`todo`/`unimplemented`/`dbg_macro` denied (tests may unwrap); `cargo fmt`, clippy
`-D warnings`, `cargo deny` (licenses, advisories, sources; `deny.toml`).

**Secrets.** The password lives in `zeroize::Zeroizing<String>` while typed and is wiped after the sign-in or test;
the JSON body that carries it is serialized from borrowed fields into a `Zeroizing<Vec<u8>>`; a sync job's password
is a `Secret` (wiped on drop, `***` in Debug); every password field has an id of its own instance and its egui state
(text and undo history) is removed when its screen or dialog goes. It is never logged, written to a file or kept in
the config.

**Sessions and dialogs.** Every sign-in (and every sign-out or expired session) builds a new `Api` — a new cookie jar
and identity — so a reply of an earlier session is dropped. Dialogs carry an instance id: a reply is delivered to the
instance that asked (`Cx::dialog_mut::<T>(id)`), never to another of the same type opened later. Every client is
`https_only`, whatever the configuration file says.

### D2. Threading: tokio off the UI thread, results by channel
eframe owns the main thread. A multi-thread tokio runtime (2 workers) runs every request. A page spawns a task; the
task sends its result as a boxed closure on an `std::sync::mpsc` channel and calls `ctx.request_repaint()`; the UI
drains the channel at the start of each frame and applies the closures to its state (the command pattern: the UI is
the only writer of UI state, no locks around it). Every page keeps a **generation** counter (bumped by a reload, a
server switch, a session change); a result for an older generation is dropped — the rule the Fyne app implements
with `target != ag`, `start != gen`, `n != asked`. Beyond dropping results:
- **actions keep their target**: an action captures the account/share/job and the server (`Api` clone with its agent
  prefix and session id) when it is clicked, and that is what the confirmation and the request use; an action
  confirmed after the server on screen changed is refused with "The list changed servers; refresh and try again."
  (Shares remove), as Fyne;
- **a server switch clears the selection** and the list, so no button acts on a row of the previous server;
- **an abandoned Configure test is signed out**: a successful test whose values were edited, retested or cancelled
  — also one that finishes after that — gets `POST /api/logout`;
- long polls (share tasks, sync runs) end when the session they belong to is no longer the current one.

### D3. HTTP and TLS
`reqwest` (rustls, `ring` provider, no default features, cookies, JSON) with:
- `redirect::Policy::none()` and any 3xx turned into the error "the panel answered with a redirect, which is not
  followed: check the panel URL" (as `errRedirect`);
- 20 s timeout, longer per call where Fyne uses `withTimeout` (sizes 150 s, browse/test 2 min, size 6 min, give-back
  and empty 30 min), none for downloads/uploads;
- a 401 on anything but `/api/login` → session expired → back to sign-in (`errSessionExpired`);
- error bodies `{"error": …}` translated with the server catalog, else `path: HTTP n`; bodies capped at 8 MiB.
Certificate trust, three rustls `ServerCertVerifier`s:
1. **system**: `rustls-platform-verifier` (the OS store), for a panel whose certificate verifies;
2. **pinned**: accepts only a leaf whose `sha256:` + hex SHA-256 of the DER equals the pin, on every connection, and
   still verifies the TLS 1.2/1.3 handshake signature with the leaf key (so presenting the certificate without its
   key fails); a mismatch is "the panel's certificate is not the one trusted in Configure (it changed): …";
3. **probe** (Configure's test only): records the chain, accepts it, sends nothing; then the chain is checked with the
   platform verifier for the URL's host to tell *trusted* from *needs pinning*, and fingerprint, subject, issuer and
   validity are shown (`x509-parser`).
**Pin precedence and the legacy exception.** A `fingerprint` always wins. A config with the legacy `insecure = true`
and no fingerprint (written before pinning existed) keeps working with any certificate — the one exception to the
pinning requirement, kept for compatibility with the Fyne app. A Save after a new successful test writes the pin (or
nothing, for a trusted certificate) and drops `insecure`; a language-only Save while signed in keeps both
`fingerprint` and `insecure` as they were (`desktop/setup.go`).

### D4. Config
Same struct and TOML keys as `desktop/config.go`: `[panel] url, fingerprint, user, insecure`, `lang`; legacy
`[painel] url, insecure, usuario`, `idioma`, `[[agents]]`/`[[agentes]]`. Same lookup (`--config`/`-config`/
`--config=`, `AD_MANAGER_DESKTOP`, `.config/desktop.toml` beside the executable unless in a macOS `.app`, the user
config dir when filled in, untouched examples ignored), same atomic write (temp file `0600` + rename, through a
symlink), same refusal of a group/world-readable file on Unix, same removal of agent tokens with the status-line notice.

### D5. i18n
`t(s)` returns the pt-BR entry when the language is pt-BR, else `s`; `tf!(fmt, args…)` formats after translating
(`%s`/`%d` placeholders kept from the Go catalog, filled positionally). The catalog is
`assets/i18n/pt-BR.json`, seeded by `scripts/gen-catalog.py` from `desktop/i18n_pt_*.go` and then owned by this app.
A unit test scans every crate of the workspace (`crates/*/src`, `apps/*/src`) for `t("…")`/`tf!("…"` literals and fails when one has no pt-BR entry. Server text:
the public `/api/i18n` catalog is fetched on its own request (not tied to health or the logo) every time the sign-in
screen is shown for a panel until one succeeds; a failed or malformed catalog leaves server messages in English.
Errors keep the server's English text and translate it when displayed, so a catalog that arrives after a fast failed
sign-in still translates the message on screen. `server_text` applies exact messages, then the first matching pattern
(`$1` captures), then splits on `": "` — the algorithm of `internal/i18n.pt`, tested with fixtures taken from
`internal/i18n`'s tests.

### D6. Themes
`assets/themes.json`: per theme `scheme`, `depthless`, `minText` (the contrast text keeps: 7 on the AAA themes) and
`focusRing` (3 px on Crystal) and the color roles, generated by `scripts/gen-themes.py` from
`desktop/themes_gen.go` (the Fyne app's generated palettes, themselves generated from the web CSS). A unit test
compares the JSON with `desktop/themes_gen.go` (found from the workspace root, `desktop-rust/..`) when that file
exists; CI sets `ADM_REQUIRE_FYNE=1`, which turns a missing file into a failure, so the check never skips silently in
the monorepo. egui `Visuals` are derived from the roles (`surface`, `fg`, `line`, `field`,
`focus`, `action`, `danger`, …); pages color text with the same roles as Fyne (`fg-strong`, `fg-muted`, `ok`, `err`,
`warn`). The focus ring is drawn outside fields, selects and checkboxes at the theme's `focusRing` width in `focus`;
Tab moves through controls and egui scrolls the focused one into view (`desktop/focus.go`). A unit test checks that
the text roles the pages use (`fg`, `fg-strong`, `fg-muted`, `ok`, `err`, `warn`, `link`) on `surface`, the selected
row's `on-selection` over `selection` (as the Fyne table), the sidebar's `on-nav-on` over `nav-on` (checked on its
own), and the header/footer inks keep the theme's `minText` (the roles Fyne checks in
`theme_test.go`).

### D7. The table
One `DataTable` widget (egui_extras `TableBuilder`, virtual rows: only visible rows are laid out, which is the point
of the benchmark) used by every list. Each cell returns `(text, color role)`; a page may return a `CellLink` (icon,
label, hover hint, action) per cell — rebound every frame from the row's own data, so a link always acts on the
account drawn in it. A link click does not change the selection; a plain click selects the row; Enter on the selected
row calls the page (Users: the actions menu). Below a width threshold (the 1280 px window) the Users action links
show their icon only, as Fyne's compact mode.

### D8. Dialogs
A stack of modals drawn over a dimmed backdrop, top at 10% of the window height, centered: title bar in
`dialog-head`, stripe in `dialog-head-bar` (red `err-fill` for destructive ones), body, footer with Cancel/OK; OK
returning `false` keeps it open. `confirm`, `confirm_typed` (case-insensitive, "The text does not match."),
`show_info`, `show_error`. Escape closes the top one.

### D9. Files
`rfd` (native dialogs, xdg portal on Linux) for CSV export, backup download/upload, quarantine file download; the
transfer streams on a task (no total timeout), with status-line results identical to Fyne's.

### D10. Logo
The company logo (`/api/brand/logo`, PNG/JPEG/SVG ≤ 2 MiB) is decoded with `image`/`resvg`; one that fails to decode
or draws nothing falls back to the AD Manager logo (`assets/`).

### D11. Pages load when opened; frames paced
The Fyne shell builds (and loads) every page at sign-in; the Rust shell loads a page the first time it is opened
(Home at sign-in), so signing in asks for less. Without vsync (VMs, remote desktops, Xvfb) egui would draw as fast as
it can while an animation or the X11 wheel smoothing runs: the app paces frames at 60 per second at most (what a
display with vsync already does) and the table scrolls without animation.

### D12. Crash-safe graphics start (rustXterm's `gpu.rs`)
A driver that crashes inside the device creation of the window kills the process before any Rust code can react, so
the state that survives is a file. Module `apps/ad-manager-desktop-rs/src/gpu.rs`, carried over from rustXterm with
its tests and the corrections below:
- **Plan before the window**: `plan(preference, died, safe, forced, os)`. `--safe-gpu` → `cpu` for this launch (not
  saved). `WGPU_BACKEND` set → a **forced** launch: wgpu's own choice, the marker written as `env`, nothing promoted,
  saved or announced; a dead `env` marker is not a crash of any rung (the next start uses the preference). Otherwise a
  marker that died → `next(tried, os)`, recorded as `crashed`; else the preference.
- **Marker, ownership first** (fixes a race in rustXterm, whose `start` truncates before it locks): `gpu-starting` in
  the folder of desktop.toml is opened with `create` and **without** `truncate`, then `try_lock`ed; only the holder
  of the lock truncates and writes the backend name. A lock that cannot be taken belongs to another instance that is
  starting: this launch neither treats it as a crash nor writes the marker (that instance's protection covers it).
  `Marker::died` reads a marker only when it can take its lock. `clear` removes the path while still holding the
  lock, then unlocks. The app clears it after `GPU_START_FRAMES` = 3 painted frames; an error creating it only loses
  the protection (a status-line notice, never fatal). A regression test runs two processes (the test binary
  re-invoked) to show a held marker is neither erased nor read as a crash.
- **Ladders**: Windows `gl → dx12 → vulkan → cpu` (OpenGL first: DX12 presentation flickers on Intel graphics);
  Linux `vulkan → gl → cpu`; macOS `auto (Metal) → gl`, `cpu` terminal — on macOS `gl` and `cpu` both mean
  `eframe::Renderer::Glow`, the safest renderer once Metal is out (hardware-preferred OpenGL, not guaranteed CPU
  rendering). eframe's `glow` feature only on macOS; Linux and Windows render through wgpu.
- **Where the choice lives**: one file, as the user asked: `[graphics] gpu_backend = "dx12"` in desktop.toml (`auto`
  writes no key). The Fyne app keeps the section when it rewrites the file (`desktop/config.go` `Graphics`, test
  `TestGraphicsKept`). The first-run rule holds: nothing is written to desktop.toml before a successful test and
  Save, so on a first run the choice stays in memory and Save writes it with the rest. `Plan::to_save` is a hardware
  backend reached after a crash, never `cpu` and never on a forced launch. It is written into the configuration as
  saved once 3 frames completed (`Context::cumulative_frame_nr`: a frame's extra layout passes do not count), and a
  write failure is a notice.
- **The marker is emptied, never removed**: removing a locked file lets a start that opened it just before take the
  lock of an unlinked file. An empty marker means the last start painted (Codex review).
- **Notice before sign-in**: the fallback notice ("The graphics driver stopped the last start (gl); now using dx12.
  Change it in Configure.") is shown under the sign-in box. Once signed in, it goes to the status line together with
  any marker or save error. The notices are translated when shown.
- **Configure › Graphics**: as rustXterm's Settings › Display › Graphics.
  - A **Rendering backend** select of `auto`, `wgpu` and the OS's ladder.
    - `wgpu` is wgpu's own choice of backend and adapter, with no preference of the app. On Windows it differs from
      `auto`, which prefers OpenGL.
    - `wgpu` is on no ladder: a crash on it starts the ladder from the top, and on macOS it moves to glow.
  - Changing it saves `[graphics]` into desktop.toml at once, with no panel test needed and the rest of the file as
    saved.
  - The hint reads "Applies at the next start. After a driver crash AD Manager picks a safer one."
- **Adapter choice** (as rustXterm): for an explicit backend, `native_adapter_selector` keeps only adapters that
  support the window surface and `pick_adapter` ranks them: normal → hardware OpenGL first, then discrete,
  integrated, virtual, other, CPU; software → a CPU adapter first, the WARP entry that owns the output before the other
  (GPU-less VMs list WARP twice; rendering on the other one leaves a black window — rustXterm's vendored `wgpu-hal`
  keeps the output-owning one, `vendor/wgpu-hal` + `[patch.crates-io]`), else the first usable adapter so the window
  still opens (NVIDIA's EGL hides llvmpipe), which may be hardware. Linux `auto` keeps wgpu's default selection.
- **Window** (as rustXterm: eframe owns it): `with_inner_size(1280×800)` (it scales on QHD and 4K displays by the
  system scale), minimum 800×600, `persist_window: true` (eframe `persistence`); restoration and its monitor clamp
  are eframe's — the app never moves the window itself (it cannot know monitor origins). `run_native` failing ends
  with "cannot open the window: …; the next start tries a safer graphics backend" (the marker stays); winit's panic
  for a missing `libxkbcommon(-x11)` becomes "cannot open the window: libxkbcommon-x11.so.0 is not installed".
- **WARP**: the CI readback test (64×64 frame on the CPU WARP adapter, `windows-latest`) shows WARP renders; it does
  not present through the output-owning adapter, so a manual gate on a GPU-less Windows VM (no 3D acceleration)
  starts the app with `--safe-gpu` and records a non-black screenshot in `desktop-rust/docs/gates/windows.md`.
- **Benchmark**: the harness records the backend and adapter each app used (`--gpu-info` prints them at start), runs
  the wgpu build with `WGPU_BACKEND=gl` plus `LIBGL_ALWAYS_SOFTWARE=1` (OpenGL on llvmpipe, as Fyne), and gives each run
  its own `HOME`/`XDG_*` so eframe's persisted window never carries over; the glow numbers stay in the report, labelled.

## Parity matrix (Fyne → egui)

| Feature | Fyne (`desktop/`) | Rust (`desktop-rust/src/`) | Notes |
|---|---|---|---|
| Config lookup, legacy keys, 0600, agent tokens removed | `config.go` | `config.rs` | same file format |
| `--config` flag | `parseFlags` | `config::parse_flags` | same spellings |
| Panel client, no redirects, 401 → sign-in | `client.go` | `api.rs` | reqwest + rustls |
| Pinned / probe / system TLS | `client.go` (`pinnedTLS`, `probeCert`) | `tls.rs` | + handshake signature check |
| Configure (test signs in, trust certificate, Save, Cancel, language-only change keeps session) | `setup.go` | `ui/setup.rs` | |
| Sign-in like the web (brand, domain, NETBIOS, logo, message ≤ 4 lines, hint, URL, Configure) | `main.go` `login` | `ui/login.rs` | flat |
| Public brand + theme from `/api/health`, setup-mode notice | `panelBrand` | `ui/login.rs` | |
| Shell: header (domain, operator, Configure, Sign out), sidebar 9 pages, status bar, footer (company, version) | `main.go` `shell`, `widgets.go` | `ui/app.rs` | |
| Home: 6 tiles, alerts (policy, locked, inactive) with links, quick reset/unlock | `page_home.go` | `ui/home.rs` | |
| Users: filters, search, count, row links, Enter menu, compact icons, lean toolbar + ⋯ | `page_users.go`, `toolbar.go`, `widgets.go` | `ui/users.rs`, `ui/widgets.rs` | |
| New / copy / edit (Account + `Groups (n)` tab) / move user | `page_users.go` | `ui/users.rs` | |
| Password dialog (generator, copy, must change, unlock; single-use link 1h/24h/3d/7d) and link dialog | `page_users.go` | `ui/users.rs` | |
| Users CSV export (`/api/export/users`, saved where the operator picks) | `page_users.go` | `ui/users.rs` | |
| Groups tab of the edit dialog: add/remove applied at once, `Groups (n)` kept current; new user: groups added after create, partial failures listed | `page_users.go` | `ui/users.rs` | payload tests |
| Password policy dialog (ranges, weak warning, confirm) | `page_policy.go` | `ui/policy.rs` | |
| Groups: list, system tag, new, description, delete (typed), members add/remove | `page_groups.go` | `ui/groups.rs` | |
| OUs: path, type, new (inside), delete (typed, only empty OU, not Domain Controllers) | `page_ous.go` | `ui/ous.rs` | |
| Computers: search, OS filter, inactive, legacy OS red, enable/disable/move/delete (not DCs), export | `page_computers.go` | `ui/computers.rs` | |
| Shares: server picker, health, sizes + total (refresh forces a new measure), new/edit/remove (task + poll) | `page_shares.go` | `ui/shares.rs` | |
| Share form: name, folder (`/srv/shares/<slug>` while typing a new name), comment, full/write/read groups and users (six lists), hidden, recycle, ABE, audit, create folder; edit keeps name and path read-only and says it reapplies permissions | `page_shares.go` | `ui/shares.rs` | payload tests |
| Backup: servers, list, back up now (kind, folders, full), send (kind, size limit), download (SHA-256 in the status), log, last restoration with its log | `page_backup.go` | `ui/backup.rs` | |
| Restore guards: a DC backup is never restored over a DC (info only); data restore defaults to a new folder, over the originals only with the typed code; config/samba-files/panel always typed code; panel restore says sign in again | `page_backup.go` | `ui/backup.rs` | payload tests |
| Backup delete: typed last 8 chars of the id; a full data backup warns its incrementals go too | `page_backup.go` | `ui/backup.rs` | |
| Schedules (agents only): new (name, kind, cron presets, chains, days, full every, folders), on/off, remove | `page_backup.go` | `ui/backup.rs` | |
| Sync: jobs, runs, preview/run/cancel, details, log, delete, auto refresh | `page_sync.go` | `ui/sync.rs` | |
| Sync wizard (4 steps, connection test, browse, size, dest check, cron check, mirror confirm) | `sync_wizard.go` | `ui/sync_wizard.rs` | |
| Sync job options: copy/mirror, max deleted (count, %), quarantine days/GiB, presets or custom cron, name/id slug, exclude, bandwidth, transfers, verify, reinherit; edit keeps the stored password unless one is typed (Kerberos clears the password file); "Save and preview" and "Only save" | `sync_wizard.go` | `ui/sync_wizard.rs` | payload tests |
| Sync quarantine (give back, replace, download, empty typed) | `sync_quarantine.go` | `ui/sync_quarantine.rs` | |
| Logs: audit (filter, failures) + Samba per server (lazy tabs, files, lines, search, entry dialog) | `page_logs.go` | `ui/logs.rs` | |
| i18n (pt-BR catalog, server catalog, dates) | `i18n*.go` | `i18n.rs`, `assets/i18n/pt-BR.json` | server catalog from `/api/i18n` |
| 18 themes' roles, flat look | `theme.go`, `themes_gen.go` | `theme.rs`, `assets/themes.json` | depth not drawn |
| Modals at 10%, red stripe for destructive, typed confirmation | `modal.go` | `ui/modal.rs` | |
| CSV export, downloads, uploads | `page_computers.go`, `page_backup.go` | `ui/files.rs` | `rfd` |

## Benchmark method

Same machine, same X server (Xvfb 1920×1080, Mesa llvmpipe for both, since both draw with OpenGL/GL-compatible
paths), same mock panel (`bench/mock_panel.py`: TLS self-signed, `domain.local`, Example Company Ltd, N users),
release builds of both. Each metric is the median of 5 runs:

Fixed for both: window 1280×800, scale 1, theme Classic blue with the flat look (the mock's brand), English, the same
5,000 users, warm file cache (one discarded warm-up run), runs alternating Fyne/Rust. **Readiness** is external and
the same for both: the mock panel has answered the request the step waits for (e.g. `/api/users`) and then the
process's CPU time has not moved for 300 ms (the frame is painted and idle); a screenshot is taken for the report and
a step that is not ready in 30 s fails the run. **Scroll**: 200 wheel steps over the table, paced 20 ms apart, then the
same settle rule; a screenshot before and after must differ (the table moved).

| Metric | How |
|---|---|
| Binary size | stripped release binary, bytes |
| Cold start | process start → the sign-in window mapped (`xdotool search --sync --name`), ms |
| Idle memory | PSS of the process 3 s after the sign-in window shows (`/proc/<pid>/smaps_rollup`), MiB |
| Sign-in to Users | the same interval for both: Enter on the password → Home ready → the script clicks Users → the 5,000-row list ready, ms (Fyne builds every page at sign-in and Rust loads Users when it opens; the interval covers both ways) |
| Memory with 5,000 users | PSS 3 s after the Users page shows them, MiB |
| Scroll CPU | user+system CPU time of the process for 200 mouse-wheel steps over the table (`/proc/<pid>/stat`), ms |

The script is `bench/run.sh`; the report (`docs/benchmark.md`) records versions, machine and raw numbers.

## Risks / Trade-offs

- [egui has no native per-theme depth] → documented flat look; colors and roles match.
- [The pinning verifier is security code] → unit tests: a matching pin passes, a different certificate fails, a
  handshake whose signature does not match the pinned key fails; probe tells trusted from untrusted.
- [Two apps drift] → the parity matrix lives here; the catalog and theme tests read the Fyne sources when present.
- [UI behavior only testable through the UI] → `egui_kittest` acceptance tests (AccessKit queries, clicks, key
  presses; no renderer needed) against an in-process mock panel that records requests: row targeting, typed
  confirmation guards, Configure races, lazy Samba tabs.
- [GPU drivers that crash at start, GPU-less VMs] → D12: a crash-safe start that walks a per-OS ladder down to
  software rendering (WARP, llvmpipe/lavapipe), with the adapter choice of rustXterm.
- [wgpu is heavier than glow] → measured: the benchmark is rerun after D12 and both results reported.
