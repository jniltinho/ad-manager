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
