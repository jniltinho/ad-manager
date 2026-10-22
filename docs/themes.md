# Default theme — web panel and desktop app

A guide to carrying the AD Manager look (the **Default** theme) over to another system: navy blue with an orange
accent, **square corners everywhere**, subtle depth (short gradients and small shadows) and a dense interface
(12–13 px text).

| Web panel | Desktop app |
|---|---|
| ![web](screenshots/03-users.png) | ![desktop](screenshots/desktop-04-users.png) |

## 1. Colors

| Token | Hex | Use |
|---|---|---|
| `cn` (navy) | `#113058` | main color: buttons, table header, links, selection |
| `cn-dark` | `#0e172d` | bar background (menu, footer), button borders, end of gradients |
| `cn-light` | `#1d4a7e` | start of navy gradients, hover |
| `cn-accent` | `#f58322` | orange: strip under the header, active tab, focus, card bar |
| `cn-accent-strong` | `#fe6101` | link/action hover and orange button hover |
| `cn-muted` | `#69788b` | labels, secondary text |
| `cn-soft` | `#8b9aad` | disabled, dimmed icons, checkbox border |
| `cn-bg` | `#d9e6f3` | page background and zebra row |
| `cn-bg-light` | `#e7ecef` | login background and desktop content background, modal footer |
| `cn-bg-soft` | `#eaf0f6` | row hover, secondary button background |
| `cn-border` | `#b9c9dc` | card, table and field borders |
| `cn-ok` | `#2fa14f` | success |
| `cn-err` | `#c62828` | error, locked, danger button |
| `cn-warn` | `#ef6c00` | warning |

"Active/ok" tag: background `#d9ecd9`, text `#1f5c2a`. Selected row (desktop): `#fde3cc`.

Font: **Inter** 400 and 600 (embedded woff2, no Google Fonts); fallback `Tahoma, Verdana, Arial, sans-serif`.
On the desktop, Fyne's default font at 13 px.

## 2. Theme rules

- **Radius 0** everywhere (web: global `border-radius: 0 !important`; desktop: every `*Radius` of the theme = 0).
- Workspace background with a **dot pattern**: `radial-gradient(rgba(17,48,88,.35) 1px, transparent 1px)` on a
  22 px grid over `cn-bg` (web) or `cn-bg-light` (login).
- A **3 px orange strip** separates headers (app top, modal, footer) from the content.
- Dark bars use a `cn` → `cn-dark` gradient; buttons and table headers, `cn-light` → `cn`.
- Nothing native from the browser/system shows: scrollbar, focus, select, checkbox, tooltip and validation messages
  are redrawn in the theme.
- Interface text in English with translation (pt-BR by default); dates in the language's format.

## 3. Web panel (Vue 3 + Tailwind 4)

### Copy

1. The whole `frontend/src/style.css` — tokens (`@theme`), fonts, large-screen zoom, components and native controls.
2. `frontend/src/assets/fonts/inter-latin-{400,600}-normal.woff2`.
3. `frontend/src/native.js` — theme tooltip (`title=` becomes `.theme-tip`, only when the text is truncated) and
   validation bubbles become a red border + toast. Call `installNativeReplacements()` in `main.js`.
4. `frontend/src/toasts.js` + `components/Toasts.vue`, `components/Modal.vue`, `dialogs.js` + `components/PanelDialogs.vue`
   (themed confirm/alert, instead of `confirm()`/`alert()`).
5. The `App.vue` shell (header, menu, `<main>`, footer and the page transition) and `pages/Login.vue`.

### Ready-made classes

| Class | What it is |
|---|---|
| `.btn` | navy primary button with gradient, short shadow, "sinks" 1 px on click (h 28 px, 12 px, bold) |
| `.btn-ghost` | secondary button white → `cn-bg-soft`, `cn-border` border |
| `.btn-danger` | red button (delete) |
| `.inp` | 28 px field/select; focus with navy border + halo; `select.inp` has an arrow and a themed open list |
| `.lbl` | 11 px label, uppercase, `cn-muted` |
| `.grid-t` | table: sticky navy header, `cn-bg` zebra, `cn-bg-soft` hover, 12 px |
| `.act` | inline action (link with icon), orange hover |
| `.tag` | 10 px uppercase tag; color through utilities (`bg-cn text-white`, `bg-cn-err text-white`…) |
| `.card` / `.card-h` | white card with shadow; header with a 4 px orange bar on the left |
| `dialog.pnl` + `.pnl-head/.pnl-body/.pnl-foot` | modal: navy header with orange strip, light footer, 10% from the top |
| `#toasts .toast(.err/.warn)` | notices in the top-right corner, green/red/orange |
| `.theme-tip` | `cn-dark` tooltip with an orange border on the left |

Example:

```html
<div class="card">
  <div class="card-h"><Users :size="14" /> Users</div>
  <table class="grid-t">…</table>
</div>
<label class="lbl" for="n">Name</label>
<input id="n" class="inp" />
<button class="btn"><Check :size="14" /> Save</button>
<button class="btn-ghost">Cancel</button>
<span class="tag bg-[#d9ecd9] text-[#1f5c2a]">active</span>
```

### Layout

- "Card" window: `mx-auto my-4 w-[calc(100%-32px)] max-w-[1840px] h-[calc(var(--view-h)-32px)]` with shadow
  `0 0 12px rgba(0,0,0,.5)`; **the page does not scroll**, only `<main>` (and lists inside it).
- White → `cn-bg-light` header with logo, title with an orange border on the left, search and operator.
- Menu: `cn-dark` → `#0a1222` bar; active tab with navy gradient, orange icon and 3 px orange underline
  (`shadow-[inset_0_-3px_0_var(--color-cn-accent)]`).
- Footer: `cn` → `cn-dark` gradient, 3 px orange top border, 11 px text.
- Page change: fades out in 0.15 s and comes in rising 6 px (`<transition name="page" mode="out-in">`), disabled with
  `prefers-reduced-motion`.
- Icons: `lucide-vue-next`, 14–15 px.
- Large screens: `zoom` 1.25 (≥ 2400 CSS px) and 1.6 (≥ 3200 px); use `calc(var(--view-h) - X)` instead of `100dvh`.

### Login

Webmail measurements (Roundcube, company skin): 580 px form 10% from the top, 480 × 267 px navy box with a
4 px orange top border, 195 × 65 logo, 41 px rows, 326 × 25 fields, orange `#f58322` button (hover
`#fe6101`), elliptical `login_shadow.png` shadow below and errors/hints **outside** the box. The same component serves
public pages (e.g. the password change link).

## 4. Desktop app (Go + Fyne v2)

### Copy

| File | What it has |
|---|---|
| `desktop/theme.go` | `defaultTheme`: colors, radius 0, 13 px text — `app.Settings().SetTheme(defaultTheme{})` |
| `desktop/button.go` | `volButton` (`newButton`): button with volume — gradient, border, top highlight, shadow, hover lightens, click sinks 1 px. `Importance`: High = navy, Danger = red, Warning = orange, Low = flat |
| `desktop/widgets.go` | `header`, `footer`, `card`, `statTile`, `newText`, `fixedSize` and `gridTable` (`newTable`) |
| `desktop/modal.go` | `showModal`/`confirm`/`confirmTyped`/`showInfo`, `navigation` sidebar with `navItem` |
| `desktop/assets/` | white logo, login shadow, app icon |

### Pieces

- **Header**: horizontal `navy` → `navyDark` gradient, 150 × 50 white logo, "Active Directory" 16 px bold,
  12 px orange subtitle, 3 px orange strip below.
- **Sidebar**: 200 px, vertical navy gradient; item with white icon, 13 px bold; active with 13% white background
  and a 4 px orange bar; 6 px shadow over the content.
- **Content**: `#e7ecef` background; cards with a light header and a 4 px orange bar.
- **Table** (`gridTable`): navy → navyDark header 12 px bold, 13 px rows with `#d9e6f3` zebra, whole row
  selected in `#fde3cc`, `#d5dfea` separator, truncated text with "…", `badge` columns become fixed-width
  tags. Use `table.view()` (clears the theme padding between cells).
- **Modal** (`showModal`): header with `#1d4a7e` → navyDark gradient and 1 px highlight, centered white title,
  white X, orange strip (red if `danger`), footer with a line and light gradient, 1 px dark border.
- **Footer**: `navyDark` 26 px, 11 px orange text (company, status in the center, version).
- **Login**: navy box like the web, orange button (`WarningImportance`).

### Fyne pitfalls

- Closing an overlay (`PopUp.Hide`) also removes the ones above it: open the next modal after closing the current
  one (`go fyne.Do(...)`).
- `widget.Table` uses the theme padding as the space between cells: clear it with `container.NewThemeOverride`.
- `canvas.Text` does not truncate text on its own: truncate with "…" by the column width.
- Windows: build with `-ldflags "-H=windowsgui -extldflags=-Wl,--subsystem,windows"` (with an external linker, both)
  so no console opens — see the `desktop-windows` target in the `Makefile`.

## 5. Checklist for a new app

- [ ] Color tokens and Inter font copied; global radius 0.
- [ ] Dotted background, orange strip under the header, navy footer.
- [ ] `.btn`/`.btn-ghost`/`.btn-danger` buttons (web) or `volButton` (desktop) — no system buttons.
- [ ] Tables with a sticky navy header and zebra; `.tag` tags with a fixed width per column.
- [ ] Themed modals, 10% from the top; confirm deletion by typing the name.
- [ ] Native controls redrawn (`native.js`, select, checkbox, scrollbar, orange focus).
- [ ] Login with the webmail measurements; language chosen at login.
- [ ] Check in Full HD, QHD (zoom 1.25) and 4K (zoom 1.6), in Chrome and Firefox.

## 6. Selectable panel themes

The web panel has 18 themes, chosen in **Settings → Theme** (applies to every operator and to the login
screen). The default is **Default**; the desktop app uses the same themes.
The **Flat look** checkbox removes gradients and shadows from any theme.

| Theme | Id | Scheme | Reference |
|---|---|---|---|
| Default | `default` | light | this guide |
| ADManager | `admanager` | light | inspired by ManageEngine ADManager Plus (no affiliation) |
| Breeze / Breeze (dark) | `breeze`, `breeze-dark` | light / dark | KDE Breeze |
| Violet / Violet (dark) | `violet`, `violet-dark` | light / dark | DashboardPack's violet dashboard |
| Aurora / Aurora (dark) | `aurora`, `aurora-dark` | light / dark | DashboardPack's gradient dashboard |
| Crystal / Crystal (dark) | `crystal`, `crystal-dark` | light / dark | macOS system colors |
| Cone (dark) | `cone` | dark | VLC 4 dark interface |
| Classic blue (XP) | `classic-blue` | light | Windows XP (Luna) |
| Classic gray (98) | `classic-gray` | light | Windows 98, with 3D bevels |
| Graphite (dark) | `graphite` | dark | Grafana 11 |
| Matrix (dark) | `matrix` | dark | the film's green phosphor terminal: monospace, a static "digital rain" of strokes on the page, a soft green glow |
| Winamp (dark) | `winamp` | dark | the Winamp 2 skin: brushed steel with a 1 px bevel, LCD readout (green on black) in fields, amber buttons, the playlist's blue selection |
| Pink Panther | `pink-panther` | light | the 1963 film titles: plum title bars with the panther-pink stripe, blush page, deep rose actions; pink only as an accent |
| High contrast (dark) | `high-contrast` | dark | accessibility: black and white, one yellow accent, 3 px focus ring, no gradients or depth shadows (WCAG AAA); Matrix's structure (window line, bars, tab marks) in that palette |

All have square corners and pass the WCAG contrast matrix (`frontend/src/themes/matrix.json`); Crystal, Crystal
(dark) and High contrast are held to AAA (normal text 7:1). Reference colors
adjusted for contrast are noted in the header comment of each theme file. In dark themes (and
in those with a dark header) the product logo switches to the light-ink version, and the customer logo gets a
light plate behind it.

![Home screen of each theme](screenshots/themes.jpg)

### Web × desktop

The desktop app uses the same colors (generated from the theme files) and the same roles in each part — header
(`header`), navigation (`tabs`, `tab*`: the top tab strip becomes the sidebar, with the `--shadow-tab-on` marker),
inner tabs (`subtab*`, with the `--shadow-subtab`/`--shadow-subtab-on` layers: the theme's bar, the 98's bevel),
card titles (`card-head`, strip of `--card-head-bar-w`), table header (`table-head`), buttons
(`action`, `action-2`, `danger`, `brand-action`, with the login's solid hover), dialogs (`dialog-head`, strip
`dialog-head-bar` of `--dialog-head-bar-w`), focus ring (`focus`, `--focus-ring-w`) and login (`login-*`). In the
flat look, decorative gradients, shadows and bevels disappear (cards, buttons, including pressed ones, headers, the
98's footer panels); only the state markers remain — current tab, selected item, focus, invalid field —, on both
sides. Accepted differences: top tabs on the web × side navigation on the desktop (the position; the colors are the
same, except in Crystal, where the sidebar is Crystal AAA+'s Finder list), system font × Fyne font (Matrix's monospace
included), language choice at the web login × "Configure" on the desktop, blur, the desktop without toasts (status
bar and dialogs), the page pattern (the dots, Matrix's rain, Winamp's brushing: the desktop page is one color), the
colored glow of Matrix (the desktop casts its neutral shadow), the field text (Fyne draws it in `fg`: Winamp's LCD green
is web only, the black field stays; its sunken fields and pressed relief are web only too) and Fyne's translucent text
selection. In every theme the desktop footer has no stripe. Desktop buttons take the keyboard focus (Tab) with the
theme's ring and are pressed with Space or Enter, as on the web (the sidebar items, inner tabs and the password
reveal button stay mouse-only). A theme with no shadow on buttons, cards and the
login box (High contrast) is always drawn flat on the desktop, as its web look has no depth, and its dialogs are
framed in the dialog line (2 px). Checked by
`TestVolumeDrawn` (volume drawn outside the flat look, 98's bevel, nothing in flat), `TestFocusRing` and by the
residue-free theme switch in the visual suite. The visual suite (Playwright) runs locally with
`scripts/visual-ci.sh`, outside CI (decision in commit `d208bad`). The mosaic below is the web home screen (visual
suite) next to the desktop home screen (`SCREEN_SHOTS=<dir> go test -run TestScreensInEveryTheme` in `desktop/`, with
a sample panel).

![Web × desktop home screen, themes 1 to 6](img/conformity/web-desktop-1.jpg)
![Web × desktop home screen, themes 7 to 12](img/conformity/web-desktop-2.jpg)
![Web × desktop home screen, themes 13 to 18](img/conformity/web-desktop-3.jpg)
