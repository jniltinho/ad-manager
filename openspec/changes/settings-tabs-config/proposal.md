## Why

The Settings screen is one long page: runtime settings, branding, the agent token, the import credential and a
read-only card "From config.toml" side by side. Changing the domain controller, the LDAPS CA, the administrators
group or the panel's certificate still means SSH to the server, editing `/opt/ad-manager/config.toml` by hand and
restarting the service — with no check that the new values work, so a typo locks every operator out of the panel.

## What Changes

- **Settings in tabs** (sub-tabs like Backup › Sync › Terminal, kept in `?tab=`): **General** (language, theme,
  session, switches, users), **Company** (name, support link, logo), **Directory** (`[ad]`), **Panel server**
  (the read-only values) and **Agents** (agent token, import credential). The runtime settings keep
  their single Save; Directory has a second form, "Apply and restart".
- **The local agent applies config.toml**: the panel cannot write `config.toml` (`root:ad-manager 0640`,
  `ProtectSystem=strict`), but the agent on the same host runs as root and already reads it. The panel sends the
  change to that agent as a `panel:config` task in its queue; the agent validates it, edits only the allowlisted
  keys of `config.toml` (keeping comments, the other keys and `[agent]` untouched, with a `config.toml.bak`),
  restarts the panel, waits for it to answer and **rolls back** the file and restarts again when it does not.
- **Allowlisted keys** (v1: the Directory tab): `ad.url`, `ad.domain`, `ad.base_dn`, `ad.start_tls`, `ad.ca_file`,
  `ad.insecure_skip_verify`, `ad.admin_group`. No root process acts on them (the root agent never reads `[ad]`; a
  root-run CLI only connects with them). The agent refuses any other key.
- **The local agent** is the one registered server whose queue equals the panel's own `agent.queue` (the same
  `config.toml` gives both the same value); none or several → the Directory tab is read-only with a hint. The task
  carries the SHA-256 of the `config.toml` the panel reads; the agent compares it with its own file when it accepts
  the task and again in the worker — another host (a duplicate queue) or a file changed meanwhile fails the task. A
  random nonce the panel writes in its own state dir for each change, read back by the agent from its disk, rules
  out a copy of the file on another server.
- **The agent's write-mode switches** (the operator's choice, on the Agents tab): `agent.dc_file_server` and
  `agent.import_modes`. They give the panel's administrators the root agent's DC file mode, GPO import and password
  migration, so the card says so. The agent writes them and restarts itself (`systemctl restart --no-block
  ad-manager-agent`, after the task ends); the panel is restarted only when `[ad]` keys change too.
- **Only through the dedicated endpoint**: `PUT /api/settings/config`; the generic task proxy
  (`POST /api/agents/:id/tasks`) refuses `panel:config`.
- **Checked in two places**: the panel, with the operator's session, checks the operator is still an administrator
  under the running configuration and can connect, bind and be found in the administrators group with the new `[ad]`
  values (no saving a lockout); the agent checks the allowlist and value formats and parses the edited file with the
  panel's own loader before replacing it.
- **Environment wins**: a key set by an `AD_*` variable in the panel's environment is shown locked with the variable.
- **Restart and rollback**: the agent runs `systemctl restart ad-manager`, then polls the panel's own `/api/health`
  on loopback (the port of `server.listen`; HTTPS when the panel has TLS, without verifying the certificate since it
  only checks readiness) for up to 60 s, accepting only the panel's answer (it has `ldap`, not the agent's `queue`)
  with the unit active. On failure it restores `config.toml.bak`, restarts again and fails the task with the reason.
  A marker file covers an agent that dies mid-apply: at its next start the agent restores the backup and restarts the
  panel. Under supervisord (the all-in-one image regenerates `config.toml` at each start) the task is refused and the
  tab is read-only. The restart ends every session (sessions live in memory): the screen says so and returns to the
  login; the last task's result shows on the Directory tab.
- **Audit**: the panel logs `settings.config` (the changed key names and the task id, never values); the agent logs
  `config.apply` to its journal; the task's result (`ok` or `rolled back: …`) shows on the Directory tab.

## Out of scope

`[server]` (listen and TLS need a finer readiness probe and certificate checks under the panel's own rights: a later
change), keys the root agent also reads (`redis.*`, `samba.log_dir`, `server.audit_log`, `server.access_log`),
the rest of `[agent]`, `[db]`, `setup`, any secret (environment only); the all-in-one Docker image; a panel without a local agent;
hot reload without a restart; keeping sessions across the restart.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `settings`: tabs on the Settings screen; the panel's `config.toml` keys editable through the local agent.
- `agent-tasks`: the `panel:config` task (allowlist, edit, restart, rollback).

## Impact

`internal/ad` (`CheckAdmin`: connect, bind, administrators group), `internal/config` (allowlist and a comment-keeping
key editor), `internal/agent` (`panel:config` validation and handler, restart, health poll, rollback, audit),
`internal/server` (`settings_config.go`: `GET/PUT /api/settings/config`), `internal/server/openapi.yaml`,
`internal/i18n`, `frontend/src/pages/Settings.vue` (tabs, config form), the pt-BR catalog, `docs/deployment.md`,
`docs/agent.md`, CHANGELOG and the Settings screenshots.
