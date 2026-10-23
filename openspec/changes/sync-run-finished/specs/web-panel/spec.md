## ADDED Requirements

### Requirement: Sync run end and duration
The Sync page's Runs table SHALL show, for each finished run, the time it ended and how long it took, with the date
when it ended on another day than it started, and SHALL show no end while the run is queued or running.

#### Scenario: Finished run
- **WHEN** a run started at 08:37:14 and finished at 08:41:52 the same day
- **THEN** its Finished cell reads the end time and the duration, `08:41 AM · 4m38s` (in the panel's locale), on one line

#### Scenario: Run across midnight
- **WHEN** a run started on one day and finished on the next
- **THEN** its Finished cell shows the end date and time, and the duration

#### Scenario: Running run
- **WHEN** a run is queued or running
- **THEN** its Finished cell is empty (`—`) and the Status cell shows the phase
