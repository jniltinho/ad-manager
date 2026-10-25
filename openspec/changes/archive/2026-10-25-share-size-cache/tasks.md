## 1. Measure (internal/shares)

- [x] 1.1 Replace `Manager.Sizes` with a per-share measure `Manager.Size(ctx, path) (Result, error)`: resolve the path
      (`filepath.EvalSymlinks`), run `nice -n 19 ionice -t -c 3 du -s -b -P -- <resolved>` (falling back without
      `nice`/`ionice` when not installed, logged once), stdout and stderr read apart and bounded to 4 KiB each (the
      rest drained), total parsed even on a non-zero exit (partial), `WaitDelay` 5 s.
- [x] 1.2 Unit tests with a fake runner: exit 0, non-zero exit with a total (partial), non-zero without a total,
      context timeout, missing `ionice`, an error flood (bounded memory), a symlinked share path.

## 2. Job and cache (internal/agent)

- [x] 2.1 New `internal/agent/sizes.go`: the cache type (memory + `<state dir>/share-sizes.json`, version 1, atomic
      write 0600 after each share), load at start (missing/corrupt/unknown version → empty), complete value kept
      apart from `partial_bytes`/`error`/`attempted_at`, prune of removed shares after a full scan, path change drops
      the old entry.
- [x] 2.2 The job goroutine, started in the agent's `Run` under its context: first full scan after 1–5 min when the
      last full scan is older than 4 h, then `end + 4h ± 10%` (also after an empty scan); per-share 30-min timeout;
      one `du` at a time (full, manual and short scans share the goroutine); short scan after a completed
      `share:create` task that does not move `full_scan_ended_at`; progress (`kind`, `started_at`, `done`, `count`,
      `current`).
- [x] 2.3 `GET /api/sizes`: snapshot under a short lock, no `List`; `refresh=1` starts a full scan and returns;
      response with `sizes`, `at`, `shares`, `total` (nested/aliased by `real_path`, only shares with a value hide
      inner ones), `total_complete`, `oldest_at`, `scanning`, `next_at`, top-level `error` of a failed listing.
      Remove `muSize`/`sizes`/`sizesAt`.
- [x] 2.4 Tests: cache round-trip and corrupt file, scheduling with an injected clock (startup delay, interval from
      the end, empty scan, short scan does not move the schedule), refresh while scanning, request cancel does not
      cancel the scan, shutdown mid-scan, failure and partial keep the complete value, reads never start a scan,
      nested/equal/unmeasured-outer totals, path change, retargeted symlink, `oldest_at` over all entries.

## 3. Panel server

- [x] 3.1 `internal/server/openapi.yaml`: response schema, summary and `refresh` description of
      `/api/agents/{id}/sizes`; `TestOpenAPICoversRoutes` stays green; check it at `/swagger`. The proxy's 2-minute
      entry for `/api/sizes` stays (older agents).
- [x] 3.2 New server messages (if any) in `internal/i18n` and `UPDATE_I18N=1 go test ./internal/i18n`.

## 4. Web panel

- [x] 4.1 `Shares.vue`: read `shares`/`total`/`total_complete`/`oldest_at`/`scanning`/`at`, matching entries by
      `path`; header "total · scanned …" with the incomplete/old warning and "measuring N/M…"; cell with "≥" for a
      partial-only value and a muted state with tooltip; Refresh non-blocking and disabled while scanning; 5 s polling
      while scanning, stopped on server change and unmount; fallback to `sizes` for an older agent.
- [x] 4.2 pt-BR strings in `frontend/src/locales/pt-BR/*.js`.

## 5. Desktop (Rust)

- [x] 5.1 `pages/shares.rs`: same behavior (`size_text` with state, header, polling via `cx.spawn` while scanning,
      150 s timeout kept for older agents), fallback to `sizes`.
- [x] 5.2 pt-BR strings in `desktop-rust/crates/core/assets/i18n/pt-BR.json`.
- [x] 5.3 Fixture `desktop-rust/crates/core/src/testkit.rs` (`/api/agents/1/sizes`) with the new fields (one stale,
      one partial, one failed, a scan running) and UI tests for the cell, the header and the legacy fallback.

## 6. Validate and document

- [x] 6.1 `make check && make test`; `make samba-up && make test-integration`; `make -C desktop-rust check`. — done: all green (make test-integration against the Docker Samba, empresa.test).
- [x] 6.2 All-in-one container (`docs/docker.md`, `scripts/demo-seed.sh`): fresh agent (no cache), restart with a
      cache, refresh while scanning, a share with an unreadable subtree (partial), a share whose folder is removed
      (error keeps the last value), nested share total, a share created from the panel, an older panel build
      against the new agent.
- [x] 6.3 Retake `docs/screenshots/11-shares.png` and `desktop-rs-08-shares.png` (2×, `classic-blue`, English,
      company "Example Company Ltd", `domain.local`), from the updated fixtures.
- [x] 6.4 CHANGELOG note on older clients (Refresh shows new values after a reload).
- [x] 6.5 `make build`; Codex review of the implementation.
