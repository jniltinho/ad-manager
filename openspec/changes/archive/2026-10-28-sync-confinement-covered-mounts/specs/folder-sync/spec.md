## MODIFIED Requirements

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

#### Scenario: A folder mounted twice
- **WHEN** a folder outside the destination is mounted twice on the same point and the worker's unit leaves the top
  mount read-only and the covered one writable
- **THEN** the run is not refused for the covered mount, and a writable mount the worker can reach is still refused
