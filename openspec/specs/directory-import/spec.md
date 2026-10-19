# directory-import Specification

## Purpose
Import users, groups and OUs from another Active Directory into the current domain after a preview and explicit confirmation.

## Requirements

### Requirement: Import from another AD
The panel SHALL connect to a source AD given by the operator (URL, domain, user and password), read users, groups and OUs and show a preview of what would be created or updated in the current domain; it writes only after confirmation, with the same rules as the CSV import (create and update only, idempotent, generated passwords that must be changed at next sign-in).

#### Scenario: Preview from AD 2008
- **WHEN** the operator connects to `ldap://10.0.0.10` with valid credentials and asks for the preview
- **THEN** the panel lists the actions by type without writing anything to the target or the source

#### Scenario: Source password is not stored
- **WHEN** the operator saves the source profile
- **THEN** URL, domain and user are stored and the password is not
