## MODIFIED Requirements

### Requirement: Administration interface
The panel SHALL offer, in an interface using the Default visual theme (square corners), screens for: overview (counts and DC health), users, groups, OUs and computers, with search, filters, inline actions and confirmation before destructive actions. The domain password policy SHALL be shown and changed in a dialog opened from the users screen, not in a screen of its own.

#### Scenario: Find and reset a password
- **WHEN** the operator searches for "john" on the users screen and chooses "reset password"
- **THEN** a dialog asks for the new password (with a generator and a change-at-next-sign-in option) and confirms success

#### Scenario: Delete confirmation
- **WHEN** the operator clicks delete on a user
- **THEN** the panel requires confirmation by typing the login before deleting

#### Scenario: Password policy from the users screen
- **WHEN** the operator clicks "Password policy" on the users screen toolbar
- **THEN** a dialog shows the domain's default password policy, and saving asks for confirmation (warning that a Windows DC's Default Domain Policy may reapply the old values) before writing it

#### Scenario: Old password policy links
- **WHEN** the operator opens `/password-policy` (or `/politica`), or follows Home's weak policy alert
- **THEN** the panel shows the users screen with the password policy dialog open, and the top tabs have no Password policy entry
