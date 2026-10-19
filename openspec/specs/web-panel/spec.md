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
