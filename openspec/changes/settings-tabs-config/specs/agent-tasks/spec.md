## ADDED Requirements

### Requirement: Panel configuration task
The agent SHALL accept a `panel:config` task only when the panel runs on its host under systemd (the `ad-manager`
unit; never under supervisord), whatever its read-only state, with no retries, and when the task's `config_sha256` matches
its own `config.toml` and its `nonce` matches the panel's nonce file in the state dir — checked when accepting and
again in the worker. It SHALL refuse any key outside the panel
allowlist (the `settings` capability's list).

#### Scenario: Another host's file
- **WHEN** the task's `config_sha256` differs from the agent's `config.toml`
- **THEN** the agent refuses it (400 on enqueue, a failed task in the worker) and changes nothing

#### Scenario: Copied config.toml on another server
- **WHEN** another server's agent has an identical `config.toml` but not the panel's nonce file
- **THEN** the agent refuses the task and changes nothing

#### Scenario: Key the agent also reads
- **WHEN** the task carries `redis.addr`, `samba.log_dir`, `server.audit_log` or an `agent.*` key other than
  `agent.dc_file_server` and `agent.import_modes`
- **THEN** the agent responds 400 and enqueues nothing

#### Scenario: Not the panel's host
- **WHEN** a file server without the panel receives `panel:config`
- **THEN** the agent responds 400 and enqueues nothing

### Requirement: Config edit keeps the file
The task SHALL change only the given keys in `config.toml`, keeping comments, the order and every other key, save the
previous file as `config.toml.bak` and replace the file atomically with the same owner and mode, after the result
parses with the panel's loader to the requested values.

#### Scenario: Comments kept
- **WHEN** the task changes `ad.url` in a commented `config.toml`
- **THEN** only the `url` line of `[ad]` differs from `config.toml.bak`

#### Scenario: Invalid value
- **WHEN** `ad.url` is not an `ldap://` or `ldaps://` URL
- **THEN** the agent refuses the task and `config.toml` is unchanged

### Requirement: Restart with rollback
After writing, the agent SHALL restart the panel and poll the panel's own `/api/health` on loopback for up to 60 s;
when the panel does not answer, it SHALL restore `config.toml.bak`, restart the panel again and fail the task with the
reason. A marker written before the replacement makes an agent that died mid-apply restore the backup at its next
start. It logs `config.apply` with the changed key names and the result to its journal; the result is the task's.

#### Scenario: Panel back with the new values
- **WHEN** the panel answers its health check after the restart
- **THEN** the task completes with `ok` and the panel's Directory tab shows the change as applied

#### Scenario: Panel does not come back
- **WHEN** the panel exits at start with the new values
- **THEN** the agent restores `config.toml.bak`, restarts the panel and the task fails with `rolled back`

#### Scenario: Agent died mid-apply
- **WHEN** the agent starts and finds the apply marker
- **THEN** it restores `config.toml.bak`, restarts the panel and removes the marker

### Requirement: Agent switches restart the agent
When the task changes `agent.dc_file_server` or `agent.import_modes`, the agent SHALL write them and queue its own
restart after the task ends; it restarts the panel only when `[ad]` keys change too.

#### Scenario: Only the agent's switches
- **WHEN** the task sets `agent.import_modes = true`
- **THEN** `config.toml` has `import_modes = true` under `[agent]`, the panel is not restarted, the task completes and
  the agent restarts with the GPO import and password migration modes on
