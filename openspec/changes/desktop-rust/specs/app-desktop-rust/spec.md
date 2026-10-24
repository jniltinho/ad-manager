## ADDED Requirements

### Requirement: Rust desktop talks only to the panel
The Rust desktop app SHALL send every request to the configured panel URL over `https://` only, reach file servers
only through the panel's `/api/agents/{id}/…` proxy, never hold an agent token, and never follow an HTTP redirect.

#### Scenario: Redirect refused
- **WHEN** the panel answers any request with a 3xx redirect
- **THEN** the request fails with "the panel answered with a redirect, which is not followed: check the panel URL"
  and nothing is sent to the redirect target

#### Scenario: Plain HTTP refused
- **WHEN** the operator types `http://panel:8443` in Configure and tests it
- **THEN** the test says "Panel URL: enter an https://server:port URL" and no connection is opened

#### Scenario: File server through the panel
- **WHEN** the Shares page lists the shares of file server 3
- **THEN** the request goes to `{panel}/api/agents/3/shares` with the panel session

### Requirement: Rust desktop certificate pinning
A panel certificate that the system does not trust SHALL be accepted only after the operator trusts its SHA-256
fingerprint in Configure; from then on only that exact certificate (its handshake signed with its key) SHALL be
accepted on every connection. A configured fingerprint SHALL take precedence over everything else.

#### Scenario: Self-signed shown before trusting
- **WHEN** Test reaches a panel whose certificate does not verify and no pin is configured
- **THEN** it shows "Certificate not trusted" with the fingerprint, subject, issuer and validity and a "Trust this
  certificate" button, and does not sign in

#### Scenario: Trusted and pinned
- **WHEN** the operator trusts the certificate, the test signs in and the operator saves
- **THEN** `desktop.toml` gets `fingerprint = "sha256:<hex>"` and later requests succeed with that certificate

#### Scenario: Certificate changed
- **WHEN** the panel presents another certificate than the pinned one
- **THEN** every request fails with "the panel's certificate is not the one trusted in Configure (it changed): check
  it with the panel's administrator, then trust it again in Configure"

#### Scenario: Certificate without its key
- **WHEN** a server presents the pinned certificate but signs the handshake with another key
- **THEN** the connection fails

### Requirement: Rust desktop legacy insecure config
A legacy file with `insecure = true` and no fingerprint (written before pinning existed) SHALL keep accepting any
certificate, the one exception to pinning, until a Save after a new successful test replaces it with a pin.

#### Scenario: Legacy insecure file
- **WHEN** the file has `insecure = true` and no fingerprint
- **THEN** the panel is reached with any certificate; a language-only Save keeps `insecure`; a Save after a new
  successful test of a self-signed panel writes its fingerprint and no `insecure`

### Requirement: Rust desktop sessions and secrets
Every sign-in SHALL start a new session (cookie jar and identity) so that nothing still running for an earlier session
reaches the screens of the next one, and a reply SHALL reach only the dialog instance that asked for it. Typed
passwords SHALL be wiped from memory after use, including the text field's undo history, and SHALL never be logged,
saved or printed.

#### Scenario: Sign out and in again
- **WHEN** the operator signs out while a list request is in flight and signs in again
- **THEN** the late answer of the first session is dropped and the new session has its own identity

#### Scenario: Password field history
- **WHEN** the operator signs in
- **THEN** the sign-in screen's password field, its text and its undo history are gone

#### Scenario: Reply to a closed dialog
- **WHEN** a dialog sends a request, is closed, and another dialog of the same kind is opened before the answer
- **THEN** the answer does not change the new dialog

#### Scenario: Password never printed
- **WHEN** a sync job with a typed password is formatted for debugging
- **THEN** the password shows as `***`

### Requirement: Rust desktop configuration file
The app SHALL read and write the same `desktop.toml` as the Fyne app (keys, legacy keys, lookup order), write it
atomically with mode `0600`, never write the password, refuse to open a file readable by group or others (Unix), and
remove agent tokens of older files on start, saying so once in the status line.

#### Scenario: Shared with the Fyne app
- **WHEN** the Rust app starts with a `desktop.toml` saved by the Fyne app
- **THEN** it uses the same panel URL, user, fingerprint and language

#### Scenario: Loose permissions
- **WHEN** the file has mode `0644`
- **THEN** the app shows "<path> must be private and has mode 0644: run chmod 600" and does not connect

#### Scenario: Old agent tokens
- **WHEN** the file lists `[[agents]]` with tokens
- **THEN** the file is rewritten without them and, after sign-in, the status line says "File-server tokens are no
  longer used: they were removed from the configuration file."

#### Scenario: First run
- **WHEN** no file exists
- **THEN** the Configure screen opens with user `administrator`, and nothing is written before a successful test and
  Save

### Requirement: Rust desktop Configure
Configure SHALL test by reading the certificate, the panel health and signing in with the account; Save SHALL be
enabled only after a successful test of the values on screen (or, signed in, when only the language changes) and SHALL
open the app with that session without asking the password again.

#### Scenario: Edit after a test
- **WHEN** a test succeeds and the operator then edits the URL, user or password
- **THEN** Save is disabled and the tested session is signed out

#### Scenario: Test finishing after an edit
- **WHEN** the operator edits a field or cancels while a test is running, and the test then succeeds
- **THEN** its result is not shown, Save stays disabled, and its session is signed out (`POST /api/logout`)

#### Scenario: Panel in setup
- **WHEN** the panel answers `/api/health` with `setup: true`
- **THEN** the test fails with "the panel is being set up: finish its setup wizard in a browser first"

#### Scenario: Language only
- **WHEN** the operator, signed in, opens Configure from the header, changes only the language and saves
- **THEN** the session stays and the interface switches language

### Requirement: Rust desktop sign-in
The sign-in screen SHALL show the panel's public brand (theme, domain, NETBIOS, company logo or the AD Manager logo),
sign in with `/api/login` then `/api/me`, show "Signing in…" and errors under the box (at most 4 lines), keep the
typed user across a rebuild, clear the password after success, and apply the session's theme and language.

#### Scenario: Wrong password
- **WHEN** the panel refuses the sign-in
- **THEN** the panel's message (translated) shows under the box and the password field keeps focus

#### Scenario: Session expires
- **WHEN** any request answers 401 after sign-in
- **THEN** the app returns to the sign-in screen

#### Scenario: Broken company logo
- **WHEN** `/api/brand/logo` is not a decodable PNG, JPEG or SVG, or is larger than 2 MiB
- **THEN** the AD Manager logo is shown

### Requirement: Rust desktop pages at parity
The app SHALL offer the pages Home, Users, Groups, OUs, Computers, Shares, Backup, Sync and Logs with the actions,
dialogs, filters and messages of the Fyne app listed in the parity matrix of this change.

#### Scenario: Home alerts
- **WHEN** the policy has no complexity, 2 accounts are locked and 3 are inactive
- **THEN** Home shows the policy alert with "see ›" (opening the policy dialog over Users), the locked alert with
  "unlock ›" and the inactive alert with "review ›" (both going to Users)

#### Scenario: Users filter
- **WHEN** the operator picks "Locked" and types "ana"
- **THEN** only locked accounts whose login, name, e-mail or department contain "ana" are listed and the count reads
  "n of total"

#### Scenario: Groups protected
- **WHEN** a built-in group is selected
- **THEN** Delete is disabled and the group shows the "system" tag

#### Scenario: OU delete guard
- **WHEN** a default container or Domain Controllers is selected
- **THEN** Delete is disabled

#### Scenario: Computers DC guard
- **WHEN** a domain controller is selected
- **THEN** Disable, Move and Delete are disabled

#### Scenario: Share task
- **WHEN** the operator creates a share
- **THEN** a `share:create` task is queued on that server, the status line says "queued", then "done" or the error
  in a dialog, and the list reloads if the same server is still on screen

#### Scenario: Stale server answer
- **WHEN** the operator switches the Shares or Sync server while a list request is in flight
- **THEN** the late answer is dropped, and the list and selection of the previous server are cleared at the switch

#### Scenario: Action confirmed after a server switch
- **WHEN** the operator asks to remove a share of server A, switches to server B, then confirms
- **THEN** nothing is sent and the dialog says "The list changed servers; refresh and try again."

#### Scenario: Share form payload
- **WHEN** the operator saves the edit form of a share
- **THEN** a `share:create` task is sent with the same name and path (not editable), the comment, the six group and
  user lists (comma-separated, trimmed), and the hidden, recycle, ABE, audit and create-folder flags

#### Scenario: Backup delete typed
- **WHEN** the operator deletes a backup
- **THEN** the confirmation asks for the last 8 characters of its id and a wrong text sends nothing; for a full data
  backup it also says "The incremental backups of this chain go too."

#### Scenario: DC backup not restored over a DC
- **WHEN** the operator asks to restore a domain-controller backup
- **THEN** only an explanation is shown (restore it in the initial setup of a new server) and nothing is sent

#### Scenario: Data restore defaults to a new folder
- **WHEN** the operator restores a data backup without changing "Where"
- **THEN** the request has `target: "new"` and no typed code is asked; choosing the original folders adds the typed
  code field and sends `target: "original"` with it

#### Scenario: Destructive restore typed
- **WHEN** the operator restores a configuration, Samba-files or panel backup
- **THEN** the request carries the typed `confirm` code (the panel refuses a wrong one), and a panel restore says to
  sign in again in a few seconds

#### Scenario: Mirror needs the word
- **WHEN** the sync wizard saves a mirror job without "MIRROR" typed
- **THEN** nothing is saved and the dialog says "A mirror deletes in the destination. Type MIRROR to confirm"

#### Scenario: Samba logs lazily
- **WHEN** the Logs page opens
- **THEN** no Samba-log request is sent (the audit and the file-server list are); a Samba tab requests its files the
  first time it is opened

#### Scenario: Sync edit keeps the password
- **WHEN** the operator edits an NTLM sync job and leaves the password empty
- **THEN** the job is saved without a password (the stored one is kept); a Kerberos job is saved with no password
  file

#### Scenario: New user with groups
- **WHEN** a new user is created with three groups and adding one of them fails
- **THEN** the user exists, the status says it was created, and a dialog lists the group that failed with its reason

### Requirement: Rust desktop Users row actions
The Users table SHALL act on the account of the row that was clicked (login → edit, editable status → toggle with a
confirmation to disable, unlock on a locked account, password, copy, move and delete columns with move/delete only on
editable accounts), never on the selected row; Enter on the selected row SHALL open a menu of its actions; at the
default 1280 px window the action links SHALL show only icons with their name in the status line on hover.

#### Scenario: Selected row is not the target
- **WHEN** row A is selected and the operator clicks delete on row B and types B's login
- **THEN** account B is deleted and A is untouched

#### Scenario: Own and built-in accounts
- **WHEN** a row is the operator's own login (any case) or a built-in account
- **THEN** its status is plain text and its move and delete cells are empty

#### Scenario: Keyboard menu
- **WHEN** a row is selected and the operator presses Enter
- **THEN** a menu with Edit, Password, Copy (+ Unlock when locked, + Disable/Enable, Move, Delete when editable)
  opens for that row

### Requirement: Rust desktop i18n and themes
The app SHALL use English source strings with the pt-BR catalog, translate server messages with the panel's
catalog, and color the interface with the panel theme's roles (18 themes, flat look).

#### Scenario: Every string translated
- **WHEN** the unit tests run
- **THEN** every `t("…")`/`tf!("…")` literal in the sources has a pt-BR entry

#### Scenario: Catalog after a fast failure
- **WHEN** the sign-in fails before the panel's catalog has arrived, and the catalog arrives afterwards
- **THEN** the error on screen is shown translated

#### Scenario: Catalog unavailable
- **WHEN** `/api/i18n` fails or is malformed
- **THEN** server messages stay in English and the app works normally

#### Scenario: Server message in Portuguese
- **WHEN** the language is pt-BR and the panel answers an error whose text is in the panel catalog (exact, by pattern
  or by ": " parts)
- **THEN** the dialog shows the Portuguese text

#### Scenario: Themes in step
- **WHEN** the unit tests run in the monorepo
- **THEN** `assets/themes.json` has the same themes and role colors as `desktop/themes_gen.go`

#### Scenario: Accessible colors and focus
- **WHEN** the unit tests run
- **THEN** in every theme the text roles the pages use keep the theme's minimum contrast on their backgrounds, and the
  focus ring is drawn at the theme's width (3 px on Crystal)

#### Scenario: Unknown theme
- **WHEN** the panel reports a theme id the app does not know
- **THEN** the app uses Default

### Requirement: Rust desktop crash-safe graphics start
The app SHALL plan its graphics backend before the window exists: a `gpu-starting` marker that no running instance
holds SHALL move the start to the next safer backend of the platform's ladder, otherwise the backend saved in
`graphics.toml` applies; `--safe-gpu` and `WGPU_BACKEND` override the plan. The marker SHALL be removed after 3
completed frames. The marker, its lock file and the saved window SHALL live in a private state folder that is
refused when it is a link or open to others.

#### Scenario: A start that died
- **WHEN** the last start on OpenGL (Windows) left a marker that no process holds
- **THEN** this start uses DX12, the sign-in screen says "The graphics driver stopped the last start (gl); now using
  dx12. Change it in Configure.", and once 3 frames painted `graphics.toml` gets `gpu_backend = "dx12"` and the
  marker is removed, desktop.toml untouched

#### Scenario: Another instance starting
- **WHEN** another process holds `gpu-starting.lock` while this instance starts
- **THEN** this start does not treat it as a crash, does not write or remove the marker, and uses the saved backend

#### Scenario: Forced by the environment
- **WHEN** `WGPU_BACKEND` is set, also when a marker of a start that died is there
- **THEN** wgpu's choice is used, nothing is saved or announced, and a start that dies under it moves no rung

#### Scenario: State folder open to others
- **WHEN** the `desktop-rs` state folder beside desktop.toml is a link, or has mode 0755 on Unix
- **THEN** the app starts without crash protection and without a saved window, writes nothing in it, and says "The
  graphics start protection is off: …" on stderr and on the sign-in screen

#### Scenario: Software is never saved silently
- **WHEN** a crash moves the start down to the software rung
- **THEN** the launch prefers a software adapter and `graphics.toml` is not changed

#### Scenario: Safe start
- **WHEN** the app is started with `--safe-gpu`
- **THEN** it prefers software rendering (WARP, llvmpipe/lavapipe; the glow renderer on macOS) and saves nothing

#### Scenario: Ladders
- **WHEN** the unit tests run
- **THEN** `next` walks Windows gl → dx12 → vulkan → cpu, Linux vulkan → gl → cpu and macOS auto → gl → cpu, `auto`
  counts as the ladder's first rung, and a backend not on the ladder goes to cpu

#### Scenario: Adapter choice
- **WHEN** the unit tests rank adapters for an explicit backend
- **THEN** only surface-compatible ones count; a hardware OpenGL one comes first, then discrete > integrated > virtual
  > other > CPU; in software a CPU adapter comes first, the output-owning WARP entry before the other, and with no
  software adapter the first usable one is picked so the window opens

#### Scenario: Graphics changed in Configure
- **WHEN** the operator, signed out and offline, changes only Graphics in Configure and saves
- **THEN** `graphics.toml` is written without a panel test, the screen says "Applies at the next start." and
  desktop.toml is unchanged

#### Scenario: WARP renders
- **WHEN** the Windows CI job runs the WARP smoke test
- **THEN** the software DX12 adapter is found as a CPU device and renders a 64×64 frame read back with the expected
  color

### Requirement: Rust desktop window placement
The window SHALL open at 1280×800 (minimum 800×600) and be restored where the operator left it by eframe, which also
keeps it on a monitor; the app SHALL never move it itself, and a window that cannot be opened SHALL end with a clear
error.

#### Scenario: Restored
- **WHEN** the app starts again after the operator moved and resized the window
- **THEN** it opens where and as large as it was (eframe's persisted window)

#### Scenario: Missing library
- **WHEN** `libxkbcommon-x11` is missing on Linux
- **THEN** the app exits with "cannot open the window: libxkbcommon-x11.so.0 is not installed" instead of a panic

#### Scenario: Window not created
- **WHEN** `run_native` fails
- **THEN** the error says the next start tries a safer graphics backend, and the marker is kept for it

### Requirement: Rust desktop isolated CI
The Rust desktop SHALL have its own CI, run only for pull requests and pushes to `main` that change `desktop-rust/`,
its workflow files or the Fyne sources its asset checks read (`desktop/themes_gen.go`, `desktop/i18n_pt_*.go`),
without changing or calling `ci.yml`, which keeps running on its own triggers.

#### Scenario: Server-only change
- **WHEN** a pull request changes only `internal/`
- **THEN** the Rust desktop CI does not run

#### Scenario: Rust desktop change
- **WHEN** a pull request changes `desktop-rust/src/main.rs`
- **THEN** the Rust desktop CI runs fmt, clippy with warnings as errors, the tests and a release build

#### Scenario: WARP on Windows
- **WHEN** the Rust desktop CI runs
- **THEN** a `windows-latest` job builds the app natively and runs the WARP render smoke test

#### Scenario: Palette change in the web
- **WHEN** a pull request changes only `desktop/themes_gen.go`
- **THEN** the Rust desktop CI runs and fails if `assets/themes.json` is out of step

### Requirement: Rust desktop isolated release
The Rust desktop SHALL be released on `desktop-rust-v*` tags matching its version, publishing Linux x86_64, Windows
x86_64 and macOS universal archives with SHA-256 sums and the notes of that version from `desktop-rust/CHANGELOG.md`,
without changing or calling `release.yml`.

#### Scenario: Release tag
- **WHEN** the tag `desktop-rust-v0.1.0` is pushed and it matches the package version
- **THEN** a release `desktop-rust-v0.1.0` is published with the three archives and `SHA256SUMS`, and `release.yml`
  does not run

### Requirement: Rust desktop benchmark
The change SHALL include a repeatable benchmark against the Fyne app (binary size, cold start, idle memory, Users load
and memory with 5,000 users, scroll CPU) with a mock panel and a report of the numbers.

#### Scenario: Reproducible run
- **WHEN** `desktop-rust/bench/run.sh` runs with both binaries built
- **THEN** it prints a table of the medians of 5 runs per metric for each app, with the same window size, scale,
  theme (flat) and data for both, runs alternating between the apps

#### Scenario: Not ready in time
- **WHEN** an app does not reach the step's readiness (the mock answered the awaited request and the process is idle
  for 300 ms) within 30 s
- **THEN** the run fails instead of recording a number
