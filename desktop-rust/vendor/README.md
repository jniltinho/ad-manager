# Vendored crates

Third-party crates patched for this app (`[patch.crates-io]` in the workspace `Cargo.toml`). They keep their own
licenses and lints and are not workspace members. Copied from rustXterm (github.com/jniltinho/rustXterm, `vendor/`),
which carries the same patch.

## wgpu-hal 30.0.1 (Windows port, add-windows-port W1 smoke)

Why: on GPU-less Windows machines (VirtualBox without 3D acceleration, such as the
Windows test VM) DXGI lists two "Microsoft Basic Render Driver" (WARP) adapters: a
non-software one that owns the display output and the software one without outputs. wgpu-hal
drops the first and renders on the second; DX12 flip presentation from that adapter leaves the
window black (desktop capture, `PrintWindow` and DirectComposition alike).

Change (3 lines + comment, `src/auxil/dxgi/factory.rs` `should_keep_adapter`): the non-software
WARP adapter is skipped only when it has no output (`EnumOutputs(0)` fails), so headless machines
keep upstream behavior. `examples/` are not vendored (their `[[example]]` entries are removed from
`Cargo.toml`). Remove the patch once wgpu keeps output-owning WARP adapters upstream.
