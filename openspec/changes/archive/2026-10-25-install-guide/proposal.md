## Why

Installing AD Manager is spread over several pages: the setup wizard (`docs/installation.md`), the manual panel setup
(`docs/deployment.md`, which also holds the upgrade, removal and "without the package" notes), the agent
(`docs/agent.md`) and the desktop app (`docs/desktop.md`, mostly about building it). Nothing explains, side by side,
how each of the three components is installed from the `.deb` and from the release binary, how it is upgraded,
verified and removed. `config.toml.example` also misses keys the binary reads (`password_migration_*`,
`dc_file_server`), so an operator configuring by hand cannot find them.

## What Changes

- A new **installation guide** (`docs/install-guide.md`): the release artifacts; prerequisites; the **web panel** and
  the **agent** (file server and DC) from the `.deb` or the `.tar.gz` (units fetched from the same tag), and the
  **desktop app** from its `.tar.gz`/`.zip`; the `config.toml` keys each one reads, TLS, tokens (setup token, agent token), the write-mode
  gates, the first run (setup wizard or manual), verification, upgrade (apt with the `.deb` vs replacing the binary)
  and uninstall.
- `docs/deployment.md` keeps the manual panel configuration and links to the guide for upgrade, removal and
  installing without the package (moved there, not duplicated); `docs/installation.md`, `docs/agent.md`,
  `docs/desktop.md` and `docs/README.md` link to it.
- `config.toml.example` documents every key the binary reads; a unit test keeps it that way.
- Out of scope: new packaging (a desktop `.deb`, an apt repository), changing the units or the postinst.

## Capabilities

### New Capabilities
- `installation-guide`: one installation guide for the three components and a complete `config.toml.example`.

### Modified Capabilities

## Impact

- Docs only, plus `config.toml.example` and a test in `internal/config`. No behaviour change.
