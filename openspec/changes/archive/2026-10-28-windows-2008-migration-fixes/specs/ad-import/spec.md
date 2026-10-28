## MODIFIED Requirements

### Requirement: GPOs and shares from another AD
The "From another AD" import SHALL be able to include the source GPOs, created in the target as new, unlinked GPOs
by the DC agent in write mode, skipping names that already exist, and the shares of a source file server,
recreated on the chosen agent with the effective access of the root, always showing everything in the plan before
writing.

#### Scenario: Source GPO
- **WHEN** the source has the GPO "Finance Mapping" and the target does not
- **THEN** the plan shows `gpo.import` and, on apply, a new unlinked GPO with that name appears in the target

#### Scenario: Default GPO
- **WHEN** the source has the "Default Domain Policy"
- **THEN** the plan shows it as skipped, because the target already has one with that name

#### Scenario: Share
- **WHEN** the source server has `Finance` at `E:\Data\Finance` and the map is `E:\=/srv/shares`
- **THEN** the plan shows `share.create Finance /srv/shares/Data/Finance` with the root groups and levels, and
  applying creates the empty folder with the permissions on the target agent

#### Scenario: Invalid source server
- **WHEN** the source server field contains `-c` or a path
- **THEN** the panel rejects it before running any command

#### Scenario: Deny entry at the source
- **WHEN** the source share (or the root ACL) has a deny entry
- **THEN** the plan shows it as skipped ("has deny entries") and nothing is created in the target

#### Scenario: Name from another domain
- **WHEN** the source ACL references `OTHER\Finance` or a local account of the server
- **THEN** that name is not carried to the target and the plan warns; names from the source domain become
  `TARGET\name`

#### Scenario: GPO too large
- **WHEN** the source GPO exceeds 16 MiB or 4000 files
- **THEN** `samba-tool` is interrupted during the copy and the GPO is reported with an error, without filling the
  DC disk

#### Scenario: GPO name as an option
- **WHEN** the source has a GPO named `--restore-metadata`
- **THEN** it is skipped in the plan and never reaches `samba-tool` as an option

#### Scenario: Share without a security descriptor
- **WHEN** the source share has no security descriptor (created with `NetShareAdd` level 2, or a NULL DACL), so
  Windows gives Everyone Full Control on the share, and the root grants `Domain Users` Change
- **THEN** the plan gives `TARGET\Domain Users` write access, as the NTFS ACL decides, instead of no access
