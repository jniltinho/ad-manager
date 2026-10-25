# initial-setup Specification

## Purpose
Protected first-run setup wizard that installs and provisions the chosen Samba role and configures the panel.

## Requirements

### Requirement: Protected setup mode
Without a valid AD configuration, the panel SHALL start in setup mode, accepting only the wizard with a one-time
token shown on the server console, and SHALL reject all other routes until setup finishes.

#### Scenario: First access
- **WHEN** the package is installed on Ubuntu 26.04 with only the base packages and Redis and the operator opens the
  panel
- **THEN** the panel asks for the setup token, which is in `/etc/ad-manager/setup-token` (readable only by root),
  and only then shows the wizard

#### Scenario: Password outside the process
- **WHEN** setup provisions a DC
- **THEN** the administrator password does not appear in `ps`, the journal, the setup log or the audit log

### Requirement: Backup before changing
When Samba or its files already exist on the server, the agent SHALL take a full backup before any change and SHALL
abort setup if the backup fails.

#### Scenario: Existing Samba
- **WHEN** setup runs on a server with `/etc/samba/smb.conf` and `/var/lib/samba`
- **THEN** the first step is the backup, which appears on the Backup screen as "before setup", and only then is
  Samba reconfigured

### Requirement: Installation and log
The agent SHALL install the missing packages, provision the chosen role, write the panel configuration without
secrets and show each step in a live log, without passwords, also saved to a file.

#### Scenario: New domain
- **WHEN** the operator chooses "new domain" with realm `example.local` and confirms
- **THEN** the log shows installation, provisioning and configuration, the panel restarts outside setup mode and a
  Domain Admin signs in over validated LDAPS

### Requirement: Step-by-step setup wizard
The setup screen SHALL be a step-by-step wizard (Access, This server, Role, Details, Review, Installation) with the
current step marked (`aria-current="step"`), SHALL validate each field before the review with the message tied to
the field, SHALL show the review as sentences (with the commands in a collapsible block) and SHALL show the
installation progress as completed steps out of a total, ending on a screen with the next steps.

#### Scenario: Invalid field
- **WHEN** the NetBIOS name has more than 15 characters or the domain has no dot
- **THEN** the review is not requested, the field gets `aria-invalid="true"`, the message appears below it and the
  focus moves to it

#### Scenario: Go back without losing data
- **WHEN** the user goes back from Review to Details
- **THEN** the fields keep what was typed, except the password, which is never kept outside the field

#### Scenario: Server problem
- **WHEN** the clock is not synchronized or disk space is low
- **THEN** the "This server" step shows the item as a problem, with what to do

#### Scenario: Installation
- **WHEN** the installation runs
- **THEN** the bar shows the completed steps out of a total, the log is collapsible and, when it finishes, the screen
  shows the result and the next steps

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
