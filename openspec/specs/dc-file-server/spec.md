# dc-file-server Specification

## Purpose
Let a domain controller also serve files through its agent, only with an explicit mode and with the DC protections in place.

## Requirements

### Requirement: Files on a DC with an explicit mode
The agent of a domain controller SHALL stay read-only by default and SHALL, with `dc_file_server = true`, create
folders and shares, apply ACLs, sync and run the file-server backups, as on a member server.

#### Scenario: DC without the mode
- **WHEN** the panel asks the agent of a DC without `dc_file_server` for a folder
- **THEN** the agent rejects it as today ("read-only on a DC")

#### Scenario: DC with files
- **WHEN** the DC has `dc_file_server = true`, `share_roots = ["/srv/shares"]` and `registry shares = yes`
- **THEN** a share created by the panel appears at `\\dc01\Name` and a domain user with permission writes to it

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

### Requirement: Setup with a file-serving DC
The initial setup SHALL offer, in `new-dc` and `dc-join`, that the DC also serves files, configuring the root,
`registry shares = yes` and the agent with `dc_file_server`, registered in the panel.

#### Scenario: New domain with files
- **WHEN** the wizard creates a new domain with "This DC also serves files"
- **THEN** at the end the panel lists the DC itself under Shares and a share created there is reachable

### Requirement: DC agent on the loopback
When the setup makes a DC also serve files, the agent it configures SHALL listen on `127.0.0.1:9443` only (the panel
beside it reaches it there), while a separate file server's agent SHALL keep listening on the network for its panel.

#### Scenario: New domain with files
- **WHEN** `samba-setup.py new-dc --panel --files` (or the wizard's "This DC also serves files") configures the agent
- **THEN** its `[agent]` listens on `127.0.0.1:9443`, the panel lists the DC under Shares, and port 9443 is not
  reachable from the network

#### Scenario: Separate file server
- **WHEN** `samba-setup.py agent` configures a file server's agent
- **THEN** it listens on `0.0.0.0:9443`, for the panel on another machine

#### Scenario: Existing agent configuration
- **WHEN** the setup runs again on a DC whose config already has an `[agent]` section
- **THEN** that section, its `listen` included, is left as it is
