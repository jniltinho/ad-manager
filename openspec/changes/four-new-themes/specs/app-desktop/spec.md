## ADDED Requirements

### Requirement: Desktop parity of the Matrix, Winamp, Pink Panther and High contrast themes
The desktop app SHALL render `matrix`, `winamp`, `pink-panther` and `high-contrast` from the generated palettes
when the panel reports them in `brand.theme`, with the same roles as the web (header and its stripe, sidebar from the
tabs, card titles, table head, buttons, dialogs, focus ring, login box and its bar, Winamp's bevel) and the flat
look, and SHALL hold `high-contrast` to 7:1 in every text color it draws, like the Crystal themes.

#### Scenario: Theme chosen in the panel
- **WHEN** the administrator saves `winamp` in Settings and an operator signs in to the desktop app
- **THEN** the desktop appears dark with the steel header and its gold stripe, the amber primary button and the Fyne
  dark variant

#### Scenario: Light theme
- **WHEN** the panel reports `pink-panther`
- **THEN** the desktop uses the light variant with the plum header, the pink stripe and the deep rose primary button

#### Scenario: High contrast without depth
- **WHEN** the panel reports `high-contrast` with the flat look off
- **THEN** the desktop draws it as the flat look (no button gloss or shadow, no card shadow, no login highlight or
  elliptic shadow), and every other theme keeps its depth

#### Scenario: High contrast drawn at AAA
- **WHEN** a desktop button, field, label or focused select is drawn in High contrast, at rest, hovered or pressed,
  normal or flat
- **THEN** its text has ≥ 7:1 over the drawn background and the focus ring is 3 px

#### Scenario: Bevel in Winamp
- **WHEN** the theme is `winamp` and the look is not flat
- **THEN** buttons and the login box are drawn with the theme's raised bevel; in the flat look, without it

#### Scenario: Accepted differences
- **WHEN** a new theme is shown on the web and on the desktop
- **THEN** the only differences are the documented ones: the page pattern, the colored glow, Winamp's green field
  text, Fyne's text-selection highlight and the font
