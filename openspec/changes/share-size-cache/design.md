## Context

### How it works today

- **Endpoint.** The panel calls `GET /api/agents/{id}/sizes[?refresh=1]` (`internal/server/extras.go`), proxied to the
  agent's `GET /api/sizes` with a 2-minute context and a write deadline of 2 min 15 s (other proxied calls get 15 s;
  the server's normal `WriteTimeout` is 60 s).
- **Agent handler** (`internal/agent/agent.go`, `api.GET("/sizes")`): takes `muSize` for the whole call. If the cache
  is empty, older than 5 minutes or `refresh=1`, it calls `mgr.Sizes(ctx)` under `context.WithTimeout(request ctx,
  2 min)`, then stores `sizes` and `sizesAt`. It answers `{"sizes": {name: bytes}, "at": time}`. The cache is memory
  only, keyed by share name (not path); a restart starts empty.
- **Measure** (`internal/shares/shares.go`, `Manager.Sizes`): lists the shares from the Samba registry (`net conf
  list` or the Python bridge, plus `adminsName`), then for each share with a path, serially, `du -sb -- <path>` through
  `shares.Exec` (`exec.CommandContext`, stdout and stderr combined and fully buffered). Any error (non-zero exit,
  context deadline, parse error) skips the share; the function never returns a per-share error.
- **What `du -sb` walks**: the whole tree; apparent size in bytes (`-b` = `--apparent-size --block-size=1`), which
  with GNU coreutils counts file contents but not directory metadata, so an empty folder is 0 B on any filesystem, and
  which differs from the allocated size `du -h` shows; hard links counted once per invocation (but again across
  shares); symlinks inside the tree not followed; the path argument itself not dereferenced (a share path that is a
  symlink measures the link); mount points crossed (no `-x`).
- **Privileges**: the agent runs as root (`deploy/ad-manager-agent.service`) with `ProtectHome=yes` (`/home`, `/root`
  hidden) and `ProtectSystem=full`. Permission errors are therefore rare, but `du` still exits 1 on entries that
  vanish during the walk or on unreadable network/FUSE mounts, and the share is dropped although `du` printed a
  total.
- **Concurrency**: one measure at a time (mutex); concurrent requests wait. No nice/ionice.
- **Clients**: `Shares.vue` loads the list, then the sizes; `bytes(undefined)` shows "…" while measuring and "—"
  after; `total` sums every value. `desktop-rust/.../shares.rs` (`load_sizes`, `size_text`) does the same with a
  150 s timeout. The Go desktop (`desktop/page_shares.go`) reads `sizes` too.

### Diagnosis of "Publico 0 B" (read-only, to run on the server)

The code path cannot turn a failure into 0: every failure leaves the share out ("—"). A 0 B means a `du -sb` completed
on the path the registry gave `Publico` and printed 0 — an empty folder, or one holding only empty files. (The only
other source would be the name-keyed 5-minute cache holding a 0 from a previous path of the share, which expires.)
The 107G is from `/srv/shares/Publico`, another path, measured as allocated size (`du -h`). Before concluding that the
share points to the wrong folder, compare like with like, inside the agent's own mount namespace, without writing
anything:

```bash
net conf showshare Publico                       # the registry share (what the panel lists)
testparm -s --section-name=Publico 2>/dev/null   # the effective definition smbd serves
grep -n -A6 '^\[Publico\]' /etc/samba/smb.conf   # a static section would shadow the registry one
stat -c '%n dev=%d ino=%i type=%F' /srv/shares/Users/Publico /srv/shares/Publico   # same folder? symlink?
findmnt -T /srv/shares/Users/Publico; findmnt -T /srv/shares/Publico
ls -la /srv/shares/Users/Publico | head
pid=$(systemctl show -p MainPID --value ad-manager-agent)
nsenter -t "$pid" -m du -sb -- /srv/shares/Users/Publico /srv/shares/Publico   # as the agent sees them
```

If the registry path is `/srv/shares/Users/Publico`, it is a distinct, empty folder, and smb.conf has no `[Publico]`
section, the 0 B is correct and SMB clients of `\\sambadc01\Publico` see an empty share — likely left by a shares import
("empty folders, no files") while the data was copied to `/srv/shares/Publico`. Fixing the path is an operator
decision (edit the share), out of scope here.

**Result (read-only, run on the file server):** the registry share and smbd's effective definition both say
`path = /srv/shares/Users/Publico`; smb.conf has no static `[Publico]` section; `/srv/shares/Users/Publico` and
`/srv/shares/Publico` are distinct directories (different inodes, same ext4 filesystem); the share's folder holds no
files (two empty subfolders) and `du -sb` inside the agent's mount namespace prints 0. So the panel's 0 B is correct:
the data sits in `/srv/shares/Publico`, which no share serves under that name. The fix is an operator edit of the
share's path, not a change of the size code.

## Goals / Non-Goals

**Goals**

- Sizes appear instantly on the Shares page, with the time they were measured.
- Measuring never blocks a request, never runs twice at once, and loads the file server as little as possible.
- A failed or partial measure is visible per share, with its reason, and never erases the last complete value.
- The cache survives agent restarts.

**Non-Goals**

- Quotas, per-user usage, or history/graphs of growth.
- Incremental scanning (inotify/fanotify) or filesystem-specific accounting (btrfs qgroups, ZFS `used`): `du` stays
  the single, portable measure. Possible follow-up if a 30-minute share timeout is not enough.
- Confining measured paths to the agent's share roots: today every registry share is measured, wherever it is (a DC
  may serve folders outside the roots); this change keeps that. The walk only reads metadata, and only a number is
  returned to authenticated panel users.
- Changing or fixing share paths.
- Changes to the older Go desktop: it keeps reading `sizes`.

## Decisions

### D1. An in-process job, not an asynq task

The scan runs in a goroutine owned by the agent (started in the agent's `Run`, stopped by its context), not in the
asynq default queue. That queue is serial and shared with share tasks, folder tasks and backups (syncs have their own
queue); a long `du` there would delay user-visible work, and a read-only measurement needs neither Redis persistence
nor retries. *Alternative:* an `asynq.Scheduler` entry — rejected for the reason above.

### D2. Schedule: every 4 hours with jitter, counted from the end of the previous full scan

- At start, the agent loads the disk cache. If the last **full** scan ended more than 4 h ago (or never), the first
  full scan starts after a random delay of 1–5 minutes (lets smbd/winbind settle and spreads agents that restart
  together). Otherwise the next full scan is due at `full_scan_ended_at + 4h ± 10%`.
- After a full scan ends, the next one is due at `end + 4h ± 10%` (uniform jitter). Counting from the end means a scan
  that takes longer than the interval never overlaps the next one.
- A full scan that finds no share still records its end, so an empty server does not rescan in a loop.
- Short scans (D6) never move `full_scan_ended_at` nor the next full scan.
- `next_at` is exposed in the API.
- The interval and timeouts are constants (`sizeInterval = 4 * time.Hour`, `sizeShareTimeout = 30 * time.Minute`);
  no configuration knob until someone needs it.

### D3. One scan at a time, one share at a time, idle priority

- A single job goroutine runs every scan (full, short or manual), so at most one `du` process exists at any time.
  `refresh` while a scan runs does nothing (the running scan is the answer). Requests only read a snapshot under a
  short lock, never during `du`.
- Shares are measured serially (concurrency 1): the walk is metadata I/O on the same disks, so parallel `du`s mostly
  add seeks.
- Each `du` runs as `nice -n 19 ionice -t -c 3 du -s -b -P -- <resolved path>` (idle CPU and I/O class). `ionice -t`
  runs `du` even when the I/O class cannot be set; the class is honored by the BFQ/CFQ schedulers and ignored by
  `mq-deadline`/`none`, where `nice` still lowers CPU. If `nice` or `ionice` is not installed, the agent logs it once
  and runs `du` without it. *Alternative:* a Go `filepath.WalkDir` with a token-bucket rate limit — more code, slower
  than `du`, and still needs hard-link bookkeeping; rejected.
- **Symlinks.** Samba serves the target of a share path that is a symlink, so the agent resolves the share path with
  `filepath.EvalSymlinks` first and passes the resolved path to `du`, which then follows nothing (`-P`): symlinks inside
  the tree are not followed (no loops, no double counting) and the resolved path is recorded (`real_path`). Mount points
  are still crossed: a disk mounted inside a share is part of what clients see. A hung network mount is bounded by the
  per-share timeout (D4).

### D4. Per-share timeout, cancellation, output, partial results and errors

- Each share gets `context.WithTimeout(jobCtx, 30m)`; `jobCtx` is derived from the agent's lifetime, **never** from a
  request. On timeout or shutdown, `du` is killed (SIGKILL through `exec.CommandContext`); `WaitDelay` (5 s) stops
  waiting for its output pipes.
- A `du` stuck in uninterruptible I/O (a dead NFS/CIFS server) may not exit even after SIGKILL. The job then keeps
  waiting for it and starts **no** other `du` (no pile-up of walks); the API keeps reporting the scan as running with
  `current` and `started_at`, so the stall is visible. On agent shutdown the agent does not wait for it: systemd's stop
  timeout ends the service.
- Output is bounded: stdout is one line (`<bytes>\t<path>`), read up to 4 KiB; stderr is read up to 4 KiB and the rest
  is drained and discarded, so an error flood (millions of unreadable entries) cannot grow memory.
- Outcomes:
  - exit 0 with a total → a **complete** value: `bytes`, `measured_at = now`; clears `partial_bytes` and `error`;
  - non-zero exit **with** a parsable total on stdout (e.g. `du: cannot read directory …`; `du` prints the total
    even after errors) → `partial_bytes` = that total (a lower bound), `error` = the first stderr line; the last
    complete `bytes`/`measured_at` are kept;
  - timeout, shutdown, missing folder or no total → `error` (`timed out after 30m`, `cancelled`, the stderr line);
    everything else is kept.
  - Each attempt sets `attempted_at`.
- Errors are logged (share name and message, no secrets) and never fail the whole scan.

### D5. Cache: memory plus `<state dir>/share-sizes.json`

- Shape on disk (and in memory):
  `{"version":1, "full_scan_ended_at":…, "shares":{"<name>":{"path":…, "real_path":…, "bytes":…, "measured_at":…,
  "partial_bytes":…, "attempted_at":…, "error":…}}}` — `bytes`/`measured_at` and `partial_bytes` absent when unknown.
- Written after each share completes (atomic: temp file in the same directory, `fsync`, `rename`, mode 0600), so a
  crash or restart mid-scan keeps the progress. The file holds only share names, paths and numbers — no secrets.
- Loaded at agent start; a missing, unreadable or unknown-version file means an empty cache (logged, not fatal).
- Each scan starts from the current share list (`Manager.List`); entries of shares that no longer exist are pruned at
  the end of a full scan. An entry whose `path` differs from the share's current path, or whose `real_path` differs
  from the newly resolved one (a retargeted symlink), loses its values before the share is measured again (the old
  number belongs to another folder), so a failed attempt never keeps another folder's bytes.
- Lifetime: no expiry. A value is "stale" for display when `measured_at` is older than 4 h + 2 h of slack (6 h); it is
  still shown, muted.

### D6. New and changed shares

The agent runs the share tasks itself. When a `share:create` task (which also updates an existing share) completes,
the job measures that share in a **short scan** — queued behind a running scan, never in parallel — so its size
appears within seconds without waiting up to 4 hours and without a full rescan. A share created or moved outside the
panel (`net conf` by hand) is measured by the next full scan or by Refresh; until then it shows "—" (not measured
yet). Reading the sizes never starts a scan by itself (only `refresh=1` does), so a share that keeps failing is not
retried on every poll.

### D7. API

`GET /api/sizes` on the agent (and `GET /api/agents/{id}/sizes` on the panel) answers from the in-memory snapshot,
without listing the shares or running anything:

```json
{
  "sizes":   { "ADM": 0, "Desenvolvimento": 45097156608 },
  "at":      "2026-10-24T08:12:03-03:00",
  "shares": {
    "Desenvolvimento": { "path": "/srv/shares/Desenvolvimento", "real_path": "/srv/shares/Desenvolvimento",
                         "bytes": 45097156608, "measured_at": "2026-10-24T08:10:41-03:00",
                         "attempted_at": "2026-10-24T08:10:41-03:00", "error": "" },
    "Remoto":          { "path": "/mnt/remoto", "real_path": "/mnt/remoto",
                         "bytes": 1048576, "measured_at": "2026-10-23T20:02:10-03:00",
                         "partial_bytes": 524288, "attempted_at": "2026-10-24T08:40:10-03:00",
                         "error": "du: cannot read directory '/mnt/remoto/x': Input/output error" }
  },
  "total": 45098205184,
  "total_complete": false,
  "oldest_at": "2026-10-23T20:02:10-03:00",
  "scanning": { "kind": "full", "started_at": "2026-10-24T12:00:05-03:00", "done": 12, "count": 40,
                "current": "Financeiro" },
  "next_at": "2026-10-24T16:05:00-03:00"
}
```

- `sizes` and `at` keep their meaning for older clients: `sizes` has every share with a complete `bytes`, `at` is the
  end of the last full scan (zero time when none).
- Clients match `shares[name]` to their own share list by `path`; an entry whose `path` differs from the listed share
  is ignored (the agent drops it at the next measure).
- `total` sums, per share, `bytes` (or `partial_bytes` when there is no complete value). A share is skipped when its
  `real_path` is inside, or equal to, the `real_path` of another share **that has a value** (component-wise prefix on
  cleaned absolute paths; equal paths count once), so nested and aliased shares are not counted twice, and an
  unmeasured outer share never hides a measured inner one. Hard links shared between different shares may still be
  counted twice (documented limit).
- `total` and the flags below cover the entries the agent holds (the shares of its last scan). `total_complete` is
  false when any entry has no complete value, a partial value or an error. `oldest_at` is the oldest `measured_at`
  among **all** entries (also nested shares left out of `total`), so the staleness warning covers every share shown.
- The client also treats the total as incomplete when its own share list and the entries disagree: a listed share
  with no entry, an entry whose `path` differs, or an entry for a share not listed (a share created, moved or removed
  since the last scan).
- `scanning` is `null` when idle; `kind` is `full`, `short` or `manual`; `done`/`count` count shares.
- `?refresh=1` starts a full scan if none is running and returns the same body with `scanning` set; it never waits
  for `du`.
- Errors: the endpoint answers 200 from the cache even when the registry cannot be read; a scan that cannot list the
  shares records the error in `error` at the top level of the body.
- The panel's proxy keeps its 2-minute entry for `/api/sizes` and the desktop its 150 s timeout, because a new panel
  may still talk to an older agent that measures inside the request. With a new agent the call returns at once anyway.
  (They can be removed once older agents are no longer supported.)
- `internal/server/openapi.yaml`: the `/api/agents/{id}/sizes` entry gets a response schema (above), the summary
  "Folder size of each share (cached; measured in the background every 4 h)" and a description of `refresh`.

### D8. Web and desktop display

Both Shares pages (web `Shares.vue`, desktop `pages/shares.rs`) behave the same:

- **Size cell**: the complete `bytes` formatted as today. When there is only a partial value: "≥ N". Muted, with a
  tooltip, when the value is stale (> 6 h), or the latest attempt failed or was partial ("measured <time>; latest
  attempt: <error>"). No value at all: "—" with "not measured yet" or the error as tooltip, or "…" while a scan runs.
- **Header**: "total N GB · scanned 10 min ago" (time of the last full scan; absolute time in the tooltip). When
  `total_complete` is false, the share list and the entries disagree (D7), or `oldest_at` is older than 6 h, a
  warning mark whose tooltip says "some sizes are
  missing, partial or older than <oldest_at>". While scanning: "measuring 12/40…" next to the total.
- **Refresh**: reloads the list and calls `?refresh=1`; the page stays usable; the button is disabled while
  `scanning` is set.
- **Polling**: while `scanning` is set, the page re-reads `/sizes` every 5 s (a cheap cache read) and stops when it is
  `null`, on another server, or when leaving the page.
- **Older agent** (no `shares` in the answer): today's behavior.
- New strings go through `t('…')` with pt-BR in `frontend/src/locales/pt-BR/*.js` and the desktop's
  `assets/i18n/pt-BR.json`.

## Risks / Trade-offs

- **A share larger than 30 minutes of `du` never completes** → it keeps its last value with "timed out"; with no
  previous value, "—" plus the reason. Mitigation: the error is visible; raise the constant or move to a filesystem
  accounting follow-up if it happens.
- **A `du` stuck in uninterruptible I/O stalls all scans** until it exits or the agent restarts; visible as a running
  scan with an old `started_at` on one share.
- **Idle I/O class may starve** under constant load (BFQ) → scans take longer but finish when the disk breathes;
  bounded by the per-share timeout.
- **4-hour-old numbers** are shown as fresh enough; a user who just copied 50 GB uses Refresh.
- **Older clients against a new agent** (older web build, older Rust desktop, Go desktop): they read `sizes` once and
  do not poll. They get the cached values at once (better than today); their Refresh starts a background scan but
  shows the previous values until the page is reloaded. Accepted and noted in the CHANGELOG.
- **GET with a side effect** (`refresh=1`): kept as GET for compatibility with the existing route, proxy and clients;
  it is authenticated, a no-op while a scan runs, and only reads the file server.

## Migration Plan

- No data migration: the first start of the new agent finds no `share-sizes.json` and runs a full scan after
  1–5 minutes; until then the page shows "—"/"…".
- Mixed versions: see Risks (older clients) and D7 (a new panel keeps the long timeouts for older agents).
- Rollback: the older agent ignores the JSON file.

## Open Questions

- Interval 4 h and per-share timeout 30 min: right for the user's servers, or should either be a setting?
- Should a scan be skipped while a sync or backup is running on the same agent (less contention), at the cost of
  older numbers?
- Cross mount points (as today) or `-x` (only the share's filesystem)?
- Should the panel warn when a share's folder is empty but a sibling folder with the same name has data (the
  "Publico" case)? Out of scope here; possibly part of a share-path check.
