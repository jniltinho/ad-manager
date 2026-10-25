## Context

- The setup-mode panel serves only the wizard (token session, no AD configuration). The "From another AD" import
  writes as the signed-in Domain Admin over LDAPS (`POST /api/import/ad/plan|apply`); objects and DNS need nothing
  else, while GPOs and Passwords go through a DC agent of this domain that reports `gpo_restore: true` /
  `password_migration: true` in `/api/health` (`dcAgentCap`).
- Each gate needs an account and a regular 0600 file (`gpoRestoreReady`, `passwordMigrationReady`). The GPO import
  signs `samba-tool` in with that file (`PASSWD_FILE`); the password migration only checks it (its helper uses the
  local system session).
- `samba-setup.py new-dc --panel --files` runs `dc_new`, `panel` (install, config, **restart**) and then `dc_files`
  (the `[agent]` section, token, registration). The wizard treats "the panel is out of setup mode" as success
  (`Setup.vue` `poll`), so today a `dc_files` failure after the restart is never shown. The administrator password is
  read once from stdin in `dc_new` (`read_password`).

## Goals / Non-Goals

**Goals:** a new domain can import from an existing AD as the setup's last step, through the existing import page and
API, with the write modes ready when asked for.

**Non-Goals:** a setup-mode import API; importing for other roles; keeping the source credential.

## Decisions

1. **Hand-off, not a copy.** The import step is a link to the existing page (`/import-export?mode=ad`) once the panel
   is out of setup mode; `App.vue` shows the sign-in and keeps the route. It is offered for every new domain
   (objects and DNS need no gate); it is the primary button when the gates were prepared.
2. **One secret file.** `--import-ready` writes the password once to `/opt/ad-manager/import.pass` (opened with
   `O_CREAT|O_TRUNC` and mode 0600, `fchmod` 0600, so it is never readable by others), and appends to `[agent]`:
   `gpo_restore_user = "<admin>"`, `password_migration_user = "<admin>@<realm>"` and both `*_password_file` keys
   pointing at it. Deleting the file turns both modes off on this DC (readiness is checked per call) and removes the
   password from disk; `apt purge` deletes it too.
3. **Password hand-off.** `dc_new` keeps the value it read on `args` only when `--import-ready` is set; `import_gates`
   uses it, never rereads stdin, and logs nothing of it (the setup log masks the password anyway).
4. **Panel last.** For DC roles with `--panel`, `panel(args, start=False)` installs and writes the config, then
   `dc_files` and `import_gates` run, then `panel_start` enables/restarts the panel and checks its port and LDAPS. The
   standalone `panel` role is unchanged. The agent still restarts 20 s after the job (`internal/agent/setup.go`), so
   the next steps say the GPO and Passwords options are ready a few seconds after the panel opens.
5. **Idempotent.** A re-run rewrites the file with the current password and appends the keys only when
   `gpo_restore_user` is absent; it refuses when `[agent]` is not the last table (the keys would land elsewhere).
6. **Validated twice.** `SetupPlan.Args` refuses `import_ad` unless role `new-dc` with `files`
   (`setup: invalid import_ad`); the script refuses `--import-ready` without `new-dc --panel --files`. The wizard
   shows the checkbox only then and sends `import_ad` only then.
7. **Setup API documented.** `openapi.yaml` gains `/api/setup/login|diagnose|status|plan|run`;
   `TestOpenAPICoversRoutes` also walks the router of a server in setup mode (the catch-all `/api/*` excluded).

## Risks / Trade-offs

- [The administrator password on disk] → only with the explicit checkbox, one 0600 root file, said in the review and
  the next steps with the command that removes it; the same thing the manual GPO gate asks for.
- [Another DC agent with the modes on] → deleting this DC's file turns off only this DC; the docs say so.
- [Admin password changed later] → the GPO gate's sign-in fails; the next steps say to delete the file after the
  import.
