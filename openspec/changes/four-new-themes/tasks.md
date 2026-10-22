## 1. Web themes

- [x] 1.1 `frontend/src/themes/matrix.css`, `winamp.css`, `pink-panther.css`, `high-contrast.css`: every color,
      background and shadow role and the recipe parameters, square corners; imported in `frontend/src/style.css`
- [x] 1.2 Registry `internal/server/themes.go` (ids, English labels, scheme); `openapi.yaml` theme enum (rule 8)
- [x] 1.3 `frontend/src/themes/matrix.json`: `high-contrast` in `aaa`; the contrast test passes for the four with no
      pending recipe (High contrast: yellow current tabs with black ink, black `nav-on` with yellow text, solid login
      focus, error text `#ffa3a3`)
- [x] 1.4 pt-BR labels in `frontend/src/locales/pt-BR/common.js` (and fix the stale keys of the existing labels)

## 2. Desktop parity

- [x] 2.1 `go generate` in `desktop/` (`themes_gen.go`); `TestThemesGenUpToDate` passes; the 14 existing entries
      are unchanged
- [x] 2.2 `themegen`: a theme with no shadow on buttons, cards and the login box is `depthless`; `applyLook` draws it
      flat
- [x] 2.3 `desktop/theme_test.go` (`TestNewThemes`): ids, schemes, High contrast `minText` 7, 3 px ring, depthless
      (and only it), Winamp bevel; `TestVolumeDrawn` accepts one-color bands by design; `TestFocusRing` covers High
      contrast and Matrix; `TestFynePairsContrast` and `TestScreensInEveryTheme` pass for the four
- [x] 2.4 A depthless theme frames its dialogs in `dialog-line`, 2 px (`modal.go`, `TestDepthlessDialogFrame`)

## 3. Docs and screenshots

- [x] 3.1 `docs/themes.md` (table, count, references, accepted differences), `docs/overview.md`, `README.md`,
      `openspec/config.yaml` counts
- [x] 3.2 `scripts/theme-mosaics.py` lists 18 themes (web × desktop in three images); retake `themes.jpg` and
      `docs/img/conformity/web-desktop-*.jpg` at 2×
- [x] 3.3 Check the four in the browser (the visual suite's captures: Home, every page, login, dialogs) and the
      desktop captures

## 4. Validation

- [x] 4.1 `make check && make test && make build`
- [x] 4.2 Visual suite: the 14 existing themes pixel-identical to the base commit (`THEMES=` their ids)
- [x] 4.3 Codex review of the diff (`codex exec --sandbox read-only`): token coverage, web/desktop parity, contrast,
      no regression; fix what it raises
