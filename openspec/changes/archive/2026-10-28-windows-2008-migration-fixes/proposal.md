## Why

The Windows Server 2008 R2 lab (2026-10-28: dockur DC `lab2008.test`, migrated into an all-in-one Samba 4.23 with the
same realm, and joined by a Samba DC that took it over) found four migration problems:

1. **Share without a security descriptor.** A share created through `NetShareAdd` level 2 has no security
   descriptor; Windows treats that (or a NULL DACL) as Everyone: Full Control, so the root's NTFS ACL alone decides.
   `migrate shares` and the panel's "From another AD" shares read no allowed ACE and gave no access to anyone but
   Domain Admins: `Domain Users`, with Change on the root, would have lost access.
2. **Source user without a password.** A user created without a password (`dsadd` without `-pwd`) replicates no NT
   hash. The password migration reported it `error`, and the CLI exited 2 as if the migration had failed.
3. **Generated passwords lost.** `ad-manager import ad --apply --dir X` with a directory that did not exist created
   the users and then failed to write their generated passwords: the only copy was lost.

4. **RC4-only accounts.** After the `dc-join`, `Administrator` and `labadmin` (passwords set while they were local
   accounts, before `dcpromo`) had no AES Kerberos keys, on the 2008 R2 DC as on Samba: Ubuntu 26.04's `kinit`
   refuses RC4 by default. Nothing warned the operator before the cutover.

## What Changes

- A share whose `netsharegetinfo 502` reply has no `DACL` section (no security descriptor, or a NULL DACL) counts as Everyone: Full Control on the share (an empty DACL
  still means no access).
- A source user without an NT hash is a new outcome, `no_password` (not an error): nothing is set, the user keeps
  the password the import gave it, and the panel keeps that generated password in its download.
- `writePasswords` creates the directory (0700) before writing the CSV (0600).
- The preflight warns about `RC4-only accounts` (Microsoft's heuristic: `pwdLastSet` older than the Read-only Domain
  Controllers group) so their passwords are reset before the cutover.

## Capabilities

### Modified Capabilities

- `ad-import`: a share without a security descriptor.
- `ad-migration`: a source user without a password; the RC4-only accounts preflight item.
- `directory-import`: the generated passwords directory.

## Impact

`internal/winshares/winshares.go`, `internal/sambadc/pwmigrate.py` (+ self-check), `internal/sambadc/pwmigrate.go`
(outcome list), `cmd/csv.go`, `internal/ad/migrate.go` (preflight), the `no_password` entry in `internal/i18n`, and `docs/password-migration.md` and
`docs/cli.md`. No API shape, UI or configuration change.
