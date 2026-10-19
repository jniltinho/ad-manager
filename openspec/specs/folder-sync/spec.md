# folder-sync Specification

## Purpose
Copy or mirror folders from SMB shares to the agent's roots with the embedded rclone, with safe SMB, mirror protections, preview and quarantine.

## Requirements

### Requirement: Bring folders through the agent
The agent SHALL copy or mirror a folder from an SMB share to a folder inside its roots, with the embedded rclone,
without an external binary or mount, in a child process confined to the destination and the quarantine.

#### Scenario: Bring a folder from another server
- **WHEN** support creates a "copy" job from `\\old-fs\Finance` to `/srv/shares/Finance` and runs it
- **THEN** the files arrive with the source dates, the NT inheritance of the destination folder is reapplied, and
  the history shows files, bytes, duration and whether the content was verified

#### Scenario: Only what changed
- **WHEN** the same job runs again after one file was changed and another created at the source
- **THEN** only those two are transferred

#### Scenario: Link in the destination
- **WHEN** someone replaces a destination subfolder with a link to `/etc` during the run
- **THEN** nothing is written outside the destination (the child can only write to it and to the quarantine)

### Requirement: Secure SMB
The SMB connection SHALL require signing, SHALL use Kerberos by default and SHALL reject guest sessions; NTLM SHALL
be an exception flagged on the job, audited, with the password in a 0600 file on the server.

#### Scenario: Server without signing
- **WHEN** the source does not sign messages
- **THEN** the connection fails with the reason, without transferring anything

### Requirement: Protected mirror
Mirror mode SHALL compute the full plan before deleting, SHALL delete nothing if the source is empty or the
deletions exceed the limit, and SHALL move deleted and overwritten files to the quarantine with a manifest.

#### Scenario: Source emptied by mistake
- **WHEN** the source folder appears empty and the mirror would delete 5,000 files with a limit of 100
- **THEN** the run stops before deleting any file and records the reason

#### Scenario: Expired approval
- **WHEN** support changes the filters of an approved mirror
- **THEN** the next manual run asks for a new preview

### Requirement: Preview
Before running or saving a mirror, the screen SHALL show the full plan (totals of added, changed, removed and
skipped, with a sample).

#### Scenario: Mirror preview
- **WHEN** support asks for the preview
- **THEN** they see the totals and the first files of each group, with nothing written to the destination

### Requirement: Secrets, queue and audit
Passwords SHALL stay only in a file on the server, never in the API, the queue, logs or the audit log; sync SHALL run
in its own queue without blocking backups; create, change, delete, preview, run and cancel SHALL be audited with the
final result.

#### Scenario: Cancel
- **WHEN** support cancels a long run
- **THEN** the child is terminated, the history shows "canceled" with what was already done and the audit log
  records it

### Requirement: Quarantine retention
After each run of a job, the agent SHALL delete that job's quarantines older than the retention (days and/or size),
never the one from the run that just finished, and SHALL audit the cleanup. A quarantine SHALL belong to the job
generation and the run recorded in it (a job deleted and recreated with the same id does not delete the previous
one's quarantines; "empty now" also sees only those of the current generation).

#### Scenario: Daily mirror
- **WHEN** a daily mirror with 30-day retention runs on day 31
- **THEN** the first day's quarantine is deleted as a whole and today's is kept

### Requirement: Restore from quarantine
The screen SHALL restore a file (or all of them) from a quarantine to its path in the **destination recorded in the
run** (not the job's current destination), asking for confirmation when the file there changed after the run. The
path SHALL be relative, without `..`, and opened from the destination folder descriptor, component by component,
without following links.

#### Scenario: File deleted by the mirror
- **WHEN** support restores `Finance/report.xlsx` from yesterday's quarantine
- **THEN** the file returns to the destination with its previous content and the audit log records the restore
