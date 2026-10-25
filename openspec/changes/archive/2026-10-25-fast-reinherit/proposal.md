## Why

The first sync of a real share (`Financeiro`, 15,828 files / 741 directories, 2.7 GB, on the Criare Samba 4.23 DC)
copied in 527 s, then spent the bridge's whole 30-minute budget reapplying Windows inheritance and stopped at ~55%
("samba bridge: no answer (read unix @->@: i/o timeout)"): 7,150 files and 260 directories were left with no stored
Windows ACL. Measured on that DC, `smbd.get_nt_acl` costs 3–6 ms per item but `smbd.set_nt_acl` costs ~180 ms
(every SID of the ACL is mapped to a uid/gid through idmap, an LDAP lookup each time) — the propagation calls it once
per item, so ~16k items need ~50 minutes. Yet the whole share holds only **3 distinct** (Windows ACL, POSIX ACL) pairs:
the children of a folder inherit the same descriptor, so almost every `set_nt_acl` repeats the previous one.

Worse, the failure is not remembered: the next run transfers nothing, so the agent does no ACL work and those items
keep the ACL Samba synthesizes from their mode bits, forever. The same happens when a run is cancelled or fails after
rclone already wrote files.

Separately, the python3 helper is started on demand but then stays up idle until the agent stops (~36 MB on the DC).

## What Changes

- **Memoized propagation** (`bridge.py` `propagate`, used by the full reinherit, the incremental `reinherit_paths`
  and a folder ACL change with propagation): every item is still read through Samba (`get_nt_acl`, a few ms) and its
  intended descriptor computed, but the expensive write is memoized. What `set_nt_acl` leaves on an item depends
  only on the descriptor set and the item's ACL state — owner, group, full mode and the `system.posix_acl_access`,
  `system.posix_acl_default`, `user.SAMBA_PAI` and `security.NTACL` xattrs (the v4 system hash covers uid, gid, full
  mode and the POSIX ACLs; its timestamp is always zero). The walk keeps, per (intended descriptor, state before),
  the state Samba left on the first item, and gives it to the next ones on their own descriptor: owner only when it
  differs, then mode only when it differs, then the POSIX ACLs and `user.SAMBA_PAI` (set or removed), the NTACL
  last. Each copied item is read back **byte for byte** (owner, group, mode, the four xattrs); one that does not
  match — or any error but "attribute absent" — goes through `set_nt_acl` with its own intended descriptor, and that
  key is not memoized any more. The memo is used only when the global VFS stack is `acl_xattr` (with `dfs_samba4`)
  and `acl_xattr:security_acl_name` is not set; otherwise every item goes through Samba as today.
- **Inheritance pending**: a non-preview run of a job with inheritance marks the job "Windows inheritance pending"
  for its destination (a marker file per job **and destination**, so a run on an old destination never clears the
  mark of a new one) before it starts — after taking the destination lock, reading the earlier mark first; the run
  fails before starting the worker if the mark cannot be written. The mark is cleared only when the run applied the
  ACL phase, or ended without error, without transfer and with no earlier pending mark; never from the run status
  alone (an ACL error leaves the run "ok" with a note). While a job is pending, its next run reapplies the whole
  destination even if it transfers nothing. A preview never reads or changes the mark. Deleting the job removes its
  marks.
- **Upgrade**: a job whose runs predate the mark is pending when, in its history (newest first), the first run that
  noted its inheritance noted "Windows inheritance not applied", or the first run that copied files without such a
  note ended in error or cancelled — so a share left half-done by an older version is repaired by its next run.
- **Idle helper**: each helper (the quick one and the long one, kept apart) stops after 5 minutes with no request,
  counted from the end of the last one; the stop takes the same turn requests take and rechecks the idle time, so it
  never interrupts a request (a 30-minute walk included).

## Capabilities

### Modified Capabilities

- `folder-sync`: a failed, cancelled or interrupted Windows-inheritance pass is repaired by the job's next run.
- `agent-samba`: propagating a folder's inheritance costs one Samba write per distinct (parent ACL, item state), not
  per item; the helper does not stay up idle.

## Impact

`internal/sambabridge/bridge.py` (`propagate` memo and state copy), `internal/sambabridge/helper_linux.go` (idle
stop), `internal/agent/sync.go` (pending marker: set, read, clear, removed with the job; reinherit trigger), tests
(`sambabridge` parity test for the memoized walk, `agent` unit tests for the pending logic). No API, UI string or
OpenAPI change: the ACL phase note keeps its text, and a run that repairs a pending tree adds a note saying so (a
server message, English, as the existing notes). Out of scope: repairing a destination a job was moved away from while pending (its mark stays until the job is
deleted; reapply it from the folder screen), carrying the source's NTFS ACLs, ACL progress counts
inside the bridge call, replacing rclone or the python helper.
