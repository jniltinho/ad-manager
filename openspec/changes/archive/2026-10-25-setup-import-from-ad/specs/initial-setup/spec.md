## ADDED Requirements

### Requirement: Prepare the import of GPOs and passwords
For the New domain role with "This DC also serves files", the setup wizard SHALL offer an optional, off-by-default
"Prepare the import of GPOs and passwords" that turns on this DC agent's GPO-import and password-migration write modes
during the installation, and the review SHALL say so, including that the administrator password is kept in one
root-only file until it is deleted.

#### Scenario: Prepared import
- **WHEN** the operator creates `example.local` with "This DC also serves files" and the option ticked
- **THEN** after the installation this DC agent's `/api/health` reports `gpo_restore: true` and
  `password_migration: true`

#### Scenario: Off by default
- **WHEN** the operator creates a new domain without ticking the option
- **THEN** `config.toml` has no `gpo_restore_*` or `password_migration_*` key and `/opt/ad-manager/import.pass` does
  not exist

#### Scenario: Only where it applies
- **WHEN** the role is not New domain, or "This DC also serves files" is unticked
- **THEN** the option is not shown, and a plan that still sends `import_ad` is refused with `setup: invalid
  import_ad`

### Requirement: Import step at the end of a new domain
The final screen of a New domain setup SHALL offer "Import from an existing AD", opening the panel's existing "From
another AD" import (the same options and source-credential handling) after the Domain Admin signs in; when the
import was prepared it SHALL be the primary action and the next steps SHALL say how to turn the write modes off. The
wizard SHALL NOT import by itself nor keep the source credential.

#### Scenario: Hand-off to the import
- **WHEN** a new-domain installation finishes and the operator clicks "Import from an existing AD"
- **THEN** the panel asks for the Domain Admin sign-in and then shows Import/export on the "From another AD" tab

#### Scenario: Other roles
- **WHEN** an Additional DC, Restore a DC, File server or Connect only setup finishes
- **THEN** the final screen does not offer the import

#### Scenario: Finished means finished
- **WHEN** the wizard shows the installation as finished after the panel left setup mode
- **THEN** the DC agent's registration and, when chosen, the write modes were already written

#### Scenario: Turning the write modes off
- **WHEN** the operator deletes `/opt/ad-manager/import.pass` after the import
- **THEN** this DC agent's `/api/health` reports `gpo_restore: false` and `password_migration: false`
