#!/usr/bin/env bash
# Benchmark of the Wails desktop (preview) against the Rust desktop, the method of desktop/bench/run.sh: the same X
# server (Xvfb, Mesa llvmpipe), the same mock panel (5,000 users of domain.local), release builds, medians of RUNS runs
# per app (one warm-up each, discarded), runs alternating Rust / Wails. Memory (PSS) and CPU are summed over the app
# and every process it starts: the webview's WebKit processes do the Wails app's rendering, so counting only its main
# process would flatter it (gate G4 of openspec/changes/desktop-wails).
#
# Needs: Xvfb, xdotool, import (ImageMagick), the WebKitGTK 6.0 runtime, and the binaries:
#   RUST  (default ../desktop/target/docker/release/ad-manager-desktop — `make -C desktop release`)
#   WAILS (default bin/linux/ad-manager-desktop-wails — `make linux`)
#   MOCK  (default ../desktop/target/docker/release/examples/mock_panel — see desktop/bench/run.sh)
#
# Readiness (the same for both): the mock answered the request a step waits for, then the CPU time of the whole
# process tree did not move for 300 ms. Cold start is reported twice: when the window maps, and when the sign-in
# screen is ready (the panel's /api/health answered and the tree is idle). A step not ready in 30 s fails the run.
set -euo pipefail
export LC_ALL=C
cd "$(dirname "$0")/.."

RUST=${RUST:-../desktop/target/docker/release/ad-manager-desktop}
WAILS=${WAILS:-bin/linux/ad-manager-desktop-wails}
MOCK=${MOCK:-../desktop/target/docker/release/examples/mock_panel}
RUNS=${RUNS:-5}
USERS=${USERS:-5000}
PORT=${PORT:-18543}
DISP=${DISP:-:78}
OUT=${OUT:-.cache/bench/$(date +%Y%m%d-%H%M%S)}
mkdir -p "$OUT"
work=$(mktemp -d)
mock_log=$work/mock.log
tick_ms=$((1000 / $(getconf CLK_TCK)))

cleanup() {
    [ -n "${app_pid:-}" ] && kill_tree "$app_pid"
    [ -n "${mock_pid:-}" ] && kill "$mock_pid" 2>/dev/null || true
    [ -n "${x_pid:-}" ] && kill "$x_pid" 2>/dev/null || true
    rm -rf "$work"
}
trap cleanup EXIT

now_ms() { echo $(($(date +%s%N) / 1000000)); }
# tree: the pid and all its descendants
tree() {
    local p
    echo "$1"
    for p in $(pgrep -P "$1" 2>/dev/null); do tree "$p"; done
}
kill_tree() {
    local pids
    pids=$(tree "$1")
    # shellcheck disable=SC2086
    kill $pids 2>/dev/null || true
    wait "$1" 2>/dev/null || true
    # shellcheck disable=SC2086
    for _ in $(seq 1 50); do kill -0 $pids 2>/dev/null || break; sleep 0.1; done
}
cpu_ms() {
    local p sum=0 v
    for p in $(tree "$1"); do
        v=$(awk -v t="$tick_ms" '{print ($14 + $15) * t}' "/proc/$p/stat" 2>/dev/null || echo 0)
        sum=$((sum + v))
    done
    echo "$sum"
}
pss_mib() {
    local p kb=0 v
    for p in $(tree "$1"); do
        v=$(awk '/^Pss:/ {print $2}' "/proc/$p/smaps_rollup" 2>/dev/null || echo 0)
        kb=$((kb + ${v:-0}))
    done
    awk -v k="$kb" 'BEGIN {printf "%.1f", k / 1024}'
}
nprocs() { tree "$1" | wc -l; }

settle() {
    local pid=$1 last cur still=0 start
    start=$(now_ms)
    last=$(cpu_ms "$pid")
    while [ $still -lt 6 ]; do
        sleep 0.05
        cur=$(cpu_ms "$pid")
        if [ "$cur" = "$last" ]; then still=$((still + 1)); else still=0; last=$cur; fi
        if [ $(($(now_ms) - start)) -gt 30000 ]; then echo "$step: not idle in 30 s" >&2; return 1; fi
    done
}

served() {
    local mark=$1 pattern=$2 n=$3 start
    start=$(now_ms)
    until [ "$(tail -n +"$((mark + 1))" "$mock_log" | grep -c -- "$pattern" || true)" -ge "$n" ]; do
        sleep 0.02
        if [ $(($(now_ms) - start)) -gt 30000 ]; then echo "$step: no $pattern in 30 s" >&2; return 1; fi
    done
}

Xvfb "$DISP" -screen 0 1920x1080x24 -dpi 96 -nolisten tcp >/dev/null 2>&1 &
x_pid=$!
export DISPLAY=$DISP LIBGL_ALWAYS_SOFTWARE=1 WINIT_X11_SCALE_FACTOR=1 GDK_BACKEND=x11 GDK_SCALE=1
sleep 1
"$MOCK" --users "$USERS" --port "$PORT" >"$mock_log" 2>&1 &
mock_pid=$!
until grep -q '^fingerprint' "$mock_log"; do sleep 0.05; done
pin=$(sed -n 's/^fingerprint = "\(.*\)"/\1/p' "$mock_log")

# the password field, the Users tab and the table, per app (both windows 1280 x 800 at the origin, undecorated)
declare -A PWD_AT=([rust]="682 248" [wails]="832 247")
declare -A USERS_AT=([rust]="161 104" [wails]="147 96")
# the mock answers /api/me without a session, so the web SPA opens signed in: the Wails run signs out first (its
# header link), then signs in on the sign-in screen as the Rust app does
SIGNOUT_AT="1142 54"
TABLE_AT="740 500"

# one run: prints "window_ms ready_ms idle_pss signin_to_users_ms users_pss scroll_cpu_ms scroll_wall_ms procs"
run() {
    local app=$1 bin=$2 t0 t1 tr t2 t3 mark cpu0 cpu1 h0 h1 win
    local home=$work/home-$app-$run_n
    rm -rf "$home"
    mkdir -p "$home/.config" "$home/.local/share" "$home/.cache"
    chmod 700 "$home"
    printf 'lang = "en"\n\n[panel]\nurl = "https://localhost:%s"\nfingerprint = "%s"\nuser = "administrator"\n' "$PORT" "$pin" \
        >"$home/desktop.toml"
    chmod 600 "$home/desktop.toml"
    mark=$(wc -l <"$mock_log")
    t0=$(now_ms)
    HOME=$home XDG_CONFIG_HOME=$home/.config XDG_DATA_HOME=$home/.local/share XDG_CACHE_HOME=$home/.cache \
        WGPU_BACKEND=gl "$bin" --config "$home/desktop.toml" >"$work/$app.log" 2>&1 &
    app_pid=$!
    until win=$(xdotool search --onlyvisible --name "AD Manager" 2>/dev/null | head -n 1) && [ -n "$win" ]; do
        sleep 0.005
        if [ $(($(now_ms) - t0)) -gt 30000 ]; then echo "$app: no window" >&2; cat "$work/$app.log" >&2; return 1; fi
    done
    t1=$(now_ms)
    xdotool windowmove --sync "$win" 0 0 2>/dev/null || true
    xdotool windowsize --sync "$win" 1280 800 2>/dev/null || true
    step="$app sign-in"
    served "$mark" "GET /api/health" 1
    settle "$app_pid" || true # a focused field's caret may keep one app busy: the time is then the health answer's
    tr=$(now_ms)
    if [ "$app" = wails ]; then
        mark=$(wc -l <"$mock_log")
        xdotool windowfocus --sync "$win" 2>/dev/null || true
        # shellcheck disable=SC2086
        xdotool mousemove $SIGNOUT_AT click 1
        step="$app sign-out"
        served "$mark" "POST /api/logout" 1
        settle "$app_pid" || true
    fi
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
    import -window root "$OUT/$app-$run_n-1b-home.png"
    # shellcheck disable=SC2086
    xdotool mousemove ${USERS_AT[$app]} click 1
    step="$app users"
    served "$mark" "GET /api/users" 2
    settle "$app_pid"
    t3=$(now_ms)
    sleep 3
    local users procs
    users=$(pss_mib "$app_pid")
    procs=$(nprocs "$app_pid")
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
    s1=$(($(now_ms) - 300))
    cpu1=$(cpu_ms "$app_pid")
    h1=$(import -window root -crop 1040x560+220+190 png:- | md5sum)
    import -window root "$OUT/$app-$run_n-3-scrolled.png"
    [ "$h0" != "$h1" ] || { echo "$app: the table did not scroll" >&2; return 1; }
    kill_tree "$app_pid"
    app_pid=
    cp "$work/$app.log" "$OUT/$app-$run_n.log"
    echo "$((t1 - t0)) $((tr - t0)) $idle $((t3 - t2)) $users $((cpu1 - cpu0)) $((s1 - s0)) $procs"
}

median() { sort -n | awk '{a[NR]=$1} END {print (NR % 2) ? a[(NR + 1) / 2] : (a[NR / 2] + a[NR / 2 + 1]) / 2}'; }

{
    echo "date: $(date -Iseconds)"
    echo "cpu: $(lscpu | sed -n 's/^Model name: *//p') ($(nproc) threads)"
    echo "memory: $(free -h | awk '/^Mem/ {print $2}')"
    echo "kernel: $(uname -r)"
    echo "display: Xvfb 1920x1080x24, 96 dpi, Mesa llvmpipe (LIBGL_ALWAYS_SOFTWARE=1)"
    echo "rust: $RUST ($(stat -c %s "$RUST") bytes, $("$RUST" --version 2>/dev/null)), wgpu with WGPU_BACKEND=gl"
    echo "wails: $WAILS ($(stat -c %s "$WAILS") bytes, $("$WAILS" --version 2>/dev/null)), WebKitGTK $(dpkg-query -W -f '${Version}' libwebkitgtk-6.0-4 2>/dev/null)"
    echo "users: $USERS, runs: $RUNS (+1 warm-up each, discarded), alternating rust/wails, fresh HOME/XDG per run"
} >"$OUT/machine.txt"
echo "app run window_ms ready_ms idle_pss_mib signin_to_users_ms users_pss_mib scroll_cpu_ms scroll_wall_ms procs" >"$OUT/raw.txt"

declare -A RES BIN=([rust]=$RUST [wails]=$WAILS)
for app in rust wails; do
    run_n=0
    run "$app" "${BIN[$app]}" >/dev/null
done
for i in $(seq 1 "$RUNS"); do
    for app in rust wails; do
        run_n=$i
        r=$(run "$app" "${BIN[$app]}")
        echo "run $i $app: $r" >&2
        echo "$app $i $r" >>"$OUT/raw.txt"
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
# budget: "<= limit" → pass/FAIL (G4); a missed budget is waived only in writing, in design.md
mark() { awk -v v="$1" -v l="$2" 'BEGIN {print (v <= l) ? "pass" : "FAIL"}'; }
exec > >(tee "$OUT/result.md")
cat "$OUT/machine.txt"
echo
echo "| Metric | Rust (egui) | Wails | G4 budget | Wails vs budget |"
echo "|---|---:|---:|---:|---|"
rs=$(size "$RUST") ws=$(size "$WAILS")
echo "| Binary size, stripped (MiB) | $rs | $ws | ≤ 35 | $(mark "$ws" 35) |"
echo "| Cold start to the window (ms) | $(col rust 1) | $(col wails 1) | — | — |"
w=$(col wails 2)
echo "| Cold start to a ready sign-in screen (ms) | $(col rust 2) | $w | ≤ 1000 | $(mark "$w" 1000) |"
w=$(col wails 3)
echo "| Idle memory at sign-in, PSS of all processes (MiB) | $(col rust 3) | $w | ≤ 300 | $(mark "$w" 300) |"
w=$(col wails 4)
echo "| Sign-in to Users with $USERS users (ms) | $(col rust 4) | $w | ≤ 1500 | $(mark "$w" 1500) |"
w=$(col wails 5)
echo "| Memory with $USERS users, PSS of all processes (MiB) | $(col rust 5) | $w | ≤ 450 | $(mark "$w" 450) |"
r=$(col rust 6) w=$(col wails 6)
echo "| CPU for 200 wheel steps, all processes (ms) | $r | $w | ≤ 1.5× Rust | $(mark "$w" "$(echo "$r * 1.5" | bc -l)") |"
echo "| Wall time of the 200 wheel steps until idle (ms) | $(col rust 7) | $(col wails 7) | — | — |"
echo "| Processes | $(col rust 8) | $(col wails 8) | — | — |"
