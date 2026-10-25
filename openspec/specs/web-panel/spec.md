# web-panel Specification

## Purpose
Web panel (Vue SPA embedded in the Go binary) with Domain Admins login, auditing and the management screens.

## Requirements

### Requirement: Login with domain credentials
The panel SHALL authenticate the operator by binding to AD with the given user and password (accepting `user`, `user@example.local` or `EXAMPLE\user`) and SHALL allow access only to direct or nested members of the administrative group. The default is the group with RID 512 (`Domain Admins`), checked by SID through `tokenGroups`; membership is re-evaluated at every login.

#### Scenario: Administrator login
- **WHEN** a member of `Domain Admins` provides valid credentials
- **THEN** the panel creates a session and opens the home page

#### Scenario: Regular user
- **WHEN** a valid user who does not belong to the administrative group tries to sign in
- **THEN** the login is rejected with "access denied" and no session is created

#### Scenario: Repeated wrong password
- **WHEN** the same IP fails the login 5 times in 5 minutes
- **THEN** new attempts from that IP are rejected with HTTP 429 until the end of the window

### Requirement: Session
The panel MUST be served over HTTPS (its own TLS or a trusted reverse proxy); without TLS it only starts with an explicit development option listening on loopback. The session SHALL be an `HttpOnly`, `SameSite=Strict` and `Secure` cookie, with configurable idle expiration (default 30 minutes). Operations SHALL run with the identity of the signed-in operator, not with a service account.

#### Scenario: Expired session
- **WHEN** the session exceeds the idle timeout
- **THEN** the next API call returns 401 and the interface goes back to the login

#### Scenario: HTTP rejected in production
- **WHEN** the panel is started without a certificate and listening on a non-loopback interface
- **THEN** it does not start and explains that HTTPS is required

### Requirement: Administration interface
The panel SHALL offer, in an interface using the Default visual theme (square corners), screens for: overview (counts and DC health), users, groups, OUs and computers, with search, filters, inline actions and confirmation before destructive actions. The domain password policy SHALL be shown and changed in a dialog opened from the users screen, not in a screen of its own.

#### Scenario: Find and reset a password
- **WHEN** the operator searches for "john" on the users screen and chooses "reset password"
- **THEN** a dialog asks for the new password (with a generator and a change-at-next-sign-in option) and confirms success

#### Scenario: Delete confirmation
- **WHEN** the operator clicks delete on a user
- **THEN** the panel requires confirmation by typing the login before deleting

#### Scenario: Password policy from the users screen
- **WHEN** the operator clicks "Password policy" on the users screen toolbar
- **THEN** a dialog shows the domain's default password policy, and saving asks for confirmation (warning that a Windows DC's Default Domain Policy may reapply the old values) before writing it

#### Scenario: Old password policy links
- **WHEN** the operator opens `/password-policy` (or `/politica`), or follows Home's weak policy alert
- **THEN** the panel shows the users screen with the password policy dialog open, and the top tabs have no Password policy entry

### Requirement: Audit
Every change made through the panel or the CLI SHALL produce an audit line with date/time, operator, source IP (panel), action, target object and result, without password values.

#### Scenario: Password change audit
- **WHEN** the operator resets the password of `john.smith`
- **THEN** the audit log records the operator, `password.reset`, `john.smith` and `ok`, and does not contain the password

### Requirement: Health and version
The panel SHALL expose `/api/health` without authentication, returning the version and whether the directory is reachable.

#### Scenario: Directory down
- **WHEN** the configured DC does not respond
- **THEN** `/api/health` returns 503 with `ldap: down`

### Requirement: Dialog position
Every panel dialog SHALL open with its top at 10% of the window height, horizontally centered, in every theme.

#### Scenario: Dialog in Crystal
- **WHEN** the theme is `crystal` and a dialog opens
- **THEN** the dialog top is at 10% of the window height, not stuck to the top

### Requirement: One file per dialog
Each panel form dialog SHALL live in its own component in `frontend/src/components/dialogs/`, with a multi-word
PascalCase name ending in `Dialog`, receiving data through props and notifying the page through events.

#### Scenario: Page without an inline dialog
- **WHEN** one searches for `<Modal` in the pages of `frontend/src/pages/` and in `App.vue`
- **THEN** there is none: the pages use the components from `components/dialogs/`

### Requirement: Loading table
Every table that fetches data SHALL show a spinning loading icon (no visible text; "Loading…" for screen readers,
`role="status"`) until the first response, instead of an empty table.

#### Scenario: Shares loading
- **WHEN** the Shares screen opens and the agent has not responded yet
- **THEN** the table shows the spinning icon (announced as "Loading…"), not the empty list message

### Requirement: Sync wizard
The sync wizard SHALL be a dialog with the same step indicator as the setup (numbered steps, completed ones checked,
the current one with `aria-current="step"` and "Step n of 5"), with a single button bar per step ("Back" on the left,
the main action on the right).

#### Scenario: Current step
- **WHEN** the wizard is on "Where to"
- **THEN** steps 1 and 2 appear completed, step 3 is marked as current and the title says "Step 3 of 5: Where to"

#### Scenario: Close during a save
- **WHEN** the user closes the wizard while the job is being saved
- **THEN** the page still receives the result and refreshes the list

### Requirement: Framed lists
Every list table that scrolls inside the page (Users, Groups, Computers and the logs) SHALL be framed on all four
sides in the theme's line color, whether or not its content overflows and a scrollbar shows; the frame belongs to
the scrolling wrapper, not to the collapsed table border.

#### Scenario: Long list
- **WHEN** a list has more rows than fit and scrolls
- **THEN** its left and right lines are visible from top to bottom, in every theme

#### Scenario: Short list
- **WHEN** a list fits without scrolling
- **THEN** it is framed the same way

### Requirement: Sync run end and duration
The Sync page's Runs table SHALL show, for each finished run, the time it ended and how long it took, with the date
when it ended on another day than it started, and SHALL show no end while the run is queued or running.

#### Scenario: Finished run
- **WHEN** a run started at 08:37:14 and finished at 08:41:52 the same day
- **THEN** its Finished cell reads the end time and the duration, `08:41 AM · 4m38s` (in the panel's locale), on one line

#### Scenario: Run across midnight
- **WHEN** a run started on one day and finished on the next
- **THEN** its Finished cell shows the end date and time, and the duration

#### Scenario: Running run
- **WHEN** a run is queued or running
- **THEN** its Finished cell is empty (`—`) and the Status cell shows the phase

### Requirement: Sync run-log modal
A sync run's log SHALL be shown in a dialog capped to a fraction of the viewport height, in which only the monospace
log body scrolls (`overflow-y:auto`) while the run's summary header (job name, run id, status and file/size counts)
and the footer stay in view. The page SHALL NOT grow with the log, and the dialog SHALL stay on screen on small
viewports. The dialog SHALL be dismissable with Escape and the Close button and SHALL offer a Copy action for the log
text.

#### Scenario: Very long log
- **WHEN** a run's log has thousands of lines (e.g. a dry-run printing a skip line per object) and the operator opens
  its Log
- **THEN** the log scrolls inside the dialog and the page itself does not overflow or grow downward

#### Scenario: Header and footer stay
- **WHEN** the operator scrolls through a long log
- **THEN** the summary header and the Close/Copy footer remain in view while the log body scrolls

#### Scenario: Small viewport
- **WHEN** the dialog is opened on a short screen
- **THEN** it is capped to the viewport and stays fully on screen, with the log body scrolling inside it

#### Scenario: Empty or short log
- **WHEN** a run has no log or only a few lines
- **THEN** the dialog renders correctly without scrolling and shows the run's summary

### Requirement: Shared size tokens
The web panel's stylesheet SHALL define its control, toolbar, table-cell and badge sizes as named CSS variables
(`--control-h`, `--input-px`, `--select-pr`, `--button-px`, `--toolbar-gap`, `--toolbar-mb`, `--th-px`, `--th-py`,
`--td-px`, `--td-py`, `--tag-px`, `--tag-py`, `--tag-size`), used by its existing classes, so that the desktop reads
the same values.

#### Scenario: No visual change
- **WHEN** the web panel is captured before and after the tokens are introduced, in every theme
- **THEN** the computed sizes of inputs, selects, buttons, table cells and badges are the same

### Requirement: A share's name edits it
In the Shares table, a share's name SHALL be the link that opens its edit form, as a user's login does in Users, and
the Actions column SHALL hold only remove. The name SHALL be disabled while the chosen file server is down. The
desktop app's Shares page SHALL behave the same.

#### Scenario: Edit from the name
- **WHEN** the operator clicks the share name "finance" on a file server that is up
- **THEN** the edit form of "finance" opens, and the row shows no separate edit link

#### Scenario: Server down
- **WHEN** the chosen file server is unavailable
- **THEN** clicking a share's name does not open the form

### Requirement: A file server's row stays on one line
In the Shares page's list of file servers, each server SHALL take one line: its name, host, DC tag and its edit and
remove actions never wrap. When the row is too narrow, the host SHALL shorten first and then the name, both ending in
"…" with the full text in a tooltip; the DC tag and the actions SHALL keep their size. The total size in the Shares
header SHALL be vertically centered on its buttons. The desktop app's Shares page SHALL behave the same.

#### Scenario: Long server name
- **WHEN** a domain controller registered as "fileserver-headquarters-saopaulo-01" is listed in the narrowest list
- **THEN** its row is one line high, the name ends in "…", and the DC tag, "edit" and "remove" are fully visible
