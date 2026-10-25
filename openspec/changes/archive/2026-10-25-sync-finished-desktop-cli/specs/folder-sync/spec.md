## ADDED Requirements

### Requirement: Run end in the CLI
`ad-manager sync list` SHALL show, for each run, when it ended and how long it took, with the date when it ended on
another day than it started, and `—` while it goes on.

#### Scenario: Listing runs
- **WHEN** support runs `ad-manager sync list <server>` after a run that started at 08:37:14 and finished at 08:41:52
- **THEN** that run's FINISHED column reads `08:41 · 4m38s`

#### Scenario: Run across midnight
- **WHEN** a run ended on the day after it started
- **THEN** its FINISHED column shows the end date and time and the duration
