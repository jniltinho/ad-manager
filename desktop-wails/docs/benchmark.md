# Benchmark: Wails (preview) vs Rust (egui)

Gate G4 of [openspec/changes/desktop-wails](../../openspec/changes/desktop-wails/design.md). The method is the Rust
benchmark's ([desktop/docs/benchmark.md](../../desktop/docs/benchmark.md)):
- the same mock panel (5,000 users of `domain.local`), and the same Xvfb with Mesa llvmpipe;
- release builds, with a fresh HOME/XDG per run;
- medians of 5 runs per app (one warm-up each, discarded), alternating Rust and Wails.

The difference is that memory (PSS) and CPU are summed over **every process** of the app: the Wails app's rendering
runs in WebKit's own processes. The script is [bench/run.sh](../bench/run.sh); the raw numbers are in
[bench-raw.txt](bench-raw.txt), the machine in [bench-machine.txt](bench-machine.txt).

## Result (2026-10-26)

| Metric | Rust (egui, wgpu) | Wails (WebKitGTK 2.52) | G4 budget | Wails |
|---|---:|---:|---:|---|
| Binary size, stripped (MiB) | 29.3 | 12.5 | ≤ 35 | pass |
| Cold start to the window (ms) | 171 | 652 | — | — |
| Cold start to a ready sign-in screen (ms) | 716 | 2054 | ≤ 1000 | **fail** |
| Idle memory at sign-in, PSS of all processes (MiB) | 126.5 | 414.8 | ≤ 300 | **fail** |
| Sign-in to Users with 5,000 users (ms) | 1182 | 9984 | ≤ 1500 | **fail** |
| Memory with 5,000 users, PSS of all processes (MiB) | 145.6 | 1374.9 | ≤ 450 | **fail** |
| CPU for 200 wheel steps on the table, all processes (ms) | 37440 | 30980 | ≤ 1.5× Rust | pass |
| Wall time of the 200 wheel steps until idle (ms) | 24599 | 30175 | — | — |
| Processes | 1 | 8 | — | — |

## Reading

- **The archive is smaller**: a 12.5 MiB binary plus the system's WebKitGTK, against the Rust app's 29.3 MiB.
- **Start-up costs about 1.3 s more**: the webview's processes start, and then the SPA loads its catalog and the
  panel's brand.
- **The 5,000-user table is the problem.** The web's Users page renders every row into the DOM. In WebKit, on
  software rendering, that takes ~10 s and ~1.4 GB, against the Rust app's virtualized table (1.2 s, 146 MiB).
  Scrolling the rendered table is cheaper than in the Rust app. A virtualized Users table in the web panel (it
  helps every browser too) is the fix to measure next. The budget is not met otherwise.
- The idle memory of the Wails run is taken on the sign-in screen after a sign-out. The mock answers `/api/me`
  without a session, so the SPA first opens Home, which leaves a little more in memory than a direct sign-in.
- Software rendering (llvmpipe) is the worst case for a webview. A machine with a GPU will start and draw faster.
  The method keeps both apps on the same footing, as the Rust benchmark did.

**G4: not met.** Four budgets fail. Before any switch, either the web's Users table is virtualized and the
benchmark run again, or the operator waives the budgets in writing in `design.md`.
