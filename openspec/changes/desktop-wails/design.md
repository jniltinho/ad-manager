## Context

- `desktop/` is the shipped desktop app: Rust, egui/eframe on wgpu, ~25,000 lines in `crates/{core,ui,platform*}`
  and `apps/`. It is a client of the panel API only, with its own redraw of every web page
  (`openspec/specs/app-desktop-rust`, `openspec/specs/app-desktop`). Its current benchmark (`desktop/docs/benchmark.md`, wgpu on
  llvmpipe, 5,000 users, medians of 5):

  | Binary | Cold start | Idle PSS | Sign-in → Users | PSS with 5,000 users | Scroll CPU |
  |---:|---:|---:|---:|---:|---:|
  | 26.4 MiB | 137 ms | 128.6 MiB | 854 ms | 140.3 MiB | 23.6 s |

  The archived Fyne app, measured in the same run, had 164 ms, 198.5 MiB, 1,467 ms and 236.8 MiB.
- `frontend/` is the web panel: Vue 3, vue-router (HTML5 history), Vite+ (`vp dev`, `vp build`, `vp test`),
  Tailwind 4, Lucide icons, the 20 themes in `src/themes/*.css`, and i18n through `t('…')`. Its pt-BR catalogs are
  imported eagerly by `src/i18n.js` (`import.meta.glob('./locales/pt-BR/*.js', { eager: true })`), plus the server
  catalog (`/api/i18n`).
  - API calls go through `api()` in `src/api.js` as a same-origin `fetch('/api' + path)` with the panel's
    `HttpOnly; Secure; SameSite=Strict` session cookie. Two call sites bypass `api()`: `GpoListDialog.vue` (`fetch`
    of a blob) and `SendBackupDialog.vue` (an `XMLHttpRequest` that sends the raw `File` as the body, with
    progress).
  - The Vite config writes web-only files into `web/dist` (the `.gitkeep` placeholder, Swagger UI).
  - `App.vue` starts panel requests at mount and hides the router while signed out.
  - The server checks no `Origin`.
- `archive/desktop-go/` (Fyne, archived, Go) already has the desktop's rules in Go: `config.go` (desktop.toml
  lookup, legacy keys, `0600`, atomic save) and `client.go` (no redirects, pinned certificate with
  `VerifyConnection`, cookie jar, but a blanket 20 s timeout). Archived code is not changed; it can be copied.
- Wails v3.0.0 (released 2026-10-26 after 34 betas; Go ≥ 1.26). The facts below come from its docs, release notes
  and its source at tag `v3.0.0`:
  - **Linux**: GTK4 + WebKitGTK 6.0 by default. GTK3/webkit2gtk-4.1 only with `-tags gtk3`, removed in v3.1.
    CGO needed.
  - **Windows**: WebView2, no CGO, cross-builds from Linux with plain `go build`.
  - **macOS**: WKWebView, macOS 12+, CGO; the universal binary comes from `wails3 tool lipo`. Signing and
    notarization need a Mac.
  - **Frontend contract**: `wails3 dev` runs the frontend's `dev` script and proxies the Vite dev server on
    `WAILS_VITE_PORT` (default 9245, `strictPort`). `wails3 build` runs its `build` script and embeds
    `frontend/dist`. The `@wailsio/runtime` Vite plugin is optional. The Taskfile is editable.
  - **Asset server**: `AssetOptions{Handler, Middleware}` takes any `http.Handler`, with an SPA `index.html`
    fallback and the runtime under `/wails/`. The webview origin is `wails://wails` or `http://wails.localhost`.
    Requests travel over a native bridge per engine, which is not a socket: the Windows bridge buffers a whole
    response before handing it to WebView2.
  - **No desktop navigation-policy hook**: the Windows `NavigationStarting` callback does not cancel, and a
    `decidePolicyForNavigationAction` handler exists only on iOS.
  - **Features**: services with generated JS/TS bindings, multi-window, tray, native dialogs, single instance, an
    in-app updater and signing commands.
  - **Server build**: `-tags server` runs the app and services over HTTP with no window, the official route for
    Playwright end-to-end tests. Native dialogs are refused there.
  - **Releases**: weekly minor versions and patch hotfixes; Go module, CLI and npm runtime in lockstep.
  - **Security**: all published advisories are fixed except GHSA-2p5f-2gx8-mh8m (same-origin child frames on
    Linux; iframes can call bound methods).
  - **Open issues**: first paint delayed 1–2 s on Windows since beta.27 (#6219); GTK4 tray and Wayland bugs.
    Nothing is published about GPU-less VMs.

## Goals / Non-Goals

**Goals**

- One UI codebase: the desktop runs the web SPA, so page, dialog, theme and i18n parity is by construction.
- Keep every client-side protection the desktop has today, enforced in Go, and add the ones a webview needs
  (navigation, bridge limits).
- Same `desktop.toml`, so the Rust and Wails apps can be swapped on the same computer.
- Builds in Docker like the rest of the repository; CI with tests that can fail; nothing that can leak into a
  release.
- Hard numbers and explicit gates before any switch, with the Rust app as the fallback.

**Non-Goals**

- Removing, freezing or changing `desktop/` (the switch and the archive are later changes).
- Changing the panel API or the server.
- Installers, signing, notarization, auto-update, tray, multi-window, mobile (possible later; listed in Risks).
- Matching the Rust app's start time and memory; the goal is a budget (gate G4), not parity there.

## Decisions

### D1. Reuse the web SPA in a `desktop` build mode, not a separate Vue app

`frontend/` is built twice: `vp build` (web, unchanged) and `vp build --mode desktop`. `vite.config.ts` becomes a
function of the mode:

- **desktop**: `build.outDir` is `../desktop-wails/frontend/dist`, because `//go:embed` cannot reach outside the
  module. Dev serves on `127.0.0.1:$WAILS_VITE_PORT` with `strictPort` and `server.hmr` on the same host and port.
  There is no `/api` dev proxy (the Go asset server handles `/api`). The web-only plugins (`.gitkeep`, Swagger UI)
  are off.
- **web**: exactly today's config.

`main.js` does `if (import.meta.env.MODE === 'desktop') await import('./desktop/index.js')` before mounting. That
module, `@wailsio/runtime` and the desktop catalog must not be in the web build:

- the eager catalog glob in `i18n.js` excludes `./locales/pt-BR/desktop.js` (`['./locales/pt-BR/*.js',
  '!./locales/pt-BR/desktop.js']`), and the desktop module registers that catalog itself, so the file stays where
  AGENTS.md puts pt-BR strings;
- a sentinel string exported by `src/desktop/index.js` and the `@wailsio` package name must be absent from every
  file of `web/dist`, and the web build's Rollup module list must contain no `src/desktop/` module (a test checks
  all three).

Desktop-only code, all in `frontend/src/desktop/`:

- **Bootstrap** (D7): load the configuration before mount, and route to Configure when it is missing or invalid.
- **The Configure screen**: panel URL, user, password, language, Test, the certificate card with "Trust this
  certificate", Appearance, Graphics, Save. It is shown unauthenticated and reachable from the sign-in box and the
  header.
- **The appearance override**: theme and flat for this computer, applied through the SPA's own theme switch.
- **The panel base URL** for links that leave the app. The single-use password link in `PasswordDialog.vue` is
  built from `location.origin`, which in the desktop is the webview's own origin. It uses a `panelOrigin()`
  helper instead: `location.origin` on the web, and the configured panel URL in the desktop.
- **The desktop side of the file helpers** (D4).
- **External links** and the navigation guard (D5).

`wails3 dev` and `wails3 build` call `dev:desktop`/`build:desktop` package.json scripts (`vp … --mode desktop`)
through the Taskfile, so `vp` is used as is. The spike (G1) confirms that `vp dev` honors the port settings and
that hot reload works through the real middleware and the development CSP (D5).

*Alternatives rejected:*

- **A second Vue app in `desktop-wails/frontend`.** It would duplicate pages and drift, which is the problem this
  change removes.
- **A webview pointed at the remote panel URL.** Certificate pinning would depend on each engine's TLS-error hooks
  (WebView2, WebKitGTK, WKWebView all differ), redirects and navigation would be the engine's, the session cookie
  would live in the webview, and Configure could not run before the panel loads.
- **Rewriting `api.js` to call Go bindings per endpoint.** It would duplicate the API surface.

### D2. Go backend: the asset-server middleware proxies `/api/` to the panel

`application.AssetOptions.Middleware` routes requests by path:

- **`/api/…`**: the panel proxy below.
- **`/wails/…`**: Wails' runtime, untouched.
- **Anything else**: the embedded SPA. A `GET`/`HEAD` for a path without a file extension gets `index.html`, so a
  router path such as `/users` reloads. A missing asset gets 404.

The proxy (`desktop-wails/internal/panel`):

- **Forwards only to `{panel URL}/api/…`.** It rebuilds the target from the path and query alone, after decoding:
  a path whose decoded segments contain `.` or `..`, an encoded slash, or a backslash is refused. It never takes a
  host from the request.
- **Uses one `http.Client` per session:**
  - TLS 1.2+;
  - the system roots, or the pinned certificate (the archived `client.go` rule: the leaf's SHA-256 must equal the
    pin, and the handshake is verified with its key);
  - `CheckRedirect` returning an error, so a 3xx is never followed and is answered with the desktop's message;
  - a `cookiejar`;
  - timeouts for dialing, the TLS handshake and response headers, with no whole-request timeout, so slow transfers
    are not cut at 20 s as the archived client would.
- **Keeps the panel session in Go.** It drops the webview's `Cookie` header and strips `Set-Cookie` from every
  response, so the session token never reaches the webview's cookie store, storage or devtools.
- **Starts a new session on `POST /api/login` and after `POST /api/logout`**: a new jar, a new context, and the
  previous session's in-flight requests cancelled. Every response carries an `X-ADM-Session` generation number for
  the SPA's own check (D3).
- **Requires the generation on requests.** Every request except `POST /api/login`, `GET /api/health`,
  `GET /api/brand…` and `GET /api/i18n` must carry the `X-ADM-Session` it was built under. Session-bound service
  calls (`Files`) take it as an argument. The proxy compares it with the current generation **under the same lock
  that picks the jar**, and a mismatch is refused with 409 (`X-ADM-Stale`) before anything is forwarded. A write
  delayed in the bridge across a sign-out and sign-in therefore can never run with the new session.
- **Limits the bridge**: request and response bodies through the bridge are capped (32 MiB, set from G1's
  measurements) in the buffers the app controls. Uploads get a lower **file limit**: the body cap minus 1 MiB of
  headroom for multipart boundaries, part headers and form fields. Wails reads a request body from the native side before the
  middleware runs, and on macOS it can copy the whole body, so the cap cannot stop native buffering. The SPA
  therefore checks sizes **before dispatch**: `FormData` uploads whose files together exceed the file limit are
  refused with the limit's message, and archives go through `Files` (D4). The proxy still refuses anything over the cap that it
  sees, and does not stream a response over the cap to the bridge. Native buffering below the cap is a recorded
  residual.

Wails services (generated bindings, used only by `frontend/src/desktop/`):

- **`Config`**:
  - `Load` returns the file's values and state (missing, loose permissions, legacy insecure).
  - `Test(url, user, password)` probes the certificate (fingerprint, subject, issuer, validity), `/api/health`
    (setup refusal), `/api/login` and `/api/me`, with the archived app's messages. Each test has an id; `Cancel(id)`
    or a newer test signs out the older one's session and drops its result.
  - `Trust(fingerprint)` pins it for the test.
  - `Save` writes the file atomically, `0600`, preserving unknown sections such as Rust's `[graphics]`, and hands
    the tested session to the proxy.
  - `SetAppearance`, `SetGraphics` and `SetLanguage` write their own keys at once, offline, without a test.
- **`Files`**:
  - `SaveText(name, text)` covers CSVs and other exports built in the browser.
  - `SaveFromPanel(apiPath, name)` streams a panel download (backups, GPO backups, quarantine files) straight to
    the file chosen in the native save dialog.
  - `SendToPanel(apiPath)` opens the native open dialog and streams the chosen file as the request body (backup
    archives), emitting progress events and accepting `Cancel`.
  - Every file written is `0600` on Unix, also over an existing file; the paths are always `/api/…` and go through
    the same proxy rules and session.
- **`Shell.OpenExternal(url)`** opens the system browser for an allowlist only: `https://` URLs on the panel's
  origin with the paths `/asynqmon` or `/swagger`, and the brand's support URL when it is `https://`.

The proxy and the services copy the archived app's Go logic into `desktop-wails/internal/{config,panel}` with its
tests. The server's `internal/` packages are not imported, to keep the desktop module free of the server's
dependencies.

### D3. Sessions and targets in the SPA

Cancelling in Go cannot stop a response that already reached JavaScript, and the web SPA has session leaks of its
own:

- raw `fetch`/XHR calls that bypass `api()`: `GpoListDialog.vue`, `SendBackupDialog.vue`, the CSV import in
  `ImportExport.vue` (whose late 401 clears `auth.user`) and the sync log in `Sync.vue`;
- `loadMe()`, whose `catch` clears `auth.user` on any error;
- `PanelDialogs` and the own-password dialog, which live outside the authenticated shell;
- the task follower in `Shares.vue`, which keeps polling for up to 24 h after its page unmounts.

So, for web and desktop alike:

- **Session generation in `api.js`.** It is set from `X-ADM-Session` (desktop) or bumped on sign-in and sign-out
  (web). Every call goes through `api()` or one of two raw helpers (`apiRaw` for blobs and text, `sendFile`), so no
  `fetch`/XHR to `/api` is left outside them.
- **Stale answers.** A response under an older generation **never settles**: nothing waiting for it runs, so no
  `catch` (including `loadMe()`'s, which clears `auth.user`) and no 401 handling can touch the new session. The
  desktop proxy marks the requests it refuses with `X-ADM-Stale`, and Go transfers of an ended session fail with
  `stale session`, which the desktop's call wrapper also turns into a promise that never settles. A 401 clears
  `auth.user` only for the current generation.
- **Session change.** A session-change hook closes the open dialogs: `dialogState` is reset, and pending
  `askConfirm` calls resolve as cancelled. Pollers registered with it stop. The `Shares.vue` follower registers,
  and it also stops on unmount.
- **Target guards.** Pages that switch servers (Sync, and Shares, which already had its own) keep a latest-request
  token. Users and Groups filter their whole list locally, so a filter cannot race. A late answer for server A is dropped once server
  B is selected, as the Rust app does.
- **Dialog instance ownership.** A dialog's completion callbacks (saved, closed, reload) run only if that dialog
  instance is still open: each open gets an id, and a late completion for a closed instance is dropped. Today
  `UserFormDialog` can close a newer dialog B when the save of a closed dialog A finishes (`Users.vue`). The
  Rust app requires the same ("Reply to a closed dialog").
- **Confirmation ownership.** The confirmation is modal, so the page cannot switch targets under it; still, an
  action resumed after a confirmation re-checks that the target it captured (server and object) is still selected
  before sending. This fixes `Shares.vue`, which checks the server only before awaiting, and `Sync.vue`, which uses the
  current server afterwards.

Tests sign out and in, with a slow list, a slow 401, an open confirmation and a running follower in flight, and
check that the new session sees none of them. A write delayed across the re-login is refused by the proxy with
nothing forwarded. Other tests switch the sync server from A to B before A's jobs answer, and switch from A to B
while a confirmation for A is open, then confirm: nothing is sent.

### D4. Files: one helper per direction, large transfers outside the bridge

Webview downloads are not uniform: WebView2 shows its own download UI, WebKitGTK needs the host to handle the
download signal, blob URLs differ, and the Windows bridge buffers whole responses. So, in `src/util.js`:

- **`saveFile({name, text | apiPath})`** replaces the five download sites (`downloadCsv`, `Backup.vue`,
  `GpoListDialog.vue`, `QuarantineDialog.vue`, `ImportExport.vue`). On the web it does today's anchor or blob
  download, unchanged. In the desktop it calls `Files.SaveText` or `Files.SaveFromPanel`.
- **`sendFile(apiPath, file, onProgress)`** replaces the XHR of `SendBackupDialog.vue`. On the web it is today's
  XHR. In the desktop it calls `Files.SendToPanel`, so the native dialog picks the file and Go streams it from
  disk: the archive never crosses the bridge or JS memory.
- **Small uploads** (logo ≤ 2 MiB, CSV imports, `FormData`) stay on the bridge under the D2 cap.

The generated-passwords export of Import/export is a file the operator asks for. It is written `0600`, like
every other save.

### D5. Navigation, external links and CSP

Wails v3.0.0 offers no navigation-policy hook on desktop, so the window's confinement to the app origin is layered:

1. **CSP** on the SPA's HTML. Production policy: `default-src 'self'; connect-src 'self'; img-src 'self' data:
   blob:; style-src 'self' 'unsafe-inline'; form-action 'self'; frame-src 'none'; child-src 'none'; object-src
   'none'; base-uri 'none'`. Development adds only `connect-src ws://127.0.0.1:$WAILS_VITE_PORT` for HMR, and only
   when `FRONTEND_DEVSERVER_URL` is set in a non-production build.
2. **SPA guard** in desktop mode: a capturing click handler on anchors, `window.open` replaced, and `target=_blank`
   handled. Links to the allowlist go to `Shell.OpenExternal`; other off-origin links are ignored.
3. **Native guard**: a per-engine navigation cancel for any top-level navigation off the app origin:
   - WebView2 `NavigationStarting` with `Cancel`, and `NewWindowRequested` handled;
   - WebKitGTK `decide-policy`;
   - a WKWebView navigation delegate.

   v3.0.0 does not expose these. Phase 0 decides between an upstream WEP/PR, a small patch carried with the pinned
   version, or no-go. **Gate G2 requires the native guard on all three engines**; layers 1–2 alone are not enough,
   because programmatic navigation, forms and `srcdoc`/`about:blank` frames bypass a link handler.

The SPA embeds no iframe, and CSP blocks frames. The residual risk of GHSA-2p5f-2gx8-mh8m (same-origin child frames
on Linux reaching bound methods) therefore requires script injection into our own SPA, which the CSP limits. It is
recorded, not claimed solved. G1 also records whether a page on another origin, if loaded, can call bound methods.

### D6. Secrets

- **Password**: it is never saved, logged or printed. It travels from the DOM field to `fetch` to the proxy
  (sign-in), or into `Config.Test`. The Rust app's "wiped from memory" guarantee (`zeroize`, egui text state)
  **cannot be kept**: JS and Go strings are immutable and garbage-collected. This is the web panel's own model,
  recorded as a guarantee the Wails app does not claim.
- **Session cookie**: only in the Go jar, in memory (D2). The tests check the proxy's response headers, the
  webview's cookie store (Playwright `context.cookies()`, which sees `HttpOnly` cookies too) and, on the mock
  panel, that every authenticated request carries only the Go jar's cookie.
- **Management secrets** pass through and are never retained by the desktop: an agent token generated in Settings,
  a file-server registration, the sync wizard's source password and the own-password change. Old `[[agents]]`
  tokens are removed from `desktop.toml` on start. A user-requested export that contains secrets (generated
  passwords) is a private file (D4).
- **OS keychain**: not needed. The desktop persists no secret today and adds none. A later "remember me" would
  store its secret in the OS keychain (Secret Service, Credential Manager, Keychain, e.g. through
  `github.com/zalando/go-keyring`), never in `desktop.toml`. That is out of scope here.

### D7. Bootstrap, `desktop.toml`, window and graphics

- **Bootstrap**: before `app.mount`, the desktop module calls `Config.Load()`.
  - A missing or invalid file (loose permissions, a parse error), or one without `[panel] url` (only preferences
    written by Configure before a successful Save), routes to `/configure`. That route renders without
    a session: `App.vue` shows it instead of the signed-out placeholder, and no panel request starts.
  - With a file, the sign-in screen's user comes from `[panel] user`.
  - The language is `lang` from the file, then the panel default, then English.
  - The theme is `[appearance]` from the file, then the session's theme, then the public one.
- **Config file**: the same keys, legacy keys, lookup order (`--config`, `AD_MANAGER_DESKTOP`, `.config/` beside
  the executable, the user config dir) and `0600` refusal as the Rust spec. A file saved by either app opens in
  the other.
- **Window**:
  - 1280×800, minimum 800×600;
  - its bounds are saved to `window.json` in a private `desktop-wails/` folder beside `desktop.toml` (`0700`,
    refused if it is a link or open to others), and restored on a visible screen;
  - `SingleInstance` focuses the running window.
- **Graphics**: no backend ladder. The engines run their own GPU process and fall back on their own (Wails already
  sets `WEBKIT_DISABLE_DMABUF_RENDERER=1` on NVIDIA). `--safe-gpu`, or `[graphics] safe = true` from Configure ›
  Graphics:
  - Linux: a **workaround preset**, not a guarantee that no hardware path is used: `GSK_RENDERER=cairo` (GTK's
    software renderer), `WEBKIT_DISABLE_DMABUF_RENDERER=1`, `WEBKIT_DISABLE_COMPOSITING_MODE=1` and
    `LIBGL_ALWAYS_SOFTWARE=1`;
  - Windows: `--disable-gpu` through `WindowsOptions.AdditionalBrowserArgs`;
  - macOS: WKWebView has no such switch, so Configure › Graphics is hidden there and the flag prints that it does
    not apply.

  The Rust `[graphics] gpu_backend` key is left alone. G3 checks GPU-less VMs with a painted-frame check (the
  smoke screenshot is not blank). If they fail, the Rust crash marker comes back as a follow-up.

### D8. Build, CI, release

- **Toolchain image**: `desktop-wails/docker/Dockerfile` on **Ubuntu 24.04**, pinned by digest. Its glibc 2.39 is
  the oldest supported ABI, and the supported distros are those with WebKitGTK 6.0. It has Go 1.27.3, Node 22,
  `wails3` and `task` at the pinned version, `libgtk-4-dev libwebkitgtk-6.0-dev build-essential pkg-config`, and
  `xvfb dbus at-spi2-core` for tests. `desktop-wails/scripts/go-docker.sh` mirrors
  `desktop/scripts/cargo-docker.sh`: the repository mounted at `/work`, the host user, the caches in the tree.
- **Linux**: built natively in the image (glibc, not the Zig/musl `wails-cross` image) with `-tags production`.
  Artifacts:
  - a `tar.gz` with the binary and a README listing the runtime packages;
  - in phase 3, nfpm packages: `.deb` with `Depends: libgtk-4-1, libwebkitgtk-6.0-4`, and `.rpm` with
    `Requires: gtk4, webkitgtk6.0`.
- **Windows**: `GOOS=windows CGO_ENABLED=0 -tags production` in the same image, `-H windowsgui`; a `zip`. With no
  WebView2 runtime the app shows a native message with Microsoft's download link and exits non-zero. The NSIS
  installer with the bootstrapper is a follow-up.
- **macOS**: on the `macos-14` runner, arm64 and amd64 combined with `lipo`, in a `zip` with the `.app`, unsigned
  as today.
- **Make targets**: `make desktop-wails`, `desktop-wails-check` (`gofmt`, `go vet`, `staticcheck`,
  `govulncheck`, Go tests, the frontend's `vp check`/`vp test`, the web-bundle checks of D1, the version lockstep)
  and `desktop-wails-e2e`. They are not part of `make build` until the switch.
- **CI**: `.github/workflows/desktop-wails-ci.yml` is **its own workflow**, triggered by `pull_request` and pushes
  to `main` with path filters (`desktop-wails/**`, `frontend/**`, the workflow), and **not called by `ci.yml` or
  `release.yml`**. `release.yml` calls `ci.yml` and publishes every artifact of its run, so a Wails artifact must
  never be in that run. It runs:
  - Linux: check, e2e and native smoke;
  - Windows: cross-build, plus a native smoke on `windows-latest`;
  - macOS: universal build and smoke on `macos-14`.

  Uploads are named `desktop-wails-preview-*`. A test of the release run checks that its artifacts are unchanged.
- **Pinning**: one exact Wails version in `go.mod`, in `frontend/package.json` for `@wailsio/runtime` (imported
  only by the desktop module) and for `wails3` in the image. A test fails if they differ. Upgrades are
  deliberate changes, never `@latest`.

### D9. Tests

| Layer | What | Where |
|---|---|---|
| Go unit | proxy (https only, redirect refused, pin match / mismatch / wrong key, path confinement with raw encoded traversal sent to the handler directly, history fallback, cookie strip both ways, session cancel and generation header, bridge cap, CSP per mode), config (lookup, legacy keys, `0600`, tokens removed, unknown sections kept, offline appearance/graphics), Config test cancellation, Files (`0600`, overwrite, streaming, cancel), Shell allowlist, version lockstep, log capture without secrets | `go test` in the image |
| Frontend unit | existing `vp test`; `saveFile`/`sendFile` web vs desktop; session generation in `api.js`; desktop strings in the pt-BR catalog | `vp test` |
| Web unaffected | `vp build` has no sentinel, no `@wailsio`, no `src/desktop/` module; existing Playwright visual tests pass | CI |
| End-to-end | Playwright (Chromium ≈ WebView2, WebKit ≈ WebKitGTK/WKWebView) against the Wails **server build** and a mock panel: Configure → trust → save → every page → exports → sign-out/in with a slow request; cookie store empty | `make desktop-wails-e2e` |
| Native smoke | the real window on Xvfb, `windows-latest` and `macos-14`, built with the `smoke` tag (test-only dialog and opener adapters): sign in to the mock, a JSON call, a small multipart upload, a `SendToPanel` and a `SaveFromPanel` with hash checks, the guard refusing `location.href = 'https://example.org'`, a non-blank screenshot, exit 0 | CI |
| Shipped binary | built with `production` and without `server`/`smoke`: opens no listening socket and ignores `FRONTEND_DEVSERVER_URL` | CI |
| Live | the e2e suite against the all-in-one test image (`domain.local`), password from a `0600` file outside the repo; never a production panel | manual / `make` |
| Benchmark | `desktop/bench` method (Xvfb, llvmpipe, 5,000-user mock, medians of 5), current Rust binary re-measured in the same run; PSS **and CPU** summed over the app and all its webview processes; readiness = a painted frame reported by the SPA, not the window mapping | `desktop-wails/bench/run.sh` |

**Server build safety (tests only)**: it binds `127.0.0.1` on a random port and checks the `Host` header. Every
request must carry a per-run random token header (Playwright sends it through `extraHTTPHeaders`), because the Go
jar would otherwise lend its session to a cross-site request. It uses a throwaway config directory and talks only
to the mock or test panel. The test-only adapters (dialogs, opener) compile only under the `server` or `smoke`
tags.

### D10. Rust vs Wails trade-offs

| | Rust (egui, wgpu), measured | Wails v3 (expected, measured in G4) |
|---|---|---|
| UI code to maintain | ~25,000 lines of Rust re-implementing the web | the web SPA + ~2,000 lines of Go + `frontend/src/desktop/` |
| Web parity | by hand, specs for geometry, fonts, depth, icons | by construction (same DOM, CSS, themes); engine differences only |
| Binary | 26.4 MiB, self-contained | ~15–25 MiB + the system webview (WebKitGTK 6.0, WebView2) |
| Cold start | 137 ms | a few hundred ms to first paint (webview processes; #6219 on Windows) |
| Idle memory | 128.6 MiB PSS | expected 150–300 MiB including the webview processes |
| 5,000 users | 140.3 MiB, 854 ms | the web's DOM table, to be measured; fixes help the web too |
| Linux deps | libxkbcommon, GL/Vulkan driver | GTK4 + WebKitGTK 6.0 and glibc ≥ 2.39: Ubuntu 24.04+, Debian 13+, Fedora 40+, RHEL 10+; Ubuntu 22.04, Debian 12 and RHEL 9 dropped |
| Windows | self-contained exe | WebView2: present on Windows 10/11 and Server 2025, missing by default on Server 2016/2019/2022 |
| GPU-less VMs | crash-safe ladder down to WARP/llvmpipe, tested | engine fallbacks + `--safe-gpu` (not macOS); untested (G3) |
| Tests | egui_kittest in-process, live test | Go unit + Playwright on two engines + native smoke; no WebDriver for the native window |
| Toolchain | pinned Rust image, Windows via mingw image | one Go image for Linux and Windows; macOS on a runner |
| Framework risk | egui 0.x, stable in this repo | Wails v3.0.0 days old, weekly minors, a navigation guard to add |

## Migration plan and gates

| Phase | Content | Exit |
|---|---|---|
| 0 Spike (≤ 2 weeks) | `wails3 init -t vue`, swap in `frontend/` desktop mode with `vp`; middleware proxying a real test panel; bridge limits; `Files` streaming; native navigation guard prototype on the three engines | **G1** |
| 1 Frontend prep | `saveFile`/`sendFile`, session generation in `api.js`, desktop mode in `vite.config.ts`, catalog glob, the dynamic `desktop/` import; web unchanged | web tests green |
| 2 Desktop backend | `internal/config`, `internal/panel`, services, navigation guard, bootstrap and Configure, appearance, window, `--safe-gpu`; Go unit tests | **G2** |
| 3 CI and numbers | image, Make targets, workflow, e2e, smoke on 3 OSes, benchmark, `.deb`/`.rpm` | **G3**, **G4** |
| 4 Preview | parity checklist signed; preview builds used by the operator on real machines for ≥ 4 weeks | **G5** |
| 5 Switch (separate change) | release publishes the Wails app as `ad-manager-desktop`; screenshots and docs; Rust kept buildable as fallback for 2 releases | — |
| 6 Archive (separate change) | `desktop/` moves to `archive/`, its CI and the `themes.json` generator go | — |

**Gates**

- **G1 (feasible)**:
  - `wails3 dev` with `vp dev` hot-reloads the SPA through the middleware and the development CSP, and
    `wails3 build` embeds `vp build --mode desktop`;
  - on WebKitGTK 6.0, WebView2 and WKWebView: JSON calls and multipart uploads up to the bridge cap work;
    `SendToPanel` and `SaveFromPanel` move a 200 MiB backup with matching hashes, progress, cancellation and a
    throttled (slow) link; the bridge's real limits and buffering are measured and the cap is set from them;
  - bindings work, and whether another origin can call them is recorded;
  - a native navigation guard is prototyped on each engine, with the route to ship it chosen (upstream or carried
    patch).

  Otherwise **no-go**.
- **G2 (security parity)**: every scenario of `app-desktop-wails` on the proxy, sessions, config, files,
  navigation and secrets passes in Go unit, e2e or smoke tests, and the native navigation guard is active on all
  three engines.
- **G3 (platforms)**: the smoke and the e2e pass on Ubuntu 24.04, Debian 13, Fedora (current), Windows 11,
  Windows Server 2022 with WebView2 installed, and macOS 14. Fedora means Fedora 40 or later, because the
  build's glibc floor is 2.39. A GPU-less VM paints frames (Linux llvmpipe, Hyper-V
  without GPU).
- **G4 (budget, medians on the benchmark machine, current Rust re-measured in the same run)**:
  - stripped Linux binary ≤ 35 MiB;
  - first painted sign-in screen ≤ 1,000 ms;
  - idle PSS of the app and its webview processes ≤ 300 MiB;
  - sign-in → Users with 5,000 users ≤ 1,500 ms (the Fyne app shipped at 1,467 ms);
  - PSS with 5,000 users ≤ 450 MiB;
  - scroll CPU of all processes ≤ 1.5× the Rust app's.

  A missed budget is either fixed or **waived in writing** by the operator in this `design.md`, with the number.
  A waived gate is reported as waived, never as passed.
- **G5 (maturity)**: Wails pinned to a release at least 4 weeks old with no open P0/P1 issue on our three
  desktop targets, the navigation guard shipped (upstream or as a carried patch with a test), and the parity
  checklist complete.

**G1 results (milestone 0, 2026-10-26, Linux)**

Measured on Ubuntu 24.04, with WebKitGTK 2.52.6 (6.0 API), Xvfb, the WebKit sandbox on and the all-in-one test
panel (`domain.local`):

- **Build**: `vp build --mode desktop` is embedded by a `-tags production` Go build. The Linux binary is 12.0 MiB
  (CGO, WebKitGTK 6.0, glibc 2.39); the Windows `.exe` is 12.4 MiB (`CGO_ENABLED=0`, cross-built in the same
  image). The archives are about 5 MB each. Dev mode with hot reload was not exercised yet.
- **Window and SPA**: the window appears in 620–830 ms, the sign-in and Home pages are the web's, and signing in
  goes through the proxy with the pinned certificate.
- **Bridge (smoke build)**: request and response bodies of 1, 16, 32, 64 and 128 MiB went through whole, with
  128 MiB echoed in 1.2 s. WebKitGTK does not limit the bridge. The cap of D2 (32 MiB) stays for WebView2, which
  buffers whole responses, until Windows is measured.
- **Files**: `SaveFromPanel` saved `/api/export/users` with mode 0600 through the smoke build's dialog adapter.
  Streaming 200 MiB down and 50 MiB up, with hashes, progress and cancellation, is covered by Go tests against a
  TLS fake panel.
- **Navigation guard**: on Linux, the webview's `decide-policy` signal (cgo, `guard_linux.go`) ignored
  `location.href = 'https://example.org/'` and new-window requests; the window stayed on `wails://localhost/`.
  Windows and macOS have no hook in v3.0.0. Their guard needs a carried patch of Wails (WebView2
  `NavigationStarting` with `Cancel`, a WKWebView navigation delegate), planned for milestone 2. G2 stays closed
  until it lands.
- **Windows and macOS (milestone 3's CI, the smoke build without a panel)**: both start, load the SPA and answer
  a binding. Bridge times for 1, 16, 32, 64 and 128 MiB, each body echoed whole:
  - WebView2 (`windows-latest`): 248 ms, 3.6 s, 7.1 s, 14.3 s and 28.9 s (about 4.5 MB/s, through Wails'
    chunked transport). This confirms keeping files off the bridge, and the 32 MiB cap.
  - WKWebView (`macos-14`): 11 ms, 86 ms, 242 ms, 507 ms and 1.1 s.
- **Linux sandbox**: WebKitGTK 6.0 runs its web process in a bubblewrap sandbox that needs unprivileged user
  namespaces. On a system whose policy blocks them, WebKit aborts at start (`bwrap: setting up uid map: Permission
  denied`). An Ubuntu desktop allows them to `bwrap` through its AppArmor profile; GitHub's runner and Docker do
  not. This is a Linux requirement of the app (G3), to be stated in its install notes.
- **Not yet measured**: whether
  another origin can call bound methods, which is moot on Linux because the guard prevents another origin from
  loading.

G1 verdict: **go** on Linux, Windows and macOS. The Windows and macOS native guard is still missing (G2).

**G4 results (milestone 3, 2026-10-26)** — [desktop-wails/docs/benchmark.md](../../../desktop-wails/docs/benchmark.md),
medians of 5 against the Rust app (wgpu) on the same mock panel and Xvfb/llvmpipe:

- **Binary**: 12.5 MiB, against Rust's 29.3. **Pass.**
- **Ready sign-in screen**: 2,054 ms, against Rust's 716. **Fail.**
- **Idle PSS** (all 8 processes): 415 MiB, against Rust's 127. **Fail.**
- **Sign-in → Users with 5,000 users**: 9,984 ms, against Rust's 1,182. **Fail.**
- **PSS with 5,000 users**: 1,375 MiB, against Rust's 146. **Fail.**
- **Scroll CPU**: 31.0 s, against Rust's 37.4. **Pass.**

The web's Users page puts all 5,000 rows in the DOM, which a webview on software rendering pays for. **G4 is not
met.** The next step is a virtualized Users table in the web panel (it helps the browsers too) and a new run; the
alternative is a written waiver.

**Rollback**

- Before the switch: nothing shipped depends on `desktop-wails/`. Stopping means deleting the folder, its workflow,
  its Make targets and the desktop mode. `saveFile`, `sendFile` and the session generation stay, because they are
  behavior-neutral on the web.
- After the switch: the release builds the Rust app again from `desktop/`, which stays buildable for two releases.
  `desktop.toml` is shared, so operators swap binaries without reconfiguring.

## Parity checklist (Rust → Wails)

| Rust app behavior | Wails app | How it is checked |
|---|---|---|
| https only, `http://` refused in Configure | `Config.Test`, proxy | Go unit |
| No redirect ever followed | proxy `CheckRedirect` | Go unit |
| Pinned certificate, key-verified; changed cert message | proxy TLS config | Go unit |
| Legacy `insecure = true` until re-test | config + proxy | Go unit |
| Only the panel; agents via `/api/agents/{id}` | proxy confined to `/api/` | Go unit |
| Password never saved, logged or printed | proxy, services | Go unit (log capture) |
| Password wiped from memory | **not kept** (D6) | documented |
| New session per sign-in, late answers dropped | proxy cancel + SPA generation, dialogs and pollers closed (D3) | Go unit + e2e |
| A reply reaches only its target (server A vs B) | target guards (D3) | `vp test` + e2e |
| `desktop.toml` lookup, legacy keys, `0600`, tokens removed | `internal/config` | Go unit |
| Configure: test, trust, Save only after a test, language-only Save | Configure + `Config` | e2e |
| Test finishing after an edit or a cancel is dropped and signed out | `Config.Test` ids | Go unit + e2e |
| Appearance and graphics changed offline | `SetAppearance`, `SetGraphics` | Go unit + e2e |
| Sign-in brand, logo fallback, 401 → sign-in | the web's Login and `api.js` | e2e |
| Every page, tab and dialog with the web's guards | the web SPA | e2e + web tests |
| Global search, my password | the web SPA | e2e |
| Single-use password link points to the panel | `panelOrigin()` (D1) | `vp test` + e2e |
| A reply reaches only the dialog instance that asked | dialog instance ownership (D3) | `vp test` + e2e |
| Confirmed action after a target switch sends nothing | confirmation ownership (D3) | `vp test` + e2e |
| Old write delayed across re-login refused | generation required on requests (D2) | Go unit + e2e |
| 20 themes, flat, per-computer theme | the web CSS + appearance override | e2e screenshots per theme |
| English + pt-BR + server catalog | the web i18n + `desktop.js` | `vp test` |
| Private downloads (`0600`), CSV exports | `saveFile` → `Files` | Go unit + smoke |
| Uploads: logo, CSV (multipart) and backup archive (raw body) | bridge under the cap; `sendFile` → `Files.SendToPanel` | e2e + smoke + G1 |
| `/asynqmon`, `/swagger` in the system browser | `Shell.OpenExternal` allowlist | Go unit + smoke |
| Window never leaves the app | CSP + SPA guard + native guard (D5) | smoke |
| 1280×800, min 800×600, restored | window options + `window.json` | Go unit + smoke |
| Crash-safe graphics start | `--safe-gpu` and `[graphics] safe` on Linux/Windows only (D7) | smoke + G3 |
| Docs screenshots at 2×, Classic blue, `domain.local` | Playwright on the server build | phase 5 |
| Live test on the all-in-one image | e2e live config | manual |
| Benchmark | same method + webview processes | G4 |

## Risks / Trade-offs

- **Wails v3 is new** (GA on 2026-10-26, weekly minors). Mitigation: exact pins in lockstep, G5, and the Rust
  fallback.
- **No navigation-policy hook** in v3.0.0. Mitigation: D5 layers; G2 needs the native guard, upstream or as a
  carried patch, so this is a hard condition and may end in no-go.
- **Bridge behavior differs per engine** (Windows buffers whole responses; macOS can copy whole request bodies
  before the middleware). Mitigation: large transfers never use the bridge (D4), the SPA checks sizes before
  dispatch, and the cap comes from G1's measurements (cap and cap + 1 tests).
- **Linux runtime dependency**: WebKitGTK 6.0 drops Ubuntu 22.04, Debian 12 and RHEL 9 once the gtk3 tag goes in
  v3.1. Mitigation: state the supported distros, and ship `.deb`/`.rpm` with dependencies. An AppImage (bundled
  WebKit, large) is a follow-up only if operators ask.
- **WebView2 on Windows Server 2016/2019/2022**: it is not installed by default, and AD admins often work there.
  Mitigation: a clear message with the download link; the NSIS installer with the bootstrapper, or the
  fixed-version runtime, as follow-ups if G3 shows it matters.
- **Memory and start time** are worse than Rust by design. Mitigation: G4 budgets with written waivers; the
  5,000-row DOM table gets better for the web too if it needs virtualization.
- **Three engines** (Chromium, WebKitGTK, WebKit): the web panel already targets Chrome, Firefox and Safari, and
  `native.js` already themes selects and tooltips. Mitigation: e2e on Chromium and WebKit, screenshots per theme.
- **Version skew**: the SPA embedded in the desktop is the desktop's version, the same risk as Rust today (both
  are released together). A mismatch warning is out of scope.
- **Bound methods reachable from frames** (GHSA-2p5f-2gx8-mh8m): residual, recorded (D5).
- **Server build**: tests only, protected by D9's rules. Shipped binaries are checked for no listener and no dev
  asset loading.
- **Lost Rust-specific guarantees**: memory wiping, the automatic GPU ladder, and the safe-GPU switch on macOS.
  All are documented; the ladder can return if G3 fails.

## Open Questions

- Should the Wails app eventually adopt the in-app updater (GitHub Releases provider, ed25519)? It would need
  signed archives in `release.yml` and its own change.
- Is Windows Server 2016/2019 a supported desktop host for operators? The answer decides whether the NSIS
  installer with the WebView2 bootstrapper is required before the switch.
- Navigation guard route: upstream WEP/PR to Wails, or a patch carried with the pinned version? Decided in
  phase 0.
