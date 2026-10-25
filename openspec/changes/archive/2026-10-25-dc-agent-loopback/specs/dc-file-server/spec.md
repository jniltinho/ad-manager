## ADDED Requirements

### Requirement: DC agent on the loopback
When the setup makes a DC also serve files, the agent it configures SHALL listen on `127.0.0.1:9443` only (the panel
beside it reaches it there), while a separate file server's agent SHALL keep listening on the network for its panel.

#### Scenario: New domain with files
- **WHEN** `samba-setup.py new-dc --panel --files` (or the wizard's "This DC also serves files") configures the agent
- **THEN** its `[agent]` listens on `127.0.0.1:9443`, the panel lists the DC under Shares, and port 9443 is not
  reachable from the network

#### Scenario: Separate file server
- **WHEN** `samba-setup.py agent` configures a file server's agent
- **THEN** it listens on `0.0.0.0:9443`, for the panel on another machine

#### Scenario: Existing agent configuration
- **WHEN** the setup runs again on a DC whose config already has an `[agent]` section
- **THEN** that section, its `listen` included, is left as it is
