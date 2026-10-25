## Why

A fresh install that replaces an existing domain (a new-domain cutover) needs the "From another AD" import right after
the domain is created. Today the setup wizard ends with "Open the panel" and the operator has to find the import on
their own, and its GPO and Passwords options cannot run because the DC agent's two write modes (`gpo_restore_*`,
`password_migration_*`) are off and are only documented as manual `config.toml` edits plus a root-only password file.

## What Changes

- The final screen of a **New domain** setup offers **"Import from an existing AD"**: it opens the panel's existing
  "From another AD" import (`/import-export?mode=ad`) after the Domain Admin sign-in — same options (objects, DNS,
  GPOs, shares, passwords), same source-credential handling, no second implementation.
- An optional, off-by-default **"Prepare the import of GPOs and passwords"** checkbox (New domain with "This DC also
  serves files", because both go through this DC's agent) turns on the DC agent's GPO-import and password-migration
  write modes for the new domain's administrator: `samba-setup.py new-dc --panel --files --import-ready` writes the
  password the wizard already asked for to **one** root-only file (`/opt/ad-manager/import.pass`, 0600 from creation)
  and points both gates' keys at it. The review says so; the next steps tell how to turn both off (delete the file).
- `samba-setup.py` (DC roles with the panel) now **starts the panel last**, after the DC agent is registered and the
  gates are written, so a wizard that sees the panel leave setup mode knows those steps finished (today the panel
  restarts before the DC agent part runs, and a later failure is not shown).
- The Import/export page honours `?mode=ad|folders|csv` to open on that tab.
- The setup-mode API (`/api/setup/*`) is documented in `openapi.yaml` (it gains `import_ad`), and the route-coverage
  test also checks the setup-mode router.
- `apt purge` removes `/opt/ad-manager/import.pass`.
- Out of scope: running the import inside the setup-mode server (no operator session or LDAPS target yet), the other
  roles (`dc-join` replicates the domain, `restore-dc` restores it, `member` and `connect` do not own the target DC),
  pre-filling the source form.

## Capabilities

### New Capabilities

### Modified Capabilities
- `initial-setup`: a New domain setup ends by offering the "From another AD" import, optionally with the GPO and
  password write modes prepared.
- `samba-installer`: `--import-ready` turns on the DC agent's two import write modes; DC roles start the panel last.

## Impact

- `scripts/samba-setup.py`, `internal/agent/setup.go` (`SetupPlan.ImportAD`), `internal/server/setup.go` (audit
  target), `internal/server/openapi.yaml` and `swagger_test.go`, `packaging/postrm.sh`,
  `frontend/src/pages/Setup.vue`, `frontend/src/pages/ImportExport.vue`, `frontend/src/locales/pt-BR/setup.js`,
  `frontend/tests/visual/setup-shots.mjs` and the setup screenshots, docs (`installation.md`, `password-migration.md`,
  `gpo.md`).
- No new server message (the existing `setup: invalid <field>` covers the validation).
- Security: the domain administrator's password lands in one 0600 root file, which is what the GPO gate already
  requires when set up by hand (the password-migration gate only checks that the file exists with that mode); the
  wizard says so before the run, and deleting the file turns both modes off on this DC.
