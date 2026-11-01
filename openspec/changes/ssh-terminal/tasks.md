## 1. Server

- [ ] 1.1 Settings: `ssh_terminal` switch (off by default) in the settings struct, API, `openapi.yaml`; `auth.ssh`
      in the session info; turning it off cancels every terminal.
- [ ] 1.2 Store: `ssh_host_key` and `ssh_host_key_rev` on the agents table with an `ALTER TABLE` migration for existing databases;
      trust update conditional on the revision; cleared (and audited) when an agent's URL host changes.
- [ ] 1.3 `internal/server/ssh.go`: `GET /api/agents/:id/ssh` (session, switch, Origin = Host, atomic slot of 4
      per lower-case user, first message ≤ 64 KiB within 30 s, `cols`/`rows` 1–1000), `x/crypto/ssh` client with a
      20 s deadline over the whole start (password, keyboard-interactive, key + passphrase), PTY, resize, pings,
      TOFU callback, fixed error codes, a codec that keeps the frame type.
- [ ] 1.4 Terminal registry per session (one mutex with the switch state, so admission and switch-off are atomic): logout, deletion, per-terminal expiry timer and switch-off cancel; input
      refreshes the idle time only on a live session; shutdown cancels and waits for `ssh.close`.
- [ ] 1.5 `DELETE /api/agents/:id/ssh/hostkey`; audit `ssh.open`, `ssh.close`, `ssh.hostkey.trust`,
      `ssh.hostkey.reset`.
- [ ] 1.6 `internal/i18n` entries for the new messages; `UPDATE_I18N=1 go test ./internal/i18n`.
- [ ] 1.7 Tests with an in-process SSH server: every scenario of the `ssh-terminal` spec (see the design's Testing
      list).

## 2. Web panel

- [ ] 2.1 `@xterm/xterm` and `@xterm/addon-fit`; `pages/Terminal.vue` (Servers list as in Backup without "This
      panel", user, password or key file, fingerprint dialog, Connect/Disconnect, Clear SSH host key, fit on
      resize); route `/terminal` in `main.js`.
- [ ] 2.2 `BackupTabs.vue`: Terminal sub-tab when `auth.ssh`, disabled in the desktop app with its message;
      `App.vue`: Backup top tab active on `/terminal`; the page opens no WebSocket in desktop mode.
- [ ] 2.3 Settings screen switch with the bypass warning.
- [ ] 2.4 pt-BR strings (`locales/pt-BR/*.js`, `desktop.js`); Playwright test (`echo $((6*7))` → `42`, desktop
      mode `/terminal` opens no WebSocket).

## 3. Docs and validation

- [ ] 3.1 `docs/` (turning it on, the bypass warning, direct-host agent URL, Nginx/Caddy `Upgrade`, `Host` and idle
      timeout, host key reset), CHANGELOG.
- [ ] 3.2 `make check && make test && make build`; `make desktop-check`; `make samba-up && make test-integration`;
      check the sub-tab in the browser against the test Samba with an `sshd`.
- [ ] 3.3 Screenshots (Backup sub-tabs, Terminal, Settings; desktop Backup tab): 2×, Classic blue, English, Example
      Company Ltd, `domain.local`.
- [ ] 3.4 Review the implementation with Codex (`codex exec --sandbox read-only …`).
