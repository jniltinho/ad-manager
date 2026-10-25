## Why

The Shares page (web and desktop) shows each share's folder size and a "total N GB". Today the agent measures them
inside the HTTP request (`GET /api/sizes`, `internal/agent/agent.go`): it holds a mutex, runs `du -sb -- <path>` for
every share **one after the other** under a **single 2-minute budget** for all of them, and caches the result in
memory for 5 minutes. On a production file server with tens of GB this is slow and unreliable:

- **Shares silently disappear from the result.** `Manager.Sizes` (`internal/shares/shares.go`) skips a share whenever
  `du` fails. `du` exits non-zero on any unreadable entry (a vanished file, a stale mount, a FUSE/CIFS subtree), even
  though it still prints a usable total, and the whole share is dropped. When the 2-minute budget runs out, every
  share not yet measured fails the same way. The page then shows "—" with no reason.
- **A cancelled request poisons the cache.** The `du` runs under the request's context. If the browser navigates away
  or the panel's proxy timeout (also 2 minutes) fires first, the remaining `du`s are killed, `Sizes` still returns the
  partial map with no error, and the agent caches it as good for 5 minutes.
- **Every page visit can start a full walk** once the 5-minute cache has expired, and all callers queue on the mutex
  for up to 2 minutes; the desktop waits 150 s and the web shows "…" the whole time.
- **The I/O load is unbounded in time**: the walk runs at normal CPU and I/O priority, right when a user opens the
  page.
- **The total double-counts nested shares** (e.g. `Comercial` at `/srv/shares/Comercial` and
  `Contrato com cliente CRIARE` at `/srv/shares/Comercial/Contrato com cliente`).
- **A share path that is a symlink measures the link, not the folder** (`du` does not dereference a symlink
  given on the command line, while Samba serves its target).
- **No measurement time is shown**, so a stale or partial number cannot be told apart from a fresh one.

### The reported "Publico 0 B" vs `du -h` 107G

The Shares table shows `Publico` with path `/srv/shares/Users/Publico` and 0 B; on the server, `du -h` in
`/srv/shares/Publico` reports 107G. Those are **two different paths**. Reading the code, a 0 B value comes from a
`du -sb` that ran to completion on the share's configured path and printed 0 (GNU `du -b` counts file contents, not
directory metadata, so an empty folder is 0 B): every failure (timeout, cancel, permission error, missing folder)
drops the share and shows "—", never "0 B". So the 0 B is most likely the true apparent size of
`/srv/shares/Users/Publico`, and the 107 GB (allocated size, `du -h`) live in `/srv/shares/Publico`, which is not the
path the Samba registry gives the `Publico` share. Several shares under `/srv/shares/Users/…` (Armando, Fabio, Paulo)
also show 0 B, consistent with empty folders created by a shares import ("empty folders, no files") while the data
was copied elsewhere. This points to a configuration issue, not a measurement bug, but it must be confirmed read-only
on the server, comparing both paths' identity and apparent size as the agent sees them — see "Diagnosis" in
`design.md`. The panel lists only registry shares, so a static `[Publico]` section in `smb.conf` with the other path
would also change what SMB clients see.

The proposal still fixes the real measuring defects above, and it makes such cases visible: each share shows when it
was measured and why it could not be.

## What Changes

- The folder-size scan becomes a **background job of the agent**, decoupled from HTTP requests: it runs at start (when
  the cache is older than the interval) and then **every 4 hours, with jitter**, one scan at a time and one share at a
  time, at idle CPU and I/O priority (`nice -n 19`, `ionice -t -c 3`), so at most one `du` exists at any time.
- Each share is measured with its own timeout (30 minutes), on its symlink-resolved path (what Samba serves), with
  bounded output. A `du` that exits non-zero but printed a total gives a **partial** value (a lower bound) with the
  error; the **last complete value** is always kept apart and never overwritten by a partial or failed attempt.
- The result is cached **in memory and on disk** (`<state dir>/share-sizes.json`, atomic write after each share), so
  it survives agent restarts and the page is never empty after an upgrade.
- `GET /api/sizes` answers **immediately from the cache**, without listing shares or running anything. `?refresh=1`
  starts a scan in the background (if none is running) and returns at once. A share created or changed through the
  panel (`share:create` task) is measured right after the task, in a short scan.
- The response keeps `sizes` and `at` for older clients and adds, per share, `path`, `real_path`, `bytes`,
  `measured_at`, `partial_bytes`, `attempted_at` and `error`; plus `total` (nested and aliased shares counted once),
  `total_complete`, `oldest_at`, `scanning` (`kind`, `started_at`, `done`, `count`, `current`) and `next_at`.
- The **web** and **desktop (Rust)** Shares pages show when the last scan ran next to the total, a warning when the
  total is incomplete or old, a muted value with a tooltip for stale, partial or failed sizes, "measuring N/M…" while
  a scan runs (polling the cache every 5 s), and the Refresh button starts a scan without blocking the page.
- The panel's proxy and the desktop keep their long timeouts for `/sizes`, still needed with older agents.
- `internal/server/openapi.yaml` documents the new response and the non-blocking `refresh`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `shares`: a new requirement for cached, background-measured folder sizes and how the panels show them.

## Impact

- Agent: `internal/agent/agent.go` (the `/sizes` handler, start/stop of the job, a short scan after `share:create`),
  a new `internal/agent/sizes.go` (job, cache, persistence), `internal/shares/shares.go` (`Sizes` becomes a per-share
  measure with partial/error).
- Server: `internal/server/openapi.yaml` (the proxy is unchanged).
- Web: `frontend/src/pages/Shares.vue`, `frontend/src/locales/pt-BR/*.js`.
- Desktop: `desktop-rust/crates/ui/src/pages/shares.rs`, `desktop-rust/crates/core/assets/i18n/pt-BR.json`, the
  fixture in `desktop-rust/crates/core/src/testkit.rs`. The older Go desktop (`desktop/page_shares.go`) keeps working
  through the unchanged `sizes` field.
- Older clients against a new agent read `sizes` at once but do not poll: their Refresh shows the new values only
  after a reload (CHANGELOG note).
- Screenshots: `docs/screenshots/11-shares.png` and `desktop-rs-08-shares.png` (rule 9).
- No change to what is written on the file server; the scan only reads metadata.
