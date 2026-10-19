# directory-management Specification

## Purpose
Manage Active Directory (users, groups, OUs, computers and password policy) through the panel, CLI and MCP, with the protection rules enforced on the server.

## Requirements

### Requirement: Secure directory connection
Every operation SHALL use authenticated LDAPS (or StartTLS); operations that change passwords MUST reject connections without TLS. Certificate validation SHALL be on by default and can only be turned off explicitly through configuration.

#### Scenario: Password change without TLS
- **WHEN** the tool is configured with `ldap://` without StartTLS and the operator tries to change a password
- **THEN** the operation is rejected with an error explaining that TLS is required

### Requirement: Manage users
The system SHALL list, search, show details, create, edit (name, email, phone, title, department, description), enable, disable, unlock, move between OUs and delete users.

#### Scenario: Create user
- **WHEN** the operator creates user `john.smith` with a name, an initial password and OU `Branch`
- **THEN** the account exists in `OU=Branch`, with `sAMAccountName` `john.smith`, UPN `john.smith@example.local`, enabled, and the initial password works for sign-in

#### Scenario: Existing login
- **WHEN** the operator creates a user with a `sAMAccountName` that already exists
- **THEN** the operation fails with a conflict error and nothing is changed

#### Scenario: Disable user
- **WHEN** the operator disables `john.smith`
- **THEN** the account disabled bit is set and the list shows the user as disabled

#### Scenario: Delete protected account
- **WHEN** the operator tries to delete or disable `Administrator`, `krbtgt` (identified by RID, even if renamed) or the signed-in account itself
- **THEN** the operation is rejected

### Requirement: Passwords
The system SHALL reset a user's password (with an option to require a change at next sign-in) and let users change their own password by providing the current one. Passwords MUST never be written to logs, the audit log or responses.

#### Scenario: Reset with required change
- **WHEN** the operator resets the password of `john.smith` checking "change at next sign-in"
- **THEN** the new password is applied and `pwdLastSet` becomes `0`

#### Scenario: Password rejected by policy
- **WHEN** the new password violates the domain policy
- **THEN** the error message says the password does not meet the policy and no password data appears in logs

### Requirement: Manage groups and members
The system SHALL list, create (security or distribution, global/domain local/universal scope), edit the description of, and delete groups, and add/remove members (users, groups and computers).

#### Scenario: Add member
- **WHEN** the operator adds `john.smith` to group `Finance`
- **THEN** `john.smith` appears in the members of `Finance` and `Finance` appears in the groups of `john.smith`

#### Scenario: Protected built-in group
- **WHEN** the operator tries to delete `Domain Admins`, `Domain Users` or another group with RID below 1000
- **THEN** the operation is rejected

### Requirement: OUs and computers
The system SHALL list the OU tree, create and delete empty OUs, list computers with operating system and approximate last logon (`lastLogonTimestamp`, replicated, ~14 day precision), and enable, disable, move or delete computer accounts. Domain controller accounts MUST NOT be deleted, disabled or moved.

#### Scenario: Protect DC account
- **WHEN** the operator tries to delete or disable computer `old-dc` while it is a DC
- **THEN** the operation is rejected

#### Scenario: Delete OU with objects
- **WHEN** the operator tries to delete an OU that contains objects
- **THEN** the operation is rejected stating that the OU is not empty

### Requirement: Domain password policy
The system SHALL show and change the domain default password policy (complexity, minimum length, history, minimum/maximum age, lockout threshold and duration). The change applies to subsequent password changes; while a Windows DC exists, the `Default Domain Policy` may override the values and the panel MUST warn about it.

#### Scenario: Harden policy
- **WHEN** the operator sets minimum length 8 and complexity on
- **THEN** the domain policy reflects the new values and a new change to a 3-character password is rejected

### Requirement: CLI and API parity
Every management operation SHALL be available in the CLI and in the HTTP API with the same result; the CLI SHALL offer table and JSON output.

#### Scenario: Same listing on both channels
- **WHEN** the operator lists users through the CLI with `--json` and through the API
- **THEN** both return the same set of users with the same fields
