## 1. Read

- [x] 1.1 `drs.Reader.Secrets`: NT hash and decrypted `supplementalCredentials`; tests for the payload checks.
- [x] 1.2 `gosamba/suppcreds`: parser with Samba-generated fixtures and a fuzz test.

## 2. Write and classify

- [x] 2.1 `ldbsearch` reader of the local users and state; `ldbmodify` writer with the LDIF on stdin (argv test).
- [x] 2.2 `sambadc.MigratePasswords` in Go: pin, classify, apply, fallback, outcomes and counts as today.

## 3. Parity and removal

- [x] 3.1a Parity script `scripts/test-pwmigrate.sh` (Python × Go): Samba 4.23 (Ubuntu 26.04) and 4.22 (Debian 13) → Samba:
      same outcomes, same stored material, NTLM and Kerberos AES with the source passwords, `same` on repeat, refusal
      without rights.
- [x] 3.1b Windows Server 2008 R2 source (dockur) → Samba: the same outcomes as the Python helper (ntlm_only — Samba
      refuses the Windows Kerberos blob, "unexpected Kerberos key count" — no_password, missing), the same stored
      material, NTLM with the source passwords, `same` on repeat, refusal without rights.
- [x] 3.1c Windows Server 2022 and 2019 sources (dockur) → Samba: the same outcomes and stored material as the Python
      helper.
- [x] 3.2 Remove `pwmigrate.py` and its self-check; docs (`password-migration.md`, `internal/gosamba/README.md`). The
      parity script reads the Python helper from git (`REF`, default 14706ec, its last commit).

## 4. Validation

- [x] 4.0 Codex review of the proposal: 2 blockers (database path fails closed; integrity of the LDAP listing) and 9
      should-fix items applied (rights only before the first read, cancellation, rebuild and idempotency rules,
      malformed output, secret sentinels).
- [x] 4.1a Codex review of the code: 1 blocker (the DRS SPN from the anonymous rootDSE → the replication SPN) and 9
      should-fix items applied (account type and RID checked in the sealed reply, AP-REP binding, RFC 4752 layer
      selection and rotated tokens, LDAP cancellation, timeouts stop the run, unreadable state is an error, zeroing,
      no password in the test's argv, unfolded LDIF in the parity comparison); Samba and 2008 R2 parity rerun.
- [x] 4.1 `make check`, `make test`, `make test-integration`, `make build`.
