## ADDED Requirements

### Requirement: Wails desktop footer version
The desktop app's footer SHALL show the app's own version (the one its release tag gave it) and, when the connected
panel runs another version, the panel's beside it, each labeled with what it is.

#### Scenario: Desktop app newer than the panel
- **WHEN** a v0.27.1 desktop app is signed in to a v0.27.0 panel
- **THEN** the footer shows "desktop v0.27.1 · panel v0.27.0"

#### Scenario: Same version
- **WHEN** the desktop app and the panel run the same version
- **THEN** the footer shows that version once, as the web panel does
