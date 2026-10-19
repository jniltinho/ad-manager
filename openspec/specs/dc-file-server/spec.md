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
`netlogon` or administrative shares, and SHALL warn when `smb.conf` lacks `registry shares = yes`.

#### Scenario: Root in SYSVOL
- **WHEN** the configuration points `share_roots` to `/var/lib/samba/sysvol`
- **THEN** the agent starts read-only and logs the reason

### Requirement: Setup with a file-serving DC
The initial setup SHALL offer, in `new-dc` and `dc-join`, that the DC also serves files, configuring the root,
`registry shares = yes` and the agent with `dc_file_server`, registered in the panel.

#### Scenario: New domain with files
- **WHEN** the wizard creates a new domain with "This DC also serves files"
- **THEN** at the end the panel lists the DC itself under Shares and a share created there is reachable
