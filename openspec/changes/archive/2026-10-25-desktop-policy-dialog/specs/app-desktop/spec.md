## ADDED Requirements

### Requirement: Password policy from Users
The desktop app SHALL open the domain's password policy in a dialog from a **Password policy** button of the Users
toolbar, as the web panel does, and SHALL NOT list it in the side menu.

#### Scenario: Open from Users
- **WHEN** the operator clicks **Password policy** on the Users toolbar
- **THEN** a dialog shows the current policy (complexity, lengths, ages, lockout) with its weak-policy warning, and
  saving asks for a confirmation before changing the domain's policy

#### Scenario: Weak policy alert
- **WHEN** the operator follows Home's policy alert
- **THEN** the app goes to Users and opens the policy dialog

#### Scenario: Side menu
- **WHEN** the app is signed in
- **THEN** the side menu has no Password policy item

### Requirement: Users toolbar on as few lines as fit
The Users toolbar SHALL put the search field, the filter and count, the action buttons and the extras (Password
policy, Export CSV, Refresh) on one line when the window is wide enough, letting the search field shrink first; when
it is not, the extras SHALL go behind a ⋯ menu, and only then SHALL the actions move to a second line.

#### Scenario: Wide window
- **WHEN** the Users page is shown in a window wide enough for everything
- **THEN** search, filter, count, the actions and the extras are on a single line

#### Scenario: Default window
- **WHEN** the window is 1280 px wide
- **THEN** the actions are on a second line and the extras stay visible beside the search, with no ⋯
