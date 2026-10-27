# release Specification

## Purpose
TBD - created by archiving change unified-release. Update Purpose after archive.

## Requirements

### Requirement: One release for the server and the desktop app
A tag `vX.Y.Z` SHALL publish one release with the server's packages and the desktop app's (Linux archive, `.deb` and
`.rpm`, Windows, macOS universal) and one `checksums.txt`, after the CI of both passed; the desktop app SHALL carry
the tag's version X.Y.Z, given to its build.

#### Scenario: Release with both
- **WHEN** the tag `v0.27.0` is pushed
- **THEN** the release `v0.27.0` has `ad-manager_v0.27.0_linux_amd64.tar.gz`, `ad-manager_0.27.0_amd64.deb`,
  `ad-manager-desktop_0.27.0_{linux_x86_64.tar.gz,windows_x86_64.zip,macos_universal.zip}`, the desktop's `.deb` and
  `.rpm`, and `checksums.txt`

#### Scenario: Versions apart
- **WHEN** the tag `v0.27.0` is pushed
- **THEN** no file holds a desktop version that could differ from the tag: the desktop's build takes `v0.27.0`

#### Scenario: The desktop's version
- **WHEN** the release's desktop app runs `ad-manager-desktop --version`
- **THEN** it prints `v0.27.0`

### Requirement: One CI for both
Every pull request and push to `main` SHALL run the server's and the desktop app's CI, whatever the files changed.

#### Scenario: A server-only change
- **WHEN** a pull request changes only Go files
- **THEN** the desktop app's checks run too
