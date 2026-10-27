#!/bin/sh
# Native smoke of the Linux build under Xvfb (run inside the toolchain image, scripts/go-docker.sh): start the real
# window against a test panel, sign in with xdotool and save a screenshot of each step in $SHOTS.
#   ADM_PANEL_URL, ADM_PANEL_FINGERPRINT: the test panel; ADM_LIVE_PASSWORD_FILE: its password (a 0600 file, read
#   by xdotool, never printed); ADM_LIVE_USER (administrator by default). Never point it at a production panel.
set -eu
bin=${1:-bin/linux/ad-manager-desktop-wails}
shots=${SHOTS:-.cache/smoke}
mkdir -p "$shots"
Xvfb "${XDISPLAY:-:99}" -screen 0 1440x900x24 -nolisten tcp >/dev/null 2>&1 &
xvfb=$!
export DISPLAY="${XDISPLAY:-:99}" GDK_BACKEND=x11 LIBGL_ALWAYS_SOFTWARE=1
sleep 1
start=$(date +%s%N)
dbus-run-session -- "$bin" >"$shots/app.log" 2>&1 &
app=$!
win=""
for _ in $(seq 1 100); do
    win=$(xdotool search --name "AD Manager" 2>/dev/null | head -n 1) && [ -n "$win" ] && break
    sleep 0.1
done
[ -n "$win" ] || { echo "no window"; cat "$shots/app.log"; exit 1; }
echo "window after $(( ($(date +%s%N) - start) / 1000000 )) ms"
sleep 3
import -window root "$shots/01-signin.png"
xdotool type --delay 20 "${ADM_LIVE_USER:-administrator}"
xdotool key Tab
xdotool type --delay 20 --file "$ADM_LIVE_PASSWORD_FILE"
xdotool key Return
sleep 5
import -window root "$shots/02-home.png"
# a smoke build (-tags smoke) runs its scripted checks now and quits; a release build is just closed
[ -n "${ADM_SMOKE_DIR:-}" ] && { wait "$app" || true; grep -a "^SMOKE" "$shots/app.log" || true; import -window root "$shots/03-after.png"; }
kill "$app" 2>/dev/null || true
wait "$app" 2>/dev/null || true
kill "$xvfb" 2>/dev/null || true
grep -iE "error|panic|fatal" "$shots/app.log" | grep -v "^$" | head -n 20 || true
