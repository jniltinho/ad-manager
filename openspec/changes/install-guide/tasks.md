## 1. Guide

- [ ] 1.1 `docs/install-guide.md`: artifacts, prerequisites, panel/agent/desktop from the `.deb` and from the binary, config keys, TLS, tokens, gates, first run, verification, upgrade, uninstall
- [ ] 1.2 `docs/deployment.md`: upgrade, removal and "without the package" moved to the guide (links left); links from `installation.md`, `agent.md`, `desktop.md`, `README.md`

## 2. Config reference

- [ ] 2.1 `config.toml.example`: `password_migration_*`, `dc_file_server`, and any other key the structs read
- [ ] 2.2 `internal/config`: `TestExampleCoversKeys`
- [ ] 2.3 `make check && make test`, `openspec validate --strict`, Codex review
