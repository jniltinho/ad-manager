# Vendored crates

Third-party crates patched for this workspace through `[patch.crates-io]` in `desktop-rust/Cargo.toml`. They are
excluded from the workspace (`exclude = ["vendor"]`), keep their upstream lints and licenses, and differ from the
published crate only as listed here.

## wgpu-hal 30.0.1 (crash-safe graphics start, design D12)

Copied from rustXterm's `vendor/wgpu-hal` (the same eframe 0.36.2 / wgpu 30 stack).

Why: on GPU-less Windows machines (VirtualBox without 3D acceleration, for example) DXGI lists two "Microsoft Basic
Render Driver" (WARP) adapters: a non-software one that owns the display output and the software one without
outputs. wgpu-hal drops the first and renders on the second, and DX12 flip presentation from that adapter leaves the
window black.

Change (3 lines + comment, `src/auxil/dxgi/factory.rs` `should_keep_adapter`): the non-software WARP adapter is
skipped only when it has no output (`EnumOutputs(0)` fails), so headless machines keep upstream behavior. `examples/`
are not vendored. Remove the patch once wgpu keeps output-owning WARP adapters upstream.
