## Why

The password migration (new-domain cutover) runs `internal/sambadc/pwmigrate.py` on the destination DC: Samba's
Python bindings read the source's secrets over DRSUAPI and write them into the local `sam.ldb`. `internal/gosamba/drs`
now reads those secrets in Go, Kerberos-sealed, and was checked against Samba 4.23/4.22 and Windows Server 2022, 2019
and 2008 R2. Moving the whole migration to Go removes the Python helper from this path (one implementation, typed,
unit-tested without a DC), and keeps every guarantee the Python one gives.

Two facts decide the design (checked on Samba 4.23.6, 2026-10-28):

- Over LDAP, even as Administrator on `ldapi`, Samba refuses the NT hash: "it's not allowed to set the NT hash
  password directly". The password-hash bypass control is honoured only on the local database.
- `ldbmodify` (C, part of Samba's ldb tools, present on every Samba DC) applies the bypass control
  (`local_oid:1.3.6.1.4.1.7165.4.3.12:0`) to `/var/lib/samba/private/sam.ldb` with the LDIF on its standard input.

## What Changes

- **Read (source)**: `gosamba/drs` returns, per user, the NT hash and the decrypted `supplementalCredentials` blob
  (same sealed `GetNCChanges` `EXOP_REPL_OBJ`, read-only, never `DRS_ADD_REF` or `UpdateRefs`). Users, SIDs and GUIDs
  come from the source over LDAP (`ldap://` + NTLM for a 2008 DC, as today).
- **Classify (Go)**: `supplementalCredentials` is parsed in Go ([MS-SAMR] 2.2.10): `Primary:CLEARTEXT` → `rejected`;
  `Primary:Kerberos-Newer-Keys` with the source realm's default salt → Kerberos keys transfer when the realms match
  exactly, else `ntlm_only`; no NT hash → `no_password`.
- **Write (destination)**: per user, one `ldbmodify` with the bypass control and an LDIF on stdin (`unicodePwd`,
  `supplementalCredentials` or its removal, an explicit non-zero `pwdLastSet`); a refused Kerberos blob falls back to
  the NT hash only (`ntlm_only`), as today. The local state for `same` and the plan is read with `ldbsearch`.
- `pwmigrate.py` and its self-check are removed; `MigratePasswords` keeps its signature and result, so the agent,
  panel, CLI and MCP do not change.

## Capabilities

### Modified Capabilities

- `ad-migration`: the password-material migration runs in Go with no Python helper; scenarios for the write path and
  for parity with the outcomes of the Python implementation.

## Impact

`internal/gosamba/drs` (supplementalCredentials), a new `internal/gosamba/suppcreds` parser, `internal/sambadc`
(`pwmigrate.go` rewritten; `pwmigrate.py` and `pwmigrate_selfcheck.py` removed), docs (`password-migration.md`,
`cli.md`), lab and integration tests. No API, UI or configuration change. The DC still needs `ldb-tools` (installed
with Samba) — `python3-samba` stays for the ACL bridge (`internal/sambabridge`), a separate proposal.
