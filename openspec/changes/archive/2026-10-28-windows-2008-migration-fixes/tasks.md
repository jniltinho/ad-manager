## 1. Share without a security descriptor

- [x] 1.1 `Collect`: a `netsharegetinfo 502` reply without a `DACL` section (no security descriptor, or a NULL DACL that
      prints only `SEC_DESC_DACL_PRESENT`) becomes Everyone (S-1-1-0) with Full Control.
- [x] 1.2 Test: no security descriptor + `Domain Users` Change on the root plans `Domain Users` write; the same
      with a NULL DACL (fails with a plain substring match, Codex review); an empty DACL gives nobody access.
- [x] 1.3 Lab: `migrate shares` from the 2008 R2 lab DC into the all-in-one Samba: `Financeiro` with
      `Domain Users` write (NTFS 0x1301bf) on the target.

## 2. Source user without a password

- [x] 2.1 `pwmigrate.py`: no NT hash → `no_password` with a sanitized reason; catalog entry and docs.
- [x] 2.2 Self-check: `apply_one` with no replicated hash returns `no_password`.
- [x] 2.3 Lab: `migrate passwords --apply` from 2008 R2: `nopw` → `no_password`, `plain`/`labadmin` → `same` on the
      second run, exit 0; `plain`'s production password works on the target over SMB and LDAP.

## 3. Generated passwords directory

- [x] 3.1 `writePasswords` creates the directory (0700); test with a nested directory that does not exist.

## 4. RC4-only accounts

- [x] 4.1 Preflight row `RC4-only accounts`: enabled users with `pwdLastSet` ≤ the RID-521 group's `whenCreated`.
- [x] 4.2 Tests: `rc4Check` (none, two, more than ten) and the Samba integration preflight reports `ok`.
- [x] 4.3 Lab: on the domain migrated from 2008 R2 it lists `Administrator`; after a reset `kinit` gets AES-256.
- [x] 4.4 Ground truth (each enabled user's keys in `sam.ldb`): the 2016-level Vagrant domains (Windows 2022 and
      2019) had `Administrator` and `vagrant` flagged with AES keys → domains born on 2016+ (Key Admins created with
      the RODC group) are `ok`; `bornOn2016` tested. Rechecked: no false positive on either, and the 2008 R2 case.
