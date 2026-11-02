## ADDED Requirements

### Requirement: Settings tabs
The Settings screen SHALL group its content in sub-tabs — General, Company, Directory, Panel server and Agents — with
the active tab in the `tab` query parameter, so a reload or a link opens the same tab. An unknown `tab` opens General.

#### Scenario: Open a tab by link
- **WHEN** an administrator opens `/settings?tab=directory`
- **THEN** the Directory tab is active and shows the `[ad]` fields

#### Scenario: Runtime settings across tabs
- **WHEN** the administrator changes the theme on General and the company name on Company and saves
- **THEN** one `PUT /api/settings` carries both changes

### Requirement: Editable panel configuration
The panel SHALL let a domain administrator change, on the Directory tab, the allowlisted `config.toml` keys
(the `[ad]` keys except secrets), and on the Agents tab the agent's switches `agent.dc_file_server` and
`agent.import_modes`, by
sending a `panel:config` task, with the SHA-256 of its `config.toml`, to the one local agent. The panel MUST NOT write
`config.toml`, and the generic task proxy MUST refuse `panel:config`.

#### Scenario: Apply a new domain controller
- **WHEN** the administrator changes `ad.url` to a reachable DC and applies
- **THEN** `PUT /api/settings/config` responds 202 with the task id and the local agent's queue gets `panel:config`

#### Scenario: Turn on the import modes
- **WHEN** the administrator ticks "GPO import and password migration" on the Agents tab and applies
- **THEN** a `panel:config` task with `agent.import_modes: true` goes to the local agent and the panel is not restarted

#### Scenario: No local agent
- **WHEN** no registered agent, or more than one, has the panel's `agent.queue`
- **THEN** `GET /api/settings/config` reports `agent: null`, the fields are read-only and `PUT` responds 409

#### Scenario: Through the generic proxy
- **WHEN** someone posts a `panel:config` task to `POST /api/agents/:id/tasks`
- **THEN** the panel responds 400 and forwards nothing

#### Scenario: Value set by the environment
- **WHEN** `AD_URL` is set in the panel's environment
- **THEN** `GET /api/settings/config` reports `ad.url` locked by `AD_URL` and a `PUT` changing it responds 400

### Requirement: Lockout check before applying
Before sending the task, the panel SHALL check, with the signed-in operator's credential, that the operator is an
administrator under the running configuration and can connect, bind and be found in the administrators group with
the new `[ad]` values; when a check fails it responds 400 naming the key and sends nothing.

#### Scenario: Lockout refused
- **WHEN** the administrator sets `ad.admin_group` to a group they are not a member of
- **THEN** the API responds 400 naming `ad.admin_group` and no task is sent

#### Scenario: Unreachable DC
- **WHEN** `ad.url` points to a host with no LDAPS
- **THEN** the API responds 400 naming `ad.url`

## MODIFIED Requirements

### Requirement: Audited and protected settings
Reading and writing settings SHALL require an administrator session; every write produces a `settings.update` (or
`settings.logo`, `settings.config`) audit line without sensitive content; `settings.config` names the changed keys
and the task id, never the values. The name, link and logo are public (login).

#### Scenario: No session
- **WHEN** someone without a session calls `PUT /api/settings` or `PUT /api/settings/config`
- **THEN** they get 401

#### Scenario: Config audit without values
- **WHEN** the administrator applies new `ad.url` and `ad.admin_group` values
- **THEN** the audit log has `settings.config` with `ad.url,ad.admin_group` and neither value
