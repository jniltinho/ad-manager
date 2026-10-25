## Why

v0.19.0 shows when each sync run finished and how long it took on the web panel only. The desktop app's Sync page
and `ad-manager sync list` still show just the start, and three server notes of the Windows-inheritance pass
(`incremental Windows inheritance failed (…)`, `an earlier run left the Windows inheritance pending: …`,
`could not mark the Windows inheritance pending: …`) have no pt-BR translation, so a Portuguese panel shows them in
English.

## What Changes

- Desktop app: a **Finished** column after **Date** in the Sync page's Runs table, as on the web panel
  (`08:41 AM · 4m38s`, `08:41 · 4m38s` in pt-BR; the date too when the run ended on another day; `—` while it goes
  on); pt-BR "Término".
- CLI: `ad-manager sync list` gets a `FINISHED` column with the end (24-hour, the date too on another day) and the
  duration (`08:41 · 4m38s`).
- `internal/i18n`: pt-BR for the three notes (and the web panel's exported copy).

## Capabilities

### Modified Capabilities

- `app-desktop`: the Sync page shows each run's end and duration.
- `folder-sync`: `sync list` shows each run's end and duration.
- `language`: the inheritance notes of a sync run are translated.

## Impact

`desktop/page_sync.go`, `desktop/i18n_pt_more.go`, `cmd/sync.go`, `internal/i18n/catalog_pt.go`,
`frontend/src/testdata/i18n-pt.json`, tests; `docs/screenshots/desktop-15-sync.png`. No API change.
