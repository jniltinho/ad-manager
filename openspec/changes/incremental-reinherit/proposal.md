## Why

After a sync run copies files with rclone, the agent reapplies the destination folder's Windows (NT) inheritance so
the new files take the permissions a Windows client would see. Today it reapplies to the **whole** destination tree
whenever any file was transferred (`res.Transfers > 0` → `Reinherit(dest)`), through the Samba NT-ACL Python helper,
which walks every item. On a ~16k-file share this runs ~30 minutes at high CPU; the run shows "running" with no sign
of progress (indistinguishable from a hang) and, because the sync queue is serial, the next job waits. Worse, every
incremental run with even one changed file pays the full-tree cost again — only a 0-transfer run skips it. The step
also runs with `context.WithoutCancel`, so Stop/Cancel cannot interrupt it.

## What Changes

- The agent reapplies inheritance only to the items rclone actually transferred (the new and changed files, plus the
  directories on the way to them), not the whole tree. rclone already reports those paths through its sync logger;
  the worker returns them in the run result.
- The Samba bridge gains a `reinherit_paths` op: given the destination folder descriptor and the relative paths, it
  recomputes inheritance for exactly those paths and their ancestor directories, parents before children, reusing the
  same per-item logic as the full walk (so each item ends identical to a full reinherit).
- A first run, or a run large enough that the transferred list is capped (> 5000 items), still uses the full-tree
  path — on a first run the transferred set is the whole tree anyway, so the common incremental case touches only the
  delta.
- The ACL phase is surfaced as run progress (`progress.phase = "acl"` with the item count) so a long pass is no longer
  indistinguishable from a hang. The whole run reports its phase (preparing → copying, with the files done out of the
  plan total → applying Windows permissions → finalizing) in the same `progress` field, shown on the Sync page, so a
  running sync never looks like an opaque "running".
- The ACL phase honors cancellation: it runs under the run's cancellable context, so Stop terminates the helper walk
  and the run records "cancelled".

## Capabilities

### Modified Capabilities

- `folder-sync`: after a run, Windows inheritance is reapplied only to the transferred items (full tree on the first
  or a capped run); the phase shows progress and can be cancelled.

## Impact

`internal/agent/sync.go` (trigger, wiring, progress, cancellation), `internal/syncer/syncer.go` (collect transferred
paths via the rclone sync logger into `Result.Changed`/`ChangedCapped`), `internal/shares/ntacl_linux.go` +
`internal/shares/folder_other.go` + `internal/shares/shares.go` (`ReinheritPaths` + `ACLBridge`),
`internal/sambabridge/sambabridge.go` + `internal/sambabridge/bridge.py` (the `reinherit_paths` op and a pruned
`propagate`). The `progress` field is already free-form in the API; `openapi.yaml` notes the ACL phase. No UI strings
change server-side; the Sync page shows the ACL phase from the existing progress field. The give-back (restore from
quarantine) path keeps the full-tree reinherit — out of scope here.
