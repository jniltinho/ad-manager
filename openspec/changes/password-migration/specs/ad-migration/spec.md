## ADDED Requirements

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

#### Scenario: Source left unchanged
- **WHEN** the plan and the apply have run against the source
- **THEN** the source directory metadata is unchanged (no `repsTo`/`UpdateRefs` write), confirmed by comparing it
  before and after

#### Scenario: Reversible-password source rejected
- **WHEN** a source account stores a reversible/cleartext secret (`Primary:CLEARTEXT`)
- **THEN** that account is reported rejected and no cleartext is read into the target

### Requirement: Import carries last-logon and operating system
The "From another AD" import SHALL carry, in addition to the objects, the last-logon timestamp
(`lastLogonTimestamp`) of users and computers and the `operatingSystem` of computers, to the matching objects of the
target domain, so the inactivity view and the operating-system column are correct right after the migration.

#### Scenario: User last-logon carried
- **WHEN** the import creates or updates a user that has a last-logon timestamp on the source
- **THEN** the target user shows the same approximate last-logon and the "no logon for 90+ days" filter matches the
  source's inactive users

#### Scenario: Computer last-logon and OS carried
- **WHEN** the import creates or updates a computer
- **THEN** the target computer shows the source's operating system and its approximate last-logon

#### Scenario: Never-logged-on object
- **WHEN** a source user or computer has never logged on (no last-logon value)
- **THEN** the target object has no last-logon set and is not reported as an error
