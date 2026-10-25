#!/usr/bin/env bash
# Benchmark of the Rust desktop against the Fyne desktop, on the same X server (Xvfb, Mesa software GL for both),
# against the same mock panel (5,000 users of domain.local), with release builds of both. Each metric is the median
# of RUNS runs per app (one warm-up run each first, discarded), runs alternating Fyne / Rust.
#
# Needs: Xvfb, xdotool, import (ImageMagick) and the binaries:
#   FYNE  (default ../dist/desktop/linux-amd64/ad-manager-desktop — `make desktop-linux` at the repo root)
#   RUST  (default target/docker/release/ad-manager-desktop — `make release`)
#   MOCK  (default target/docker/release/examples/mock_panel — cargo build --release -p adm-core --example mock_panel
#          --features testkit)
#
# Graphics: both draw with OpenGL on Mesa llvmpipe. The Rust app renders through wgpu (design D12), so it runs with
# WGPU_BACKEND=gl (its GL backend on llvmpipe, as Fyne's GL) and --gpu-info, whose lines go to machine.txt. Each run has
# its own HOME and XDG folders, so neither app's persisted window or preferences carry over between runs.
#
# Readiness (the same for both): the mock answered the request a step waits for, then the process's CPU time did not
# move for 300 ms (the frame is painted and the app idle). A step not ready in 30 s fails the run. The sign-in window
# is measured when it maps, and its memory 3 s later (a blinking caret is never idle).
set -euo pipefail
export LC_ALL=C
cd "$(dirname "$0")/.."

FYNE=${FYNE:-../dist/desktop/linux-amd64/ad-manager-desktop}
RUST=${RUST:-target/docker/release/ad-manager-desktop}
MOCK=${MOCK:-target/docker/release/examples/mock_panel}
RUNS=${RUNS:-5}
USERS=${USERS:-5000}
PORT=${PORT:-18443}
DISP=${DISP:-:77}
# raw numbers and the screenshots of every step are kept here (the report quotes them)
OUT=${OUT:-target/bench/$(date +%Y%m%d-%H%M%S)}
mkdir -p "$OUT"
work=$(mktemp -d)
mock_log=$work/mock.log
tick_ms=$((1000 / $(getconf CLK_TCK)))

cleanup() {
    [ -n "${app_pid:-}" ] && kill "$app_pid" 2>/dev/null || true
    [ -n "${mock_pid:-}" ] && kill "$mock_pid" 2>/dev/null || true
    [ -n "${x_pid:-}" ] && kill "$x_pid" 2>/dev/null || true
}
trap cleanup EXIT

now_ms() { echo $(($(date +%s%N) / 1000000)); }
cpu_ms() { awk -v t="$tick_ms" '{print ($14 + $15) * t}' "/proc/$1/stat"; }
pss_mib() { awk '/^Pss:/ {printf "%.1f", $2 / 1024}' "/proc/$1/smaps_rollup"; }

# waits until the process's CPU time is still for 300 ms
settle() {
    local pid=$1 last cur still=0 start
    start=$(now_ms)
    last=$(cpu_ms "$pid")
    while [ $still -lt 6 ]; do
        sleep 0.05
        cur=$(cpu_ms "$pid")
        if [ "$cur" = "$last" ]; then still=$((still + 1)); else still=0; last=$cur; fi
        if [ -n "${BENCH_TRACE:-}" ]; then echo "settle $step $cur $still" >&2; fi
        if [ $(($(now_ms) - start)) -gt 30000 ]; then echo "$step: not idle in 30 s" >&2; return 1; fi
    done
}

# waits until the mock has answered `n` requests matching `pattern` after line `mark` of its log
served() {
    local mark=$1 pattern=$2 n=$3 start
    start=$(now_ms)
    until [ "$(tail -n +"$((mark + 1))" "$mock_log" | grep -c -- "$pattern" || true)" -ge "$n" ]; do
        sleep 0.02
        if [ $(($(now_ms) - start)) -gt 30000 ]; then echo "$step: no $pattern in 30 s" >&2; return 1; fi
    done
}

Xvfb "$DISP" -screen 0 1920x1080x24 -dpi 96 >/dev/null 2>&1 &
x_pid=$!
# the same scale for both: 1 (96 dpi), software GL
export DISPLAY=$DISP LIBGL_ALWAYS_SOFTWARE=1 WINIT_X11_SCALE_FACTOR=1 FYNE_SCALE=1
sleep 1
"$MOCK" --users "$USERS" --port "$PORT" >"$mock_log" 2>&1 &
mock_pid=$!
until grep -q '^fingerprint' "$mock_log"; do sleep 0.05; done
pin=$(sed -n 's/^fingerprint = "\(.*\)"/\1/p' "$mock_log")
cfg=$work/desktop.toml
printf 'lang = "en"\n\n[panel]\nurl = "https://localhost:%s"\nfingerprint = "%s"\nuser = "administrator"\n' "$PORT" "$pin" >"$cfg"
chmod 600 "$cfg"

# the password field and the table, per app (both windows 1280 x 800 at the origin; the sidebar's Users item is at
# the same place in both)
declare -A PWD_AT=([fyne]="680 245" [rust]="640 207")
USERS_AT="60 132"
TABLE_AT="740 500"

# one run: prints "cold idle signin users_pss scroll"
run() {
    local app=$1 bin=$2 t0 t1 t2 t3 mark cpu0 cpu1 h0 h1 win
    local home=$work/home-$app-$run_n
    rm -rf "$home"
    mkdir -p "$home/.config" "$home/.local/share" "$home/.cache"
    local extra=()
    [ "$app" = rust ] && extra=(--gpu-info)
    t0=$(now_ms)
    HOME=$home XDG_CONFIG_HOME=$home/.config XDG_DATA_HOME=$home/.local/share XDG_CACHE_HOME=$home/.cache \
        WGPU_BACKEND=gl "$bin" --config "$cfg" "${extra[@]}" >"$work/$app.log" 2>&1 &
    app_pid=$!
    until win=$(xdotool search --onlyvisible --name "AD Manager" 2>/dev/null | head -n 1) && [ -n "$win" ]; do
        sleep 0.005
        if [ $(($(now_ms) - t0)) -gt 30000 ]; then echo "$app: no window" >&2; return 1; fi
    done
    t1=$(now_ms)
    # the same geometry for both, whatever each app asked for
    xdotool windowmove --sync "$win" 0 0 2>/dev/null || true
    xdotool windowsize --sync "$win" 1280 800 2>/dev/null || true
    # no idle wait here: the focused password field's blinking caret keeps redrawing (Fyne redraws the window for it)
    sleep 3
    local idle
    idle=$(pss_mib "$app_pid")
    import -window root "$OUT/$app-$run_n-1-signin.png"
    xdotool windowfocus --sync "$win" 2>/dev/null || true
    # shellcheck disable=SC2086
    xdotool mousemove ${PWD_AT[$app]} click 1
    xdotool type --delay 5 "Secret#1"
    mark=$(wc -l <"$mock_log")
    t2=$(now_ms)
    xdotool key Return
    step="$app home"
    served "$mark" "GET /api/summary" 1
    settle "$app_pid"
    # shellcheck disable=SC2086
    xdotool mousemove $USERS_AT click 1
    step="$app users"
    served "$mark" "GET /api/users" 2
    settle "$app_pid"
    t3=$(now_ms)
    sleep 3
    local users
    users=$(pss_mib "$app_pid")
    import -window root "$OUT/$app-$run_n-2-users.png"
    # shellcheck disable=SC2086
    xdotool mousemove $TABLE_AT
    h0=$(import -window root -crop 1040x560+220+190 png:- | md5sum)
    cpu0=$(cpu_ms "$app_pid")
    local s0 s1
    s0=$(now_ms)
    for _ in $(seq 1 200); do
        xdotool click 5
        sleep 0.02
    done
    step="$app scroll"
    settle "$app_pid"
    s1=$(($(now_ms) - 300)) # the idle window is not part of the scroll
    cpu1=$(cpu_ms "$app_pid")
    h1=$(import -window root -crop 1040x560+220+190 png:- | md5sum)
    import -window root "$OUT/$app-$run_n-3-scrolled.png"
    [ "$h0" != "$h1" ] || { echo "$app: the table did not scroll" >&2; return 1; }
    kill "$app_pid"
    wait "$app_pid" 2>/dev/null || true
    app_pid=
    # the graphics the run used (the Rust app's --gpu-info), kept per run; a Rust run without it fails
    cp "$work/$app.log" "$OUT/$app-$run_n.log"
    if [ "$app" = rust ] && ! grep -q '^gpu: adapter' "$work/$app.log"; then
        echo "rust: no --gpu-info adapter line" >&2
        return 1
    fi
    echo "$((t1 - t0)) $idle $((t3 - t2)) $users $((cpu1 - cpu0)) $((s1 - s0))"
}

median() { sort -n | awk '{a[NR]=$1} END {print (NR % 2) ? a[(NR + 1) / 2] : (a[NR / 2] + a[NR / 2 + 1]) / 2}'; }

{
    echo "date: $(date -Iseconds)"
    echo "cpu: $(lscpu | sed -n 's/^Model name: *//p') ($(nproc) threads)"
    echo "memory: $(free -h | awk '/^Mem/ {print $2}')"
    echo "kernel: $(uname -r)"
    echo "gl: Mesa llvmpipe on Xvfb 1920x1080x24, 96 dpi (LIBGL_ALWAYS_SOFTWARE=1)"
    echo "fyne: $FYNE ($(stat -c %s "$FYNE") bytes, $(strings "$FYNE" | grep -o 'v[0-9][0-9.]*-[0-9]*-g[0-9a-f]*' | head -n 1))"
    echo "rust: $RUST ($(stat -c %s "$RUST") bytes, $("$RUST" --version 2>/dev/null))"
    echo "users: $USERS, runs: $RUNS (+1 warm-up each, discarded), alternating fyne/rust"
    echo "rust graphics: wgpu with WGPU_BACKEND=gl (OpenGL on llvmpipe), fresh HOME/XDG per run"
} >"$OUT/machine.txt"
echo "app run cold_ms idle_pss_mib signin_to_users_ms users_pss_mib scroll_cpu_ms scroll_wall_ms" >"$OUT/raw.txt"

declare -A RES
for app in fyne rust; do
    bin=$FYNE
    [ "$app" = rust ] && bin=$RUST
    run_n=0
    run "$app" "$bin" >/dev/null # warm-up (file cache, fonts)
done
for i in $(seq 1 "$RUNS"); do
    for app in fyne rust; do
        bin=$FYNE
        [ "$app" = rust ] && bin=$RUST
        run_n=$i
        r=$(run "$app" "$bin")
        echo "run $i $app: $r" >&2
        echo "$app $i $r" >>"$OUT/raw.txt"
        [ "$app" = rust ] && grep '^gpu:' "$OUT/rust-$i.log" | sed "s/^/rust run $i /" >>"$OUT/machine.txt"
        RES[$app]+="$r"$'\n'
    done
done

size() {
    local s
    s=$(mktemp)
    cp "$1" "$s"
    strip "$s" 2>/dev/null || true
    printf '%.1f' "$(echo "$(stat -c %s "$s") / 1048576" | bc -l)"
    rm -f "$s"
}

col() { printf '%s' "${RES[$1]}" | awk -v c="$2" 'NF {print $c}' | median; }
exec > >(tee "$OUT/result.md")
cat "$OUT/machine.txt"
echo
echo "| Metric | Fyne (Go) | egui (Rust) |"
echo "|---|---:|---:|"
echo "| Binary size, stripped (MiB) | $(size "$FYNE") | $(size "$RUST") |"
echo "| Cold start to the sign-in window (ms) | $(col fyne 1) | $(col rust 1) |"
echo "| Idle memory at sign-in, PSS (MiB) | $(col fyne 2) | $(col rust 2) |"
echo "| Sign-in to Users with $USERS users (ms) | $(col fyne 3) | $(col rust 3) |"
echo "| Memory with $USERS users, PSS (MiB) | $(col fyne 4) | $(col rust 4) |"
echo "| CPU for 200 wheel steps on the table, all threads (ms) | $(col fyne 5) | $(col rust 5) |"
echo "| Wall time of the 200 wheel steps until idle (ms) | $(col fyne 6) | $(col rust 6) |"
