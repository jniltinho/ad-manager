## ADDED Requirements

### Requirement: Shared size tokens
The web panel's stylesheet SHALL define its control, toolbar, table-cell and badge sizes as named CSS variables
(`--control-h`, `--input-px`, `--select-pr`, `--button-px`, `--toolbar-gap`, `--toolbar-mb`, `--th-px`, `--th-py`,
`--td-px`, `--td-py`, `--tag-px`, `--tag-py`, `--tag-size`), used by its existing classes, so that the desktop reads
the same values.

#### Scenario: No visual change
- **WHEN** the web panel is captured before and after the tokens are introduced, in every theme
- **THEN** the computed sizes of inputs, selects, buttons, table cells and badges are the same
