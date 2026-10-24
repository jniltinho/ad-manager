## 1. Agent

- [x] 1.1 `shares.IsAdminShare`; `SyncJob.Kind` (`folder`/`server`) and its validation (no share, no path, no
      destination, copy only)
- [x] 1.2 `serverTargets`: source shares (worker listing) × this server's registry shares → folder or skip reason
      (not published, nested, invalid destination)
- [x] 1.3 The per-share loop in `handleSync` (one tree at a time: lock, inheritance mark, worker, inheritance,
      quarantine), the summed result and notes; a part suffix for each share's unit, run folder and quarantine
- [x] 1.4 `browse` with `targets: true`
- [x] 1.5 Tests: validation, targets (admin, not published, nested, invalid), result sum

## 2. Panel and clients

- [x] 2.1 `openapi.yaml`: the job's `kind`, browse's `targets` and its answer
- [x] 2.2 Web wizard and jobs table; pt-BR
- [x] 2.3 Rust desktop wizard and jobs table; pt-BR; fixtures and UI test

## 3. Validate

- [x] 3.1 `make check && make test`; `make -C desktop-rust check`
- [x] 3.2 All-in-one container: a server job against a second SMB server or the container itself
- [x] 3.3 Codex review; screenshots of the wizard step if it changed visibly
