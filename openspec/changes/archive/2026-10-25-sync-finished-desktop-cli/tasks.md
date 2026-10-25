## 1. Changes

- [x] 1.1 Desktop: Finished column in the Sync Runs table (`syncFinished`, `shortDuration`), pt-BR "Término"; tests.
- [x] 1.2 CLI: FINISHED in `sync list` (`runFinished`); test.
- [x] 1.3 i18n: pt-BR for the three inheritance notes; `UPDATE_I18N=1 go test ./internal/i18n`; tests.
- [x] 1.4 Retake `docs/screenshots/desktop-15-sync.png` (2×, classic-blue, English, domain.local demo) from fixtures
      (`desktop/docs_fixture_shots_test.go`, `TestDocsFixtureShots`).
- [x] 1.5 Codex review (no blockers; applied: wider Finished column with a cross-day test, the fixture shot stops the page polling and restores its globals).
