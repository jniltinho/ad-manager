## ADDED Requirements

### Requirement: Virtualized Users table
The Users table SHALL keep in the DOM only the rows in view plus a small overscan, whatever the number of users.
Sort, filter, selection, the row actions and keyboard focus SHALL act on every user, and the table SHALL report the
full row count to screen readers.

#### Scenario: Large domain
- **WHEN** the Users screen lists 5,000 users
- **THEN** fewer than 100 rows are rendered, and scrolling to the end shows the last user

#### Scenario: Sort over every row
- **WHEN** the operator sorts the 5,000 users by last logon
- **THEN** the first rendered row is the most recent logon of all users, not only of the rendered ones

#### Scenario: Screen reader count
- **WHEN** a screen reader reads the table
- **THEN** it announces 5,000 rows (`aria-rowcount`) and each rendered row's position (`aria-rowindex`)
