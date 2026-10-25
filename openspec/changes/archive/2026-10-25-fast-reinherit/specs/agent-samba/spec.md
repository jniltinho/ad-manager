## ADDED Requirements

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

## MODIFIED Requirements

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
