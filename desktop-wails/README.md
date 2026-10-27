# AD Manager desktop on Wails v3 (preview)

A **preview** of the desktop app on [Wails v3](https://wails.io): the web panel's own SPA (`../frontend`, Vue 3 +
Vite+ + Tailwind 4, built with `vp build --mode desktop`) in the system's webview, and a Go backend that is its only
way to the panel. The shipped desktop app is still the Rust one in [`../desktop`](../desktop/README.md); this one
replaces it only after the gates of the OpenSpec change
[`openspec/changes/desktop-wails`](../openspec/changes/desktop-wails/design.md).

## Run

Linux needs GTK4 and WebKitGTK 6.0 (Ubuntu 24.04+, Debian 13+, Fedora 40+, RHEL 10+):

```bash
sudo apt install libgtk-4-1 libwebkitgtk-6.0-4    # Debian, Ubuntu
sudo dnf install gtk4 webkitgtk6.0                # Fedora, RHEL
```

Windows needs the WebView2 runtime (part of Windows 10/11 and Server 2025; on older Windows Server install it from
Microsoft). macOS builds only on a Mac.

Until the Configure screen comes (milestone 2), the panel is given on the command line or in the environment:

```bash
ADM_PANEL_URL=https://panel.example.local:8443 ADM_PANEL_FINGERPRINT=sha256:<hex> ./ad-manager-desktop-wails
```

`ADM_PANEL_FINGERPRINT` is needed only for a self-signed panel certificate (the panel logs it at start:
`journalctl -u ad-manager | grep sha256`). Then sign in on the panel's own sign-in screen.

## How it works

- `main.go`: the window (1280×800, minimum 800×600), the asset server with the embedded SPA, the CSP, and the
  `Shell` service the SPA calls through `@wailsio/runtime` (`frontend/src/desktop/`).
- `internal/panel`: the asset-server middleware that forwards the SPA's same-origin `/api/…` requests to the panel.
  It speaks https only, accepts the system's roots or exactly the pinned certificate, never follows a redirect,
  keeps the panel's session cookie in Go (the webview never sees it) and refuses paths that could leave `/api/`.

## Build and test

Everything but the SPA runs in Docker (`docker/Dockerfile`: Go, GTK4, WebKitGTK 6.0, Xvfb; built on first use):

```bash
make dist      # Linux tar.gz, Windows zip, SHA256SUMS and the Linux binary in dist/
make check     # gofmt, go vet, go test
```

From the repository root: `make desktop-wails` (into `dist/desktop-wails/`) and `make desktop-wails-check`.
