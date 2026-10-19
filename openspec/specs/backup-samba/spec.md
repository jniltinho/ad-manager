# backup-samba Specification

## Purpose
Back up and restore DCs and file servers through their agents: manual and scheduled backups, chain retention and protected restores.

## Requirements

### Requirement: Protected restore
The Backup screen and the CLI SHALL restore a chosen or uploaded backup, by type, first showing what changes,
asking for typed confirmation and backing up the current state when the restore overwrites something; a DC backup
SHALL never be restored over a running DC, only through the initial setup on a new machine.

#### Scenario: Folder deleted by mistake
- **WHEN** support restores yesterday's data backup of `Finance`
- **THEN** the chain (full + incrementals up to yesterday) is extracted to `/srv/shares/_restored/<date>/Finance`
  with the Windows permissions, without touching the current folder

#### Scenario: Overwrite the original
- **WHEN** support chooses to overwrite the original folder and confirms by typing the name
- **THEN** the current folder first becomes a data backup, and only then the restore writes to it

#### Scenario: Uploaded backup
- **WHEN** support uploads a configuration `.tar.gz` from another server
- **THEN** the agent checks the content, it appears in the list as "uploaded" with its SHA-256, and it can be
  restored

#### Scenario: DC backup
- **WHEN** someone tries to restore a `dc` backup from the screen of a running DC
- **THEN** the action does not exist there; the screen points to the initial setup ("restore DC") on a new machine

### Requirement: Manual backup and list
The panel and the desktop app SHALL list the backups of each server with an agent (date, type, size, origin, status,
SHA-256) and SHALL allow starting a backup immediately, downloading and deleting a backup, always auditing who
requested it.

#### Scenario: DC backup before a change
- **WHEN** support clicks "Back up now" on the lab DC
- **THEN** the agent produces the `samba-tool domain backup offline` file, it appears in the list with status ok and
  the audit log records `backup.run`

#### Scenario: Manual deletion
- **WHEN** support deletes a backup confirming its name
- **THEN** the file disappears from the agent disk and from the list, and the audit log records `backup.delete`

### Requirement: Scheduling and retention
The panel SHALL define schedules per server and type and the agent SHALL store and run them without depending on
the panel being up, including after a restart, applying chain retention after each run: only whole chains (full and
its incrementals) are deleted, never the backup just taken.

#### Scenario: Chain retention
- **WHEN** retention is 2 chains and the third weekly chain starts
- **THEN** the oldest chain (full and incrementals) is deleted as a whole and the log shows which files

#### Scenario: Panel down
- **WHEN** the panel is stopped and a schedule comes due
- **THEN** the agent takes the backup, and it appears in the list when the panel comes back

### Requirement: Tested recovery
The process SHALL have a recovery test: the DC backup restored on a new server brings the domain up, and the folders
restored from a chain match the source by hash.

#### Scenario: Restored DC
- **WHEN** the offline backup of the lab DC is restored with `samba-tool domain backup restore` on a new VM
- **THEN** the restored DC comes up and a user from the dataset signs in with Kerberos

### Requirement: Protected secrets
Backups SHALL stay only in the agent folder with root 0600 permissions, downloads SHALL require a signed-in
administrator, and no password or backup content SHALL appear in logs or in the audit log.

#### Scenario: Forged path
- **WHEN** a download or delete request contains `../` or a nonexistent id
- **THEN** the agent rejects it and nothing is read or deleted
