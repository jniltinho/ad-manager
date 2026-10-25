## ADDED Requirements

### Requirement: Framed lists
Every list table that scrolls inside the page (Users, Groups, Computers and the logs) SHALL be framed on all four
sides in the theme's line color, whether or not its content overflows and a scrollbar shows; the frame belongs to
the scrolling wrapper, not to the collapsed table border.

#### Scenario: Long list
- **WHEN** a list has more rows than fit and scrolls
- **THEN** its left and right lines are visible from top to bottom, in every theme

#### Scenario: Short list
- **WHEN** a list fits without scrolling
- **THEN** it is framed the same way
