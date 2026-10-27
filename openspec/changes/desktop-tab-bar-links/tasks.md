## 1. Implement

- [x] 1.1 `App.vue`: Configure after Queues; Queues disabled in the desktop; hover changes only the text color.
- [x] 1.2 Themes: `--color-tab-aux-hover` removed; a distinct `--color-on-tab-aux-hover` where it equalled the normal
      color (Classic blue, Classic gray, Breeze, Breeze dark, AD Manager, RabbitMQ); the matrix checks it on the tab bar.
- [x] 1.3 pt-BR string in `locales/pt-BR/desktop.js`; `docs/desktop.md`; CHANGELOG.

## 2. Validate

- [ ] 2.1 `make check && make test && make build`; check the desktop app against the test panel.
- [ ] 2.2 Retake the affected screenshots (2×, Classic blue, English, Example Company Ltd, `domain.local`).
