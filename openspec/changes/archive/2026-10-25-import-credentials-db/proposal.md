## Why

Two secrets of the "From another AD" import live where they should not:

- The import write modes of a DC's agent (GPO import/restore and password migration) need the target domain's
  administrator password in a **file on the DC** (`/opt/ad-manager/import.pass`, 0600 root), written by
  `samba-setup.py --import-ready` and referenced from `config.toml`. A clear-text password on disk, forgotten after the
  import, is exactly the kind of secret the sync feature already avoids: its passwords are kept **encrypted in the
  panel's database**.
- A saved source profile keeps URL, domain and user but **not the password**, so every run asks for it again.

## What Changes

- **Import credential in the panel's database.** *Settings → Import from an existing AD*: the target domain's
  administrator (user and password), checked as a Domain Admin when saved, kept sealed with the panel's secret box
  (AES-256-GCM, the key the sync passwords use), never returned by the API; removable. The panel sends it to the DC
  agent with each GPO restore/import task only (TLS, agent token); the agent hands it to `samba-tool` through a
  0600 temporary file removed at the end.
- **No password on the DC.** The agent's write modes are turned on by `import_modes = true` in `[agent]` (an explicit
  switch, no secret). Password migration never used the password (the file was only the gate): it depends on the
  switch alone, also for `ad-manager migrate passwords`. The old `gpo_restore_*`/`password_migration_*` keys keep
  working (deprecated).
- **`samba-setup.py --import-ready`** writes `import_modes = true` and stores the credential in the panel's database
  (`ad-manager import-credential`, run as the service user, the password on stdin), instead of `import.pass`.
- **Source profiles keep their password, encrypted.** Saving a profile seals the source password (bound to the
  profile); the profile list says only `has_password`; running with a profile and an empty password uses the kept
  one. Web panel and desktop app alike.
- Swagger, the pt-BR catalogs, the docs (password migration, GPO, installation) and the setup wizard's texts follow.

## Capabilities

### New Capabilities
- `import-credentials`: where the import's secrets are kept (panel database, sealed) and how they reach the DC agent.

### Modified Capabilities
- `directory-import`: a saved profile keeps the source password, encrypted.
- `agent-samba`: the GPO write mode is switched on without a password file; the credential comes from the panel.

## Impact

- `internal/store` (`source_secrets` table, sealed import credential in `settings`), `internal/server` (settings
  routes, profile password, credential sent to the agent), `internal/agent` and `internal/config` (`import_modes`,
  credential per task), `cmd` (`import-credential`), `scripts/samba-setup.py`, `frontend` (Import/export, Settings,
  Setup), `desktop-rust` (Import/export, Settings), `internal/server/openapi.yaml`, docs.
- Supersedes the `import.pass` file of the open change `setup-import-from-ad`; agents with the old keys keep working.
