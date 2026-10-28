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
- [ ] 3.1b Parity with a Windows Server 2008 R2 source (dockur) → Samba; then 2022/2019.
- [ ] 3.2 Remove `pwmigrate.py` and its self-check; docs.

## 4. Validation

- [x] 4.0 Codex review of the proposal: 2 blockers (database path fails closed; integrity of the LDAP listing) and 9
      should-fix items applied (rights only before the first read, cancellation, rebuild and idempotency rules,
      malformed output, secret sentinels).
- [ ] 4.1 `make check`, `make test`, `make test-integration`, `scripts/test-dc.sh`; Codex review of the code.
