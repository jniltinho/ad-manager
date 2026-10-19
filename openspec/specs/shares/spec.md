# shares Specification

## Purpose
List, create and remove folders and SMB shares on registered file servers through their agents.

## Requirements

### Requirement: List shares and folders
The panel SHALL list, per registered file server, the existing shares (name, path, comment, read and write groups) and the subfolders of each allowed root.

#### Scenario: Server down
- **WHEN** the server agent does not respond
- **THEN** the screen shows the server as unavailable and does not break the others

### Requirement: Create folder and share
The system SHALL create a folder inside an allowed root, apply the owner group and read/write ACLs for domain groups and publish it as an SMB share, as an asynchronous task with a queryable result.

#### Scenario: Finance share
- **WHEN** the operator creates `Finance` at `/srv/shares/finance` with write access for `EXAMPLE\Finance` and read access for `EXAMPLE\Management`
- **THEN** the folder exists with owner group `Finance`, a write ACL for Finance and a read ACL for Management, and `\\server\Finance` appears in the share list

#### Scenario: Path outside the roots
- **WHEN** the requested path is `/etc`, contains `..` or goes through a symbolic link outside the root
- **THEN** the task is rejected before reaching the queue

#### Scenario: Reserved name
- **WHEN** the share name is `sysvol`, `netlogon`, `IPC$`, `global`, `homes` or `printers`
- **THEN** the creation is rejected

### Requirement: Remove a share without deleting data
Removing a share SHALL only unpublish it; the folder and files MUST remain.

#### Scenario: Remove
- **WHEN** the operator removes the `Old` share
- **THEN** `\\server\Old` disappears from the list and the directory remains on disk
