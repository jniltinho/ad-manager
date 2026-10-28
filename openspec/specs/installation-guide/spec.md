# installation-guide Specification

## Purpose
One installation guide for the web panel, the agent and the desktop app (prerequisites to uninstall), linked from the other pages, and a complete configuration example.

## Requirements

### Requirement: Installation guide for every component
The documentation SHALL have one installation guide covering prerequisites, installation, configuration, first run,
verification, upgrade and uninstall of the web panel and the agent (from the `.deb` and from the release `.tar.gz`)
and of the desktop app (from its AppImage, `.zip` or macOS `.app`), and the other pages SHALL link to it instead of repeating those
steps.

#### Scenario: Binary install of the agent
- **WHEN** an operator installs the agent on a file server from `ad-manager_<v>_linux_amd64.tar.gz`
- **THEN** the guide gives the commands for the layout, the unit (fetched from the same release tag, since the
  tarball has only the binary, `config.toml` and the README), the token and the registration in the panel, and how
  to check `ad-manager agent check` and `/api/health`

#### Scenario: Upgrade
- **WHEN** an operator upgrades the panel or the agent installed from the `.deb` or from the tarball, or the desktop app
- **THEN** the guide shows `apt install ./ad-manager_<new>_amd64.deb` with the services restarted, replacing
  `/opt/ad-manager/ad-manager` and restarting for the tarball (keeping `config.toml` either way), and replacing the
  desktop AppImage, executable or `.app` while keeping its `desktop.toml`

#### Scenario: Index
- **WHEN** a reader opens `docs/README.md`
- **THEN** "Where to start" links to the guide

### Requirement: Complete configuration example
`config.toml.example` SHALL mention every key the binary reads from `config.toml`, and a unit test SHALL fail when a
key is added to the configuration without it.

#### Scenario: New key
- **WHEN** a field with a `toml` tag is added to `config.Config` (or a nested section) and not to the example
- **THEN** `go test ./internal/config` fails naming the key

#### Scenario: Password-migration gate documented
- **WHEN** an operator reads `config.toml.example`
- **THEN** it shows `password_migration_user`, `password_migration_password_file` and `dc_file_server` with what they
  do
