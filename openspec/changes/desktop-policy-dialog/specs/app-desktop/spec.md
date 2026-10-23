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
