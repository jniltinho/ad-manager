## 1. Installer and agent

- [x] 1.1 `samba-setup.py`: `--import-ready` (new-dc `--panel --files` only), `import_gates` (one 0600 file, four `[agent]` keys, idempotent), password handed from `dc_new`; DC roles start the panel last (`panel(start=False)` + `panel_start`)
- [x] 1.2 `internal/agent/setup.go`: `SetupPlan.ImportAD` (`import_ad`) → `--import-ready`, refused for other roles or without `files`; Go tests for the validation and for the script (wrong role refused; `import_gates` creates 0600, appends once, refuses when `[agent]` is not last)
- [x] 1.3 `internal/server/setup.go`: the `setup.run` audit target says `import_ad` when set
- [x] 1.4 `packaging/postrm.sh`: purge removes `import.pass`

## 2. Wizard and import page

- [x] 2.1 `Setup.vue`: checkbox under "This DC also serves files" (new-dc only), review sentence, next steps, "Import from an existing AD" link to `/import-export?mode=ad` for every new domain (primary when prepared)
- [x] 2.2 `ImportExport.vue`: `?mode=ad|folders|csv` opens that tab
- [x] 2.3 pt-BR strings in `frontend/src/locales/pt-BR/setup.js`
- [x] 2.4 `setup-shots.mjs` ticks the option; retake `setup-5-details`, `setup-6-review`, `setup-8-done` (2×, classic-blue)

## 3. API, docs and checks

- [x] 3.1 `openapi.yaml`: `/api/setup/*`; `TestOpenAPICoversRoutes` covers the setup-mode router
- [x] 3.2 `docs/installation.md` (the import step), `docs/password-migration.md` and `docs/gpo.md` (gates set by the setup)
- [ ] 3.3 `make check && make test`, `openspec validate --strict`, Codex review
