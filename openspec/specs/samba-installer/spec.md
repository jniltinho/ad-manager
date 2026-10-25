# samba-installer Specification

## Purpose
Install Samba 4 (DC, member, panel and agent) with an idempotent Python script, ready for ad-manager.

## Requirements

### Requirement: DC with the panel ready
`samba-setup.py new-dc --panel` (or `dc-join --panel`) SHALL provision the DC and leave the ad-manager panel
running, speaking LDAPS with `ca_file` pointing to the CA of the Samba certificate, without `insecure_skip_verify`.

#### Scenario: New domain
- **WHEN** the operator runs `new-dc --realm example.local --domain EXAMPLE --ip 10.0.0.10 --panel --deb <package>`
- **THEN** the panel responds at `https://10.0.0.10:8443`, a Domain Admin can sign in and
  `ad-manager user list` validates the LDAPS certificate

### Requirement: File server with the agent ready
`samba-setup.py member --agent` SHALL join the server to the domain, start the agent with a queue on the local Redis
and show the token only once; without `--new-token`, a new run keeps the existing token.

#### Scenario: Share created by the panel
- **WHEN** the agent installed by the script is registered in the panel with the token shown
- **THEN** the panel creates a share on it and a user of the write group writes over SMB

### Requirement: Password outside the arguments
The script SHALL read the administrator password from the terminal, from `AD_PASSWORD_FILE` (0600) or from stdin,
and `--dry-run` SHALL show the commands without changing the system or asking for a password.

#### Scenario: Preview
- **WHEN** the operator runs any role with `--dry-run`
- **THEN** each command appears prefixed with `[dry-run]`, nothing is installed and the password is not requested

### Requirement: Import write modes prepared by the installer
`samba-setup.py new-dc --panel --files --import-ready` SHALL turn on the DC agent's GPO-import and
password-migration write modes for the new domain's administrator: the password it already read written once to
`/opt/ad-manager/import.pass`, 0600 root from creation, and the `gpo_restore_*` and `password_migration_*` keys in
`[agent]` pointing at it. It SHALL refuse `--import-ready` with any other role or without `--panel --files`, and a
re-run SHALL NOT duplicate the keys.

#### Scenario: File and keys
- **WHEN** `new-dc --realm example.local --domain EXAMPLE --ip 10.0.0.10 --admin Administrator --panel --files
  --import-ready` finishes, with the password on stdin
- **THEN** the file is `0600 root` with only the password, and `[agent]` has `gpo_restore_user = "Administrator"` and
  `password_migration_user = "Administrator@example.local"`, both password-file keys pointing at it

#### Scenario: Wrong role
- **WHEN** the operator runs `dc-join … --panel --files --import-ready` or `new-dc … --panel --import-ready`
- **THEN** the script stops before changing anything, saying `--import-ready` goes with `new-dc --panel --files`

#### Scenario: Preview
- **WHEN** the same command runs with `--dry-run`
- **THEN** the step and the file appear as `[dry-run]` lines and no password is asked or written

#### Scenario: Run again
- **WHEN** the setup runs a second time with the option
- **THEN** the file is rewritten with the current password and the four keys appear once

### Requirement: Panel started last on a DC
For the DC roles with `--panel`, the script SHALL start (or restart) the panel only after the DC agent part
(`--files`) and the import write modes (`--import-ready`) are done, so the panel leaving setup mode means those steps
finished.

#### Scenario: Agent registration fails
- **WHEN** registering the DC agent in the panel fails during a wizard setup
- **THEN** the panel is not restarted and the wizard shows the installation stopped with the error; for a new domain,
  a re-run goes on from the already provisioned domain
