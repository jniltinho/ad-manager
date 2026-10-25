## ADDED Requirements

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
