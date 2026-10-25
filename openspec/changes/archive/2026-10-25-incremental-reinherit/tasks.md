## 1. Worker: collect transferred paths

- [x] 1.1 Register an rclone sync logger on the run copy (`operations.WithLogger`) that collects the remote-relative
      path of every transferred entry (sigils MissingOnDst and Differ) into `Result.Changed`, capped at 5000; set
      `Result.ChangedCapped` when the cap is passed.
- [x] 1.2 Add `Changed []string` and `ChangedCapped bool` to `syncer.Result`.

## 2. Samba bridge: reinherit only given paths

- [x] 2.1 `bridge.py`: give `propagate` an optional `only` set (prune the fwalk to the dirs in the set, apply only to
      entries in the set); add op `reinherit_paths` that expands the given rels to include their ancestor dirs and
      calls the pruned `propagate` with `reset_unstored=True`.
- [x] 2.2 `sambabridge.go`: `Reapply`-style `ReinheritPaths(ctx, dir, path, roots, rels)` → `reinherit_paths`.
- [x] 2.3 `shares.go` `ACLBridge`: add `ReinheritPaths`; `ntacl_linux.go`: `Manager.ReinheritPaths` (open+verify top
      folder, call the bridge); `folder_other.go`: stub; update the fake in `ntacl_linux_test.go`.

## 3. Agent: wire, progress, cancel

- [x] 3.1 `sync.go`: when transfers > 0, pick incremental (`ReinheritPaths` with `res.Changed`) vs full
      (`Reinherit`, when `ChangedCapped` or the list is empty); run under the run's cancellable context.
- [x] 3.2 Set `r.Progress` to the ACL phase and item count before the call; map a cancellation to status "cancelled".

## 4. Docs and validation

- [x] 4.1 `openapi.yaml`: note the ACL phase in the `progress` field description; `make check` keeps the swagger test
      green.
- [x] 4.2 Unit tests: incremental re-inherits only the given paths; a 0-transfer run does no ACL work; cancellation
      stops the phase. `make check && make test`.
- [x] 4.3 Integration (Samba): a full and an incremental reinherit leave identical ACLs on the changed items.
      `make samba-up && make test-integration`.
