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
