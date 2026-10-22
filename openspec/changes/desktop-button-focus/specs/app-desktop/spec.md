## ADDED Requirements

### Requirement: Keyboard focus on desktop buttons
Every button of the desktop app SHALL take the keyboard focus (Tab, skipping disabled buttons), SHALL show the
theme's focus ring while focused (the ring drawn around entries, selects and checks: the theme's ring width, 3 px in
Crystal and High contrast, in its focus color), and SHALL be pressed with Space or Enter. A mouse click SHALL NOT
focus a button.

#### Scenario: Tab reaches a button
- **WHEN** the focus is on a field and the operator presses Tab, with a disabled button and then an enabled one after
  the field
- **THEN** the focus skips the disabled button and lands on the enabled one, which shows the focus ring

#### Scenario: Press with the keyboard
- **WHEN** a focused button receives Space, Return or Enter
- **THEN** its action runs once per key; a disabled button's action does not run

#### Scenario: Ring leaves with the focus
- **WHEN** the focus moves away from the button
- **THEN** the ring disappears and the button looks as before

#### Scenario: Click does not show the ring
- **WHEN** the operator clicks a button with the mouse
- **THEN** its action runs and no focus ring appears
