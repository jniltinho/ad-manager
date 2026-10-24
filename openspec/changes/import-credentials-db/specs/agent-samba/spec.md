## MODIFIED Requirements

### Requirement: Restore a GPO as a new GPO
The panel SHALL restore a GPO backup only as a new GPO (new GUID, unlinked), with explicit confirmation, through the
agent of a DC whose import write mode is on (`import_modes = true`, or the former account and 0600 password file);
the administrator credential SHALL come from the panel's sealed import credential, sent with the task. Without the
mode the DC agent SHALL stay read-only. The file SHALL be validated before any write and the response SHALL list the
other DCs, whose SYSVOL Samba does not replicate.

#### Scenario: Restore
- **WHEN** support uploads the "Default Domain Policy" backup with the name "Copy of Default"
- **THEN** a new unlinked GPO with that name appears and the audit log records `gpo.restore` with the new GUID

#### Scenario: Agent without write mode
- **WHEN** the DC agent has neither `import_modes` nor `gpo_restore_user`
- **THEN** the panel responds 503 and nothing is written

#### Scenario: Malicious file
- **WHEN** the `.tar.gz` contains `../`, an absolute path, a link or two GPOs
- **THEN** the agent rejects it before calling `samba-tool`
