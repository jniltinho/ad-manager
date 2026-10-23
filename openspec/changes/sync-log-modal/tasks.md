## 1. Modal

- [ ] 1.1 Add `frontend/src/components/dialogs/SyncLogDialog.vue`: a `Modal`-based dialog whose header shows the job
      name, run id, status and file/size counts, whose monospace log body scrolls (`overflow-auto`, capped to a
      fraction of the viewport), and whose footer has Copy and Close.
- [ ] 1.2 Rewrite `Sync.vue` `showLog` to fetch the log text and open `SyncLogDialog` (a `logRun` ref), instead of
      `showAlert`; render the dialog in the template.

## 2. i18n

- [ ] 2.1 Add the new English source strings (title, Copy/Copied) to `frontend/src/locales/pt-BR/sync.js`.

## 3. Validate

- [ ] 3.1 `openspec validate sync-log-modal --strict`.
- [ ] 3.2 `make check && make test && make build`.
- [ ] 3.3 Retake the affected Sync screenshots (2×, classic-blue, English, Example Company Ltd, domain.local) showing
      a long log scrolling inside the modal.
- [ ] 3.4 Review with Codex (`codex exec --sandbox read-only …`).
