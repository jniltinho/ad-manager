## MODIFIED Requirements

### Requirement: DC protections
The agent of a DC SHALL reject share roots inside `/var/lib/samba` or `/etc/samba`, shares named `sysvol`,
`netlogon` or administrative shares (a drive letter followed by `$`, `ADMIN$`, `IPC$`, `print$`) and the shares
`smb.conf` itself defines, and SHALL warn when `smb.conf` lacks `registry shares = yes`. A hidden data share (a name
ending in `$` that is not administrative) SHALL be handled like any other share.

#### Scenario: Root in SYSVOL
- **WHEN** the configuration points `share_roots` to `/var/lib/samba/sysvol`
- **THEN** the agent starts read-only and logs the reason

#### Scenario: Administrative share
- **WHEN** the panel asks the agent of a DC to create or delete `C$`, `ADMIN$` or `IPC$`
- **THEN** the agent rejects it as a share of the domain controller itself

#### Scenario: Hidden data share
- **WHEN** the panel asks the agent of a DC to create `Faturamento Sefaz$` at `/srv/shares/Faturamento Sefaz`
- **THEN** the share is created, hidden from browsing, and reachable at `\\dc01\Faturamento Sefaz$`
