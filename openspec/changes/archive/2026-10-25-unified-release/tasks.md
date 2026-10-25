## 1. CI and release

- [x] 1.1 `desktop-rust-ci.yml` reusable only; `ci.yml` calls it
- [x] 1.2 `release.yml`: version check, desktop Linux/Windows and macOS jobs, one publish with one `checksums.txt`
- [x] 1.3 `desktop-rust-release.yml` removed; the desktop's version set to the server's (0.23.1)

## 2. Docs

- [x] 2.1 `create-release` skill (the desktop's version step, the assets), development, desktop, install guide
- [x] 2.2 desktop README and CHANGELOG; root CHANGELOG entry
