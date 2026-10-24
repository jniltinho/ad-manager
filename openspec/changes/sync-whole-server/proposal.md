## Why

A sync job brings one folder of one source share into one destination folder. Moving a whole file server (the
new-domain cutover of a Windows server's shares, after the "From another AD" import has created them here) needs
one job per share, typed by hand, and every share created later on the source needs another job. The operator
asked for a job that brings **every share of the source server** at once; today it takes a script on the server
(`sync-shares.py`), outside the panel, its schedule and its runs.

## What Changes

- A sync job gets a kind: `folder` (today's, the default) or `server`: the source is a server alone (no share, no
  path), there is no destination folder, and the mode is copy only (adds and updates, never deletes).
- Each preview or run of a server job lists the source server's shares (the agent's SMB listing, already used by
  the wizard's Browse), leaves out the administrative ones (a drive letter's `X$`, `ADMIN$`, `IPC$`, `print$`),
  and brings each into the folder of the share **of the same name published on this server** (its registry path):
  - a share not published here is skipped, with a note (the shares import creates them first);
  - a share whose folder is inside another one's is skipped, copied with it (the source's nesting);
  - a folder this server cannot sync into (outside its share roots…) is skipped with the reason.
  Every share goes through the same confined worker, quarantine and Windows-inheritance pass as a folder job,
  one at a time; one run records the sum and a note per share.
- The browse request takes `targets: true` and answers, besides the shares, that mapping (share → folder or the
  reason it is skipped), so the wizard shows what a server job will do before it is saved.
- Wizard (web and Rust desktop): step "What to copy" offers "A folder" or "Every share of the server"; with the
  latter, "Where to" shows the mapping instead of a destination, and the mode is copy.
- The jobs table shows "every share" as the source path and "the shares' folders" as the destination.

## Capabilities

### Modified Capabilities

- `folder-sync`: sync jobs of a whole source server.

## Impact

- Agent: `internal/agent/sync.go` (kind, validation, targets, the per-share loop), `internal/shares` (the
  administrative-share rule exported).
- Panel: `internal/server/openapi.yaml` (the job's `kind`, browse's `targets`); the proxy is unchanged.
- Web: `frontend/src/pages/Sync.vue` and its wizard; pt-BR strings.
- Desktop (Rust): `pages/sync.rs`, `pages/sync_wizard.rs`; pt-BR strings.
- No change to what a folder job does.
