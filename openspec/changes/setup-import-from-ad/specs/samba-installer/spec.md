## ADDED Requirements

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
- **THEN** the panel is not restarted, the wizard shows the installation stopped with the error, and a re-run goes on
  from there
