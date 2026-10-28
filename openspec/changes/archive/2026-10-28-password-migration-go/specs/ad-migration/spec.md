## MODIFIED Requirements

### Requirement: Password-material migration
The tool SHALL migrate password material from a source domain to the ad-manager-managed domain over directory
replication (DRSUAPI), never over LDAP and never as plaintext, setting it on the matching user (by `sAMAccountName`)
through the DC agent's dedicated password-migration write mode and never creating a user. The operation SHALL be
audited as `password.migrate` with counts only.

#### Scenario: Preserve the production password
- **WHEN** the operator runs the password migration after importing the objects, for a user that exists on both sides
- **THEN** the user's `unicodePwd` is set from the source so the user authenticates with the production password and
  `pwdLastSet` is non-zero (no "change at next sign-in")

#### Scenario: Kerberos keys copied or ntlm-only
- **WHEN** a user's stored Kerberos salt and key types transfer to the target
- **THEN** the Kerberos keys are copied so Kerberos logon works without a password change; **and when they do not
  transfer** (a salt or realm mismatch), only NTLM is set, the user is reported `ntlm-only`, and the Kerberos keys
  regenerate on the next password change

#### Scenario: No plaintext, no LDAP, no stored secret
- **WHEN** the migration runs
- **THEN** no password plaintext is produced, the material travels only over the replication channel and the agent's
  local database write, and it is absent from the API responses, the server and agent logs, the audit log and every
  file on disk

#### Scenario: Replication rights required
- **WHEN** the source account lacks *Replicating Directory Changes All*
- **THEN** the extraction fails with a clear message, nothing is written to the target, and the command exits with a
  non-zero code

#### Scenario: User missing in the target
- **WHEN** a source user has no matching `sAMAccountName` in the local domain
- **THEN** that user is reported as skipped (import the objects first) and no user is created

#### Scenario: Built-ins and secrets never migrated
- **WHEN** the migration runs over the whole domain
- **THEN** `Administrator`, `Guest`, `krbtgt`, trust accounts and machine accounts are not set on the target

#### Scenario: Plan writes nothing and reads no secret
- **WHEN** the operator runs the plan
- **THEN** it reports, per user, whether a hash would be set, the user is missing, or it is a built-in, using
  authenticated connectivity and non-secret metadata only — reading no password material and writing nothing; the
  `ntlm-only`, cleartext-rejected and `same` outcomes are reported during the authorized apply

#### Scenario: Idempotent apply
- **WHEN** the migration is applied twice with no change at the source
- **THEN** the second run reports every user as `same` and changes nothing

#### Scenario: Write mode required
- **WHEN** the DC agent is not in its dedicated password-migration write mode
- **THEN** the operation is refused with a clear error (the panel shows 503) and nothing is read from the source

#### Scenario: Driven from the "From another AD" import
- **WHEN** the operator ticks the `Passwords` option in the "From another AD" import (beside Objects, GPOs, DNS and
  Shares) and runs it with the source URL, domain, user and password entered in that form
- **THEN** the server hands that source credential to the DC agent and the password migration runs, with no separate
  credential file required; the typed password is not stored, logged or audited

#### Scenario: Credential from env or config
- **WHEN** the migration is run from the CLI or an automation with the source credential in `AD_SOURCE_PASSWORD_FILE`
  (0600), stdin or a configured source profile instead of the form
- **THEN** it runs the same way, and in no case does the source credential appear as a command argument

#### Scenario: Source left unchanged
- **WHEN** the plan and the apply have run against the source
- **THEN** the source directory metadata is unchanged (no `repsTo`/`UpdateRefs` write), confirmed by comparing it
  before and after

#### Scenario: Reversible-password source rejected
- **WHEN** a source account stores a reversible/cleartext secret (`Primary:CLEARTEXT`)
- **THEN** that account is reported rejected and no cleartext is read into the target

#### Scenario: Source user without a password
- **WHEN** a source user has no password (created without one, e.g. `dsadd` without `-pwd`), so no NT hash replicates
- **THEN** the apply reports it `no_password`, sets nothing, the user keeps the password the import gave it, and the
  command does not fail because of it

#### Scenario: No Python helper
- **WHEN** the password migration runs on a DC without `python3-samba` but with Samba's `ldb-tools`
- **THEN** the plan and the apply complete in the agent itself, reading over DRSUAPI and writing through `ldbmodify`
  with the password-hash bypass control, with the secrets only on its standard input

#### Scenario: Same outcomes as before
- **WHEN** the same source is migrated into twin destinations by the previous (Python) and the Go implementation
- **THEN** every user has the same outcome and the same stored `unicodePwd` and `supplementalCredentials`

#### Scenario: Tampered listing
- **WHEN** the source's LDAP listing names an account whose sealed DRS reply carries another `sAMAccountName`, or a
  SID outside the source domain
- **THEN** the run stops before writing anything for that account and reports the mismatch

#### Scenario: Database path from Samba's configuration
- **WHEN** Samba's configuration cannot be read, or names a private dir without `sam.ldb`
- **THEN** the migration refuses, without falling back to a default database path
