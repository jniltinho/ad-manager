## Why

The AppImage replaced every Linux package of the desktop app, but a bare binary is still wanted where GTK4 and
WebKitGTK 6.0 are already installed (a smaller download, no AppImage runtime).

## What Changes

- The release's desktop app for Linux is the AppImage and `ad-manager-desktop_X.Y.Z_linux_x86_64.tar.gz` (the
  binary and its README, as up to v0.27.1); no `.deb` or `.rpm`. v0.28.0 got the `.tar.gz` added after publishing.

## Capabilities

### Modified Capabilities

- `release`: the desktop's Linux assets.
- `app-desktop`: what `make build` puts in `dist/desktop/`.
- `installation-guide`: installing the desktop app from the `.tar.gz`.

## Impact

`desktop/Makefile` (`dist`), `release.yml`, the docs and the release skill.
