## Context

- Release artifacts (`.github/workflows/release.yml`): `ad-manager_<v>_linux_amd64.tar.gz`, `ad-manager_<v>_amd64.deb`
  (nfpm, `packaging/nfpm.yaml`), `ad-manager_<v>_windows_amd64.zip`, `ad-manager-desktop_<v>_linux_amd64.tar.gz`,
  `…_windows_amd64.zip`, `…_darwin_{arm64,amd64,universal}.zip`, `checksums.txt`.
- One binary (`ad-manager`) is the panel, the CLI, MCP and the agent; the `.deb` installs it in `/opt/ad-manager` with
  two units and a `setup = true` config (the wizard); `deploy/install.sh` installs the panel from a bare binary.
- The desktop app is a separate binary with its own config (`desktop.toml`).

## Decisions

1. **One guide, one place per fact.** The guide owns install/upgrade/verify/uninstall for the three components;
   `deployment.md` keeps the manual `[ad]`/TLS/Settings detail and links to the guide instead of repeating the
   upgrade and binary sections; the agent and desktop pages keep their feature detail and link back.
2. **Config reference by test.** `TestExampleCoversKeys` walks the `toml` tags of `config.Config` and fails when one is
   missing from `config.toml.example` (commented or not), so the guide can point to the example as the full key
   list.
3. **Binary installs mirror the package layout** (`/opt/ad-manager`, the same units from `deploy/`), so upgrade and
   support instructions are the same for both.
