## Why

The desktop app for Linux came as a `.tar.gz`, a `.deb` and a `.rpm` that need GTK4 and WebKitGTK 6.0 from the
system: three files to choose from, and none for a distribution without those packages. One AppImage, with GTK4 and
WebKitGTK 6.0 inside, runs on any x86_64 Linux desktop.

## What Changes

- The release's desktop app for Linux is `ad-manager-desktop_X.Y.Z_linux_x86_64.AppImage` (Wails' AppImage generator,
  in the toolchain image); the Linux `.tar.gz`, `.deb` and `.rpm` are no longer built (nfpm leaves the image).
- The AppImage keeps `desktop.toml` in the user config folder (`~/.config/ad-manager/`): its own files are a read-only
  mount, or a temporary copy gone at the next start.
- Docs: the Linux requirements (glibc 2.39+, `bubblewrap` and `xdg-dbus-proxy` for WebKit's sandbox), install and
  upgrade.

## Capabilities

### Modified Capabilities

- `release`: the desktop's Linux asset.
- `app-desktop`: what `make build` puts in `dist/desktop/`.
- `app-desktop-wails`: where an AppImage keeps `desktop.toml`.
- `installation-guide`: installing and upgrading the desktop app from the AppImage.

## Impact

`desktop/Makefile` (`appimage` replaces `packages`), `desktop/docker/Dockerfile` (`file` in, nfpm out),
`desktop/packaging/linux/nfpm.yaml` removed, `desktop/internal/config`, `release.yml`, `desktop-ci.yml`, the docs and
the release skill. A Linux user of the `.deb` or `.rpm` removes it and runs the AppImage; `desktop.toml` stays.
