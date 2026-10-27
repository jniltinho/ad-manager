## 1. Proposal

- [x] 1.1 Proposal, design (architecture, gates, parity checklist, trade-offs), tasks and the `app-desktop-wails`
      spec; `openspec validate desktop-wails --strict`.
- [x] 1.2 Codex review of the proposal, four rounds:
      - round 1: 4 blockers and 11 should-fix (streaming, release artifacts, navigation guard, SPA session
        isolation, …);
      - round 2: 1 blocker left and 1 new, plus should-fix;
      - round 3: 1 new blocker (old writes across a re-login) and 3 should-fix;
      - round 4: no blockers, 1 should-fix (dialog instance ownership).

      All applied.

## 2. Phase 0 — spike (gate G1, at most two weeks)

- [x] 2.1 Wails v3.0.0 app (`desktop-wails/`, without the `wails3` CLI: plain `go build`, and bindings called by
      name through `@wailsio/runtime`), pinned to an exact version. Replace its frontend
      with `frontend/` in desktop mode (`vp dev`/`vp build --mode desktop`) through the Taskfile. Confirm hot
      reload through the middleware with the development CSP, and the embedded production build.
- [x] 2.2 Asset-server middleware proxying `/api/` to the all-in-one test panel (`domain.local`): sign-in, Users,
      a multipart CSV and logo upload. Measure the bridge's request and response limits and buffering on
      WebKitGTK 6.0, WebView2 and WKWebView, and set the cap.
- [x] 2.3 `Files.SendToPanel` and `Files.SaveFromPanel` (Go tests with 200 MiB; Linux smoke; Windows and macOS in
      milestone 3's CI): hashes,
      progress, cancellation and a throttled link.
- [ ] 2.4 Native navigation guard prototype on the three engines (WebView2 `NavigationStarting`/
      `NewWindowRequested`, WebKitGTK `decide-policy`, WKWebView delegate). Choose an upstream WEP/PR or a carried
      patch. Record whether another origin can call bound methods. (Linux done in milestone 0; Windows and macOS
      need the carried patch, task 4.5.)
- [x] 2.5 One bound service called from the SPA (`Shell.Version`, read back by the smoke build). The server build
      driven by Playwright moves to task 5.3.
- [x] 2.6 Write the G1 result in `design.md` (go / no-go); stop here on no-go.

## 3. Phase 1 — frontend prep (web unchanged)

- [x] 3.1 `saveFile` and `sendFile` in `src/util.js`, with the file-limit check before dispatch (several
      files and fields, at the limit and the limit + 1). Move `downloadCsv`, `Backup.vue`, `GpoListDialog.vue`,
      `QuarantineDialog.vue`, `ImportExport.vue` and `SendBackupDialog.vue` to them. Unit tests for the web
      path.
- [x] 3.2 Session generation in `api.js`:
      - `apiRaw` for the raw call sites (`GpoListDialog`, `ImportExport` CSV import, the `Sync` log);
      - `StaleSession` ignored by every `catch`, `loadMe()` included;
      - a session-change hook that closes `PanelDialogs` and own-password dialogs and pending confirmations, and
        stops pollers (the `Shares` follower, also on unmount).

      Tests: a late answer, a late 401, an open confirmation and a running follower from an earlier session change
      nothing.
- [x] 3.3 Target guards (latest-request token) on Sync; Shares had its own (Users and Groups filter locally).
      Confirmation ownership: resumed actions re-check their captured target (Sync delete and stop, Shares
      unpublish). Test: `latest()` drops a replaced answer (`api.test.js`); the page flows are in the e2e (5.3).
- [x] 3.3a Dialog instance ownership for completion callbacks: Vue 3 drops the `emit` of an unmounted component
      (`runtime-core` `emit`: `if (instance.isUnmounted) return`), and every form dialog is mounted with `v-if`, so
      a closed dialog A cannot reach dialog B. No code change.
- [x] 3.4 `panelOrigin()` for links built for outside use (`PasswordDialog.vue`); the web keeps
      `location.origin`.
- [x] 3.5 `vite.config.ts` by mode: desktop out dir `../desktop-wails/frontend/dist`, `WAILS_VITE_PORT`,
      `strictPort`, HMR host and port, no `/api` dev proxy, web-only plugins off. Add the `dev:desktop` and
      `build:desktop` scripts.
- [x] 3.6 (main.js part done in milestone 0; the catalog glob and `App.vue` route come with milestone 2) `main.js` imports `./desktop/index.js` only in desktop mode. `i18n.js` excludes
      `locales/pt-BR/desktop.js` from the eager glob. `App.vue` renders the unauthenticated `/configure` route
      when the desktop asks.
- [x] 3.7 Web-bundle checks: no sentinel, no `@wailsio`, no `src/desktop/` module and no desktop catalog in
      `vp build`. The web Playwright suite stays green.

## 4. Phase 2 — desktop backend and screens (gate G2)

- [x] 4.1 `desktop-wails/` Go module, `main.go`, Makefile (no Taskfile or `wails3` CLI: plain `go build`),
      `.gitignore`, README. The lockstep test of the Wails and `@wailsio/runtime` versions comes with task 5.2.
- [x] 4.2 `internal/config`, from the archived `config.go` rules: lookup, legacy keys, `0600`, atomic save, agent
      tokens removed, unknown keys and sections kept, `[appearance]`, `[graphics] safe`. With tests.
- [x] 4.3 `internal/panel`. With tests.
      - Routing: `/api/` to the proxy; `/wails/` untouched; history fallback for `GET`/`HEAD`.
      - Proxy: https only, pin, redirects refused, decoded-path confinement, a cookie jar in Go,
        `Set-Cookie`/`Cookie` stripped, cancel and generation header on login/logout, the generation required on
      requests (409 on mismatch, checked under the jar lock), bridge cap, phase timeouts.
      - CSP by mode.
- [x] 4.4 Services `Config` (with test ids and cancel), `Files` (`SaveText`, `SaveFromPanel`, `SendToPanel`,
      `0600`, progress, cancel) and `Shell.OpenExternal` (allowlist). With tests, including log capture without
      secrets.
- [ ] 4.5 The native navigation guard on the three engines, upstream or as a carried patch with a test. Linux is
      done (`guard_linux.go`, exact `wails://localhost` origin, installed on a blank page before the app loads,
      fail closed). Windows and macOS need a change to Wails (it keeps the WebView2 controller and the WKWebView
      delegate private): an upstream PR or a carried patch, which needs the operator's go-ahead. **G2 stays closed
      until then.**
- [x] 4.6 `frontend/src/desktop/`:
      - bootstrap: `Config.Load` before mount; user, language and theme precedence;
      - the Configure screen and its certificate card;
      - Appearance and Graphics (Graphics hidden on macOS);
      - the desktop side of `saveFile`/`sendFile`;
      - the SPA navigation guard and external links;
      - pt-BR strings in `locales/pt-BR/desktop.js`.
- [x] 4.7 Window 1280×800 (min 800×600) with bounds in the private `desktop-wails/` folder; single instance;
      `--safe-gpu` on Linux (the workaround preset) and Windows.

## 5. Phase 3 — build, CI, numbers (gates G3, G4)

- [x] 5.1 `desktop-wails/docker/Dockerfile` (Ubuntu 24.04, pinned by digest) and `scripts/go-docker.sh`. Linux
      `production` build, Windows `CGO_ENABLED=0` cross-build, and the archives.
- [x] 5.2 Make targets `desktop-wails` and `desktop-wails-check` (not in `make build`); the version lockstep test
      (`lockstep_test.go`). `desktop-wails-e2e` comes with task 5.3.
- [ ] 5.3 Test server build: loopback, `Host` check, per-run token, throwaway config. Playwright e2e on Chromium
      and WebKit against the mock panel, with cookie-store and response-header checks. The live config for the
      all-in-one image.
- [ ] 5.4 (written; checked by its first runs) `.github/workflows/desktop-wails-ci.yml`, a workflow of its own: path-filtered, not called by `ci.yml`
      or `release.yml`, artifacts named `desktop-wails-preview-*`. Native smoke with the `smoke` tag on Ubuntu,
      `windows-latest` and `macos-14` (universal). The shipped-binary check (no listener, no dev assets, no test
      adapters).
- [ ] 5.5 `desktop-wails/bench/run.sh`: the current Rust binary re-measured in the same run; PSS and CPU over all
      webview processes; painted readiness. Report in `desktop-wails/docs/benchmark.md`, each budget marked pass,
      fail or waived.
- [x] 5.6 nfpm `.deb` (`libgtk-4-1, libwebkitgtk-6.0-4`) and `.rpm` (`gtk4, webkitgtk6.0`).
- [ ] 5.7 G3 runs: Ubuntu 24.04, Debian 13, Fedora 40+, Windows 11, Windows Server 2022 with WebView2, macOS 14, and
      GPU-less Linux and Hyper-V VMs with painted-frame checks. Write the G3 and G4 results, and any written
      waivers, in `design.md`.

## 6. Phase 4 — preview (gate G5)

- [ ] 6.1 Parity checklist of `design.md` complete, each row pointing to its test.
- [ ] 6.2 Operator uses preview builds for at least 4 weeks; issues fixed or recorded.
- [ ] 6.3 `docs/desktop.md` gets a "Wails preview" section (install deps, `--safe-gpu`, known limits).
- [ ] 6.4 Decision recorded: switch (open the switch change) or stop (rollback in `design.md`).

## 7. Validate

- [ ] 7.1 `make check && make test`; `make desktop-check` still green (Rust untouched); `make desktop-wails-check`.
- [ ] 7.2 Codex review of the implementation of each phase (`codex exec --sandbox read-only …`).
