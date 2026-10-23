## 1. Table

- [x] 1.1 `gridTable`: per-cell links (the page returns, per cell, icon, text, hover hint and action, or none);
      rebound on every `UpdateCell`; pointer cursor and hover hint on links; a link tap leaves the selection as it
      is; Enter on the selected row calls the page back.
- [x] 1.2 Icon-only action links when the table is narrower than all columns at full width.

## 2. Users

- [x] 2.1 `editable` (not protected, not the own login, case-insensitive); login → edit; editable status → toggle
      (confirm to disable); tag column: system if built-in, unlock link if locked (both when both); password, copy, move,
      delete columns (move/delete only editable); actions take the tapped row's account.
- [x] 2.2 Enter → menu of the row's actions.
- [x] 2.3 Lean toolbar (`toolbarRow`): search, filter, count | Password policy, Export CSV, New user, refresh; ⋯
      (with New user) when narrow.
- [x] 2.4 pt-BR for the new labels and hints.

## 3. Tests, docs, screenshots

- [x] 3.1 Tests: per-cell links (only where the page gives one, rebound on reuse); Users links per account kind
      (editable, built-in, own, locked, locked built-in); tapped row vs selected row, and the target kept when the
      list reloads during a confirmation; cancelled confirmations send nothing; Enter on the selected row opens its
      menu; the toolbar has no row action; widths at 1280 px.
- [x] 3.2 `docs/desktop.md`, CHANGELOG; Users screenshots retaken (2×, classic-blue, domain.local).
- [x] 3.3 Codex review of the proposal (two passes, fixes applied) and of the code.
