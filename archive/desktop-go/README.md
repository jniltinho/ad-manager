# Desktop app (Fyne) — archived

This is the first desktop app of ad-manager, written in Go with [Fyne](https://fyne.io). It is **archived**: kept only
as code to study. It is no longer built by `make`, tested by CI or published by the release, and its docs and
screenshots were replaced by the Rust app's.

The desktop app is now [`desktop/`](../../desktop/README.md) (Go, Wails v3), documented in
[docs/desktop.md](../../docs/desktop.md); the Rust app that replaced this one is archived too, in
[`archive/desktop-rust/`](../desktop-rust/README.md). The theme generator here (`internal/themegen`) produced the
Rust app's `themes.json`; nothing uses it now.
