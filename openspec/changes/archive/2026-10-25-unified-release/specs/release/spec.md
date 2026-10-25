## ADDED Requirements

### Requirement: One release for the server and the desktop app
A tag `vX.Y.Z` SHALL publish one release with the server's packages and the desktop app's (Linux, Windows, macOS
universal) and one `checksums.txt`, after the CI of both passed; the desktop app SHALL carry the same version X.Y.Z.

#### Scenario: Release with both
- **WHEN** the tag `v0.24.0` is pushed and `desktop-rust/Cargo.toml` has version `0.24.0`
- **THEN** the release `v0.24.0` has `ad-manager_v0.24.0_linux_amd64.tar.gz`, `ad-manager_0.24.0_amd64.deb`,
  `ad-manager-desktop-rs_0.24.0_{linux_x86_64.tar.gz,windows_x86_64.zip,macos_universal.zip}` and `checksums.txt`

#### Scenario: Versions apart
- **WHEN** the tag `v0.24.0` is pushed and `desktop-rust/Cargo.toml` still has `0.23.1`
- **THEN** the release workflow fails before building anything and nothing is published

### Requirement: One CI for both
Every pull request and push to `main` SHALL run the server's and the desktop app's CI, whatever the files changed.

#### Scenario: A server-only change
- **WHEN** a pull request changes only Go files
- **THEN** the desktop app's checks run too
