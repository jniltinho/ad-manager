## MODIFIED Requirements

### Requirement: Source domain preflight
The tool SHALL run a read-only preflight against the source DC and report `ok`, `warning` or `blocker` for each item: network reachability (DNS 53, Kerberos 88, RPC 135, LDAP 389, SMB 445, LDAPS 636, global catalog 3268 and the dynamic RPC range 49152-65535 when tested from the future DC), administrator authentication, domain and forest functional level, schema version, owner of each FSMO role, presence of the Exchange schema, number of DCs and the enabled users whose password predates the domain's AES Kerberos keys.

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

#### Scenario: RC4-only accounts
- **WHEN** enabled users of the source have a `pwdLastSet` older than the creation of the Read-only Domain Controllers
  group (RID 521, created when the domain reaches 2008): passwords set before that level, or while the account was
  still local before `dcpromo`, carry no AES Kerberos keys
- **THEN** the preflight shows `RC4-only accounts` as `warning` with their count and up to ten names, advising a
  password reset before the cutover; with none it shows `ok`. It is a heuristic (a password set later on a
  remaining 2003 DC is missed), and users that must change their password at next logon are not listed

#### Scenario: Domain created by Windows Server 2016 or later
- **WHEN** the Key Admins group (RID 526) was created together with the Read-only Domain Controllers group (the same
  `dcpromo`): the domain was born on a 2016+ DC, whose local accounts already carry AES keys before `dcpromo`
- **THEN** `RC4-only accounts` is `ok` ("the domain was created by Windows Server 2016 or later") and no account is
  listed

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
