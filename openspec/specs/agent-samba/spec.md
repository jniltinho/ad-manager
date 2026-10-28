# agent-samba Specification

## Purpose
File-server agent integration with Samba: Windows ACLs on share folders, ACL history, sessions, protected DNS writes, GPO restore and the persistent Samba helper.

## Requirements

### Requirement: Folders with Windows ACLs
The agent SHALL create share folders with the Windows ACL written by `smbd` (`set_nt_acl`), from full SIDs:
write = Modify, read = Read & execute, Administrators and SYSTEM = Full control, inherited by subfolders and files.

#### Scenario: Permission seen in Explorer
- **WHEN** the panel creates the `Finance` folder with write access for the Finance group and read access for
  Management
- **THEN** Explorer shows "Modify" for Finance and "Read & execute" for Management, applied to "this folder,
  subfolders and files", and a Management user cannot write over SMB

#### Scenario: Full control
- **WHEN** the folder is created with full control for Management (`full_groups`, column `full_control_groups`)
- **THEN** Explorer shows "Full control" for Management, one of its users can change the ACL over SMB, and
  `/api/shares` lists Management in `full_groups` (not in `rw_groups`)

#### Scenario: Unresolved domain name
- **WHEN** a group `EXAMPLE\Missing` is not resolved by winbind
- **THEN** the task fails with an invalid input error and the folder ACL does not change

#### Scenario: setfacl over a Windows ACL
- **WHEN** the server lacks the helper and the folder already has `security.NTACL`
- **THEN** the agent rejects the task instead of applying `setfacl` (Samba would discard the Windows ACL)

#### Scenario: Server without python3-samba
- **WHEN** the helper is not available on the server
- **THEN** the agent reports it in `/api/health` and uses the current path (POSIX ACL), without failing the task

### Requirement: Undo permissions
Before changing the ACL of an existing folder, the agent SHALL save the previous Windows ACL (SDDL, in the
`acl-history.jsonl` history, `0600`) and SHALL allow restoring it.

#### Scenario: History
- **WHEN** a share with a Windows ACL is edited
- **THEN** the history gains a line with the path and the before and after SDDL

#### Scenario: Restore
- **WHEN** the administrator restores the snapshot taken before a change
- **THEN** the folder gets back exactly the previous ACL, in Explorer and on Linux

### Requirement: Sessions and open files
The agent SHALL list sessions and open files (`smbstatus --json`) and SHALL disconnect a session only with explicit
confirmation, recording it in the audit log.

#### Scenario: Locked spreadsheet
- **WHEN** support looks for `spreadsheet.xlsx` in the open files list of `fs01`
- **THEN** they see the user and the machine holding it open

### Requirement: Helper security
The helper SHALL accept only operations from a closed list, revalidate paths (allowed roots, no symbolic links) and
SIDs, and never receive an operator password as an argument or through the queue.

#### Scenario: Path outside the roots
- **WHEN** an ACL request for `/etc` arrives
- **THEN** the helper rejects it and nothing is changed

### Requirement: Domain DNS with protected writes
The panel SHALL add and delete A, CNAME and PTR records in the DC DNS (MS-DNSP, with the operator credential, RPC
bind sealed by Kerberos) and the server SHALL reject, before any call to the DC, any other type (SOA, NS, SRV…),
the zone itself (`@`), the `_msdcs` zone, names with a `_` label, `DomainDnsZones`/`ForestDnsZones`/`gc`, the name
of a domain controller and a PTR pointing to one. Every attempt is audited.

#### Scenario: Application server record
- **WHEN** support adds `intranet A 192.168.10.50` to the domain zone
- **THEN** the record appears in the list and the audit log has `dns.add` with an ok result

#### Scenario: DC record
- **WHEN** someone asks to delete the A record of `dc1` (or create a PTR for it, or an SRV)
- **THEN** the server responds 400, nothing is sent to the DC and the audit log records the rejection

#### Scenario: Forged response
- **WHEN** a host in the middle alters or injects the response of an RPC call
- **THEN** the sealed bind signature does not match and the call fails, without showing or writing forged data

### Requirement: Restore a GPO as a new GPO
The panel SHALL restore a GPO backup only as a new GPO (new GUID, unlinked), with explicit confirmation, through the
agent of a DC whose import write mode is on (`import_modes = true`, or the former account and 0600 password file);
the administrator credential SHALL come from the panel's sealed import credential, sent with the task. Without the
mode the DC agent SHALL stay read-only.

#### Scenario: Restore
- **WHEN** support uploads the "Default Domain Policy" backup with the name "Copy of Default"
- **THEN** a new unlinked GPO with that name appears and the audit log records `gpo.restore` with the new GUID

#### Scenario: Agent without write mode
- **WHEN** the DC agent has neither `import_modes` nor `gpo_restore_user`
- **THEN** the panel responds 503 and nothing is written

### Requirement: GPO backup validated before the restore
The GPO backup file SHALL be validated before any write, and the response SHALL list the other DCs, whose SYSVOL
Samba does not replicate.

#### Scenario: Malicious file
- **WHEN** the `.tar.gz` contains `../`, an absolute path, a link or two GPOs
- **THEN** the agent rejects it before calling `samba-tool`

### Requirement: Domain administrators on shares
Every share published by the agent SHALL allow Domain Admins in (`valid users` and `write list`), in addition to the
chosen groups and users, and reading it back SHALL show only the chosen ones.

#### Scenario: Import from a server managed by ad-manager
- **WHEN** the "From another AD" import reads the shares and ACLs of a file server whose agent published `Finance`
  only for the Finance group
- **THEN** the domain administrator account enters the share and reads the ACL, without `ACCESS_DENIED`

#### Scenario: Group list on screen
- **WHEN** the screen lists the `Finance` share (write: Finance; read: Management)
- **THEN** the groups shown are only Finance and Management

### Requirement: Samba bridge without startup cost
The agent SHALL talk to the Samba API through a helper opened on demand and reused, which serves one request at a
time, is reopened if it dies or times out, gives the same results as the current bridge, and is stopped after 5
minutes with no request, counted from the end of the last one, never while a request runs (the next request opens a
new one).

#### Scenario: Share list
- **WHEN** the panel lists the shares of a DC with 29 folders
- **THEN** the ACLs come from the already open helper, without a new process per folder or per listing

#### Scenario: Dead helper
- **WHEN** the helper process exits in the middle of work
- **THEN** the in-flight request fails with an error and the next one opens a new helper

#### Scenario: Idle helper
- **WHEN** no request reaches the helper for 5 minutes
- **THEN** the helper process exits, and the next request starts a new one and is answered normally

#### Scenario: Long request
- **WHEN** a propagation runs for longer than 5 minutes
- **THEN** its helper is not stopped under it, and the idle countdown starts when it ends

### Requirement: ACL reading in Go with parity
The agent SHALL read the Windows ACL in Go only when the result matches Samba's in the parity test, and SHALL use the
helper in every case the Go reading does not confirm by hash.

#### Scenario: POSIX ACL changed externally
- **WHEN** the POSIX ACL of a folder was changed with `setfacl` after the Windows ACL
- **THEN** the Go reading does not confirm the hash and the result comes from the helper, matching Samba's

### Requirement: Memoized inheritance propagation
Propagating a folder's Windows inheritance SHALL still read each item and compute its descriptor, but SHALL call
`set_nt_acl` once per distinct (descriptor, item ACL state) within a walk; later items with that key SHALL receive
the state Samba left, verified byte for byte, or SHALL go through `set_nt_acl` when it does not land.

#### Scenario: Large share, few distinct ACLs
- **WHEN** inheritance is propagated over a tree of 16,000 files written without Samba under folders that share
  their ACL
- **THEN** `set_nt_acl` runs only for the first item of each key, the walk finishes within the bridge's timeout,
  and every item ends with the same owner, group, mode and ACL xattrs, byte for byte, as a per-item propagation

#### Scenario: Copy does not land
- **WHEN** a memoized state cannot be written on an item or does not read back byte for byte
- **THEN** that item is set through `set_nt_acl` with its own descriptor, and later items of the key are not
  memoized

#### Scenario: Item with its own explicit ACEs
- **WHEN** an item already has a stored Windows ACL with explicit entries
- **THEN** its descriptor is computed from its own ACL, it shares a key only with items of identical descriptor and
  state, and it keeps its explicit entries

#### Scenario: Unverified Samba configuration
- **WHEN** the global VFS stack has a module other than `acl_xattr` and `dfs_samba4`, or a custom
  `acl_xattr:security_acl_name`
- **THEN** every item goes through `set_nt_acl`, as without the memo
