## MODIFIED Requirements

### Requirement: Import from another AD
The panel SHALL connect to a source AD given by the operator (URL, domain, user and password), read users, groups and OUs and show a preview of what would be created or updated in the current domain; it writes only after confirmation, with the same rules as the CSV import (create and update only, idempotent, generated passwords that must be changed at next sign-in). A saved source profile SHALL keep the source password sealed in the panel's database, bound to the profile, never returned through the API; running a profile with an empty password SHALL use the kept one.

#### Scenario: Preview from AD 2008
- **WHEN** the operator connects to `ldap://10.0.0.10` with valid credentials and asks for the preview
- **THEN** the panel lists the actions by type without writing anything to the target or the source

#### Scenario: Source password is not stored
- **WHEN** the operator saves the source profile
- **THEN** URL, domain and user are stored and the password is not stored in clear: only sealed, bound to the
  profile, and the profile list reports `has_password` without it

#### Scenario: Reuse a profile
- **WHEN** the operator picks a profile with a kept password and runs the preview with the password field empty
- **THEN** the panel uses the kept password and the preview runs

#### Scenario: Generated passwords directory does not exist
- **WHEN** the CLI import runs with `--apply` and a `--dir` that does not exist yet
- **THEN** the directory is created (0700) and the generated passwords CSV (0600) is written there, so the users just
  created never lose their only copy of the password
