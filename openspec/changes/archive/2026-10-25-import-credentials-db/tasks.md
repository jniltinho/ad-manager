## 1. Server and agent

- [x] 1.1 `store`: `source_secrets` table; `Source.HasPassword`; sealed import credential in `settings`
- [x] 1.2 `server`: GET/PUT/DELETE `/api/settings/import-credential` (Domain Admin check, sealed, audited)
- [x] 1.3 `server`: profile saved with its sealed password; `profile` + empty password uses it
- [x] 1.4 `server`: the credential sent with `/api/dc/gpos/restore` and `/api/dc/gpos/import`
- [x] 1.5 `config`/`agent`: `import_modes`; credential from the request (0600 temp file); former keys still work
- [x] 1.6 `cmd`: `ad-manager import-credential` (stdin, `--clear`)
- [x] 1.7 `samba-setup.py --import-ready`: `import_modes = true` + the credential through the CLI, no `import.pass`

## 2. Interfaces

- [x] 2.1 Web: Settings card; Import/export profile with kept password; Setup texts; pt-BR
- [x] 2.2 Desktop (Rust): the same in Settings and Import/export; pt-BR
- [x] 2.3 `openapi.yaml`, server i18n catalog, docs

## 3. Validation

- [x] 3.1 Tests (store, server, agent); `make check && make test`
- [x] 3.2 On the lab VM: credential in Settings, `import.pass` removed, import with GPOs and passwords, profile reuse
- [x] 3.3 Codex review
