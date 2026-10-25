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

### Requirement: Incremental Windows inheritance after a run
After a non-preview run of a job that has `reinherit`, the agent SHALL reapply the destination's Windows inheritance
only to the files rclone transferred and their ancestor directories, falling back to the whole tree only when the
transferred list is empty or capped. Each re-inherited item SHALL end with the same ACL a full reinherit gives it.
The ACL phase SHALL run under the run's cancellable context and SHALL surface itself as run progress.

#### Scenario: Only the changed files are re-inherited
- **WHEN** a job with inheritance runs again and rclone transfers K files (new or changed) out of a large tree
- **THEN** the agent reapplies inheritance to exactly those K files and their ancestor directories, not to the rest
  of the tree, and each re-inherited item ends with the ACL a full reinherit would give it

#### Scenario: First or very large run
- **WHEN** the run is the first for the destination, or transfers more than the cap (so the list is capped)
- **THEN** the agent reapplies inheritance to the whole destination tree

#### Scenario: Nothing transferred
- **WHEN** a run finds the destination already up to date and transfers no file
- **THEN** the agent does no ACL work

#### Scenario: Cancel during the ACL phase
- **WHEN** support cancels a run while it is reapplying Windows inheritance
- **THEN** the helper walk is interrupted and the run records "cancelled"

#### Scenario: ACL phase progress
- **WHEN** the ACL phase is running
- **THEN** the run progress shows the ACL phase and the number of items being re-inherited, so a long pass is not
  indistinguishable from a hang

### Requirement: Run phase in progress
A running sync SHALL report which phase it is in — preparing, copying (with the files transferred and the planned
total), applying Windows permissions, or finalizing — so the panel never shows an opaque "running"; a finished run
SHALL show its summary, not a phase.

#### Scenario: Phase reflects the work
- **WHEN** the panel polls a running sync
- **THEN** its progress names the current phase (and, while copying, the files done out of the planned total)

#### Scenario: Finished run
- **WHEN** a run finishes
- **THEN** it shows its status and summary (files, size, quarantine), with no leftover phase

### Requirement: Pending Windows inheritance is repaired
A non-preview run of a job with inheritance SHALL mark its destination's Windows inheritance pending before the copy
and SHALL clear that mark only after applying it, or after a run with no error, no transfer and nothing pending
before. While pending, the job's next run SHALL reapply inheritance to the whole destination even with no transfer.

#### Scenario: Reinherit timed out
- **WHEN** a run copies 15,000 files and its inheritance pass fails or times out
- **THEN** the job stays pending, and its next run — even with nothing to transfer — reapplies inheritance to the
  whole destination and clears the mark once that succeeds

#### Scenario: Cancelled after copying
- **WHEN** support cancels a run after rclone already wrote files, or the agent dies during the run
- **THEN** the mark stays and the next run reapplies inheritance to the whole destination

#### Scenario: Mark cannot be written
- **WHEN** the agent cannot write the pending mark
- **THEN** the run fails before copying anything

#### Scenario: Run of an older version
- **WHEN** the agent is upgraded and the job's last run that applied inheritance noted "Windows inheritance not
  applied" (or copied files and then failed or was cancelled), with only runs that copied nothing since
- **THEN** the job's next run reapplies inheritance to the whole destination

#### Scenario: Preview
- **WHEN** support previews a job that is pending
- **THEN** the preview does no ACL work and leaves the mark as it is

#### Scenario: Up to date and clean
- **WHEN** a job with no pending mark runs and transfers nothing
- **THEN** the agent does no ACL work and leaves no mark

### Requirement: Run end in the CLI
`ad-manager sync list` SHALL show, for each run, when it ended and how long it took, with the date when it ended on
another day than it started, and `—` while it goes on.

#### Scenario: Listing runs
- **WHEN** support runs `ad-manager sync list <server>` after a run that started at 08:37:14 and finished at 08:41:52
- **THEN** that run's FINISHED column reads `08:41 · 4m38s`

#### Scenario: Run across midnight
- **WHEN** a run ended on the day after it started
- **THEN** its FINISHED column shows the end date and time and the duration

### Requirement: Sync every share of a source server
A sync job of kind `server` SHALL name a source server without a share or a path, SHALL copy only, and each run
SHALL bring every non-administrative share of the source into the folder of the share of the same name published on
this server, one share at a time, recording the summed result and a note per share.

#### Scenario: Whole server copied
- **WHEN** a server job runs against a source with the shares `Financeiro`, `Publico` and `C$`, and this server
  publishes `Financeiro` at `/srv/shares/Financeiro` and `Publico` at `/srv/shares/Users/Publico`
- **THEN** the files of `Financeiro` and `Publico` are copied into those folders, `C$` is not touched, and the run
  shows the summed counts with a note per share

### Requirement: Shares a server job skips
A server job SHALL skip, with the reason, a share not published on this server, a share whose folder is inside
another selected share's folder, and a folder the server cannot sync into.

#### Scenario: Share not published here
- **WHEN** the source has a share `Projetos` that this server does not publish
- **THEN** the run skips it with the note "not published on this server" and copies the others

#### Scenario: Nested share
- **WHEN** this server publishes `Comercial` at `/srv/shares/Comercial` and `Contrato` at
  `/srv/shares/Comercial/Contrato`
- **THEN** `Contrato` is skipped as copied with `Comercial`

#### Scenario: Mirror refused
- **WHEN** a server job is saved with the mirror mode
- **THEN** the agent refuses it: a server job copies only

#### Scenario: The wizard shows the mapping
- **WHEN** the wizard lists a source server with `targets: true`
- **THEN** the answer has, per share, the folder it goes to or the reason it is skipped, before the job is saved

### Requirement: The jobs list names a server job's destination
The Sync page (web and desktop) SHALL show, as the destination of a server job, the share folders of this server
(the agent's share roots), with the mapping by name in its tooltip.

#### Scenario: Destination of a server job
- **WHEN** the Sync page lists a server job on a server whose shares live under `/srv/shares`
- **THEN** its Destination cell shows `/srv/shares` and hovering it says the shares of the same name

### Requirement: Stopping a run asks first
Stopping a queued or running sync run from the web panel or the desktop app SHALL ask for confirmation and SHALL
send nothing until it is given.

#### Scenario: Stop confirmed
- **WHEN** the operator clicks the stop link of a running run and confirms **Stop run**
- **THEN** the cancel request is sent and the list refreshes

#### Scenario: Stop dismissed
- **WHEN** the operator dismisses the confirmation
- **THEN** no request is sent and the run goes on

### Requirement: A share that denies the account is read through its disk's administrative share
When a server job cannot open a source share's root because the share's permission leaves its NTLM account out, the
agent SHALL read that share's folder (from the source's share list) through its disk's administrative share, read
only, and note it in the run; it SHALL never read a whole disk, and a saved job SHALL NOT name an administrative
share.

#### Scenario: Denied share read through E$
- **WHEN** a server job copies a source whose share `ADM` (folder `E:\ADM`) denies the job's account at share level
- **THEN** the run notes that `ADM` is read through `\\source\E$\ADM` and copies its files into the share `ADM` here

#### Scenario: No disk root and no saved administrative share
- **WHEN** a share's folder is a disk's root, or a job is saved with an administrative share
- **THEN** the share fails as before, and the job is refused
