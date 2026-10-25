## Why

The server and the desktop app were released apart (tags `vX.Y.Z` and `desktop-rust-vX.Y.Z`) with different version
numbers and their own CI triggers, so a user had to find two releases and two checksum files, and a pull request could
pass the server's CI while the desktop's was skipped by its path filter.

## What Changes

- `ci.yml` calls `desktop-rust-ci.yml` (now reusable only): every pull request and push to `main` runs both.
- `release.yml` on `vX.Y.Z` also builds the desktop app (Linux and Windows in the pinned toolchain images, macOS
  universal) and publishes everything in one release with one `checksums.txt`; `desktop-rust-release.yml` is removed.
- The desktop app carries the server's version: `desktop-rust/Cargo.toml` is set to X.Y.Z in the release commit and
  the release refuses a tag that differs. It moves to 0.23.1 now.
- The release skill, docs and the desktop CHANGELOG say so; the desktop's changes go to the root CHANGELOG.

## Impact

- `.github/workflows/{ci,release,desktop-rust-ci}.yml`, `desktop-rust-release.yml` removed, `desktop-rust/Cargo.toml`
  and `Cargo.lock`, the `create-release` skill, docs. No code change.
