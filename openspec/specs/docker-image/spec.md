# docker-image Specification

## Purpose
Demo Docker image that provisions a Samba 4 domain on first start and runs the panel and agent against it.

## Requirements

### Requirement: Demo image
The project SHALL have a Docker image that, on first start, provisions a Samba 4 domain and connects the panel and
the agent to it, with HTTPS through a self-signed certificate generated in the image, and that on later starts uses
the domain and configuration stored in the volumes.

#### Scenario: Password provided
- **WHEN** the container starts for the first time with `AD_DOMAIN=example.local` and `AD_ADMIN_PASSWORD`
- **THEN** the domain `example.local` exists, Administrator signs in to the panel with that password and the password
  does not appear in any command line or in the log

#### Scenario: Generated password
- **WHEN** the container starts for the first time without `AD_ADMIN_PASSWORD`
- **THEN** a strong password is generated, applied to Administrator and shown once in the log, together with the
  panel URL

#### Scenario: Weak password
- **WHEN** `AD_ADMIN_PASSWORD` has fewer than 8 characters or fewer than three character types (lowercase,
  uppercase, digits, symbols)
- **THEN** the container stops before provisioning, stating the rule

#### Scenario: Restart
- **WHEN** the container is recreated with the same volumes
- **THEN** the domain is not provisioned again, the certificate and the agent token are kept and the panel comes
  back with the same domain

#### Scenario: Agent
- **WHEN** the panel opens after the first start
- **THEN** this DC's agent appears under Shares, registered, with the root `/srv/shares`
