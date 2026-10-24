## ADDED Requirements

### Requirement: The desktop app is the Rust one
The desktop app the project builds, tests and releases SHALL be `desktop-rust/`; the Fyne app in `desktop/` SHALL be
kept as code only and SHALL NOT be built by `make`, tested by CI or published by a release.

#### Scenario: Build
- **WHEN** a developer runs `make build`
- **THEN** `dist/` gets the server and, in `dist/desktop-rust/`, the Rust app's archives and Linux binary, built in
  Docker, and nothing from `desktop/`

#### Scenario: Release
- **WHEN** a `vX.Y.Z` tag is pushed
- **THEN** the release has the server tarball, the `.deb` and `checksums.txt`, and no Fyne desktop archive

#### Scenario: Themes in step
- **WHEN** a theme's CSS changes and `themes.json` was not regenerated
- **THEN** `make desktop-check` and the desktop CI fail until `make desktop-themes` is run
