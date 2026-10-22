## 1. Extraction (source, read-only secrets)

- [x] 1.1 DCSync of the requested users over sealed DRSUAPI (`samba.drs_utils`/`drsuapi`), decrypt in memory to `{sam -> nt_hash, kerberos package}`
- [x] 1.2 Read-only: no `DRS_ADD_REF`, no `UpdateRefs`, no join/provision; a test proves source metadata unchanged
- [x] 1.3 Require *Replicating Directory Changes* **and** *…All*; reject `Primary:CLEARTEXT` sources; clear failure (no write) otherwise
- [x] 1.4 Source credential from `AD_SOURCE_PASSWORD_FILE` (0600) or stdin, never an argument; nothing persisted; no secret in error passthrough

## 2. Apply (target, dedicated write mode)

- [x] 2.1 Set `unicodePwd` by NT hash on the local `sam.ldb` with Samba's password-module controls, one transaction per user, `pwdLastSet` non-zero
- [x] 2.2 Copy the Kerberos keys when the salt transfers; `ntlm-only` otherwise, reported per user; allowlisted packages only
- [x] 2.3 Match and protect by SID/RID on both sides (RID 500/501/502, trusts, machine accounts); skip (never create) a missing user
- [x] 2.4 Dedicated `password_migration` gate (default off, target-admin authorized, explicit apply), **not** the GPO account; `/api/health` flag; pin destination by SID/GUID and reject source == destination
- [x] 2.5 Authenticated DRS connectivity check before apply; per-user rollback on partial failure; `same` only on full-state match

## 3. Import UI and metadata

- [x] 3.1 `Passwords` option in the "From another AD" import (beside Objects/GPOs/DNS/Shares): fold into `POST /api/import/ad/plan|apply` with `passwords: true`, reusing the form's source credential and the server→agent path; `internal/server/extras.go`, `frontend/src/pages/ImportExport.vue`
- [x] 3.2 Source credential from either the form (panel) or `AD_SOURCE_PASSWORD_FILE`/stdin/source profile (CLI); never an argument; not stored/logged/audited
- [x] 3.3 Import carries `lastLogonTimestamp` of users and computers and `operatingSystem` of computers; a never-logged-on object sets nothing and is not an error
- [x] 3.4 Saved-profile delete: a trash icon per profile with an `askConfirm` popup, calling `DELETE /api/ad-sources/{id}` (API exists); the import log/progress modal is a rectangle; `frontend/src/pages/ImportExport.vue`

## 5. Interfaces

- [x] 5.1 API `POST /api/import/ad/passwords` (plan/apply); `internal/server/openapi.yaml` (rule 8) and `swagger_test`
- [x] 5.2 CLI `migrate passwords` (plan, `--apply`); MCP `ad_password_migrate` (write mode only, `--allow-write`)
- [x] 5.3 Audit `password.migrate` with counts only; `internal/i18n` entries (`UPDATE_I18N=1 go test ./internal/i18n`)

## 6. Safety and docs

- [x] 6.1 Tests: no secret in API/logs/audit/files; idempotent on full state; missing user skipped; rights required; dedicated-gate; source unchanged
- [x] 6.2 `docs/migration-runbook.md` (new-domain cutover: objects → passwords → apps) and `docs/cli.md`
- [ ] 6.3 Validate against the Samba lab (`vagrant/labs`): a user signs in with the source password after the migration

## 7. Validation

- [ ] 7.1 `make check && make test`; `make samba-up && make test-integration`
- [x] 7.2 Implement with the Opus 5.5 model; review with Codex (`codex exec --sandbox read-only`): no plaintext path, no LDAP path, no secret leak, source unchanged
