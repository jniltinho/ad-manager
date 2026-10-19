# samba-installation Specification

## Purpose
Automated, idempotent installation of Samba 4 from distribution packages in the new-domain, additional-DC and member file-server roles.

## Requirements

### Requirement: Automated Samba installation
A script SHALL install and configure Samba 4 with distribution packages on Ubuntu 26.04 in three roles: new domain, additional DC of an existing domain and member file server with registry shares; it must be idempotent and have a simulation mode.

#### Scenario: Simulation
- **WHEN** the operator runs the script with `--dry-run`
- **THEN** it checks the prerequisites and shows what it would do, without changing the system

#### Scenario: Member file server
- **WHEN** the script runs in the member role with valid credentials
- **THEN** the server joins the domain, winbind resolves the AD groups and `net conf list` works for creating shares
