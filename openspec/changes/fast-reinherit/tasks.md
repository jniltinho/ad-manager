## 1. Bridge: memoized propagation

- [x] 1.1 `bridge.py` `propagate`: read each item and compute its descriptor as today; key = (descriptor SDDL,
      `acl_state` before: uid, gid, full mode, `system.posix_acl_access`, `system.posix_acl_default`,
      `user.SAMBA_PAI`, `security.NTACL`); first item of a key through `set_nt_acl`, memoize the state it left.
- [x] 1.2 `put_state` on the item's fd: chown only when it differs, chmod only when it differs, POSIX ACLs and
      `user.SAMBA_PAI` set/removed (only ENODATA ignored), NTACL last; read the state back byte for byte, else
      `set_nt_acl` and stop memoizing the key.
- [x] 1.3 `memo_safe`: memo only with a global VFS stack within {acl_xattr, dfs_samba4} and no custom
      `acl_xattr:security_acl_name`.
- [x] 1.4 Parity test (`ntacl_parity_test.go`, real Samba, `map acl inherit = yes`): a memoized full reapply and a
      per-item pass (one `reinherit_paths` per item) leave every item byte-identical; mutation check that the test
      fails when the copy is wrong and the read-back is skipped.

## 2. Helper idle stop

- [x] 2.1 `helper_linux.go`: after each request (turn held), arm a 5-minute timer per helper; the stop takes the
      turn and rechecks the idle time. Tests: stops when idle and comes back; never stops under a request longer than
      the timeout.

## 3. Agent: pending inheritance

- [x] 3.1 `sync.go`: marker `sync/acl-pending/<job>.<hash of dest>`; after the tree lock read it (or the legacy
      history), write it (fail the run if it cannot), do the whole tree when pending (even with 0 transfers), clear on
      ACL success or a clean 0-transfer run with nothing pending before; previews untouched; removed with the job.
- [x] 3.2 `legacyACLPending`: pending from an older version's history (inheritance not applied, or copied and then
      failed/cancelled), not hidden by later runs that copied nothing.
- [x] 3.3 Unit tests: `aclPlan`, the per-destination mark, the legacy history.

## 4. Validation

- [x] 4.1 `make check && make test`; parity tests against the Docker file server with `acl_xattr`.
- [x] 4.2 Codex review of the proposal and of the code (fixes applied: legacy nil-result cancel, marker lookup error, SAMBA_PAI in the parity test).
- [x] 4.3 CHANGELOG `[Unreleased]`.
