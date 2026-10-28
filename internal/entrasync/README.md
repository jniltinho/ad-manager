# entrasync

Go port of [AzureADConnect_Samba4](https://github.com/tranquilit/AzureADConnect_Samba4) (with
[AADInternals_python](https://github.com/tranquilit/AADInternals_python) and
[python-wcfbin](https://github.com/tranquilit/python-wcfbin)): one-way sync of users, groups, devices and password
hashes from Active Directory (Samba 4 or Windows) to Microsoft Entra ID, without Python. Design and status:
`openspec/changes/entra-sync-go/`.

| Package | Replaces | Status |
|---|---|---|
| `nbfx` | python-wcfbin ([MC-NBFX]/[MC-NBFS] binary XML) | done, fuzzed |
| `aadsync` | AADInternals_python SOAP calls, `create_aadhash` | password hash done |
| `identity`, `graph`, `sync` | `_aad_certificate_manager.py`, Graph calls, `SambaInfo` + `run_sync.py` | planned |

The AD side (`AdAccountReader`, `SamDB`) comes from [gosamba](../gosamba).

```bash
go test ./internal/entrasync/...
scripts/test-containers.sh   # gosamba and entrasync tests in ubuntu:26.04 and debian:13, offline
```
