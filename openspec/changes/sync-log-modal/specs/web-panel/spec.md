## ADDED Requirements

### Requirement: Sync run-log modal
A sync run's log SHALL be shown in a dialog capped to a fraction of the viewport height, in which only the monospace
log body scrolls (`overflow-y:auto`) while the run's summary header (job name, run id, status and file/size counts)
and the footer stay in view. The page SHALL NOT grow with the log, and the dialog SHALL stay on screen on small
viewports. The dialog SHALL be dismissable with Escape and the Close button and SHALL offer a Copy action for the log
text.

#### Scenario: Very long log
- **WHEN** a run's log has thousands of lines (e.g. a dry-run printing a skip line per object) and the operator opens
  its Log
- **THEN** the log scrolls inside the dialog and the page itself does not overflow or grow downward

#### Scenario: Header and footer stay
- **WHEN** the operator scrolls through a long log
- **THEN** the summary header and the Close/Copy footer remain in view while the log body scrolls

#### Scenario: Small viewport
- **WHEN** the dialog is opened on a short screen
- **THEN** it is capped to the viewport and stays fully on screen, with the log body scrolling inside it

#### Scenario: Empty or short log
- **WHEN** a run has no log or only a few lines
- **THEN** the dialog renders correctly without scrolling and shows the run's summary
