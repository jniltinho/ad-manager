## Why

A Windows server migrated to a Samba DC keeps its hidden data shares (a name ending in `$`, such as
`Faturamento Sefaz$`). The panel's "From another AD" import plans them, but the agent of a DC that serves files
refused every name ending in `$` as "a share of the domain controller itself", so the share could not be created.
The `dc-file-server` spec only asks the DC to protect its administrative shares (`C$`, `ADMIN$`, `IPC$`, `print$`),
`sysvol` and `netlogon`; a hidden data share is not one of them.

## What Changes

- On a DC, the agent reserves only the administrative `$` shares (a drive letter's, `ADMIN$`, `IPC$`, `print$`),
  `sysvol`, `netlogon` and the shares `smb.conf` itself defines. A hidden data share is created, changed and removed
  like any other.

## Impact

- `internal/shares` (`Manager.reserved`), with a test.
- No API, UI or configuration change.
