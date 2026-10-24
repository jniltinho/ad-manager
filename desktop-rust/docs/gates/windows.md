# Gate: WARP presentation on a GPU-less Windows machine

The CI job `windows` (`.github/workflows/desktop-rust-ci.yml`, `windows-latest`) runs `tests/warp.rs`: the CPU WARP
adapter renders a 64×64 frame that is read back. That proves WARP renders, not that a window *presents* through the
output-owning WARP adapter, which on GPU-less machines needs the vendored `wgpu-hal` patch (`vendor/README.md`):
without it the window stays black. This gate is that check, done by hand on a real GPU-less machine.

## Procedure

1. A Windows 10/11 VM **without 3D acceleration** (VirtualBox with 3D off, or Hyper-V without GPU-P): its DXGI lists
   two "Microsoft Basic Render Driver" adapters.
2. Unzip `ad-manager-desktop-rs_<version>_windows_x86_64.zip` (the release, or `make dist-windows`).
3. Run `ad-manager-desktop-rs.exe --safe-gpu --gpu-info` from a console: it prints the adapter
   (`gpu: wgpu Dx12 Other Microsoft Basic Render Driver …` is the output-owning entry).
4. The sign-in window must show its content (not black); take a screenshot.
5. Start it without `--safe-gpu`: the default ladder (OpenGL first) must also open, or fall back by itself after a
   crash (the second start says so under the sign-in box).

## Receipt

| Field | Value |
|---|---|
| Source commit | — |
| Machine | — |
| `--gpu-info` line | — |
| Result | pending (no GPU-less Windows machine was available when the crash-safe start was added) |
