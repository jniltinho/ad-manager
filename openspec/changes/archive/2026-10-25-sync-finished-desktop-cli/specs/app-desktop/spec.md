## ADDED Requirements

### Requirement: Desktop sync run end and duration
The desktop app's Sync page SHALL show, in its Runs table, when each finished run ended and how long it took, in the
same format as the web panel, and SHALL show no end while the run is queued or running.

#### Scenario: Finished run
- **WHEN** a run started at 08:37:14 and finished at 08:41:52 the same day, in an English interface
- **THEN** its Finished cell reads `08:41 AM · 4m38s` (`08:41 · 4m38s` in pt-BR)

#### Scenario: Running run
- **WHEN** a run is queued or running
- **THEN** its Finished cell reads `—`
