# ad-migration Specification

## Purpose
Migrate a legacy Windows DC to Samba 4: preflight, inventory, export and import, never writing to the production AD without an explicit request.

## Requirements

### Requirement: Source domain preflight
The tool SHALL run a read-only preflight against the source DC and report `ok`, `warning` or `blocker` for each item: network reachability (DNS 53, Kerberos 88, RPC 135, LDAP 389, SMB 445, LDAPS 636, global catalog 3268 and the dynamic RPC range 49152-65535 when tested from the future DC), administrator authentication, domain and forest functional level, schema version, owner of each FSMO role, presence of the Exchange schema and number of DCs.

#### Scenario: Eligible domain
- **WHEN** the operator runs the preflight against `ldaps://10.0.0.10` with valid credentials and all ports respond
- **THEN** the output lists each item with its status, shows functional level `2008` and FSMO on `old-dc`, and exits with code 0

#### Scenario: Essential port closed
- **WHEN** port 389 or 445 of the source DC does not respond
- **THEN** the matching item appears as `blocker` and the command exits with a non-zero code

#### Scenario: Test from a workstation over VPN
- **WHEN** the preflight runs outside the LAN (VPN/NAT)
- **THEN** the output warns that the result does not prove DC-to-DC connectivity and that the cutover check must be repeated on the future DC

#### Scenario: Invalid credential
- **WHEN** the administrator password is wrong
- **THEN** the command reports an authentication failure without showing the password and exits with a non-zero code

### Requirement: Exportable inventory
The tool SHALL export a domain inventory as JSON containing users (with enabled/disabled state and groups), groups (with members), OUs, computers (with operating system) and the password policy, without any hashes or secrets.

#### Scenario: Export inventory
- **WHEN** the operator runs the export pointing to a file
- **THEN** the JSON file contains the counts and lists of each object type and no password attribute (`unicodePwd`, `ntPwdHistory`, `supplementalCredentials`)

#### Scenario: Compare source and target
- **WHEN** the operator exports the inventory of the legacy DC and of the Samba DC and compares them
- **THEN** the comparison lists objects present on only one side and exits with code 0 only if there is no difference

### Requirement: Isolated migration rehearsal
The process SHALL allow rehearsing the migration in an isolated lab (`vagrant/labs`), with a Windows or Samba
source DC populated with the common dataset, with no route to the production networks (IPv4 and IPv6, persistent
across reboots) from the first boot.

#### Scenario: Isolated lab
- **WHEN** the operator brings up the lab
- **THEN** the VMs cannot reach the configured production networks and the import and DC join rehearsal runs on them

#### Scenario: Rehearsal limit
- **WHEN** the rehearsal finishes without errors
- **THEN** the runbook records that it validates the tool and the data, but does not replace the go/no-go criteria
  of the production window (bidirectional join, FSMO transfer and `dcpromo`)

### Requirement: Samba DC join and cutover
The process SHALL join the new Samba DC to the existing domain as an additional controller, confirm bidirectional convergence, copy SYSVOL with GPO editing frozen, transfer the five FSMO roles and the DNS zone owners (`DomainDnsZones`, `ForestDnsZones`), and only then demote the legacy DC with a normal `dcpromo` (not "last DC" and without `forceremoval`), preserving the `old-dc$` account, its A/PTR records and SPNs.

#### Scenario: Healthy replication before cutover
- **WHEN** the Samba DC has joined
- **THEN** `showrepl` on both DCs shows recent success on all naming contexts in both directions, a test object created on each side appears on the other, and the inventory (compared by login and SID) has no differences other than the DC accounts

#### Scenario: Cutover blocked by failed replication
- **WHEN** there is a replication failure or an inventory difference
- **THEN** the runbook forbids transferring FSMO or demoting the legacy DC until the failure is resolved

### Requirement: Rollback
The process SHALL keep a way back: before the demotion, the FSMO roles return to `old-dc` (transfer performed on `old-dc` with `ntdsutil`) and the Samba DC is demoted; after the demotion, going back requires an AD-aware system state backup of `old-dc` taken before `dcpromo`, with a known DSRM credential and the Samba DC isolated during recovery.

#### Scenario: Undo before demotion
- **WHEN** a problem is found after the join and before the legacy `dcpromo`
- **THEN** the FSMO roles return to `old-dc`, the Samba DC is demoted and its metadata removed, and logins keep working through `old-dc`

#### Scenario: Backup required before demotion
- **WHEN** there is no same-day system state backup of `old-dc` with the DSRM password recorded
- **THEN** the runbook forbids running `dcpromo`
