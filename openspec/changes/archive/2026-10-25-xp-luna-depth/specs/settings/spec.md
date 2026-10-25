## ADDED Requirements

### Requirement: Classic blue Luna depth at AAA
The `classic-blue` theme SHALL draw the Windows XP Luna look (vertical title-bar gradient in the header and dialog
heads, taskbar footer, green primary button, XP push buttons, task-pane card heads, listview column headers, orange
selected-tab top) through color, gradient and shadow values only, with the same layout as the other themes, and SHALL
hold normal text to 7:1, large text to 4.5:1 and components to 3:1, on the web and in the colors the desktop draws.
The flat look SHALL remove its gradients and depth shadows like any theme.

#### Scenario: Text contrast
- **WHEN** the contrast test runs for `classic-blue`
- **THEN** all normal text recipes reach 7:1, large text recipes 4.5:1 and component recipes 3:1

#### Scenario: Highlights away from text
- **WHEN** the header, a dialog head or the footer is drawn in Classic blue
- **THEN** the light Luna band is only at the top edge and the text sits over colors with ≥ 7:1 against white

#### Scenario: Same structure
- **WHEN** a page is drawn in Classic blue before and after this change
- **THEN** every element keeps its position and size; only colors, gradients and shadows differ

### Requirement: Flat look keeps the page dots
The flat look SHALL keep the page's standard dot pattern (in the theme's `color-page-dots`) in every theme, on the web
and on the desktop, while it removes gradients and depth shadows.

#### Scenario: Dots in the flat look
- **WHEN** the flat look is turned on in Classic blue
- **THEN** the page still shows the dot pattern, and the header, buttons and cards lose their gradients and shadows

## MODIFIED Requirements

### Requirement: Matrix theme
The theme registry SHALL include `matrix` (dark). With it, `color-scheme: dark`, the page is near-black `#010502`
with a static rain of short phosphor strokes, surfaces `#07130b`, text phosphor green `#a8f0b8`, the primary action
phosphor green (`#3dff6c → #00e03a`) with dark text, a monospace font and a green glow around the window, focused fields and dialogs. Normal
text reaches ≥ 4.5:1.

#### Scenario: Choose Matrix
- **WHEN** the administrator chooses "Matrix (dark)" and saves
- **THEN** `brand.theme` is `matrix`, the panel and the login turn dark with no light surface (except the customer
  logo plate), the text is in a monospace font and the primary button is phosphor green with dark text

#### Scenario: Pattern only outside the flat look
- **WHEN** the `matrix` theme is active and the flat look is on
- **THEN** the page has the standard dots instead of the rain pattern and the window no glow, and the colors stay the
  same
