# Manual gate: software rendering on a GPU-less Windows machine

The CI's WARP test (`apps/ad-manager-desktop-rs/tests/warp.rs`, `windows-latest`) shows that the software DX12
adapter renders a frame. It does not show **presentation** through the output-owning WARP adapter: a CI runner has a
display adapter. This gate covers that case on a GPU-less machine.

## Procedure

1. A Windows 10/11 or Server 2022 VM without 3D acceleration (VirtualBox: Display › Enable 3D Acceleration off).
2. Unzip `ad-manager-desktop-rs_<version>_windows_x86_64.zip`. It is a GUI program with no console of its own, so from
   `cmd.exe` redirect its output to a file:
   `ad-manager-desktop-rs.exe --safe-gpu --gpu-info > gpu-info.txt 2>&1`
3. `gpu-info.txt` has `gpu: backend cpu, renderer wgpu` and `gpu: adapter Microsoft Basic Render Driver (…, Dx12)`.
   The output-owning WARP entry is the one that is **not** `Cpu`: the vendored `wgpu-hal` reports it as an integrated or
   discrete adapter.
4. The window draws the Configure or sign-in screen, not a black window. Take a screenshot.
5. Close it and start it again without flags. It opens with the saved or automatic backend, and `gpu-starting` in the
   folder of desktop.toml is empty.

## Record

| Date | Version | Machine | Adapter printed | Window drawn | Screenshot |
|---|---|---|---|---|---|
| pending | — | — | — | — | — |
