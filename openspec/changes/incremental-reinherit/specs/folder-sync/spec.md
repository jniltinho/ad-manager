## ADDED Requirements

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
