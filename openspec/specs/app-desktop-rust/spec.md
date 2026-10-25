# app-desktop-rust Specification

## Purpose
TBD - created by archiving change desktop. Update Purpose after archive.

## Requirements

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
- **THEN** `assets/themes.json` has the same themes and role colors as `archive/desktop-go/themes_gen.go`

#### Scenario: Accessible colors and focus
- **WHEN** the unit tests run
- **THEN** in every theme the text roles the pages use keep the theme's minimum contrast on their backgrounds, and the
  focus ring is drawn at the theme's width (3 px on Crystal)

#### Scenario: Unknown theme
- **WHEN** the panel reports a theme id the app does not know
- **THEN** the app uses Default

### Requirement: Rust desktop crash-safe graphics start
The app SHALL plan its graphics backend before the window exists: a `gpu-starting` marker that no running instance
holds SHALL move the start to the next safer backend of the platform's ladder, otherwise the backend saved as
`[graphics] gpu_backend` in desktop.toml applies; `--safe-gpu` and `WGPU_BACKEND` override the plan. The marker SHALL
be emptied (never removed) after 3 completed frames.

#### Scenario: A start that died
- **WHEN** the last start on OpenGL (Windows) left a marker that no process holds
- **THEN** this start uses DX12, the sign-in screen says "The graphics driver stopped the last start (gl); now using
  dx12. Change it in Configure.", and once 3 frames painted desktop.toml gets `[graphics] gpu_backend = "dx12"` with
  the rest of its configuration unchanged, the marker is emptied, and after sign-in the notice is in the status line

#### Scenario: Another instance starting
- **WHEN** another process holds the marker's lock while this instance starts
- **THEN** this start does not treat it as a crash, does not truncate or rewrite the marker, and uses the saved
  backend

#### Scenario: Forced by the environment
- **WHEN** `WGPU_BACKEND` is set, also when a marker of a start that died is there
- **THEN** wgpu's choice is used, nothing is saved or announced, and a start that dies under it moves no rung

#### Scenario: Software is never saved silently
- **WHEN** a crash moves the start down to the software rung
- **THEN** the launch prefers a software adapter and `[graphics]` is not changed

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
- **WHEN** the operator, signed out and offline, changes the Rendering backend in Configure › Graphics
- **THEN** desktop.toml gets `[graphics] gpu_backend` at once, without a panel test, the rest of the file unchanged,
  and the screen says "Applies at the next start. After a driver crash AD Manager picks a safer one."

#### Scenario: The Fyne app keeps the choice
- **WHEN** the Fyne desktop saves its Configure on a desktop.toml that has `[graphics]`
- **THEN** the section is still there

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
The Rust desktop SHALL have its own CI, run only for pull requests and pushes to `main` that change `desktop/`,
its workflow files or the Fyne sources its asset checks read (`archive/desktop-go/themes_gen.go`, `archive/desktop-go/i18n_pt_*.go`),
without changing or calling `ci.yml`, which keeps running on its own triggers.

#### Scenario: Server-only change
- **WHEN** a pull request changes only `internal/`
- **THEN** the Rust desktop CI does not run

#### Scenario: Rust desktop change
- **WHEN** a pull request changes `desktop/src/main.rs`
- **THEN** the Rust desktop CI runs fmt, clippy with warnings as errors, the tests and a release build

#### Scenario: WARP on Windows
- **WHEN** the Rust desktop CI runs
- **THEN** a `windows-latest` job builds the app natively and runs the WARP render smoke test

#### Scenario: Palette change in the web
- **WHEN** a pull request changes only `archive/desktop-go/themes_gen.go`
- **THEN** the Rust desktop CI runs and fails if `assets/themes.json` is out of step

### Requirement: Rust desktop isolated release
The Rust desktop SHALL be released on `desktop-rust-v*` tags matching its version, publishing Linux x86_64, Windows
x86_64 and macOS universal archives with SHA-256 sums and the notes of that version from `desktop/CHANGELOG.md`,
without changing or calling `release.yml`.

#### Scenario: Release tag
- **WHEN** the tag `desktop-rust-v0.1.0` is pushed and it matches the package version
- **THEN** a release `desktop-rust-v0.1.0` is published with the three archives and `SHA256SUMS`, and `release.yml`
  does not run

### Requirement: Rust desktop benchmark
The change SHALL include a repeatable benchmark against the Fyne app (binary size, cold start, idle memory, Users load
and memory with 5,000 users, scroll CPU) with a mock panel and a report of the numbers.

#### Scenario: Reproducible run
- **WHEN** `desktop/bench/run.sh` runs with both binaries built
- **THEN** it prints a table of the medians of 5 runs per metric for each app, with the same window size, scale,
  theme (flat) and data for both, runs alternating between the apps

#### Scenario: Not ready in time
- **WHEN** an app does not reach the step's readiness (the mock answered the awaited request and the process is idle
  for 300 ms) within 30 s
- **THEN** the run fails instead of recording a number

### Requirement: Web shell in the desktop
After sign-in, the Rust desktop SHALL show the web panel's shell: the header (logo or company, domain, global search,
user with my password and sign out), the top tab strip in the web's order and grouping, and the web's footer. It
SHALL NOT show a side menu.

#### Scenario: Navigation strip
- **WHEN** the operator signs in
- **THEN** the tabs read Home, Users, Groups, OUs, Computers, Shares, Import/export, Backup, Logs, Settings under the
  header, the current one with the theme's active-tab look, and no side menu is shown

#### Scenario: Sync inside Backup
- **WHEN** the operator opens Sync from the Backup page tabs
- **THEN** the Backup top tab stays active and the page tabs read Backup, Sync

#### Scenario: Import/export and Settings in the app
- **WHEN** the operator activates Import/export or Settings
- **THEN** the desktop shows its own page with the web's cards and actions; no tab opens the browser

#### Scenario: Global search
- **WHEN** the operator types "carol" in the header search and presses Enter
- **THEN** the Users page opens with "carol" in its filter and every status shown, and "carol" stays in the header
  field

#### Scenario: My password
- **WHEN** the operator chooses my password and submits the current and the new password
- **THEN** the desktop posts them to `/api/me/password` and shows the panel's answer

### Requirement: Page and dialog parity
Each desktop page and dialog SHALL take the structure, field order and breakpoints of its web counterpart, as listed
in the change's parity matrix.

#### Scenario: Groups as the web
- **WHEN** the Groups page is shown
- **THEN** it is a full-width table with the web's columns, and a group's members open in a detail dialog

#### Scenario: User form as the web
- **WHEN** the user edit dialog is open
- **THEN** each label is above its field, the fields fill a two-column grid row by row, and the buttons read Save,
  Cancel (Create, Cancel when creating a user)

### Requirement: Web control geometry
The Rust desktop SHALL draw text fields, selects and buttons as 28 px border boxes with the web's paddings and text
sizes in the Default theme, except on the sign-in screen, which keeps the web login's own control sizes; every control
on one toolbar line SHALL share one height and one text baseline.

#### Scenario: Toolbar controls line up
- **WHEN** the Users page is shown at 1280×800 in the Default theme
- **THEN** the search field, the filter select and the toolbar buttons are 28 px high with the same top and baseline,
  matching the web fixture within 1 px (AccessKit bounds in egui_kittest)

#### Scenario: Select has a field border
- **WHEN** a select is drawn in any theme
- **THEN** it has the 1 px field border and the arrow of the web's select

#### Scenario: Sign-in controls
- **WHEN** the sign-in screen is shown in the Default theme
- **THEN** its inputs are 25 px and its button 26 px high, as the web login, within 1 px of the fixture

#### Scenario: Fields as tall as the buttons beside them
- **WHEN** the Home reset field and its Open button, the Groups member field and its Add button, and the copy field
  of a generated link with its copy button are drawn
- **THEN** each field and its button have the same height (28 px in the Default theme), where egui drew 19 and 26 px

#### Scenario: Entry behavior kept
- **WHEN** the operator clicks inside a 28 px entry, selects text, or reveals a password
- **THEN** the caret, the selection and the reveal behave as before

### Requirement: Web card, table, badge and dialog geometry
Card heads, table headers and rows, badges, dialogs, the header and the footer SHALL use the web's sizes, weights and
alignment from the theme's tokens and recipe (`audit.md` A4–A15).

#### Scenario: Table header and rows as the web
- **WHEN** the Users, Groups, Computers, Logs or Backup table is drawn in the Default theme
- **THEN** its header is one continuous band with bold labels and a label over every column (an "Actions" header over
  the action columns), with no gap before the first row, and its row pitch, dividers and striping match the web
  fixture of that same table within 1 px, with numbers and sizes right-aligned where the web aligns them so

#### Scenario: Badges as the web
- **WHEN** the Users status badges (active, locked, deactivated, system) and the Home alert badges are drawn
- **THEN** each has the web's variant: bold uppercase 10 px text centered, 17 px tall; the Users status badge
  (active, locked, deactivated) 104 px wide, pale for active and solid for locked and deactivated; SYSTEM in its own
  column, as wide as its text; the alert badges as wide as their text — as `Users.vue:232-253,301-307`

#### Scenario: Dialog layout
- **WHEN** the new-user, password-policy or reset-password dialog is open in the Default theme
- **THEN** its title is bold and left-aligned with no notch in its bar, the footer is a full-width band, the primary
  action (Create, Save policy, Generate link) comes before Cancel, and the fields follow the web dialog: labels above
  fields with every field of a column at the same x in the new-user and reset dialogs, and label | input | unit columns
  in the password-policy dialog

#### Scenario: Header and footer
- **WHEN** the shell is shown in the Default theme at 1280×800
- **THEN** the header is 64 px tall with the logo about 114 px wide and a bold, vertically centered title, and the
  footer is 35 px tall with its 3 px top line and the web's items

### Requirement: Web features found missing
The Rust desktop SHALL offer the web features the audit found missing: Home's Domain controllers and Operating systems
panels, Groups' filter, type select, count, Export CSV and Scope column with members in a detail dialog, the Shares
File servers list with add, edit and remove, Tasks and Sessions, Logs' action filter, Export CSV and HTTP tab, Sync's
Last run, Quarantine and Size columns, OUs' GPOs, Computers' DNS, and the sign-in screen's language switch.

#### Scenario: Home panels
- **WHEN** Home is shown
- **THEN** it has the Domain controllers and the Operating systems panels, laid out as the web's

#### Scenario: Shares servers
- **WHEN** the Shares page is shown
- **THEN** its left column lists the panel's file servers with add, edit and remove, and a Tasks card, as the web's

#### Scenario: Logs HTTP tab
- **WHEN** the Logs page is shown
- **THEN** its page tabs include the panel's HTTP log, and the audit tab has the action filter and Export CSV

#### Scenario: Groups filter and export
- **WHEN** the operator types "fin" in the Groups filter, chooses Security in the type select and clicks Export CSV
- **THEN** the table and the "x of y" count show only matching security groups, the Scope column is shown, and the
  CSV the panel returns is offered for saving

#### Scenario: Sync columns
- **WHEN** the Sync page lists jobs and runs
- **THEN** jobs show Last run and Quarantine, runs show Size, and numbers are right-aligned

#### Scenario: GPOs, DNS and Sessions
- **WHEN** the operator opens GPOs on OUs, DNS on Computers, and Sessions on Shares
- **THEN** each opens the web's dialog (`GpoListDialog`, `DnsRecordsDialog`, `ShareSessionsDialog`) with its data

#### Scenario: Language switch on sign-in
- **WHEN** the operator chooses Português on the sign-in screen
- **THEN** the screen switches to Portuguese at once, as the web's switch

#### Scenario: Conditional tabs
- **WHEN** the panel's Swagger setting is off and its queue monitor is off
- **THEN** the API and Queues tabs are not shown, and they are when the settings are on

#### Scenario: CSV of the rows on screen
- **WHEN** the operator filters Users, Groups or Computers and clicks Export CSV
- **THEN** the desktop saves the rows on screen with the web's columns, `;` between quoted fields and a BOM

#### Scenario: Sync preview followed in the wizard
- **WHEN** the operator clicks Save and preview in the sync wizard
- **THEN** the wizard shows a fifth step that follows the preview every 2 s until it ends, with its plan, and offers
  Run now and Close (the job is saved)

#### Scenario: Suggestions while typing
- **WHEN** the operator types "bru" in Home's quick box (or a group's new member, a user's group, a share list)
- **THEN** a list under the field offers the matching values with their names; arrows move, Enter picks, Esc
  closes the list only

#### Scenario: Backup schedules edited in place
- **WHEN** the operator changes a schedule's cron in its row and clicks Save schedules
- **THEN** the desktop sends every schedule with `PUT /api/agents/{id}/backup-schedules`

### Requirement: Theme chosen on this computer
The Rust desktop SHALL let the operator choose, in Configure › Appearance, any of the panel's themes and the flat or
depth look for this computer only, applied at once and saved as `[appearance]` in desktop.toml; "Panel default"
SHALL follow the panel's Settings.

#### Scenario: Another theme on this computer
- **WHEN** the operator chooses Matrix (dark) in Configure › Appearance
- **THEN** the desktop redraws in Matrix at once and desktop.toml holds `[appearance] theme = "matrix"`, without a
  panel test

#### Scenario: Back to the panel's theme
- **WHEN** the operator chooses Panel default
- **THEN** the desktop draws with the theme saved in the panel's Settings and desktop.toml has no `[appearance]`

### Requirement: Native Settings and Import/export
The Rust desktop SHALL offer the web's Settings (general, users, company with the logo, the values from config.toml,
the agent token) and Import/export (exports, CSV and other-AD and folder imports previewed as a plan before writing,
with a log of the write) as pages of its own.

#### Scenario: Panel theme saved from the desktop
- **WHEN** the operator chooses Breeze (dark) in Settings and saves
- **THEN** the desktop sends the settings with `PUT /api/settings`, keeps the other values as they were, and redraws
  with the panel's new theme unless Appearance overrides it

#### Scenario: Import from another AD
- **WHEN** the operator fills the source AD, clicks Connect and preview, then Write to the domain and confirms
- **THEN** the desktop posts to `/api/import/ad/plan`, shows the plan, then posts to `/api/import/ad/apply` and
  shows the log of the write

### Requirement: Expired session from any call
When any call of the session answers that it ended, the Rust desktop SHALL go back to the sign-in screen, also when
the page that made the call showed or ignored its error.

#### Scenario: A background call hears the end
- **WHEN** a call made without error handling answers 401
- **THEN** the sign-in screen opens

### Requirement: Web typography
The Rust desktop SHALL resolve the active theme's font stack as a browser does: the first family of the stack installed
in the system or user font folders, matched by family name and CSS weight (400, and 600 or the next heavier face); the embedded Inter 4.001 for the Inter stacks and as the last resort of
sans stacks, egui's monospace as the last resort of monospace stacks; ghost buttons in regular weight.

#### Scenario: Default typeface
- **WHEN** a page is drawn in the Default theme
- **THEN** body text uses Inter regular, headings and primary buttons Inter semibold, and ghost buttons Inter regular

#### Scenario: Theme typeface
- **WHEN** a page is drawn in a Classic theme on Windows, and in Matrix on any OS
- **THEN** the Classic theme uses Tahoma (from the system's fonts) at base 11, and Matrix the first installed family
  of its monospace stack at base 12

#### Scenario: Missing font
- **WHEN** a Classic theme runs on a Linux system without Tahoma, Verdana or Segoe UI
- **THEN** the text uses the next family of the stack that is installed, or Inter, and nothing fails

#### Scenario: Semibold weight
- **WHEN** Breeze runs where Noto Sans Regular and SemiBold are installed
- **THEN** semibold text uses Noto Sans SemiBold (600), not Bold

#### Scenario: Broken font file
- **WHEN** the stack's first family is an unreadable, invalid or wrongly indexed file
- **THEN** it is skipped for the next family, and the app starts

### Requirement: Theme recipes for geometry
The Default theme's numbers SHALL be the baseline, and a theme's own recipe (case, bars, frames, title sizes, line
widths) SHALL take precedence, as on the web.

#### Scenario: Crystal Dark dialog tabs
- **WHEN** the user edit dialog is open in Crystal Dark
- **THEN** its tabs are mixed case without a left bar, the frame is 1 px and the title 15 px, as the web's Crystal
  Dark

#### Scenario: Default dialog tabs
- **WHEN** the user edit dialog is open in the Default theme
- **THEN** its tabs are mixed case without icons and without a left bar, 2 px apart in a strip with 6 px padding, the
  active one with a 3 px top mark, and the strip runs to the dialog edges, as the web's Default

#### Scenario: Page tabs
- **WHEN** the Logs page is shown in any theme
- **THEN** its tabs follow that theme's `.subtab` recipe, including the strip line width

### Requirement: Web viewport and zoom
The Rust desktop SHALL place its shell in an app box of `min(W − 32z, 1840z)` logical px (W the client width, z the zoom), SHALL scale all its
geometry 1.25× from 2400 and 1.6× from 3200 logical px of client width, and SHALL lay out every page from 1024×640,
keeping the 800×600 native minimum.

#### Scenario: 1280 window
- **WHEN** the client area is 1280×800 at OS scale 1
- **THEN** the app box is 1248 px wide and controls are 28 px high

#### Scenario: QHD window
- **WHEN** the client area is 2560 px wide at OS scale 1
- **THEN** the zoom is 1.25, the app box is 1840 × 1.25 = 2300 px wide at most, and controls are 35 px high

#### Scenario: 4K at OS scale 2
- **WHEN** a 3840 px wide screen runs at OS scale 2
- **THEN** the client area is 1920 logical px and no extra zoom is applied

#### Scenario: Crossing a step
- **WHEN** a window with an open dialog is resized from 2300 to 2500 px and back
- **THEN** the zoom changes to 1.25 and back to 1 once each, and the dialog stays centered and inside the window

#### Scenario: Wide dialog in the smallest window
- **WHEN** the user edit dialog opens in an 800×600 window
- **THEN** it is 752 px wide (94% of the window), centered, its top at 60 px

#### Scenario: Smallest shell
- **WHEN** the client area is 1024×640
- **THEN** every page shows its toolbar and table, and every dialog's header and footer stay visible while its body
  scrolls

### Requirement: Keyboard access and contrast
The navigation and page tabs SHALL be reachable and operable by keyboard with a visible, unclipped focus ring, dialogs
SHALL keep and restore focus, and every theme's text and non-text UI colors SHALL meet the web theme's contrast, at
least WCAG 2.2 AA (4.5:1 text, 3:1 large text and non-text UI), and AAA for text where the web pair does.

#### Scenario: Tabs by keyboard
- **WHEN** the operator presses Tab until the tab strip and then Right and Enter
- **THEN** focus moves through the strip in visual order with a visible ring, and Enter opens the focused page

#### Scenario: Dialog focus
- **WHEN** a dialog opened from a button closes
- **THEN** Tab never left the dialog while it was open and focus returns to that button

#### Scenario: Contrast matrix
- **WHEN** the contrast test runs over every theme and state
- **THEN** no text pair is below 4.5:1 (3:1 large) or below the web's ratio, text pairs the web keeps at AAA stay at
  7:1 (4.5:1 large), non-text UI is at least 3:1, and disabled controls are not measured

### Requirement: Web depth
The Rust desktop SHALL draw each part's gradients, bevels and shadows from the theme's recipe as the web does; the flat
look SHALL clear exactly the layers that `style.css`'s flat override clears, and keep the state marks, the dialog and
popup shadows and each theme's structural lines (High Contrast's lines included).

#### Scenario: Depth outside flat
- **WHEN** the Default theme is active, the look is not flat, and the primary button, the header and a card head are
  captured
- **THEN** each has a top pixel different from its bottom pixel, as the web's gradient, and the dialog has a shadow

#### Scenario: Flat look clears decoration only
- **WHEN** the flat look is chosen
- **THEN** the parts `style.css` lists in its flat override (header, tabs, buttons, card and table heads, dialog head
  and foot, footer, app box, login box, fields) are single-colored without shadows, while the dialog and popup shadows,
  the current tab's mark, the focus ring and an invalid field's mark are still drawn

#### Scenario: Depthless theme
- **WHEN** High Contrast is active
- **THEN** no decorative gradient is drawn, while its structural lines (drawn as shadows in its CSS) and the state
  marks are

#### Scenario: Shadow layer order
- **WHEN** a Winamp app box (an outline layer listed before a blurred shadow) is drawn
- **THEN** the first listed layer is on top, as in the browser's capture

#### Scenario: Gradient direction and interpolation
- **WHEN** the Classic blue header and the Default dialog title are drawn outside the flat look
- **THEN** their gradients run horizontally, with colors matching the web's (Oklab where the CSS says so) within 2 per
  channel at the start, middle and end

### Requirement: Screen-reader names
Every field, select and icon-only control SHALL carry an AccessKit name taken from its visible label or its action.

#### Scenario: Names
- **WHEN** the AccessKit tree of the Users page and the user edit dialog is read
- **THEN** every text input and combo box has a name equal to its visible label, and every icon link names its action

### Requirement: Mouse cursors unchanged
The visual parity work SHALL NOT change which mouse cursor any control shows.

#### Scenario: Cursors
- **WHEN** the pointer is over a button, a navigation tab, a page tab, a table link or a text field
- **THEN** it shows the same cursor as before this change

### Requirement: Dialog actions reach their own dialog
A footer click SHALL act only on the dialog it was drawn in, and a reply SHALL reach only the dialog and the session
that asked for it.

#### Scenario: Confirmation over a form
- **WHEN** Restore of an earlier permission is confirmed over the open share form
- **THEN** an `acl:restore` task is queued and no `share:create` task

#### Scenario: Late reply after sign-out
- **WHEN** a Settings save or an import answers after the operator signed out and another signed in
- **THEN** the reply changes nothing in the new session

#### Scenario: Run now after a preview
- **WHEN** Run now is clicked on the wizard's preview step after the job id field was edited
- **THEN** the run is asked for the previewed job

### Requirement: Secrets on this computer
Files the app downloads SHALL be readable by their owner only, and passwords and tokens SHALL NOT outlive their use in
plain buffers or in the toolkit's text state.

#### Scenario: Private downloads
- **WHEN** a backup or a CSV export is saved on Unix, also over an existing file
- **THEN** the file's mode is 0600

#### Scenario: Secret request bodies
- **WHEN** the sync wizard, the own-password change or a file server registration sends a password or token
- **THEN** the JSON body is built straight into a buffer wiped on drop, with the secret as its last member

#### Scenario: Secret fields forgotten at sign-out
- **WHEN** the operator signs out after typing the source AD's password or generating an agent token
- **THEN** egui keeps no text state (and no undo history) for those fields

### Requirement: Footer panes and focused fields as the web's
The footer SHALL draw the theme's status-bar panes and line when the theme defines them, and a focused text field
SHALL show the theme's field-focus border and ring.

#### Scenario: Classic gray footer
- **WHEN** the Classic gray theme is on
- **THEN** each footer item sits in a sunken pane 3 px from the next, under a white line

#### Scenario: Focused field
- **WHEN** the new-user dialog opens with the login field focused
- **THEN** the field shows a 1 px border in the field-focus color with its 2 px ring outside

### Requirement: The web's icons
The app SHALL draw the web's Lucide icons (the version the web panel uses) wherever the web shows one, never emoji, and
a control's accessible name SHALL NOT include the icon.

#### Scenario: Users actions
- **WHEN** the Users page is shown
- **THEN** the password, copy, move and delete links show the key-round, copy, folder-input and trash-2 outlines, and
  the New user button the user-plus outline

#### Scenario: Names without icons
- **WHEN** the AccessKit tree of a dialog footer is read
- **THEN** its buttons are named "Save" or "Cancel", without the icon's character

#### Scenario: Wrapped alerts
- **WHEN** an alert's text is longer than its row on Home (the Matrix theme's monospace face)
- **THEN** it wraps to the next line, as the web's, instead of ending in an ellipsis

### Requirement: The theme regardless of the system's
The app SHALL keep its theme whatever light or dark theme the operating system reports, and SHALL work against a real
domain as against the mock.

#### Scenario: System theme switch
- **WHEN** the system switches between light and dark while the app runs
- **THEN** fields, selects and panels keep the theme's colors

#### Scenario: A real test domain
- **WHEN** the live test runs against the all-in-one test image
- **THEN** the app signs in, opens every page and tab, and no error dialog opens
