## ADDED Requirements

### Requirement: Shared size tokens
The web panel's stylesheet SHALL define its control, toolbar, table-cell and badge sizes as named CSS variables
(`--control-h`, `--input-px`, `--select-pr`, `--button-px`, `--toolbar-gap`, `--toolbar-mb`, `--th-px`, `--th-py`,
`--td-px`, `--td-py`, `--tag-px`, `--tag-py`, `--tag-size`), used by its existing classes, so that the desktop reads
the same values.

#### Scenario: No visual change
- **WHEN** the web panel is captured before and after the tokens are introduced, in every theme
- **THEN** the computed sizes of inputs, selects, buttons, table cells and badges are the same

### Requirement: A share's name edits it
In the Shares table, a share's name SHALL be the link that opens its edit form, as a user's login does in Users, and
the Actions column SHALL hold only remove. The name SHALL be disabled while the chosen file server is down. The
desktop app's Shares page SHALL behave the same.

#### Scenario: Edit from the name
- **WHEN** the operator clicks the share name "finance" on a file server that is up
- **THEN** the edit form of "finance" opens, and the row shows no separate edit link

#### Scenario: Server down
- **WHEN** the chosen file server is unavailable
- **THEN** clicking a share's name does not open the form

### Requirement: A file server's row stays on one line
In the Shares page's list of file servers, each server SHALL take one line: its name, host, DC tag and its edit and
remove actions never wrap. When the row is too narrow, the host SHALL shorten first and then the name, both ending in
"…" with the full text in a tooltip; the DC tag and the actions SHALL keep their size. The total size in the Shares
header SHALL be vertically centered on its buttons. The desktop app's Shares page SHALL behave the same.

#### Scenario: Long server name
- **WHEN** a domain controller registered as "fileserver-headquarters-saopaulo-01" is listed in the narrowest list
- **THEN** its row is one line high, the name ends in "…", and the DC tag, "edit" and "remove" are fully visible
