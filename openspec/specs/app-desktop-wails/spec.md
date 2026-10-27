# app-desktop-wails Specification

## Purpose
TBD - created by archiving change desktop-wails. Update Purpose after archive.

## Requirements

### Requirement: Wails desktop runs the web SPA
The Wails desktop app SHALL run the web panel's SPA from `frontend/`, built with `vp build --mode desktop` and
embedded in the binary. Desktop-only code SHALL live in `frontend/src/desktop/` and its pt-BR strings in
`frontend/src/locales/pt-BR/desktop.js`, both loaded only in that mode. The web build SHALL contain no desktop
module, no desktop catalog and no Wails runtime, and SHALL write the same files as before.

#### Scenario: Web build unaffected
- **WHEN** `vp build` builds the web panel
- **THEN** no file in `web/dist` contains the desktop sentinel string or `@wailsio`, the build's module list has
  no module under `src/desktop/` and not `locales/pt-BR/desktop.js`, and the web's Playwright suite passes
  unchanged

#### Scenario: Same pages as the web
- **WHEN** the operator signs in to the Wails app
- **THEN** the header, tabs, pages and dialogs are the web panel's components, in the theme the panel reports

#### Scenario: Development with hot reload
- **WHEN** a developer runs `wails3 dev` in `desktop-wails/` and edits a `.vue` file
- **THEN** the Vite+ dev server of `frontend/` in desktop mode on `WAILS_VITE_PORT` pushes the change through
  the Go middleware and the development CSP, and the window shows it without a restart

#### Scenario: Router path reload
- **WHEN** the window is on `/users` and the operator reloads it
- **THEN** the asset server answers `index.html` and the Users page shows again

### Requirement: Wails desktop talks only to the panel
The app SHALL forward only the SPA's same-origin `/api/` requests to the configured panel URL over `https://`. It
SHALL rebuild the target from the decoded path and query alone, refuse dot segments, encoded slashes and
backslashes, never follow an HTTP redirect, and reach file servers only through the panel's `/api/agents/{id}/…`
proxy. Paths outside `/api/` SHALL be served by the embedded SPA and never reach the panel.

#### Scenario: Redirect refused
- **WHEN** the panel answers a proxied request with a 3xx redirect
- **THEN** the SPA receives an error with "the panel answered with a redirect, which is not followed: check the
  panel URL" and nothing is sent to the redirect target

#### Scenario: Plain HTTP refused
- **WHEN** the operator types `http://panel:8443` in Configure and tests it
- **THEN** the test says "Panel URL: enter an https://server:port URL" and no connection is opened

#### Scenario: Encoded traversal refused
- **WHEN** the proxy handler receives the raw paths `/api/%2e%2e/asynqmon` or `/api/users%2F..%2Fx` (sent to the
  Go handler directly, as browsers normalize them)
- **THEN** it sends nothing to the panel and answers 404

#### Scenario: Normalized path stays local
- **WHEN** the webview requests `/api/../asynqmon`, which the engine normalizes to `/asynqmon`
- **THEN** the embedded SPA answers it and the mock panel receives no request

#### Scenario: File server through the panel
- **WHEN** the Shares page lists the shares of file server 3
- **THEN** the proxy requests `{panel}/api/agents/3/shares` with the panel session

#### Scenario: Upload over the file limit refused before dispatch
- **WHEN** the operator picks two files and form fields for a `FormData` upload whose files total the file limit
  (the body cap minus 1 MiB) plus one byte
- **THEN** the SPA shows the limit's message and sends nothing, while the same upload with files totaling exactly
  the file limit succeeds through the proxy

#### Scenario: Response over the cap
- **WHEN** a proxied panel response is larger than the cap
- **THEN** the proxy answers with an error naming the limit instead of passing the body to the bridge

### Requirement: Wails desktop certificate pinning
A panel certificate that the system does not trust SHALL be accepted only after the operator trusts its SHA-256
fingerprint in Configure, and from then on only that certificate, with its handshake signed by its key, SHALL be
accepted. A legacy file with `insecure = true` and no fingerprint SHALL keep accepting any certificate until a Save
after a new successful test replaces it with a pin.

#### Scenario: Self-signed shown before trusting
- **WHEN** Test reaches a panel whose certificate does not verify and no pin is configured
- **THEN** Configure shows "Certificate not trusted" with the fingerprint, subject, issuer and validity and a
  "Trust this certificate" button, and does not sign in

#### Scenario: Certificate changed
- **WHEN** the panel presents another certificate than the pinned one
- **THEN** every proxied request fails with "the panel's certificate is not the one trusted in Configure (it
  changed): check it with the panel's administrator, then trust it again in Configure"

#### Scenario: Certificate without its key
- **WHEN** a server presents the pinned certificate but signs the handshake with another key
- **THEN** the connection fails

#### Scenario: Legacy insecure file
- **WHEN** the file has `insecure = true` and no fingerprint
- **THEN** the panel is reached with any certificate, and a Save after a new successful test of a self-signed
  panel writes its fingerprint and no `insecure`

### Requirement: Wails desktop session cookie in Go
The panel session cookie SHALL live only in the Go backend's memory: the proxy SHALL drop the webview's `Cookie`
header and strip `Set-Cookie` from every response.

#### Scenario: Token never in the webview
- **WHEN** the operator signs in and opens Users
- **THEN** no proxied response has a `Set-Cookie` header, the webview's cookie store (including `HttpOnly`
  cookies) holds no panel cookie, and the mock panel sees each authenticated request with only the Go jar's cookie

### Requirement: Wails desktop session generations
Each sign-in and each sign-out SHALL start a new session, cancel the previous session's in-flight requests, and
advance a session generation. Authenticated requests and session-bound service calls SHALL carry their generation,
and the proxy SHALL refuse a mismatch before forwarding. An answer, including a 401, from an earlier generation
SHALL NOT change the SPA's state, and a session change SHALL close open dialogs and confirmations and stop pollers.

#### Scenario: Sign out and in again
- **WHEN** the operator signs out while a slow list request is in flight and signs in again before it answers
- **THEN** the old answer never reaches the new screens, and its 401 (if any) does not sign the new session out

#### Scenario: Old write delayed across a re-login
- **WHEN** a `POST` built under session 1 reaches the proxy after the operator signed out and in again (session
  2)
- **THEN** the proxy answers 409 and the panel receives nothing for it

#### Scenario: Dialogs and pollers of the old session
- **WHEN** the operator signs out with a confirmation open and a file-server task being followed, and signs in
  again
- **THEN** the confirmation is gone without running its action, and the follower sends no request under the new
  session

#### Scenario: Session expires
- **WHEN** the panel answers 401 to a request of the current session
- **THEN** the SPA returns to the sign-in screen, as the web does

### Requirement: Wails desktop replies reach their target
A page that switches targets (a file server) SHALL drop a late answer for a target that is no longer selected, so
that later actions use the data of the selected target only. An action resumed after a confirmation SHALL send
nothing if its captured target is no longer selected.

#### Scenario: Sync server switched
- **WHEN** the operator selects file server A on the Sync page and, before A's jobs answer, selects server B
- **THEN** A's late jobs are not shown under B, and B's jobs are

#### Scenario: Confirmation after a switch
- **WHEN** the operator asks to delete a share on file server A, switches to server B while the confirmation is
  open, and the confirmation is answered
- **THEN** no delete is sent to A or to B

### Requirement: Wails desktop replies reach their dialog
A dialog's completion SHALL act only on the dialog instance that started it; a late completion for a closed
instance SHALL NOT change or close another dialog.

#### Scenario: Reply to a closed dialog
- **WHEN** the operator saves user dialog A, closes it before the answer, and opens dialog B for another user
- **THEN** A's answer arriving later does not close B or change its fields

### Requirement: Wails desktop links to the panel
Links the SPA builds for use outside the app, such as the single-use password link, SHALL use the configured
panel URL in the desktop and the page's origin on the web.

#### Scenario: Password link copied
- **WHEN** the operator creates a single-use password link in the desktop for a panel at
  `https://panel.domain.local:8443`
- **THEN** the link shown and copied starts with `https://panel.domain.local:8443/password/`

### Requirement: Wails desktop secrets
The app SHALL never save, log or print a password, a session token or an agent token, SHALL retain no agent
authentication credential, and SHALL persist no secret on its own. Secrets typed or generated for a management
operation SHALL pass through to the panel only. A file the operator asks to export SHALL be written privately even
when it contains secrets. The Rust app's guarantee of wiping passwords from memory SHALL NOT be claimed, because JS
and Go strings cannot be wiped.

#### Scenario: Password never logged
- **WHEN** a sign-in, a Configure test, an own-password change and a file-server registration run with logging at
  debug level
- **THEN** the log has no password, token or cookie value

#### Scenario: Nothing secret in the configuration
- **WHEN** Configure saves after a successful test
- **THEN** `desktop.toml` has the URL, user, fingerprint, language and appearance, and no password, token or
  cookie

#### Scenario: Old agent tokens
- **WHEN** the file lists `[[agents]]` with tokens
- **THEN** the file is rewritten without them and, after sign-in, the status line says "File-server tokens are no
  longer used: they were removed from the configuration file."

### Requirement: Wails desktop configuration file
The app SHALL read and write the same `desktop.toml` as the Rust app, with the same keys, legacy keys and lookup
order (`--config`, `AD_MANAGER_DESKTOP`, `.config/desktop.toml` beside the executable, the user config folder). It
SHALL write the file atomically with mode `0600`, keep keys and sections it does not use, and refuse a file
readable by group or others (Unix).

#### Scenario: Shared with the Rust app
- **WHEN** the Wails app starts with a `desktop.toml` saved by the Rust app that has `[graphics] gpu_backend`
- **THEN** it uses the same panel URL, user, fingerprint, language and appearance, and a Save keeps the
  `[graphics] gpu_backend` key

#### Scenario: Loose permissions
- **WHEN** the file has mode `0644`
- **THEN** the Configure screen shows "<path> must be private and has mode 0644: run chmod 600" and no panel
  request is made

#### Scenario: First run
- **WHEN** no file exists
- **THEN** the Configure screen opens unauthenticated with user `administrator`, no panel request starts, and
  nothing but language, appearance or graphics preferences is written before a successful test and Save

#### Scenario: Preferences only
- **WHEN** the file has `lang` and `[appearance]` but no `[panel] url`
- **THEN** the app starts on Configure, as unconfigured, in that language and theme

### Requirement: Wails desktop Configure
Configure SHALL test by reading the certificate, the panel health and signing in with the account. Save SHALL be
enabled only after a successful test of the values on screen, and SHALL open the app with the tested session
without asking the password again. The language, appearance and graphics SHALL be saved at once, offline, without
a test.

#### Scenario: Edit after a test
- **WHEN** a test succeeds and the operator then edits the URL, user or password
- **THEN** Save is disabled and the tested session is signed out

#### Scenario: Test finishing after an edit
- **WHEN** the operator edits a field or cancels while a test is running, and the test then succeeds
- **THEN** its result is not shown, Save stays disabled, and its session is signed out (`POST /api/logout`)

#### Scenario: Panel in setup
- **WHEN** the panel answers `/api/health` with `setup: true`
- **THEN** the test fails with "the panel is being set up: finish its setup wizard in a browser first"

#### Scenario: Theme for this computer, offline
- **WHEN** the operator, signed out and with the panel unreachable, picks Breeze dark in Configure › Appearance
- **THEN** the app switches theme at once and `desktop.toml` gets `[appearance] theme = "breeze-dark"` with the
  rest of the file unchanged, and choosing "Panel default" follows the panel's theme again

#### Scenario: Configure in the tab bar
- **WHEN** the operator is signed in
- **THEN** Configure is at the right of the tab bar, after Queues, and the header has only "my password" and
  "sign out", as in the web panel

### Requirement: Wails desktop start-up preferences
The sign-in screen SHALL start with the configured user. The language SHALL be the file's, then the panel's
default, then English. The theme SHALL be the file's appearance, then the session's, then the public one.

#### Scenario: Configured user and language
- **WHEN** `desktop.toml` has `user = "carol"` and `lang = "pt-BR"` and the panel's default is English
- **THEN** the sign-in screen shows `carol` in the user field and the interface in Portuguese

#### Scenario: Appearance over the session theme
- **WHEN** the file has `[appearance] theme = "matrix"` and the operator signs in to a panel whose theme is
  Default
- **THEN** the app keeps the Matrix theme after sign-in

### Requirement: Wails desktop file transfers
Every file the SPA saves in the desktop SHALL go through the native save dialog and the Go backend, never through
the webview's download handling, and on Unix SHALL be written with mode `0600`, also over an existing file. Panel
downloads and backup uploads SHALL be streamed by Go between the panel and the disk, never through the webview
bridge or JavaScript memory, with progress and cancellation for uploads. On the web, the same helpers SHALL keep
today's behavior.

#### Scenario: CSV export
- **WHEN** the operator exports the Users list to CSV and picks `users.csv`
- **THEN** the file has the web's CSV content and mode `0600`

#### Scenario: Backup download
- **WHEN** the operator downloads a 200 MiB backup from the Backup page
- **THEN** Go streams it to the chosen file with mode `0600`, its SHA-256 matches the panel's, and the webview's
  memory does not grow by the file's size

#### Scenario: Backup upload
- **WHEN** the operator sends a 200 MiB backup archive from the send-backup dialog over a throttled link
- **THEN** the native dialog picks the file, Go streams it as the request body with progress shown, the panel
  receives the same SHA-256, and cancelling stops the transfer with no partial restore

#### Scenario: Web unchanged
- **WHEN** the same export and upload run in the web panel
- **THEN** the browser downloads with an anchor and uploads with XHR, as before

### Requirement: Wails desktop stays on the app
The window SHALL stay on the embedded SPA's origin: a top-level navigation, form submission or new window to
another origin SHALL be blocked by the native webview on Windows, Linux and macOS. The SPA's HTML SHALL carry a
Content-Security-Policy that forbids frames, objects and foreign form targets. Only the panel's `/asynqmon` and
`/swagger` pages over `https://` and the brand's `https://` support URL SHALL open, in the system browser.

#### Scenario: Queue monitor
- **WHEN** the operator opens the queue monitor link
- **THEN** the system browser opens `{panel}/asynqmon` and the window still shows the app

#### Scenario: Programmatic navigation blocked
- **WHEN** script in the window sets `location.href = 'https://example.org'` or opens it with `window.open`
- **THEN** the window stays on the app and nothing opens

#### Scenario: Opener allowlist
- **WHEN** `Shell.OpenExternal` is called with `file:///etc/passwd`, `http://panel/asynqmon` or
  `https://other.example/asynqmon`
- **THEN** nothing opens and the call fails

#### Scenario: Policy without frames
- **WHEN** the SPA's HTML is served by a production build
- **THEN** its `Content-Security-Policy` has `frame-src 'none'`, `child-src 'none'`, `object-src 'none'` and
  `form-action 'self'`, and no `ws:` source

### Requirement: Wails desktop fills its window
The desktop's panel SHALL fill its window at every size (16 px margin), without the web's widest box, and SHALL keep
the web's zoom steps (1.25× from 2400 and 1.6× from 3200 CSS px of window width). The usable height SHALL be measured
from the window, not computed from `dvh`, so that WebKit (which does not scale `dvh` with the zoom) and Chrome agree.

#### Scenario: QHD at OS scale 1
- **WHEN** the window is 2560×1440 at OS scale 1
- **THEN** the zoom is 1.25, the panel is 2528 px wide and its footer sits 16 px above the window's bottom

#### Scenario: 4K at OS scale 2
- **WHEN** a 3840×2160 screen runs at OS scale 2 and the window fills it
- **THEN** the page is 1920 CSS px wide with no extra zoom, drawn at 2× and filling the window

The window SHALL open at 1280×800 (minimum 800×600), fitted and centred on a screen whose work area is smaller, and
be restored where the operator left it, on a visible screen; a window closed maximised SHALL start maximised. A
second start SHALL focus the running window. The webview SHALL deny the microphone, camera, geolocation,
notifications and clipboard reading without asking (WebView2 otherwise grants them all). `--safe-gpu` or `[graphics] safe = true` SHALL apply a
workaround preset: on Linux, GTK's software renderer and WebKit without DMA-BUF and compositing; on Windows,
WebView2 with `--disable-gpu`. On macOS the option SHALL NOT be offered.

#### Scenario: A small screen
- **WHEN** the app starts for the first time on a 1024×768 screen
- **THEN** the window fits the screen's work area, centred, with its title bar visible

#### Scenario: Quit from the menu
- **WHEN** the operator quits with Cmd+Q (or the Dock) without closing the window
- **THEN** the window's place, size and maximised state are kept, and the next start has the same size

#### Scenario: Local Network on macOS
- **WHEN** macOS 15 refuses the connection to a panel on the local network because the app is not allowed there
- **THEN** Configure says that macOS blocks the app from the local network and where to allow it

#### Scenario: Closed maximised
- **WHEN** the operator maximises the window and closes the app
- **THEN** the next start opens the window maximised, and restoring it gives the size it had before

#### Scenario: The app's icon
- **WHEN** the window opens on Linux (X11)
- **THEN** the window carries the AD Manager icon (`_NET_WM_ICON`) and the program name `ad-manager-desktop-wails`

#### Scenario: Restored
- **WHEN** the app starts again after the operator moved and resized the window
- **THEN** it opens where and as large as it was

#### Scenario: Safe start on Linux
- **WHEN** the app is started with `--safe-gpu` on Linux under Xvfb with llvmpipe
- **THEN** the process starts with `GSK_RENDERER=cairo`, `WEBKIT_DISABLE_DMABUF_RENDERER=1`,
  `WEBKIT_DISABLE_COMPOSITING_MODE=1` and `LIBGL_ALWAYS_SOFTWARE=1`, the sign-in screen is painted (the
  screenshot is not blank), and nothing is saved

#### Scenario: Missing webview runtime
- **WHEN** the app starts on Windows without the WebView2 runtime
- **THEN** a native message says the WebView2 runtime is required, with Microsoft's download link, and the app
  exits with a non-zero status

### Requirement: Wails desktop CI isolation
The Wails desktop CI SHALL be a workflow of its own, run only for changes to `desktop-wails/`, `frontend/` or
itself, SHALL NOT be called by `ci.yml` or `release.yml`, and SHALL publish builds as workflow artifacts only.

#### Scenario: Server-only change
- **WHEN** a pull request changes only `internal/`
- **THEN** the Wails desktop CI does not run

#### Scenario: Frontend change
- **WHEN** a pull request changes `frontend/src/pages/Users.vue`
- **THEN** the Wails desktop CI runs the Go tests, the end-to-end suite on Chromium and WebKit, and the native
  smoke on Linux, Windows and macOS

#### Scenario: Release run untouched
- **WHEN** a `vX.Y.Z` tag runs `release.yml`
- **THEN** no Wails workflow runs in it and its published files are exactly the server's and the Rust desktop's

### Requirement: Wails desktop build
The Wails app SHALL build with the `production` tag in a Docker image pinned by digest (Linux natively on the
oldest supported glibc, Windows with `CGO_ENABLED=0`) and on the macOS runner as a universal binary. The Wails Go
module, the `wails3` CLI and `@wailsio/runtime` SHALL have one exact version.

#### Scenario: Versions out of step
- **WHEN** `go.mod` pins Wails v3.0.1 and `frontend/package.json` pins `@wailsio/runtime` 3.0.0
- **THEN** `make desktop-wails-check` fails naming both versions

#### Scenario: Shipped binary has no test surface
- **WHEN** the binary built for distribution runs with `FRONTEND_DEVSERVER_URL` set
- **THEN** it opens no listening TCP socket, serves the embedded SPA and has no test dialog or opener adapter

### Requirement: Wails desktop test server build
The server build used by the end-to-end tests SHALL listen on `127.0.0.1` only, check the `Host` header, require a
per-run random token header on every request, use a throwaway configuration directory, and talk only to the mock
or test panel.

#### Scenario: Cross-site request refused
- **WHEN** a request reaches the test server's `/api/` without the run's token header
- **THEN** it answers 403 and nothing is sent to the panel

### Requirement: Wails desktop benchmark
The change SHALL include a benchmark of the Wails app against the current Rust app, with the Rust benchmark's
method and mock panel and both measured in the same run. Memory and CPU SHALL be summed over the app and all its
webview processes, and readiness SHALL be a frame painted by the SPA. The report SHALL compare each number with the
G4 budget, and a missed budget SHALL be shown as failed or as waived with the operator's written waiver.

#### Scenario: Reproducible run
- **WHEN** `desktop-wails/bench/run.sh` runs with both binaries built
- **THEN** it prints the medians of 5 alternating runs for binary size, first painted sign-in, idle PSS, sign-in
  to Users with 5,000 users, PSS with 5,000 users and scroll CPU, each marked pass, fail or waived against its
  budget

### Requirement: Rust app kept until the gates pass
The Rust app in `desktop/` SHALL stay the desktop app that `make build` builds and the release publishes, unchanged,
until gates G1 to G5 of this change pass (or are waived in writing where the design allows) and a separate change
performs the switch.

#### Scenario: Release during the preview
- **WHEN** a `vX.Y.Z` tag is pushed while this change is in progress
- **THEN** the release publishes the Rust desktop archives as before and no Wails archive

#### Scenario: Rollback
- **WHEN** the spike or a gate ends in no-go
- **THEN** removing `desktop-wails/`, its workflow, its Make targets and the desktop mode leaves the web panel and
  the Rust desktop working, and `saveFile`, `sendFile` and the session generation behave on the web as before the
  change
