# Screen-by-screen audit: web vs egui

Measured on the docs screenshots (both 2×, Classic blue, `domain.local`) with PIL color-run and bounding-box scans
(the scripts are kept with the change: `audit-tools/runs.py`, `audit-tools/bbox.py`).
All sizes are logical px. The web window is 1600×900 and its app box starts at 16; the egui window is 1280×800. So
sizes, paddings, gaps and alignment are compared, and positions are given relative to their container. Some value
differences are only demo data (counts, LOCKED vs DEACTIVATED, OU paths), not layout.

## Fixes that repeat on every screen

| # | Part | Web | egui today | Fix |
|---|---|---|---|---|
| A1 | Single-line text fields | 28 (login 25) | **19** | 28 border box (login 25); same height as the buttons beside them |

Measurements count the painted border box; a button's drawn depth (gradient edge, shadow) is noted apart where it
changes the number (the web login button is 26 in CSS and paints 28 with its depth).
| A2 | Buttons, selects | 28; primary gradient, **bold** | 26; flat; regular | 28; gradient outside flat; primary and secondary semibold, ghost regular |
| A3 | Selects | White fill, small 10×6 arrow | Grey `#dcdbd6`, large arrow | Field look of the web select |
| A4 | Table header | One continuous band 29–31, 2 px bottom rule, **bold** | 26 cells with 4 px gaps, regular, 6 px gap before row 1 | One band, bold, no gaps, no gap before row 1; a header over every column, including "Actions" |
| A5 | Table rows | 27–28 + 1 px full-width divider (pitch 28–29); first row white, then striped | Pitch 32; first row striped; no full dividers | Pitch 29 with dividers; stripes from the second row |
| A6 | Table frame | Full content width, equal insets, 1 px border | Uneven 10/16 insets, no border | Equal insets, web border |
| A7 | Cell styles | Names bold; DN, paths, actions, sources monospace; numbers and sizes right-aligned; dates plain | Regular link blue; proportional; left-aligned; dates as links | Web cell styles |
| A8 | Badges | Bold UPPERCASE 10 px, pale fill, width fits the text, 17 tall | Fixed 64×18, solid fill, regular, lowercase | Web `.tag` per tone; text centered |
| A9 | Card head | 28, bold, light gradient, no bar (where the web has a card at all) | 38, regular, 4 px green bar, on every page | 28 bold head from the theme's card-head recipe; no card where the web has none |
| A10 | Header | 64, gradient; logo 114×26; bold title vertically centered; global search; bold user + "my password · sign out" links | 72, flat; logo 171×40; regular title; Configure and Sign out buttons | Web header (Configure stays as a header link) |
| A11 | Navigation | Top tabs 33 (active 37, white, 3 px orange top), gap 2, bold; strip 42 with a 1 px bottom line | Sidebar 199 | Top tab strip |
| A12 | Page tabs | Bordered 31 (active 34, white, 3 px orange top), bold, icon | Filled-blue pill (active), plain text (inactive) | `.subtab` recipe |
| A13 | Dialogs | Title 36–41, **bold, left-aligned**; labels UPPERCASE bold 11 px **above** fields; footer band 44 with gradient and top border; **primary first**, then Cancel; 460/760 | Title 43, regular, centered, 3 px underline with an 11 px notch at the right (bug); labels beside; inset line footer; Cancel first | Web dialog recipe; fix the notch |
| A14 | Callouts | Info: light blue with a green left border; warning: cream with a brown left border; line pitch ≈20 | Plain paragraphs, pitch ≈15 | Web callouts |
| A15 | Footer | 35 = 3 px green top line + 32 gradient; company (bold), Support, domain, user, session, version with icons | 25 flat; company, status, version | Web footer; the status line stays as an extra item |
| A16 | App box | 16 px margins on a page color; at QHD ×1.25 and capped at 2300 (1840 × 1.25) | Edge to edge | App box and zoom |
| A17 | Toolbar alignment | Right group flush with the table's right edge; toolbar → table 12 | Right group short of the edge; 6 | Right-align to the content edge; gap 12 |
| A18 | Row hover vs selection | Hover: light tint `#e8eefc`; text selection `#316ac5` with white text | Solid `#316ac5` on a selected row | Web hover tint on rows; keep `#316ac5` for text selection |

## Parity gaps found (features, not only looks)

- Login: the Português / English switch.
- Home: the **Domain controllers** and **Operating systems** panels.
- Groups: filter, "All types", count, Export CSV, the **Scope** column, per-row "members" / "delete", and members in a
  detail dialog instead of the split view.
- OUs: GPOs button and per-row "delete".
- Computers: DNS button and per-row actions ("enable", "delete", "sessions").
- Shares: the File servers list (add / edit / remove servers), "Tasks of this session", Sessions, and per-row actions.
- Logs: "All actions" select, Export CSV, Queues, and the "HTTP — panel" tab.
- Backup / Sync: the Servers list with help text; Sync's Last run, Quarantine and Size columns; per-row icon actions.
- Reset password: tabs, explanation callout, "Generate link" instead of "OK".
- egui-only, to decide (keep as extras or drop): bulk action toolbars on Computers, Shares, Backup and Sync; the
  Groups split view; the Configure screen (desktop-only, stays).

Captures still needed on the egui side: the Samba logs tab and the Set password tab of the reset dialog.

## Login — `01-login` ↔ `desktop-rs-01-login`

| Element | Web | egui | Difference |
|---|---|---|---|
| Card | 480×277 at y 90; 4 px blue top bar; drop shadow | 482×219 at y 54; 5 px top bar; 1 px border; no shadow | 58 px shorter, bar 5 vs 4, no shadow |
| Logo | 186×44, inset 29.5 left / 25 top | 186×44, inset 23.5 / 34 | Same size; 6 px left, 9 px lower |
| "ACTIVE DIRECTORY" | Bold, letter-spaced, blue | Regular, no spacing | Bold and spacing lost |
| Domain line | Monospace; "DOMAIN" dark, "(domain.local)" grey | Monospace, all grey | One color instead of two |
| Labels User / Password | Right-aligned, 21.5 px before the field | Left-aligned at card + 20 | Alignment |
| User field | 326×25 (330×29 with the focus ring) | 320×19 | **6 px shorter**, 6 px narrower |
| Focus | 2 px grey ring + 1 px green | 2 px black border | Another focus style |
| Field row pitch | 43 | 33 | 10 px tighter |
| Sign in | 77×28, gradient green, **bold**, no icon | 77×26, flat, key icon, regular | 2 px shorter, flat, not bold |
| Password → button | 34 | 9 | The button crowds the field |
| Padding under the button | ≈35 | 18 | Too tight |
| Hint lines | 17.5 pitch; `user` / `user@domain.local` in blue monospace | Proportional grey, 20 pitch | No code style |
| Language switch "Português English" | Present | **Missing** | Missing |
| Server URL + Configure | — | Present | Desktop-only (expected) |

## Home — `02-home` ↔ `desktop-rs-03-home`

| Element | Web | egui | Difference |
|---|---|---|---|
| Header | 64 tall, gradient | 72 tall, flat | 8 px taller |
| Logo | 114×26.5 | 171×40.5 | About 1.5× bigger |
| Title | Bold | Regular | Not bold |
| Global search | Field 196×28 + green Search 92×29 | **Missing** | Missing |
| User block | Bold "Administrator", "my password · sign out" links, 20 px from the edge | Name + role, Configure 95×26 and Sign out 87×26 grey buttons, 10 px from the edge | Links became buttons; no my password |
| Navigation | Top tabs 33 tall (active white, 3 px orange top), gap 3; API and Queues right | Sidebar 199, items 187×34 | Import/export, Settings, API, Queues missing |
| Content | White, 20 px padding | Light blue `#dde6fb`, 8 px padding | Background and padding |
| Stat tiles | 245×83, gap 12; 11 px **bold** caps label; **bold** number (cap 23); faint outline icon right | 170.5×76.5, gap 8; regular label and number; no icons | Bold and icons missing; 6.5 px shorter |
| Panel head | 28 tall, bold 13 px, no accent | 38 tall, regular ≈15 px, 4 px green accent | 10 px taller, not bold |
| Reset input + Open | Input 382×28, Open 80×28 with key icon | Input 396×19, Open 72×26 | **Field 7 px shorter than the button** |
| Panel layout | Row 2: Reset (495) + **Domain controllers** (1011); row 3: Alerts + **Operating systems** | One row: Reset + Alerts | **Domain controllers and Operating systems missing** |
| Alert badges | Bold caps, fit the text, 17 tall | Fixed 64×18, regular | Not bold, fixed width |
| Alert rows | 34 pitch, 1 px separators; "view ›" right-aligned | 32 pitch, no separators; "see ›" right after the text | No separators, link not right-aligned, wording |
| Footer | 35 (3 px green line + gradient); 6 items with icons, left and right groups; company bold | 26 flat; 3 items, no icons | 9 px shorter, items missing |

## Users — `03-users` ↔ `desktop-rs-04-users`

| Element | Web | egui | Difference |
|---|---|---|---|
| Container | Table directly on the page, no title | Card with a 38 tall "Domain users" head | Extra wrapper |
| Filter | 256×28 | 360×19 with a magnifier | 104 px wider, **9 px shorter** |
| Status select | 176×28, white | 170×26, grey | 2 px shorter, grey |
| Password policy / Export CSV / New user | 140 / 113 / 109 × 28, gap 8 | 129.5 / 103.5 / 101 × 26, gap 8 | ≈10 px narrower, 2 px shorter |
| Toolbar → table | 12 | 6 | |
| Table header | One band 31 tall, **bold**, 1 px bottom border | Cells 26 tall with 4 px gaps, regular, 6 px gap before row 1 | Fragmented, not bold |
| Column headers | Login, Name, E-mail, OU, Status, Last logon, **Actions** | An unlabeled badge column and 3 unlabeled icon columns | Empty header cells |
| Rows | 29 (28 + 1 px separator), striped | 32, striped, no separators | 3 px taller |
| Login link | Bold blue | Regular blue | Not bold |
| Status badge | 104×17, pale green, bold dark-green "ACTIVE" + power icon | 64×26, solid green, white regular | Fills the row; colors inverted |
| SYSTEM badge | 57×17, bold caps | 64×18, lowercase | Case, weight |
| Actions | Icon + text: password, copy, move, delete | Icon-only columns ≈34 px | No text |
| Empty last logon | "—" | "never" | Wording |

## New user dialog — `05-new-user` ↔ `desktop-rs-05-new-user`

| Element | Web | egui | Difference |
|---|---|---|---|
| Dialog | 760×548, top y 90 | 792×452, top y 80 | 32 px wider, 96 px shorter |
| Title bar | 36 + 3 px border; **bold**, left-aligned; × 10.5 px | 43 + 3 px; regular, **centered**; × 7.5 px | 7 px taller, centered, not bold |
| Labels | Uppercase bold letter-spaced 11 px, **above** the field | Mixed case regular ≈14 px navy, **left of** the field | Another pattern |
| Grid | Columns 354 wide, gap 16, full width | Label 86 + field 220 per column; ≈95 px empty right | Fields ≈134 px narrower |
| Field height | 27 | 19 | **8 px shorter** |
| Row pitch | 60.5 | 32 | |
| Field left edges | All at x 438 | 348 / 361 / 418 / 421 | **Misaligned by up to 73 px** |
| Field order | Login\|Display name; First\|Last; E-mail\|Phone; Job title\|Department; Description; OU; Initial password | Login; OU; Display\|Phone; First\|Job title; Last\|Department; E-mail\|Description; Initial password | Another order and pairing |
| OU select | Full width 724×27, white, shows the DN | 400×26, grey, "Users" | Narrower, other content |
| Initial password | Field ≈675 wide with an attached "generate" text button | Field 260×19 + regenerate and copy icons 32×26 | Icons, height mismatch |
| "Require change at next logon" | Green filled check at the left edge | Outlined, indented | Indent and style |
| Groups section | — | Combo 300×26 + Add 63×26 + hint | egui only |
| Footer | Full-width tinted bar 44 tall, top border | White, inset line | No footer bar |
| Footer buttons | **Create** (green ✓), then **Cancel**; 28; 14 px from the right | **Cancel**, then **Create**; 26; 27 px | Order reversed, 2 px shorter |

## Groups — `07-groups` ↔ `desktop-rs-06-groups`

| Element | Web | egui | Difference |
|---|---|---|---|
| Layout | One full-width table; members in a dialog | Split: Groups 652 + Members 397.5 | Master–detail in egui |
| Panel heads | None | "Groups" and "Members", 38 tall, green accent | egui only |
| Filter / "All types" / count | Filter 256×28, select 160×28, "46 of 46" | **Missing** | No filtering or count |
| Toolbar | Right: Export CSV 113×28, New group 119×28 (green) | Left: New group, Edit description, Delete (red), refresh | Side, Export CSV missing, 2 px shorter |
| Table header | Band 31, bold | Cells 26 with gaps, regular, empty badge header | As Users |
| Columns | Group, [SYSTEM], Group type, **Scope**, Members (right-aligned), Description, **Actions** | Group, badge, Group type, Members (left), Description | Scope and Actions missing |
| Rows | 29 with separators | 32 | 3 px taller |
| Group name | Bold blue | Regular blue | Not bold |
| SYSTEM badge | 57×17 bold caps | 64×18 lowercase | As Users |
| Selected row | Light tint `#e8eefc` | Solid `#316ac5`, white text | Much heavier |
| Row actions | "members" / "delete" per row | Toolbar acting on the selection | Interaction |
| Description | Full text | Cut at ≈270 px | Narrow column |

## OUs — `09-ous` ↔ `desktop-rs-12-ous`

| Element | Web | egui | Difference |
|---|---|---|---|
| Header | 64 tall, gradient | 72 tall, flat | egui is 8 px taller |
| Logo | 114×26 (about 18 px font) | 171×39 (about 28 px) | egui is about 1.5× bigger |
| Header title | "Active Directory" **bold** | Regular | Not bold |
| Header right | Search 195×27 joined to a green Search 92×27; bold name + "my password · sign out" | No search; two-line user, Configure 95×26, Sign out 87×26 | Global search and my password missing |
| Navigation | Top tabs 33 tall, 2 px gaps, bold; active white, raised 3 px, 3 px orange top | Sidebar 199 wide, items 187×34, regular; active with orange left bar | Another layout |
| Content container | Plain page, 20 px padding | A card with an 8 px gutter and 10/16 insets | Extra frame, uneven insets |
| Section title band | None | 38 tall, 4 px green bar | Only in egui |
| Description line | Same row as the buttons, "domain.local" bold | Its own row, nothing bold | Two rows instead of one |
| Toolbar buttons | Top right: GPOs 77×28, New OU 98×29 (green) | Left row: New OU 88×26, Delete 75×26; count and Refresh 31×26 on the right | 2–3 px shorter, left instead of right, no GPOs |
| Toolbar → table | 12 | 6 | |
| Table header | 30 tall, one continuous bar, 2 px bottom rule, **bold** | 26 tall, a box per column with 4 px gaps, regular, 6 px gap before row 1 | 4 px shorter, split, not bold |
| Columns | Name (with badge) · Description · DN · Actions | Path · Type · Description · DN | Type and path added, Actions missing |
| Rows | 28 + 1 px rule (29 pitch), striped | 30 + 1 px (32 pitch) | 3 px taller |
| Name cell | Green square marker + **bold** black name | Regular blue link | |
| Container/OU badge | "CONTAINER" 79×17 inline after the name, bold uppercase ≈10 px, dark on light blue | Type column, fixed 64×18 badges, white on grey or blue, lowercase regular | Another style and place |
| DN cell | Monospace ≈11 px | Proportional 13 px grey | |
| Row actions | "delete" link at the right | None (bulk Delete) | |
| Footer | 35 tall: 3 px green line + 32 gradient; company (bold), Support, domain, user, session, version | 25 tall flat; company, status, version | 10 px shorter, items missing |

## Computers — `10-computers` ↔ `desktop-rs-07-computers`

| Element | Web | egui | Difference |
|---|---|---|---|
| Filter field | 224×28 | 401×19 with a magnifier | **9 px shorter** than the controls beside it |
| System select | 256×28, white | 240×26, grey fill | 2 px shorter, grey |
| "no logon 90+ days" and count | 14 px box, label, "9 of 9" | Same | Equivalent |
| Right buttons | DNS 71×28, Export CSV 113×28, flush with the table's right edge | Export CSV 103×26, Refresh 32×26, 42 px short of the table edge | Not right-aligned, no DNS |
| Bulk action row | None | Disable, Move, Delete (disabled) | Only in egui |
| Table header | 30, bold, continuous | 26, regular, boxes | As OUs |
| Columns | Name · System · Version · Status · Last logon · Actions | + DC column, no Actions | |
| Rows | 29 pitch | 32 pitch | 3 px more per row |
| Name cell | Bold black uppercase | Regular blue link | |
| Status badges | DC 27×17 and DEACTIVATED 91×17, bold uppercase, 17 tall | ACTIVE 64×18 green, DEACTIVATED 81.5×18 grey, regular | ACTIVE added; 1 px taller, not bold |
| DC badge | In Status, centered | Own column, 50×18, text pushed to the **right** | Text not centered |
| Old-system highlight | None | System red, last logon brown | Only in egui |
| Row actions | "enable", "delete" (or "sessions" on the DC), right-aligned | None | Missing |

## Shares — `11-shares` ↔ `desktop-rs-08-shares`

| Element | Web | egui | Difference |
|---|---|---|---|
| Layout | Left column 226 wide, 16 px gap, right card 1285 | One card with a "Shares" band 38 tall | The left column is missing |
| "File servers" card | Head 29 ("File servers", "+ new"); selected row 35 tall blue: "dc1 (DC)", "This server", DC badge, "edit remove" | "Server" label + combo 360×26 | Add/edit/remove servers missing |
| "Tasks of this session" card | Head 29 with "queue ↗"; "None." | None | Missing |
| Share list head | 40 tall band: bold "Shares on dc1 (DC)", server URL, bold "total 1.1 MB" | Toolbar row: "online", "total 12.7 GB" | No URL, no bold title |
| Right buttons | Sessions 101×28, Refresh 95×28, New share 115×28 (green) | Refresh 82×26 | Sessions missing; New share on row 2 |
| Action row | None (per-row actions) | New share, Edit, "Remove (keeps the files)" 179×26 dark red | Only in egui, heavy |
| Table header | 30, bold | 26, regular, boxes | As OUs |
| Rows | 28 + 1 px; selected row tinted | 32 pitch; no selection shown | |
| Name / Path | Bold name; path monospace ≈11 px | Blue link; path proportional 13 px | |
| Size | Right-aligned | **Left-aligned** | Numbers must be right-aligned |
| Actions column | "edit", "remove" (red on the selected row) | None | Missing |

## Password policy dialog — `13-password-policy` ↔ `desktop-rs-13-password-policy`

| Element | Web | egui | Difference |
|---|---|---|---|
| Dialog | 760×455, 2 px blue border | 652×415, 1 px border | 108 px narrower, 40 px shorter |
| Title bar | 41 tall, **bold**, left-aligned 12 px in, × right | 43 + 3 px underline, regular, **centered** | Centered and not bold |
| Title bar bug | — | The 3 px underline stops 11 px before the right edge (a white notch; also in Send link) | Rendering bug |
| Complexity check | 14 px green filled box, **bold** label | Outlined box, regular | |
| Field labels | UPPERCASE bold letter-spaced grey, cap 8; label column 191 | Mixed case regular dark blue 13 px; column 111 | Another style, 80 px narrower |
| Number fields | 100×28 | 100×19 | **9 px shorter** |
| Unit hints | 18 px after the field, grey | 8.5 px after | |
| Row pitch | 40 | 32 | |
| "Recommended" note | Callout 34 tall, cream `#fff9e0`, 3 px brown left border | Brown text over 2 lines, no box | Callout missing |
| Footer | Full-width band 44 tall, gradient, top border | 1 px inset line, white | |
| Footer buttons | **Save policy 122×28 (green ✓), then Cancel 66×28**, 12 px right padding | **Cancel 77×26, then Save policy 103×26**, 27 px right padding | Order reversed, 2 px shorter |

## Reset password / Send link — `04`, `21` ↔ `desktop-rs-10`; `22` ↔ `desktop-rs-11`

| Element | Web | egui | Difference |
|---|---|---|---|
| Dialog (Send link) | 460×311 | 612×250 | 152 px wider, 61 px shorter |
| Title bar | 41, bold, left-aligned "Reset password — Ana Souza (ana.souza)" | 43 + 3 px underline (same notch), centered, regular | As the policy dialog |
| Tabs | Classic tabs 34 tall: inactive grey "Set password" 113, active white 162 with orange top, rule across | Toggle buttons: active filled blue 154×26 with a black border; inactive plain text | Another tab style, 8 px shorter |
| "Valid for" | Uppercase bold label **above** a select 160×28 | Regular label **beside** a combo 160×26 | Label position, 2 px shorter |
| Explanation | Callout 94 tall, light blue with a green left border; line pitch ≈19.7 | Plain paragraph, pitch ≈15.4 | Callout missing, tighter lines |
| Footer (Send link) | Band 44: **Generate link 137×29 (green ✓), then Cancel 66×28** | **Cancel 78×26, then OK 57×26** | Order reversed; "OK" instead of "Generate link" |
| Set-password tab (04) | Password field 240×28 (monospace) joined to hide/generate/copy (184), two checks; Reset + Cancel | Not captured | Capture it |
| Generated link container | The same dialog with its tabs; "LINK FOR ANA.SOUZA" label above the field | A separate dialog 652×176 "Link for bruno.silva", no tabs | Label moved into the title |
| URL field and copy | Monospace field 370×28 joined to a copy button 54×28 | Field 470×19 (larger font), 8 px gap, separate copy button 66×26 | 7 px shorter than its button, not joined |
| Validity text | Grey, 2 lines | Black, 1 line | |
| Footer | Close (green ✓) + Cancel, 28, band | Close 71×26 only, inset line | egui drops the redundant Cancel (fine) |

## Logs — `15-logs-audit` / `16-logs-samba` ↔ `desktop-rs-09-logs`

| Element | Web | egui | Difference |
|---|---|---|---|
| App frame | Box x16–1584 × y16–884 (16 margin), `#ece9d8` page behind | Edge to edge, no margin | egui has no app box or margin |
| Header bar | 64 tall, gradient `#0054e3`→`#1969e1` | 72 tall, flat `#1f6ee0` | egui is 8 px taller, no gradient |
| Logo | 114×21.5 at x39, vertically centered | 171×39.5 at x14, y16 | egui logo is about 1.5× bigger |
| Logo separator | 2 px `#8cb4f0` | 2 px `#8cb4f0` | Same |
| Title "Active Directory" | Bold, cap 11, vertically centered | Regular, cap 11, top-aligned | egui is not bold or centered |
| Header right | Search 195×28, Search button 93×28, bold user name, "my password · sign out" links | Two-line user name/role, Configure 95×26 and Sign out 87×26 buttons | Different layout; egui buttons 26 px vs 28 |
| Main navigation | Top tabs 33 tall (37 active, 3 px orange top bar), gap 2, bold; strip 42 with a 1 px `#919b9c` bottom line | Left sidebar 199 wide, items 187×34, gap 2, active white with orange left bar, regular | Web-only: Import/export, Settings, API, Queues; egui text not bold |
| Page tabs (Audit / Samba…) | Bordered tabs 31 tall (34 active, white, 3 px orange top), gap 2, bold with icon | 44-tall `#dde6fb` strip; active 67×26 filled `#316ac5`, white text; inactive as plain text | A different tab style; egui lacks "HTTP — panel" |
| Card head | None: the table sits under the toolbar | 38 tall, 4 px green bar, "Audit" | egui adds a card head |
| Filter input | 256×28 | 622×19, stretched | egui is **19 px tall vs 28** and too wide |
| Action select | "All actions" 224×28 | Missing | Missing in egui |
| "failures only" check | 14×14 | 14×14 | Same |
| Toolbar buttons | Queues 167×28, Refresh 91×28, Export CSV 113×28, gap 8, **right-aligned** | Refresh 83×26 right after the count | Not right-aligned; Queues and Export CSV missing |
| Table frame | Full content width, 1 px `#d6d2c2` border | Uneven 10/16 insets, no outer border | Uneven insets |
| Table header | 29 + 2 px `#cecdc1` bottom border, one continuous band, bold | 26 tall, separate cells with 4 px gaps, regular | 5 px shorter, gaps, not bold |
| Header → first row | 0 | 6 | egui adds a 6 px gap |
| Body rows | 27 + 1 px divider (28 pitch), first row white, then striped `#f6f5ee` | 32 pitch, first row striped, divider only under the last column | 4 px more per row, stripes inverted, no full dividers |
| Cell text | Operator bold dark blue, action monospace | Operator regular link blue, action proportional | egui drops bold and monospace |
| Result badge | "OK" pill 29×17, pale `#e5eee5`, dark-green text | "ok" 64×18 solid `#2a702a`, white text | egui badge is fixed-width and solid |
| Samba tab | Files panel 260 wide, search 360×28, "last 500" select 200×28, level check, Refresh right, monospace messages | Not captured | Recapture the egui Samba tab |
| Footer | 3 px green top line + 32 tall blue gradient; company, Support, domain left; user, session, version right | 25 tall flat `#194299`, no line; company, status, version | 10 px shorter, no green line, fewer items |

## Backup — `29-backup` ↔ `desktop-rs-14-backup`

| Element | Web | egui | Difference |
|---|---|---|---|
| Page tabs Backup / Sync | 31–34 tall (Backup 125, Sync 103) | None: Sync is a sidebar item | Missing |
| Server picker | "Servers" card 300 wide: head 28 with "refresh", rows 34 with a status square and DC tag, selected `#316ac5` | Combo 320×26 | A list in the web, a combo in egui |
| Help paragraph | 4 lines under Servers | Missing | Missing |
| Main card head | 28 tall `#e1eafc`, icon + bold "This panel" + "1 backup(s)" | 38 tall, green bar, regular "Backup"; count and refresh in the toolbar | 10 px taller, generic title |
| Back up now | 134×28, green gradient, bold | 112×26, flat, regular | 2 px shorter, flat, not bold |
| Send a backup | 135×28 | 121×26 | 2 px shorter |
| Download / Restore / Log / Delete | Per-row 16 px icon links at the row's end | Toolbar buttons enabled by the selection | Row actions became a toolbar |
| Table | Flush with the card; header 29+2 bold; rows 28 | Insets 10/16; header 26 in separate cells; rows 32; 6 px gap | Same table issues as Logs |
| Date cell | Plain text | Link blue | egui colors it as a link |
| Size cell | Plain, right-aligned | Solid blue badge 64×18 | egui adds a badge |
| Status cell | Plain green "ok" | Solid green badge | egui adds a badge |

## Sync — `30-sync` ↔ `desktop-rs-15-sync`

| Element | Web | egui | Difference |
|---|---|---|---|
| Server picker | Servers card 260 wide; rows 51 tall (bold name + IP), selected with green left bar | Combo 320×26 | As Backup |
| Help text | 7 lines | Missing | Missing |
| Jobs card head | 28, icon + bold "Sync jobs", "+ new" link right | 38, green bar, regular; refresh button in the toolbar | "+ new" became a button |
| Jobs actions | Per-row icons (view, run, edit, delete) | Toolbar: New job, Edit, Preview, Run now, Quarantine, Delete | Row actions became a toolbar |
| Jobs columns | Name (bold), Source (monospace + KERBEROS tag), Destination (monospace), Mode, When, Last run (amber), Quarantine, actions | Name (link), Source + " · Kerberos" text, Destination, Mode, When | Missing Last run, Quarantine, the tag and monospace |
| Jobs header / rows | 29+2 / 26+1 | 26 in cells / 32 | 5 px shorter / 5 px taller |
| Jobs card height | Fits content (≈158) | Fixed ≈307, mostly empty | About 150 px wasted |
| Gap between cards | ≈20 | 12 | 8 px smaller |
| Runs head | 28, bold "Runs" | 38, green bar, regular | 10 px taller |
| Runs actions | Per-row view and log icons; stop icon on a running row | Toolbar: Details, Log, Cancel | Toolbar |
| Runs columns | Date, Finished, Job, Kind, Status, Files, **Size**, Deleted, actions; numbers right-aligned | No Size; numbers left-aligned | Missing column, wrong alignment |
| Runs rows | 27 pitch, first row white | 32, first row striped, 6 px gap | 5 px more per row |

## Configure (egui only) — `desktop-rs-02-configure` vs the web's form style (`17-settings`)

| Element | Web form (Settings) | egui Configure | Difference |
|---|---|---|---|
| Card head | 28 tall `#e2ebfc` gradient, **bold**, no bar | 38 tall, 4 px green bar, regular | 10 px taller, not bold, extra bar |
| Labels | **Above** the field, UPPERCASE bold ≈11 px, 10 px gap | **Left** of the field, regular blue | Another form layout |
| Text inputs | **28 tall**, 1 px `#6e8ba8` border | **19 tall**, 520 wide | 9 px short |
| Select | 28, white fill, small arrow | 26, 200 wide, grey fill, large arrow | 2 px short, other fill |
| Help text | Small grey under the field | Body-size black paragraph | Heavier |
| Secondary buttons | 28, `#e8e7e2`, `#003c74` border | 26, `#dcdbd6` | 2 px short |
| Primary button | 138×28, green gradient, bold, shadow | 66×26, flat, regular | 2 px short, flat, not bold |
| Button placement | Left, ≈20 px under the last card, Save first | Right, **≈247 px below the card**, Cancel first | Far from the form, other order |

## Web at QHD (`20-qhd`, 2560×1440 logical)

| Element | 1600×900 | 2560×1440 | Ratio |
|---|---|---|---|
| App box | 1568 wide (16 margin) | **2300 wide**, x130–2430 | Capped at 1840 × 1.25 |
| Header | 64 | 80 | 1.25 |
| Navigation strip / tab | 42 / 33–34 | 53 / ≈42 | 1.26 |
| Inputs, buttons | 28 | 35–36 | 1.25 |
| Card head | 28 | 36 | 1.25–1.29 |
| Footer | 32 + 3 | 41 + 3 | 1.28 |
