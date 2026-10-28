## MODIFIED Requirements

### Requirement: The desktop app is the Wails one
The desktop app the project builds, tests and releases SHALL be `desktop/` (Go, Wails v3, the web SPA in desktop
mode); the Rust app in `archive/desktop-rust/` and the Fyne app in `archive/desktop-go/` SHALL be kept as code only
and SHALL NOT be built by `make`, tested by CI or published by a release.

#### Scenario: Build
- **WHEN** a developer runs `make build`
- **THEN** `dist/` gets the server and, in `dist/desktop/`, the Wails app's Linux AppImage, Windows zip and Linux
  binary, built in Docker, and nothing from `archive/`

#### Scenario: A configuration of the Rust app
- **WHEN** the new app starts on a computer whose `desktop.toml` was written by the Rust app
- **THEN** it uses the same panel, user, fingerprint, language and appearance, and keeps `[graphics] gpu_backend`

#### Scenario: Docs screenshots
- **WHEN** `make desktop-shots` runs
- **THEN** `docs/screenshots/desktop-*.png` are taken from the real app at 2×, in Classic blue, with Example Company
  Ltd and `domain.local`, against a demo panel, with no domain needed
