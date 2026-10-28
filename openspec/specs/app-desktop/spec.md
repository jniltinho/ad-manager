# app-desktop Specification

## Purpose
The desktop app (Go + Fyne) that managers run on their own machine: it talks to the panel API (users, groups,
logs) and to the file-server agents, with the same theme, layout and login as the web panel.

## Requirements

### Requirement: Desktop app theme
The desktop app SHALL use the theme and flat look saved in the panel Settings (`brand.theme`, `brand.flat`), with
colors generated from the same theme files as the web, and SHALL fall back to the Default theme when the API does
not respond or the theme is unknown.

#### Scenario: Theme chosen in the panel
- **WHEN** the administrator saves the `graphite` theme in Settings and an operator signs in to the desktop app
- **THEN** the desktop appears dark, with the Graphite background, text and primary button, and the Fyne dark
  variant

#### Scenario: Flat look
- **WHEN** `brand.flat` is true
- **THEN** desktop buttons, header and card titles appear without gradient and without shadow

#### Scenario: No API
- **WHEN** the desktop cannot read `/api/health` or `/api/me`
- **THEN** it uses the Default theme, as today

#### Scenario: Colors in sync with the web
- **WHEN** a web theme changes color and the generated desktop file is not regenerated
- **THEN** the desktop test fails pointing to the outdated theme

### Requirement: Desktop login screen
The desktop app login screen SHALL follow the web login in geometry and content (480 px box at 10% of the height,
company or AD Manager logo, "ACTIVE DIRECTORY" with "NETBIOS (domain)", "Sign in" button, error below the box, hint
with the user name formats), with the box always in the same place.

#### Scenario: Same position
- **WHEN** the theme changes
- **THEN** the login box keeps the same position and size

#### Scenario: Long error in a small window
- **WHEN** the window is 800 × 600 and the login fails with a long message
- **THEN** the message appears below the box, wrapped to at most 4 lines (the last one with an ellipsis), without
  leaving the window

### Requirement: Desktop login branding
The desktop app login screen SHALL use the branding from `/api/health`, even when it responds 503.

#### Scenario: Company logo
- **WHEN** the panel reports `brand.logo`
- **THEN** the login shows the company logo; if the image does not load or is an SVG that Fyne cannot draw, the AD
  Manager logo

#### Scenario: LDAP unavailable
- **WHEN** `/api/health` responds 503 with domain and branding
- **THEN** the login applies the theme and shows "NETBIOS (domain)"

### Requirement: Desktop login depth
The desktop app login screen SHALL have visible depth in every theme outside the flat look (except a depthless theme,
High contrast, which is always drawn flat).

#### Scenario: Shadow in a light theme
- **WHEN** the theme is light and the look is not flat
- **THEN** the box has the theme shadow (`--shadow-login-box`) and the elliptical shadow below it

#### Scenario: Bevel in Classic gray
- **WHEN** the theme is `classic-gray` and the look is not flat
- **THEN** the box appears raised (light on top and left, dark on bottom and right), as on the web

#### Scenario: Highlighted box in a dark theme
- **WHEN** the theme is dark and the look is not flat
- **THEN** the box has a light top border and a frame with ≥ 1.5:1 contrast against the page

#### Scenario: Flat look
- **WHEN** `brand.flat` is true, in any theme
- **THEN** the box has only the frame, with no shadow, bevel, highlight, gradient or gloss

### Requirement: Visual parity with the web
Each part of the desktop app SHALL use the same color roles as the equivalent part of the web panel (with the
accepted differences listed in the design), and each theme SHALL have drawn depth (short gradients or bevel, and
shadows) outside the flat look and no depth in the flat look; a theme whose web look has no depth (High contrast:
one-color bands and no shadow on buttons, cards and the login box) is drawn flat in both looks.

#### Scenario: Same roles
- **WHEN** the roles test runs for a part of the equivalence table
- **THEN** the desktop draws that part with the roles listed for it

#### Scenario: Drawn depth outside flat
- **WHEN** a theme is active, the look is not flat and the primary button, table header and card title are captured
- **THEN** the top pixel differs from the bottom one, except in Classic gray, where the bevel pixels exist, and in a
  theme whose top and base colors are equal by design (High contrast)

#### Scenario: Flat without depth
- **WHEN** the look is flat
- **THEN** those elements have a single color and there is no shadow pixel outside them

#### Scenario: Theme switch without residue
- **WHEN** the theme changes from Crystal to another
- **THEN** the look of the new theme matches its baseline

### Requirement: Desktop dialog position
Every desktop app dialog SHALL open with its top at 10% of the window height, horizontally centered, in every theme,
and SHALL return to that position when the window is resized.

#### Scenario: Dialog and resize
- **WHEN** a dialog is open and the window changes from 900 × 600 to 1200 × 800
- **THEN** the dialog top is at 80 px and it stays horizontally centered

### Requirement: Keyboard focus on desktop buttons
The desktop app's buttons (`volButton`: action, dialog and toolbar buttons) SHALL take the keyboard focus with the
theme's focus ring, as entries, selects and checks do, and SHALL be pressed with Space or Enter. The sidebar items,
the inner tabs and the password reveal button are not covered.

#### Scenario: Tab reaches a button
- **WHEN** the focus is on a field and the operator presses Tab, with a disabled button and then an enabled one after
  the field
- **THEN** the focus skips the disabled button and lands on the enabled one, which shows the focus ring

#### Scenario: Press with the keyboard
- **WHEN** a focused button receives Space, Return or Enter
- **THEN** its action runs once per key; a disabled button's action does not run

#### Scenario: Focus inside a scroll
- **WHEN** Tab lands on a button below or above the visible part of a scrolling list (a user's Groups)
- **THEN** the list scrolls so that the button and its ring are visible

#### Scenario: Ring leaves with the focus
- **WHEN** the focus moves away from the button
- **THEN** the ring disappears and the button looks as before

#### Scenario: Click does not show the ring
- **WHEN** the operator clicks a button with the mouse, focused or not
- **THEN** its action runs and no focus ring remains

### Requirement: Desktop parity of the Matrix, Winamp, Pink Panther and High contrast themes
The desktop app SHALL render `matrix`, `winamp`, `pink-panther` and `high-contrast` from the generated palettes
when the panel reports them in `brand.theme`, with the same roles as the web (header and its stripe, sidebar from the
tabs, card titles, table head, buttons, dialogs, focus ring, login box and its bar, Winamp's bevel) and the flat
look, and SHALL hold `high-contrast` to 7:1 in every text color it draws, like the Crystal themes.

#### Scenario: Theme chosen in the panel
- **WHEN** the administrator saves `winamp` in Settings and an operator signs in to the desktop app
- **THEN** the desktop appears dark with the steel header and its gold stripe, the amber primary button and the Fyne
  dark variant

#### Scenario: Light theme
- **WHEN** the panel reports `pink-panther`
- **THEN** the desktop uses the light variant with the plum header, the pink stripe and the deep rose primary button

#### Scenario: High contrast without depth
- **WHEN** the panel reports `high-contrast` with the flat look off
- **THEN** the desktop draws it as the flat look (no button gloss or shadow, no card shadow, no login highlight or
  elliptic shadow), and every other theme keeps its depth

#### Scenario: High contrast dialog frame
- **WHEN** a dialog opens in High contrast on the desktop
- **THEN** it is framed in the theme's dialog line (white), 2 px, since no shadow sets it apart; every other theme
  keeps its 1 px frame in the dialog title color

#### Scenario: High contrast drawn at AAA
- **WHEN** a desktop button, field, label or focused select is drawn in High contrast, at rest, hovered or pressed,
  normal or flat
- **THEN** its text has ≥ 7:1 over the drawn background and the focus ring is 3 px

#### Scenario: Bevel in Winamp
- **WHEN** the theme is `winamp` and the look is not flat
- **THEN** buttons and the login box are drawn with the theme's raised bevel; in the flat look, without it

#### Scenario: Accepted differences
- **WHEN** a new theme is shown on the web and on the desktop
- **THEN** the only differences are the documented ones: the page pattern, the colored glow, Winamp's green field
  text and its sunken field and pressed relief, Fyne's text-selection highlight, the font, the footer stripe (the desktop footer has none in any theme) and the hover tone of one-color buttons

### Requirement: Desktop sync run end and duration
The desktop app's Sync page SHALL show, in its Runs table, when each finished run ended and how long it took, in the
same format as the web panel, and SHALL show no end while the run is queued or running.

#### Scenario: Finished run
- **WHEN** a run started at 08:37:14 and finished at 08:41:52 the same day, in an English interface
- **THEN** its Finished cell reads `08:41 AM · 4m38s` (`08:41 · 4m38s` in pt-BR)

#### Scenario: Running run
- **WHEN** a run is queued or running
- **THEN** its Finished cell reads `—`

### Requirement: Panel as the single point of access
The desktop app SHALL reach only the panel: file servers' shares, folder sizes, share tasks and Samba logs SHALL go
through the panel's `/api/agents/{id}/…` proxies with the operator's session, and the app SHALL NOT keep, ask for or
send an agent token.

#### Scenario: Shares through the panel
- **WHEN** the operator opens Shares on a desktop whose workstation cannot reach any file server's port 9443
- **THEN** the page lists the panel's registered file servers and their shares, sizes and share tasks work, and each
  task is in the panel's audit under the operator's name

#### Scenario: Legacy config with agent tokens
- **WHEN** the app starts with a `desktop.toml` that still has `[[agents]]` (or `[[agentes]]`) with tokens
- **THEN** it ignores them, rewrites the same file without the section (0600, the other settings kept) and says once
  that file-server tokens are no longer used

#### Scenario: Legacy config that cannot be rewritten
- **WHEN** the file with agent tokens cannot be replaced (a read-only folder)
- **THEN** the original stays intact, nothing is written elsewhere and the status line says the tokens are still in
  the file, with the reason

#### Scenario: Slow folder sizes
- **WHEN** measuring the shares' sizes takes longer than the panel's 60-second write timeout
- **THEN** the answer still reaches the desktop whole, and an agent answer too large to pass on is an error, never a
  cut JSON

#### Scenario: Session lost while a share task runs
- **WHEN** the panel session ends while the app polls a share task
- **THEN** the polling stops and the app asks to sign in again

#### Scenario: No file server registered
- **WHEN** the panel has no registered file server
- **THEN** Shares says there is none and points to the panel's Settings

### Requirement: Configure validates the sign-in
Configure SHALL ask for the panel URL, user, password and language; Test SHALL reach the panel and sign in with the
given account, showing the domain, the user and the panel version or the panel's error; Save SHALL be possible only
after a successful test of the current values, SHALL write `desktop.toml` (0600) without the password and SHALL open
the app signed in with that session.

#### Scenario: Wrong password
- **WHEN** the operator tests a correct URL with a wrong password
- **THEN** Test shows the panel's "invalid user or password" message and Save stays disabled

#### Scenario: Not a Domain Admin
- **WHEN** the account signs in but is not in Domain Admins
- **THEN** Test shows the panel's refusal and Save stays disabled

#### Scenario: Successful test and save
- **WHEN** Test succeeds and the operator saves
- **THEN** `desktop.toml` holds the URL, the user, the language and the certificate pin (if any) but no password,
  and the app opens the shell signed in, without asking for the password again

#### Scenario: Values changed after or during the test
- **WHEN** the operator edits the URL, the user or the password after a successful test, or while a test runs
- **THEN** Save is disabled until a new test of the current values succeeds, and an older test's result is discarded

#### Scenario: Language change from the signed-in app
- **WHEN** the operator opens Configure from the app, changes only the language and saves without typing a password
- **THEN** the language is saved and the session and the certificate settings are kept

#### Scenario: Redirect from the panel
- **WHEN** the panel answers a request with a redirect
- **THEN** the desktop does not follow it and reports it

### Requirement: Pinned panel certificate
A panel certificate that does not verify SHALL be trusted only by its SHA-256 fingerprint, accepted by the operator
in Configure and stored as `fingerprint` under `[panel]`; every connection SHALL accept exactly that leaf certificate.

#### Scenario: Self-signed panel
- **WHEN** Test reaches a panel whose certificate is self-signed
- **THEN** it reads the certificate without sending any credential, shows its fingerprint, subject, issuer and
  validity and asks whether to trust it; after the operator accepts, the test signs in over a connection that accepts
  only that certificate, and Save stores the pin

#### Scenario: Trusted certificate
- **WHEN** the system trusts the panel's certificate for its host name
- **THEN** nothing is pinned and the normal certificate checks apply

#### Scenario: Certificate changed
- **WHEN** the panel's certificate no longer matches the stored pin
- **THEN** the sign-in fails with a message that the panel's certificate changed and to check it in Configure, and
  nothing is sent to the server beyond the TLS handshake

#### Scenario: Legacy insecure config
- **WHEN** the config has `insecure = true` and no fingerprint
- **THEN** the app still connects, and the next Save replaces `insecure` with the pin of the certificate the
  operator accepted in Test

### Requirement: Password policy from Users
The desktop app SHALL open the domain's password policy in a dialog from a **Password policy** button of the Users
toolbar, as the web panel does, and SHALL NOT list it in the side menu.

#### Scenario: Open from Users
- **WHEN** the operator clicks **Password policy** on the Users toolbar
- **THEN** a dialog shows the current policy (complexity, lengths, ages, lockout) with its weak-policy warning, and
  saving asks for a confirmation before changing the domain's policy

#### Scenario: Weak policy alert
- **WHEN** the operator follows Home's policy alert
- **THEN** the app goes to Users and opens the policy dialog

#### Scenario: Side menu
- **WHEN** the app is signed in
- **THEN** the side menu has no Password policy item

### Requirement: Users toolbar on as few lines as fit
The Users toolbar SHALL put the search field, the filter and count, the action buttons and the extras (Password
policy, Export CSV, Refresh) on one line when the window is wide enough, letting the search field shrink first; when
it is not, the extras SHALL go behind a ⋯ menu, and only then SHALL the actions move to a second line.

#### Scenario: Wide window
- **WHEN** the Users page is shown in a window wide enough for everything
- **THEN** search, filter, count, the actions and the extras are on a single line

#### Scenario: Default window
- **WHEN** the window is 1280 px wide
- **THEN** the actions are on a second line and the extras stay visible beside the search, with no ⋯

### Requirement: Users row actions
The desktop's Users table SHALL let the operator act on each row in place, as the web panel, on the account of the
row that was tapped: the login opens the edit form; an editable account's status toggles it; a locked account shows
an unlock link; one column per action offers password, copy, move and delete, move and delete only for editable
accounts. An account is editable when it is not built-in and its login is not the operator's own (case-insensitive).

#### Scenario: Edit from the login
- **WHEN** the operator clicks a user's login
- **THEN** that user's edit form opens, and the selected row does not change

#### Scenario: Selected row is not the target
- **WHEN** row A is selected and the operator clicks delete on row B and types B's login
- **THEN** account B is deleted and A is untouched

#### Scenario: Disable from the status
- **WHEN** the operator clicks the "active" (or "locked") status of an editable account and accepts the confirmation
- **THEN** the account is disabled and the list reloads; when the confirmation is cancelled, nothing is sent

#### Scenario: Enable from the status
- **WHEN** the operator clicks the "deactivated" status of an editable account
- **THEN** it is enabled at once, without a confirmation

#### Scenario: Built-in or own account
- **WHEN** the row is a built-in account (Administrator, Guest, krbtgt) or the operator's own
- **THEN** its status is plain text, its move and delete cells are blank and inert, and its login, password and copy
  links work

#### Scenario: Locked account
- **WHEN** an account is locked
- **THEN** an unlock link stands beside its status and clicking it unlocks the account; a locked built-in account
  shows the unlock link and its system tag

#### Scenario: Delete
- **WHEN** the operator clicks delete on a row
- **THEN** the confirmation asks to type that row's login, and a wrong or cancelled one sends nothing

#### Scenario: Panel refuses
- **WHEN** the panel refuses an action with the session still valid
- **THEN** its message is shown and the list is left as it was (an expired session still returns to sign-in)

#### Scenario: List changes during a confirmation
- **WHEN** the operator opens B's disable confirmation, then the list reloads or another row is selected, and accepts
- **THEN** account B is the one disabled

#### Scenario: Keyboard
- **WHEN** the operator selects a row with the keyboard and presses Enter
- **THEN** a menu lists that row's actions, with the same availability, and choosing one runs it

#### Scenario: Default window
- **WHEN** the Users page is shown on a 1280 px window, in English or Portuguese
- **THEN** every column, the action links included (as icons), is visible without scrolling sideways, and hovering
  a link names its action in the status bar

### Requirement: Lean Users toolbar
The Users toolbar SHALL hold only the search, the filter and count, Password policy, Export CSV, New user and refresh,
on one line on the default window, the right group (New user included) behind ⋯ when the window is too narrow.

#### Scenario: Toolbar on the default window
- **WHEN** the Users page is shown on a 1280 px window
- **THEN** the toolbar is one line and no action needs a row to be selected first

#### Scenario: Home's locked alert
- **WHEN** the operator follows Home's locked-accounts alert
- **THEN** the Users page opens, where each locked account has its unlock link

### Requirement: Group count on the edit dialog
The desktop's user edit dialog SHALL name its Groups tab with the number of groups the user is in, as the web panel
(`Groups (n)`), and keep it current as groups are added or removed.

#### Scenario: Count follows the memberships
- **WHEN** the operator opens a user who is in 2 groups and removes one on the Groups tab
- **THEN** the tab reads `Groups (2)` when the dialog opens and `Groups (1)` after the removal

### Requirement: The desktop app is the Wails one
The desktop app the project builds, tests and releases SHALL be `desktop/` (Go, Wails v3, the web SPA in desktop
mode); the Rust app in `archive/desktop-rust/` and the Fyne app in `archive/desktop-go/` SHALL be kept as code only
and SHALL NOT be built by `make`, tested by CI or published by a release.

#### Scenario: Build
- **WHEN** a developer runs `make build`
- **THEN** `dist/` gets the server and, in `dist/desktop/`, the Wails app's Linux and Windows archives, `.deb`, `.rpm`
  and Linux binary, built in Docker, and nothing from `archive/`

#### Scenario: A configuration of the Rust app
- **WHEN** the new app starts on a computer whose `desktop.toml` was written by the Rust app
- **THEN** it uses the same panel, user, fingerprint, language and appearance, and keeps `[graphics] gpu_backend`

#### Scenario: Docs screenshots
- **WHEN** `make desktop-shots` runs
- **THEN** `docs/screenshots/desktop-*.png` are taken from the real app at 2×, in Classic blue, with Example Company
  Ltd and `domain.local`, against a demo panel, with no domain needed
