## ADDED Requirements

### Requirement: Wails desktop has no SSH terminal
The desktop app SHALL show the Terminal sub-tab disabled when the panel's SSH terminal is on, with "The SSH terminal
is not available in the desktop app" on hover, and SHALL open no WebSocket to the panel, also when `/terminal` is
reached directly or reloaded.

#### Scenario: Panel with the terminal on
- **WHEN** the operator signs in from the desktop app to a panel whose SSH terminal is on
- **THEN** Terminal shows disabled beside Backup and Sync, says "The SSH terminal is not available in the desktop
  app" on hover, and a click opens nothing

#### Scenario: Direct navigation
- **WHEN** the desktop window navigates to `/terminal` or reloads it
- **THEN** the page shows the same message and no WebSocket is opened
