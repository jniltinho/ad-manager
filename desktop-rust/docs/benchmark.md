# Benchmark: Rust (egui) vs Fyne (Go)

The question of this app: is an immediate-mode Rust GUI faster than the Fyne desktop at the same job? Both apps
were run on the same machine, the same X server and the same mock panel, with release builds, and driven the same
way. The method is the OpenSpec change's ([design.md](../../openspec/changes/desktop-rust/design.md#benchmark-method));
the script is [bench/run.sh](../bench/run.sh) (`make bench`).

## Result

Median of 5 runs per app (one warm-up run each first, discarded), runs alternating Fyne / Rust, 5,000 users:

| Metric | Fyne (Go) | egui (Rust) | Rust vs Fyne |
|---|---:|---:|---:|
| Binary size, stripped (MiB) | 25.7 | 21.0 | −18% |
| Cold start to the sign-in window (ms) | 118 | 64 | 1.8× faster |
| Idle memory at sign-in, PSS (MiB) | 186.8 | 95.2 | −49% |
| Sign-in to the Users page with 5,000 users (ms) | 1469 | 853 | 1.7× faster |
| Memory with 5,000 users on screen, PSS (MiB) | 234.0 | 111.3 | −52% |
| CPU for 200 wheel steps on the table, all threads (ms) | 22600 | 19580 | −13% |
| Wall time of the 200 wheel steps until idle (ms) | 24573 | 24448 | same (paced by the script) |

**The Rust app is faster or lighter on every metric**: it starts in about half the time, uses about half the memory
(with or without 5,000 users), reaches the populated Users page sooner and spends less CPU scrolling. Its times are
also steadier (sign-in to Users: 853 ± 1 ms in every run; Fyne: 937 to 1519 ms).

![The Users page of both apps in the benchmark (Fyne above, Rust below), the same 5,000 users](benchmark-users.png)

## Machine and builds

| | |
|---|---|
| CPU | AMD Ryzen 9 9900X, 12 cores / 24 threads |
| Memory | 186 GiB |
| OS | Linux 6.8 (Ubuntu 24.04, glibc 2.39) |
| Display | Xvfb 1920×1080×24, 96 dpi, Mesa llvmpipe (software OpenGL) for both, scale 1 |
| Fyne app | `dist/desktop/linux-amd64/ad-manager-desktop` v0.21.0-2-gd4dffd4 (Go, Fyne, release build) |
| Rust app | `ad-manager-desktop-rs` 0.1.0 (Rust 1.99, eframe 0.36 glow, `lto = "thin"`, stripped) |
| Panel | the mock panel of this repository (`crates/core/examples/mock_panel.rs`), HTTPS self-signed, pinned |

Raw numbers (`target/bench/<date>/raw.txt` of a run):

```
app run cold_ms idle_pss_mib signin_to_users_ms users_pss_mib scroll_cpu_ms scroll_wall_ms
fyne 1 115 186.4 1499 239.1 22890 24557
rust 1 64 95.2 853 111.3 19290 24450
fyne 2 118 173.2 1463 234.0 22600 24715
rust 2 62 95.2 853 110.9 19580 24439
fyne 3 115 186.8 937 234.7 25050 29187
rust 3 68 95.3 854 112.0 19850 24448
fyne 4 120 186.9 1519 232.4 18370 24573
rust 4 66 95.1 853 111.3 19660 24444
fyne 5 119 192.9 1469 230.9 18970 24558
rust 5 64 95.3 853 112.0 19230 24453
```

Every run also keeps a screenshot of the sign-in window, of the Users page and of the scrolled table for each app
(`target/bench/<date>/`): both apps show the same accounts in the same 1280×800 window, and the scroll check
compares the table before and after.

## How it is measured

- **Readiness** is the same for both: the mock panel has answered the request a step waits for (the summary for Home,
  the second list of users for the Users page), then the process's CPU time has not moved for 300 ms. The sign-in
  window is measured when it maps (its blinking caret is never idle); a step not ready in 30 s fails the run.
- **Sign-in to Users** covers the same work for both: Enter on the password, Home ready, a click on Users, the
  5,000-row list ready. The Fyne shell builds and loads every page at sign-in; the Rust shell loads Home at sign-in
  and Users when it opens (the interval covers both ways).
- **Memory** is the process's PSS (`/proc/<pid>/smaps_rollup`) 3 s after the step.
- **Scroll**: 200 mouse-wheel steps over the table, 20 ms apart, then the idle rule; CPU is user + system time of all
  the process's threads (Mesa's rendering threads included).

## What made the difference in the Rust app

Two changes, both in the app, brought the scroll CPU below Fyne's (the first measurement was 2.3× Fyne's):

1. **Frames paced at 60 per second.** Without vsync (Xvfb here; also VMs and remote desktops) egui draws as fast as it
   can while something animates. The app now waits so two frames are at least 16.6 ms apart — what a display with
   vsync already does.
2. **A wheel notch scrolls in one frame.** X11 and Windows wheels report "lines"; egui spreads each over several
   frames, every one a full redraw. The app hands a notch to egui as the small steps a touchpad sends, which egui
   applies at once; floating scroll bars (which fade in and out) and widget animations are off.

## Limits

- **Software rendering.** Both apps rendered with llvmpipe on Xvfb: the benchmark needs a display it can drive with
  `xdotool` without touching the operator's screen. On a GPU, rendering costs both apps less CPU; egui redraws the
  whole window on every input frame (a mouse move over the window costs ~30 ms of CPU here; Fyne redraws only what
  changed), which a GPU makes cheap.
- **Linux only.** Windows and macOS builds are produced by the release workflow but were not measured.
- **The Fyne binary** is the one in `dist/` of the monorepo (v0.21.0 + the next commit); 0.22.0 only adds a group count
  to a dialog and a framework update, nothing on the measured paths.
