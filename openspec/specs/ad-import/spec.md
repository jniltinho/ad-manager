# ad-import Specification

## Purpose
Import GPOs, shares and DNS records from another Active Directory into the managed domain, always showing a plan before writing.

## Requirements

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

### Requirement: Import through the CLI and MCP
The CLI (`ad-manager import ad`) and MCP (`ad_import_plan`, `ad_import_apply`) SHALL produce the same plan as the
panel for the same source and target, SHALL only read from the source, and SHALL write to the target only with
`--apply` (CLI) or with the `plan_id` of a plan that is still valid and unchanged (MCP).

#### Scenario: Repeatable plan
- **WHEN** the lab runs `ad-manager import ad --from ldaps://dc2019.lab --profile lab2019` twice with no changes at
  the source
- **THEN** both outputs are identical and the exit code is 1 (there are changes to apply)

#### Scenario: Stale plan
- **WHEN** the target changes after `ad_import_plan` and MCP calls `ad_import_apply` with that `plan_id`
- **THEN** the apply is rejected and nothing is written

### Requirement: DNS in the import
With the DNS option, the import SHALL bring only static A, CNAME and PTR records from the source, SHALL never copy
DC records, SOA, NS, SRV, `_msdcs` or the zone root, and SHALL show conflicts and missing zones without writing
them.

#### Scenario: Application server
- **WHEN** the source has `intranet A 192.168.10.50` and the target has no `intranet`
- **THEN** the plan shows `dns.add intranet A 192.168.10.50` and the apply creates it

#### Scenario: Missing zone
- **WHEN** the source has the reverse zone `10.168.192.in-addr.arpa` and the target does not, without
  `--create-zones`
- **THEN** the plan shows `zone.missing` and its PTR records are not written; with `--create-zones`, the plan shows
  `zone.create` before the PTR records

#### Scenario: Plan for another target
- **WHEN** a `plan_id` generated for target A is applied with the configuration of target B
- **THEN** the apply is rejected

#### Scenario: Source DC record
- **WHEN** the source has the A and PTR records of its own DC
- **THEN** they do not appear in the plan
