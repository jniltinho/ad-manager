# packaging Specification

## Purpose
TBD - created by archiving change deb-restart-on-upgrade. Update Purpose after archive.

## Requirements

### Requirement: Services on install and upgrade
The `.deb` SHALL NOT enable or start any service on a fresh install, and on an upgrade SHALL restart only the panel
and agent services that are running, so they run the new binary, reporting which ones it restarted.

#### Scenario: Upgrade with the services running
- **WHEN** the operator installs a newer `.deb` while `ad-manager` and `ad-manager-agent` are active
- **THEN** both are restarted by the package and `/api/health` reports the new version

#### Scenario: Upgrade with a service stopped
- **WHEN** the operator upgrades a machine where `ad-manager-agent` is stopped or disabled
- **THEN** the package leaves it stopped and does not enable it

#### Scenario: Fresh install
- **WHEN** the package is installed for the first time
- **THEN** no service is started or enabled
