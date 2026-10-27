## Why

The desktop app (`desktop/`, Rust + egui) is a second implementation of the web panel: about 25,000 lines of Rust
redraw, widget by widget, what about 9,000 lines of Vue/JS in `frontend/` already do. Every web feature, theme,
dialog and fix has to be ported by hand, and a large part of the Rust specs (`app-desktop-rust`: control geometry,
typography, depth, icons, footer panes, theme recipes) exists only to keep the copy pixel-equal to the original.

[Wails v3](https://github.com/wailsapp/wails) (v3.0.0, released 2026-10-26) runs a Go backend with the system's
webview (WebView2, WebKitGTK 6.0, WKWebView). With it, the desktop can run **the web panel's own Vue 3 + Vite+ +
Tailwind 4 SPA**, so pages, dialogs, the 20 themes, i18n and every future web change come to the desktop for free,
while a small Go backend keeps the desktop's protections (panel-only access, certificate pinning, no redirects,
private `desktop.toml`).

The price is real and has to be measured, not assumed: webview memory and start time, WebKitGTK 6.0 as a Linux
runtime dependency, WebView2 on Windows Server, three browser engines to test, and a framework whose first stable
release is days old and has no navigation-policy hook on desktop. This change proposes the migration **as a gated plan**: build the Wails app beside the Rust
one, prove parity and the numbers, and only then switch, in a separate change.

## Recommendation

**Conditional GO**: start the Wails app now as a time-boxed spike, then a preview built beside the Rust app;
**no-go on replacing the Rust app** until every gate in `design.md` passes (spike, security parity, platforms,
benchmark budget, Wails maturity) — including a native navigation guard on all three webview engines, which
Wails v3.0.0 does not offer yet. The Rust app stays the shipped desktop, untouched, until then.

Why go: one UI codebase instead of two (the main maintenance cost of the desktop today), exact web parity by
construction, Go everywhere on the backend (the panel and the archived Fyne app's config and pinning code are Go),
Windows built without CGO. Why conditions: the Rust app is faster and lighter today (137 ms start, 129 MiB
idle, 140 MiB with 5,000 users on wgpu) and a webview app will not match that; Linux users need WebKitGTK 6.0 (Ubuntu 24.04+,
Debian 13+, RHEL 10+); and Wails v3 changes weekly.

## What Changes

- **New app `desktop-wails/`** (Go module `ad-manager/desktop-wails`, binary `ad-manager-desktop` in the end,
  `ad-manager-desktop-wails` while both exist), Wails v3 pinned to an exact version, beside `desktop/`.
- **The web SPA in a `desktop` build mode**: `frontend/` gets `vp build --mode desktop` (and `vp dev --mode
  desktop` on `WAILS_VITE_PORT`) whose output is embedded by the Wails app. Desktop-only code (Configure screen,
  per-computer appearance, file saving, external links) lives in `frontend/src/desktop/`, loaded only in that mode;
  the web build stays byte-for-byte free of it and of `@wailsio/runtime`.
- **File transfers behind two helpers**: the five places that save files call `saveFile`, and the backup
  upload calls `sendFile`. The web keeps today's anchor download and XHR. The desktop moves the bytes in Go
  (native dialog, streamed from or to disk, mode `0600`), so large transfers never cross the webview bridge.
- **Session and target guards in the SPA** (web and desktop):
  - an answer from an earlier session is dropped, and a late 401 cannot sign out the new session;
  - dialogs, confirmations and pollers end with the session;
  - a late answer for a file server no longer selected is dropped.

  These fix leaks the web has today.
- **Go backend**: an asset-server middleware proxies the SPA's same-origin `/api/…` requests to the configured
  panel with the desktop's rules (https only, pinned certificate, no redirects, only `/api/`, session cookie kept
  in Go and never given to the webview). Services cover Configure (test, trust, save), file transfers and an
  allowlisted opener for the panel's `/asynqmon` and `/swagger`. The window is confined to the app by CSP, an SPA
  guard and a native navigation guard per engine. `desktop.toml` keeps its keys, lookup order and `0600`
  rule, ported from the archived Fyne app's Go code.
- **Build, CI and benchmark**: a pinned Docker image (Go, Node, GTK4/WebKitGTK 6.0 dev, Xvfb) for Linux builds and
  tests; Windows cross-built from Linux with `CGO_ENABLED=0`; macOS universal on the macOS runner. A separate,
  path-filtered workflow, **not called by `ci.yml` or `release.yml`** (the release publishes every artifact of
  its run), uploads preview builds as workflow artifacts. The Rust benchmark method is reused against the same
  mock panel, counting the webview processes too.
- **Not in this change**: removing or changing `desktop/`, changing the release, the in-app updater, installers
  (NSIS/MSIX/DMG), code signing, tray, multi-window, mobile. The switch (Wails becomes `ad-manager-desktop` in the
  release, Rust is archived) is a later change, allowed only after the gates.

## Capabilities

### New Capabilities

- `app-desktop-wails`: the Wails desktop app — reuse of the web SPA, the Go panel proxy and its protections,
  Configure, file saving, config file, platforms, CI, benchmark and the gates before any switch.

### Modified Capabilities

None. `app-desktop`, `app-desktop-rust`, `web-panel` and `release` keep their requirements; the web build's
"no desktop code" rule is part of `app-desktop-wails`.

## Impact

- New: `desktop-wails/` (Go, Taskfile, Dockerfile and script, Makefile), `frontend/src/desktop/`,
  `frontend/src/locales/pt-BR/desktop.js`, `.github/workflows/desktop-wails-ci.yml`, a `desktop-wails` section in
  `docs/desktop.md`.
- Changed:
  - `frontend/vite.config.ts` (desktop mode: output dir, dev port, HMR, web-only plugins off);
  - `frontend/src/i18n.js` (the desktop catalog kept out of the web glob);
  - `frontend/src/main.js` and `App.vue` (desktop bootstrap and the unauthenticated Configure route);
  - `frontend/src/api.js` (session generation), `PanelDialogs` and the dialogs helpers (closed on session change),
    and the pages that switch targets or poll (Sync, Shares, Logs, Users, Groups: target guards, follower stop);
  - `frontend/src/util.js` and the six transfer call sites (`saveFile`, `sendFile`);
  - the root `Makefile` (`desktop-wails*` targets, not part of `make build` until the switch).
- Unchanged: `desktop/` (Rust), its CI, `ci.yml`, `release.yml`, the server and its API, and `archive/desktop-go/`
  (its Go code is copied, not changed).
- Dependencies: `github.com/wailsapp/wails/v3` and `@wailsio/runtime` at the same exact version (desktop mode only),
  Go ≥ 1.26 (the repository uses 1.27.3). Users on Linux need GTK4 and WebKitGTK 6.0; on Windows, the WebView2
  runtime.
