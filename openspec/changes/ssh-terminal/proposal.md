## Why

Support still leaves the panel for PuTTY or a terminal to run `samba-tool`, `wbinfo`, `smbstatus` or read a log on
a DC or file server. A Terminal sub-tab in the panel lets an administrator open an SSH session to a server the panel
already manages, from the same browser session, with an audit line for each session.

## What Changes

- **New Terminal sub-tab** (`/terminal`) beside Backup and Sync under the Backup top tab (`BackupTabs.vue`), with
  the same Servers list on the left (the registered agents; not "This panel"). Pick a server, enter an SSH user with
  a password or a private key, and get an interactive terminal (xterm.js) on the right.
- **The SSH client runs in the panel** (`golang.org/x/crypto/ssh`), not in the agent and not in the browser. The
  SPA talks to the panel over a WebSocket on the panel's own origin (`GET /api/agents/:id/ssh`); the panel opens
  the SSH connection to the host of the agent's URL, port 22.
- **Off by default**: a Settings switch "SSH terminal" (like Swagger), with a warning that commands run in the
  terminal bypass the panel's protections and audit. Off → the sub-tab is hidden and the WebSocket route answers
  404; turning it off closes open terminals.
- **Only registered servers**: the API takes no free host or port, so the panel is never an open TCP relay. The
  agent's URL host must be the server itself (not a reverse proxy in front of the agent).
- **Host key pinned on first use**: the first connection shows the key's SHA-256 fingerprint and asks the operator
  to confirm it; it is saved per server. A different key later refuses the connection until an administrator
  clears the saved key from the Terminal sub-tab (audited).
- **Credentials never kept**: the SSH password, key and passphrase travel only in the WebSocket's first message,
  are dropped after the handshake, and never reach the database, logs, audit, responses or browser storage. SSH
  errors are reported as fixed messages, never the server's own text.
- **Audit**: `ssh.open` (operator, IP, `server:user`, result), `ssh.close` (duration), `ssh.hostkey.trust`,
  `ssh.hostkey.reset`. Keystrokes and output are not recorded: the panel audits the connection, not what is done in
  it.
- **Session bound to the panel session**: logout, panel session expiry, panel shutdown or switch off closes the
  terminal; terminal input counts as activity. At most 4 terminals per operator.
- **Desktop app**: the Wails asset proxy carries no WebSocket, so the Terminal sub-tab shows disabled there with
  "The SSH terminal is not available in the desktop app", like Queues, and `/terminal` opens nothing.

### Why the panel and not the agent

- An agent shell would run as the agent's service account (root or SYSTEM) behind one shared Bearer token: any
  holder of the token gets a root shell, and the host's own login, `sshd` policy and `auth.log` are bypassed. SSH
  keeps the host's authentication and logs the real user.
- The agent API is request/response plus a Redis task queue; an interactive stream does not fit it and would add a
  new, large attack surface to every server.
- SSH from the panel also reaches servers whose agent is down, and Windows servers with OpenSSH.

### Why not a browser SSH client (sshterm)

[c2FmZQ/sshterm](https://github.com/c2FmZQ/sshterm) runs a Go SSH client compiled to WebAssembly in the browser and
uses the server only as a WebSocket→TCP relay. That keeps the SSH password, key and terminal contents away from a
passive relay, but a compromised panel serves the SPA and can replace that client anyway, and the panel already
holds Domain Admin passwords in memory. It adds a WASM build of several MB, a key store in browser storage and a
relay to fence as tightly as the server-side client. We take its xterm.js front end and endpoint-per-server idea,
not the WASM client.

## Out of scope

Free host or port, a per-server SSH port, using the panel's AD credential for SSH, SFTP/file transfer, jump hosts,
SSH agent forwarding, session recording, command filtering, several terminals inside one page (open another browser
tab), SSH to "This panel"'s host, the desktop app.

## Capabilities

### New Capabilities

- `ssh-terminal`: the Terminal sub-tab, its WebSocket, host key pinning, limits and audit.

### Modified Capabilities

- `app-desktop-wails`: the Terminal sub-tab disabled in the desktop app (added requirement).
- `web-panel`: the Audit requirement gains the SSH terminal exception (connection audited, not its commands).

## Impact

`internal/server` (WebSocket handler, host key route, the Settings switch, shutdown, `openapi.yaml`),
`internal/store` (host key per agent, with a migration for existing databases), `internal/i18n`,
`frontend/src/pages/Terminal.vue`, `components/BackupTabs.vue`, `App.vue` (Backup tab active on `/terminal`),
`main.js` (route), the pt-BR catalogs, `frontend/package.json` (`@xterm/xterm`, `@xterm/addon-fit`), `go.mod`
(`golang.org/x/crypto` and `golang.org/x/net` become direct), `docs/`, CHANGELOG and screenshots (Backup sub-tabs,
Terminal, Settings).
