# gosamba

Go replacements for the `python3-samba` / `python3-ldb` calls ad-manager and [entrasync](../entrasync) make, so
those paths run without Python. Not a general port: `python3-samba` binds Samba's C libraries (ldb, tdb, smbd,
NDR for hundreds of interfaces); this package tree covers only what is used, over network protocols. Design:
`openspec/changes/entra-sync-go/` (D1, D3).

| Package | Replaces | Status |
|---|---|---|
| `samdb` | `SamDB.schema_format_value` (SID), GUID text, FILETIME, `samba.dsdb.UF_*` / `GTYPE_*` | done |
| `drs` | `samba.drs_utils.drsuapi_connect`, `DsGetNCChanges` (`EXOP_REPL_OBJ`), `samba.net.replicate_decrypt` with CRC check | done: Samba 4.23 (Ubuntu 26.04), 4.22 (Debian 13), Windows Server 2022, 2008 R2 |
| `internal/ad` | `SamDB.search`, `get_default_basedn`, `get_config_basedn` over LDAPS (`Search`, `NamingContexts`) | done; the writes (`modify_ldif`, `newcomputer`, `setpassword`) come with the Entra writes |

Where `python3-samba` stays: on a Samba DC (samba-tool and the daemon's own scripts are Python), the NT ACL store
of `internal/sambabridge` (a separate change) and the hash *write* of `internal/sambadc/pwmigrate.py` (Kerberos
keys need ldb's system session).

```bash
go test ./internal/gosamba/...
scripts/test-containers.sh   # ubuntu:26.04 and debian:13, offline
scripts/test-dc.sh           # drs and the LDAPS reads against a Samba DC on Ubuntu 26.04 and Debian 13 (internal Docker network)
```
