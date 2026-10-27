## 1. Decision

- [x] 1.1 The operator's decision and the open gates written in `openspec/changes/desktop-wails/design.md`.

## 2. Move

- [ ] 2.1 `git mv desktop archive/desktop-rust`, `git mv desktop-wails desktop`.
- [ ] 2.2 Names:
      - binary, module, program name, icon, `.desktop`, nfpm package and macOS bundle `ad-manager-desktop` / `AD
        Manager.app`;
      - the state folder `desktop-state/`;
      - paths in the Docker script, the Vite config, tests and comments.
- [ ] 2.3 Root `Makefile`: `desktop`, `desktop-check`, `desktop-shots` for the new app; no `desktop-themes`, no
      `desktop-wails*`.

## 3. CI and release

- [ ] 3.1 `desktop-ci.yml`: the new app's checks, builds and native smokes, called by `ci.yml`, with uploads only on
      failure. Remove `desktop-wails-ci.yml`.
- [ ] 3.2 `release.yml`: the new app's Linux/Windows (with `.deb`/`.rpm`) and macOS universal jobs, with the tag's
      version; no `Cargo.toml` check.

## 4. Docs and screenshots

- [ ] 4.1 `desktop/tools/demopanel` (a demo panel in Go) and `make desktop-shots` (2×, Classic blue, Example Company
      Ltd, `domain.local`).
- [ ] 4.2 New `docs/screenshots/desktop-*.png`; remove `desktop-rs-*.png`; screenshots README.
- [ ] 4.3 `docs/desktop.md`, `desktop/README.md`, install guide, overview, README, `docs/README.md`, themes,
      development, AGENTS.md, the release skill, CHANGELOG.

## 5. Validate

- [ ] 5.1 `make check`, `make test`, `make desktop-check`, `make build`; the native smokes.
- [ ] 5.2 Codex review; `openspec validate desktop-switch --strict`.
