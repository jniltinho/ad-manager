## Why

On the Sync page, a run's **Log** button (the ScrollText icon) poured the raw run log into the generic alert dialog
(`showAlert`), which has no height cap. A long log — e.g. a dry-run that prints "Skipped set directory modification
time as --dry-run is set" for every object, thousands of lines — stretched the dialog past the viewport and spilled
off the bottom of the page instead of scrolling inside a box. Seen on a production deploy.

## What Changes

- The Sync **Log** opens a dedicated, polished dialog (`SyncLogDialog`) built on the panel's `Modal`, consistent with
  the other sync dialogs. The run's summary (job name, run id, status, file/size counts) stays in view at the top and
  the Close/Copy footer stays at the bottom while only the monospace **log body** scrolls (`overflow-auto`, capped to
  a fraction of the viewport). The page never grows with the log, and the dialog stays on screen on small viewports.
- `Sync.vue`'s `showLog` stops routing the log through `showAlert`; it fetches the text and opens `SyncLogDialog`.
- A "Copy" action in the footer copies the whole log to the clipboard.
- No API change: the log is still fetched from `GET /api/.../sync-runs/{id}/log`.

## Capabilities

### Modified Capabilities

- `web-panel`: the sync run log is shown in a capped, scrollable modal instead of an unbounded alert.

## Impact

`frontend/src/pages/Sync.vue` (new `logRun` ref, rewritten `showLog`, the dialog in the template),
new `frontend/src/components/dialogs/SyncLogDialog.vue`, `frontend/src/locales/pt-BR/sync.js` (new source strings).
UI-only; the affected Sync screenshots are retaken. No server, route or response change.
