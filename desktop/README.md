# AD Manager desktop

The desktop client of the [ad-manager](../README.md) panel, in Go with [Wails v3](https://wails.io): the web panel's
own SPA (`../frontend`, Vue 3 + Vite+ + Tailwind 4, built with `vp build --mode desktop`) in the system's webview,
and a Go backend that is its only way to the panel. Every page, dialog, theme and translation is the web's. The user
guide is [docs/desktop.md](../docs/desktop.md); the OpenSpec changes are
[desktop-wails](../openspec/changes/desktop-wails/design.md) and
[desktop-switch](../openspec/changes/desktop-switch/proposal.md). It replaced the Rust app, archived in
[`archive/desktop-rust/`](../archive/desktop-rust/README.md).

| Sign-in | Users |
|---|---|
| ![sign-in](../docs/screenshots/desktop-03-signin.png) | ![users](../docs/screenshots/desktop-05-users.png) |

## Run

Linux needs GTK4 and WebKitGTK 6.0 (Ubuntu 24.04+, Debian 13+, Fedora 40+, RHEL 10+):

```bash
sudo apt install libgtk-4-1 libwebkitgtk-6.0-4    # Debian, Ubuntu
sudo dnf install gtk4 webkitgtk6.0                # Fedora, RHEL
```

Windows needs the WebView2 runtime (part of Windows 10/11 and Server 2025; on older Windows Server install it from
Microsoft). macOS builds only on a Mac.

On first run the app opens **Configure**: panel URL, user and password. **Test connection** reads the panel's
certificate, its health and signs in for real. A self-signed certificate is shown (fingerprint, subject, issuer,
validity) and trusted only when the operator clicks **Trust this certificate**; compare the fingerprint with the one
the panel logs at start (`journalctl -u ad-manager | grep sha256`). **Save** writes `desktop.toml` (mode 0600, the
same file and lookup as the archived Rust app: `--config`, `AD_MANAGER_DESKTOP`, `.config/desktop.toml` beside the executable,
the user's config folder) and opens the app signed in. The password is never saved. Language, theme, look and safe
rendering are this computer's preferences, saved without a test.

```bash
./ad-manager-desktop                     # Configure on first run, then the panel's sign-in
./ad-manager-desktop --config ~/lab.toml # another desktop.toml
./ad-manager-desktop --safe-gpu          # GPU acceleration off for this start (Linux, Windows)
```

## How it works

- `main.go`, `desktop.go`, `routes.go`: the window (1280×800, minimum 800×600, restored where it was left, one
  instance), the asset server (the embedded SPA with its CSP and router fallback, Wails' runtime without its
  Browser object, Configure's test, the panel's API) and the `Shell`, `Configure` and `Files` services the SPA calls
  through `@wailsio/runtime` (`frontend/src/desktop/`).
- `internal/config`: `desktop.toml`, the same file as the archived apps' (unknown keys and sections are kept).
- `guard_linux.go`: the webview never leaves the app (WebKitGTK `decide-policy`). Windows and macOS have no native
  guard yet: there the CSP and the SPA's link guard keep it on the app (`desktop-wails` task 4.5).
- `icon*.go`, `window.go`, `graphics.go`: the app's icon on the window, the window's place, `--safe-gpu`.
- `internal/panel`: the asset-server middleware that forwards the SPA's same-origin `/api/…` requests to the panel.
  It speaks https only, accepts the system's roots or exactly the pinned certificate, never follows a redirect,
  keeps the panel's session cookie in Go (the webview never sees it), refuses paths that could leave `/api/` and
  requests of an ended session, and caps what crosses the webview's bridge at 32 MiB; files (backups, exports) move
  in Go between the panel and the disk (`Files`).

## Build and test

Everything but the SPA runs in Docker (`docker/Dockerfile`: Go, GTK4, WebKitGTK 6.0, Xvfb; built on first use):

```bash
make dist      # Linux AppImage, Windows zip, SHA256SUMS and the Linux binary in dist/
make check     # gofmt, go vet, go test, and the web build without the desktop's code
make shots     # the docs screenshots, on the host (Xvfb, xdotool): 2x, Classic blue, the demo panel
```

From the repository root: `make desktop` (into `dist/desktop/`, also part of `make build`), `make desktop-check`,
`make desktop-shots`. macOS builds on a Mac: `cd frontend && npm run build:desktop`, then in `desktop/` a
`go build -tags production` per architecture, `lipo`, and `packaging/macos/bundle.sh` (as `release.yml`).

`tools/demopanel` is a demo panel (HTTPS, self-signed, Example Company Ltd, `domain.local`, password `Secret#1`) for
the screenshots; `bench/run.sh` is the benchmark against the archived Rust app
([docs/benchmark.md](docs/benchmark.md)).

`scripts/smoke-linux.sh` drives the real window under a private Xvfb against a **test** panel (the all-in-one image,
`domain.local`); with a smoke build (`go build -tags production,smoke`) it also checks the bridge, a file save, the
navigation guard and a binding, and fails without `SMOKE PASS`:

```bash
ADM_SMOKE_DIR=$PWD/.cache/smoke-files ADM_PANEL_URL=https://127.0.0.1:18443 \
  ADM_LIVE_PASSWORD_FILE=$HOME/.adm-live/password scripts/smoke-linux.sh bin/smoke/ad-manager-desktop
```
