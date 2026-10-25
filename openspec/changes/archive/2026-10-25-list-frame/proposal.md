## Why

On the list pages (Users, Groups, Computers, Logs) the table lost its left and right lines in the browser: the
`.grid-t` frame is a collapsed table border, and the outer half of a collapsed border lies outside the table box,
so the scrolling wrapper (`overflow-auto`) clips it. Only the inner row lines stayed. It is visible in every theme and
glaring in High contrast, whose lines are light on black.

## What Changes

- The scrolling wrapper of each list carries the frame (`border border-line`) and its table uses `.grid-t border-0`,
  the pattern the quarantine dialog already used. Six lists: Users, Groups, Computers, audit log, HTTP log and the
  Samba log.
- A comment in `style.css` records the rule next to `.grid-t`.

## Capabilities

### Modified Capabilities

- `web-panel`: the list tables are framed on all four sides, scrolled or not.

## Impact

`frontend/src/pages/{Users,Groups,Computers,Logs}.vue`, `frontend/src/components/{HttpLog,SambaLogs}.vue`,
`frontend/src/style.css`. The frame moves from the table to its wrapper, so those six screens change by one pixel in
every theme (the frame now also encloses the scrollbar); the other tables are unchanged.
