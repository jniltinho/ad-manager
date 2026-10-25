## ADDED Requirements

### Requirement: Import credential kept sealed in the panel
The panel SHALL keep the target domain's administrator credential used by the import write modes in its database,
sealed with the panel's secret box and bound to the user name, SHALL check it as a Domain Admin (RID 512, whatever
group the panel's sign-in requires) before saving it,
SHALL never return the password through the API, logs or audit, and SHALL let an administrator remove it.

#### Scenario: Save the credential
- **WHEN** an administrator saves user `Administrator` and its password in Settings
- **THEN** the panel stores only the sealed password, `GET /api/settings/import-credential` answers
  `{"user":"Administrator","set":true}` and the audit records `settings.import_credential` without the password

#### Scenario: Wrong credential
- **WHEN** the password is wrong or the user is not a Domain Admin
- **THEN** nothing is saved and the panel answers with the reason

#### Scenario: Remove
- **WHEN** an administrator removes the credential
- **THEN** the sealed value is deleted and a GPO import answers that the credential is not set

### Requirement: Write modes without a password on the DC
A DC agent SHALL turn its import write modes on with `import_modes = true` (no secret on disk) and SHALL take the
administrator credential for a GPO restore or import from the panel's request, through a 0600 temporary file
removed at the end; password migration SHALL depend on the switch alone. The former `gpo_restore_*` and
`password_migration_*` keys SHALL keep working.

#### Scenario: GPO import with the panel's credential
- **WHEN** the agent has `import_modes = true`, no password file, and the panel sends a GPO import with the kept
  credential
- **THEN** the GPOs are created and no file with the password remains on the DC

#### Scenario: Only to a domain controller
- **WHEN** an agent whose health says it is a DC of this domain is registered by a host that is neither this host
  (loopback) nor a domain controller of this domain in the directory (by its DNS name or an address that name
  resolves to)
- **THEN** the panel never picks it for a GPO task or a password migration, so neither the import credential nor a
  source's password reaches it, whatever its health says

#### Scenario: Switch off
- **WHEN** `import_modes` is false or absent and no former key is set
- **THEN** `/api/health` reports `gpo_restore` and `password_migration` false and the agent refuses both

### Requirement: Installer stores the credential in the panel
`samba-setup.py new-dc --panel --files --import-ready` SHALL write `import_modes = true` and store the administrator
credential in the panel's database (the password on stdin, as the service user), and SHALL NOT write
`/opt/ad-manager/import.pass`.

#### Scenario: Prepared domain
- **WHEN** a new domain is created with `--import-ready`
- **THEN** `/opt/ad-manager/import.pass` does not exist, `config.toml` has `import_modes = true` and Settings shows the
  import credential set
