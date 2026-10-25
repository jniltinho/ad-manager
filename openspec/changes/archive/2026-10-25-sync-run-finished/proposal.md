## Why

The Sync page's Runs table shows only when a run started. To know how long a sync took — or when the last one
ended — support opens each run's details or log. The run record already carries `finished` (`SyncRun.Finished`).

## What Changes

- A **Finished** column, right after **Date** in the Runs table: the time the run ended and its duration
  (`08:41 AM · 4m38s`, one line); the date too when it ended on another day than it started. Empty (`—`) while the run is
  queued or running (the Status column shows its phase).
- pt-BR translation of the column title.

## Capabilities

### Modified Capabilities

- `web-panel`: the Sync page's Runs table shows when each run finished and how long it took.

## Impact

`frontend/src/pages/Sync.vue`, `frontend/src/locales/pt-BR/sync.js`; `30-sync.png` and `33-sync-log.png` retaken from
fixtures by `frontend/tests/visual/sync-log-shot.mjs` (the old `30-sync.png` showed a real host name).
No API change: `finished` is already in `GET /api/agents/{host}/syncs` runs.
