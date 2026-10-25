## ADDED Requirements

### Requirement: Keyboard focus on desktop buttons
The desktop app's buttons (`volButton`: action, dialog and toolbar buttons) SHALL take the keyboard focus with the
theme's focus ring, as entries, selects and checks do, and SHALL be pressed with Space or Enter. The sidebar items,
the inner tabs and the password reveal button are not covered.

#### Scenario: Tab reaches a button
- **WHEN** the focus is on a field and the operator presses Tab, with a disabled button and then an enabled one after
  the field
- **THEN** the focus skips the disabled button and lands on the enabled one, which shows the focus ring

#### Scenario: Press with the keyboard
- **WHEN** a focused button receives Space, Return or Enter
- **THEN** its action runs once per key; a disabled button's action does not run

#### Scenario: Focus inside a scroll
- **WHEN** Tab lands on a button below or above the visible part of a scrolling list (a user's Groups)
- **THEN** the list scrolls so that the button and its ring are visible

#### Scenario: Ring leaves with the focus
- **WHEN** the focus moves away from the button
- **THEN** the ring disappears and the button looks as before

#### Scenario: Click does not show the ring
- **WHEN** the operator clicks a button with the mouse, focused or not
- **THEN** its action runs and no focus ring remains
