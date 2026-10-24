# Desktop app (Fyne) — archived

This is the first desktop app of ad-manager, written in Go with [Fyne](https://fyne.io). It is **archived**: kept only
as code to study. It is no longer built by `make`, tested by CI or published by the release, and its docs and
screenshots were replaced by the Rust app's.

The desktop app is now [`desktop-rust/`](../desktop-rust/README.md) (Rust/egui), documented in
[docs/desktop.md](../docs/desktop.md). The theme generator here (`internal/themegen`) still produces its
`themes.json` (`make desktop-themes` at the repository root, in Docker).
