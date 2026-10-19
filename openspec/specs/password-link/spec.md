# password-link Specification

## Purpose
One-time link that lets users choose their own new password, generated through the panel, CLI or MCP.

## Requirements

### Requirement: Generate a password change link
The signed-in administrator SHALL be able to generate, for a regular user, a one-time link valid between 1 hour and
7 days. On generation, the account gets a random temporary password that must be changed and is unlocked. Protected
accounts (built-in, domain controllers, the operator's own account), groups and computers are rejected.

#### Scenario: Link for a user
- **WHEN** the administrator chooses "Send link to the user" for `jane.doe` with 24 h validity
- **THEN** they get `https://<panel>/password/<token>` and the expiry date, and the audit log records `password.link`

#### Scenario: Protected account
- **WHEN** someone asks for a link for `Administrator`
- **THEN** the API responds 403 and the password does not change

### Requirement: Use the link
The public link page SHALL show the user and change the password via kpasswd with the temporary password. The first
browser that opens the link owns it for at most 30 minutes; another browser gets 410. After a successful change the
link ceases to exist; a password outside the policy keeps the link (up to 5 attempts).

#### Scenario: Change with minimum password age
- **WHEN** the policy requires a minimum age of 1 day and the user sets a valid password through the link
- **THEN** the change works, the user signs in with the new password and the audit log records `password.link.use`

#### Scenario: Link opened in another browser
- **WHEN** the link was already opened and someone accesses it from another browser
- **THEN** the API responds 410 "this link was already opened in another browser"

#### Scenario: Used or expired link
- **WHEN** the link was already used or is past its validity
- **THEN** the API responds 404 "invalid or expired link"
