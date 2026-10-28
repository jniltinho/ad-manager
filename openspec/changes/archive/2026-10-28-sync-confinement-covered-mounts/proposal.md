## Why

The Samba → Samba lab (2026-10-28) found every sync on `samba-dst-fs` refused with "confinement: /vagrant is
writable to the sync worker" after the machine rebooted. `/vagrant` was mounted twice on the same point (at boot by
`vagrant.mount`, then again by Vagrant). Inside the worker's unit systemd's `ProtectSystem=strict` made the top mount
read-only but left the covered one `rw`, and the confinement check read every line of `mountinfo` alike. The covered
mount is unreachable by any path, so the refusal was a false positive that blocks syncs on any server with a folder
mounted twice (an automount, a bind over a mount).

## What Changes

- The worker's confinement check ignores a mount covered by another mount on the same mount point, and every mount
  under it; a writable mount anyone can reach is still refused, as before.
- The mount table check is a pure function (`checkMounts`) tested with the lab's real `mountinfo`.

## Capabilities

### Modified Capabilities

- `folder-sync`: the confinement scenario for a folder mounted twice.

## Impact

`internal/syncer/confine_linux.go` and its test. No API, UI or configuration change.
