# logs-samba Specification

## Purpose
Read Samba logs through the panel, agent and MCP to diagnose replication, DNS and access issues.

## Requirements

### Requirement: Read Samba logs
The panel SHALL list the `log.*` files of the Samba log directory (of the server itself and of each file server
through its agent) and show the most recent entries of a file, with date, level, source and message, a text filter
and an entry limit, read-only and only for a signed-in administrator.

#### Scenario: DNS error on the DC
- **WHEN** the administrator opens Logs → Samba — this server → `log.samba`
- **THEN** they see the newest entries first, with level 0 highlighted

#### Scenario: Path outside the directory
- **WHEN** someone asks for `../../etc/shadow`, a symbolic link or a file that does not start with `log.`
- **THEN** the API responds 404 and nothing is read
