## 1. AppImage

- [x] 1.1 `make -C desktop appimage`: Wails' generator in the toolchain image (`file` added), the versioned AppImage in DIST.
- [x] 1.2 `dist` builds the AppImage and the Windows zip; no `.tar.gz`, `.deb` or `.rpm`; nfpm removed.
- [x] 1.3 An AppImage (`$APPIMAGE`) uses the per-user `desktop.toml`; test.
- [x] 1.4 Tested on Ubuntu 24.04, Ubuntu 26.04, Debian 13 and Fedora 42 without the system's GTK4 or WebKitGTK.

## 2. Release and docs

- [x] 2.1 `release.yml` uploads the AppImage; the release notes and `desktop-ci.yml` name it.
- [x] 2.2 `docs/desktop.md`, `docs/install-guide.md`, `desktop/README.md` and the release skill.
