## ADDED Requirements

### Requirement: SSH terminal switch
The panel SHALL offer an "SSH terminal" switch on the Settings screen, off by default, audited as
`settings.update`, with a warning that commands run in the terminal bypass the panel's protections and audit.
While it is off the Terminal sub-tab SHALL be hidden and `GET /api/agents/:id/ssh` SHALL answer 404; turning it
off SHALL close every open terminal.

#### Scenario: Off by default
- **WHEN** an administrator signs in to a freshly installed panel
- **THEN** the Backup tab shows only the Backup and Sync sub-tabs and a WebSocket to `/api/agents/1/ssh` gets 404

#### Scenario: Turned on
- **WHEN** the administrator turns the switch on and saves
- **THEN** the Terminal sub-tab appears beside Backup and Sync without a restart and the audit log has
  `settings.update`

#### Scenario: Turned off with a terminal open
- **WHEN** an administrator turns the switch off while another operator has a terminal open
- **THEN** that terminal closes and the audit log has its `ssh.close`

#### Scenario: Turned off during a connection
- **WHEN** the switch is turned off while a terminal is between its checks and its SSH handshake
- **THEN** that terminal never reaches a shell and its slot is released

The Terminal sub-tab SHALL sit beside Backup and Sync under the Backup top tab, list the registered servers
(agents) on the left as the Backup page does, and open an interactive SSH shell, in an xterm.js view, to the host of
the selected server's URL on port 22, with a user and a password or a private key (with optional passphrase) typed
by the operator. The API SHALL take only the server's id, never a host or port.

#### Scenario: Server list
- **WHEN** the operator opens the Terminal sub-tab on a panel with `sambadc01` registered
- **THEN** the Servers list shows `sambadc01` and not "This panel", and the Backup top tab stays highlighted

#### Scenario: Password login
- **WHEN** the operator selects `sambadc01`, enters `root` and its password and clicks Connect
- **THEN** the page shows the shell prompt of `sambadc01`, and `echo $((6*7))` prints `42`

#### Scenario: Key login
- **WHEN** the operator picks a private key file protected by a passphrase and enters the passphrase
- **THEN** the session opens with that key

#### Scenario: Wrong password
- **WHEN** the password is wrong
- **THEN** the page shows "SSH authentication failed", no shell opens and the audit log has `ssh.open` with
  `auth_failed`

#### Scenario: Window resized
- **WHEN** the operator resizes the browser window with `top` running
- **THEN** the remote PTY gets the new size and `top` redraws to fit

#### Scenario: Unregistered target
- **WHEN** a client sends a host or port in the query or the first message
- **THEN** the panel ignores it and dials only the registered server's host on port 22

#### Scenario: Shell exits
- **WHEN** the operator types `exit`
- **THEN** the terminal shows "Connection closed" and the audit log has `ssh.close` with the duration

#### Scenario: Server stalls
- **WHEN** the server accepts TCP but never finishes the SSH handshake
- **THEN** after 20 seconds the page shows "The server did not answer" and the slot is released

### Requirement: Host key pinned on first use
The panel SHALL show the SHA-256 fingerprint of a server's unknown host key and connect only after the operator
confirms that exact fingerprint, then save the key for that server only if no other key was saved meanwhile. A
later key that differs SHALL refuse the connection before any credential is sent, saving nothing, until an
administrator clears the saved key; trusting and clearing (also when an agent's URL host changes) are audited. A
database error SHALL refuse the connection.

#### Scenario: First connection
- **WHEN** the operator connects to `sambadc01` for the first time
- **THEN** the page shows `SHA256:…` and asks to trust it; after confirming, the shell opens and the audit log has
  `ssh.hostkey.trust` for `sambadc01`

#### Scenario: Confirmed fingerprint does not match
- **WHEN** the client sends a `trust` fingerprint different from the key the server presents
- **THEN** nothing is saved and the page asks again with the presented fingerprint

#### Scenario: Two first connections
- **WHEN** two operators confirm different fingerprints for the same server at the same time
- **THEN** only one key is saved and the other connection is refused

#### Scenario: Reset during a trust
- **WHEN** an administrator clears the key of `sambadc01` while a connection that confirmed an older fingerprint is
  still in its handshake
- **THEN** that connection saves nothing and is refused, and the next one asks to trust again

#### Scenario: Key changed
- **WHEN** `sambadc01` presents a host key different from the saved one
- **THEN** the page shows "The host key of sambadc01 changed", no credential is sent to the server and no shell
  opens

#### Scenario: Key cleared
- **WHEN** an administrator clicks "Clear SSH host key" for `sambadc01` in the Terminal sub-tab
- **THEN** the audit log has `ssh.hostkey.reset` and the next connection asks to trust the new fingerprint

#### Scenario: Server moved
- **WHEN** an administrator changes the URL of `sambadc01` to another host
- **THEN** the saved host key is cleared and the audit log has `ssh.hostkey.reset`

### Requirement: SSH credentials are not kept
The SSH password, private key and passphrase SHALL travel only in the WebSocket's first message, SHALL be used only
for the handshake, and SHALL never be written to the database, logs, audit, responses or browser storage. The URL
SHALL carry no credential. Errors SHALL be reported as fixed codes, never as the SSH server's or library's text.

#### Scenario: Audit after a session
- **WHEN** an operator opens and closes a session with a password, and another with a key and passphrase
- **THEN** `audit.log`, the server log and the database contain none of the password, key or passphrase

#### Scenario: Server echoes the password
- **WHEN** the SSH server disconnects with a message containing the password it received
- **THEN** the page, the audit log and the server log show only the error code

#### Scenario: Reload
- **WHEN** the operator reloads the Terminal page
- **THEN** the form is empty and nothing of the credential is in `localStorage` or `sessionStorage`

### Requirement: Terminal access is guarded
The WebSocket SHALL require a valid administrator session and an `Origin` whose host and port equal the request's
`Host` (a missing, `null` or malformed Origin is refused), SHALL allow at most 4 terminals per operator across their
sessions (handshakes in progress included), and SHALL accept a first message of at most 64 KiB within 30 seconds,
with `cols` and `rows` from 1 to 1000, and frames of at most 64 KiB.

#### Scenario: No session
- **WHEN** someone without a session opens the WebSocket
- **THEN** they get 401 and no SSH connection is made

#### Scenario: Cross-site page
- **WHEN** a page on another origin, or a sandboxed frame with `Origin: null`, opens a WebSocket to
  `/api/agents/1/ssh` with the operator's cookie
- **THEN** the panel answers 403 before the upgrade

#### Scenario: Fifth terminal
- **WHEN** an operator with 4 open terminals, in two browser sessions, opens another
- **THEN** the panel answers 429 and the 4 keep running

#### Scenario: Silent client
- **WHEN** a client upgrades and sends nothing for 30 seconds
- **THEN** the panel closes the WebSocket without dialing the server and releases the slot

#### Scenario: Bad first message
- **WHEN** the first message is larger than 64 KiB, is not JSON, or asks for 0 or 5000 columns
- **THEN** the panel closes the WebSocket without dialing the server

### Requirement: Terminal bound to the panel session
A terminal SHALL close when its panel session ends (logout, idle expiry even with no other panel request, or
panel shutdown); terminal input SHALL count as activity for the session's idle timer, output and pings SHALL NOT,
and input SHALL never revive an expired session. Panel shutdown SHALL write `ssh.close` for every open terminal.

#### Scenario: Logout
- **WHEN** the operator signs out in another browser tab
- **THEN** every terminal of that session closes and the audit log has `ssh.close` for each

#### Scenario: Typing keeps the session
- **WHEN** the operator types in the terminal for longer than the session timeout without other panel calls
- **THEN** the session does not expire

#### Scenario: Idle terminal with output
- **WHEN** a terminal runs `top` with no input for longer than the session timeout
- **THEN** the session expires and the terminal closes

#### Scenario: Panel stopped
- **WHEN** the panel service stops with a terminal open
- **THEN** the terminal closes and the audit log has its `ssh.close`

### Requirement: Terminal outside the panel's protections
The panel SHALL state, on the Settings switch and in `docs/`, that commands run in the terminal are not checked by
the panel's protection rules and are not in its audit log beyond `ssh.open` and `ssh.close`; the host's own login
records are the record of what was done.

#### Scenario: Warning on the switch
- **WHEN** the administrator views the "SSH terminal" switch on the Settings screen
- **THEN** the warning about bypassing the panel's protections and audit is shown beside it

### Requirement: SSH terminal documented
`internal/server/openapi.yaml` SHALL document `GET /api/agents/:id/ssh` (WebSocket upgrade, frames and error codes)
and `DELETE /api/agents/:id/ssh/hostkey`, and `docs/` SHALL explain turning it on, the direct-host prerequisite of
the agent URL, and the reverse proxy `Upgrade`, `Host` and idle timeout settings for Nginx and Caddy.

#### Scenario: Swagger coverage
- **WHEN** `TestOpenAPICoversRoutes` runs
- **THEN** it passes with both routes documented
