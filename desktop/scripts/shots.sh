#!/bin/sh
# The desktop app's docs screenshots (docs/screenshots/desktop-*.png), taken from the real app: 1280×800 at 2×
# (GDK_SCALE=2, 2560×1600), Classic blue, English, against the demo panel (tools/demopanel: Example Company Ltd,
# domain.local), on the host under a private Xvfb (WebKit's sandbox needs user namespaces, which Docker has not).
#   scripts/shots.sh <app binary> <demo panel binary> <out dir>
set -eu
if [ -z "${ADM_SHOTS_IN_XVFB:-}" ]; then
    ADM_SHOTS_IN_XVFB=1 exec xvfb-run -a -s "-screen 0 2560x1600x24 -dpi 192 -nolisten tcp" "$0" "$@"
fi
bin=$1 demo=$2 out=$3
mkdir -p "$out"
cfg=$(mktemp -d)
log=$cfg/demo.log
app="" panel=""
trap 'kill $app $panel 2>/dev/null || true; rm -rf "$cfg"' EXIT
chmod 700 "$cfg"
"$demo" --port 18643 >"$log" 2>&1 &
panel=$!
for _ in $(seq 1 100); do
    grep -q '^fingerprint' "$log" && break
    kill -0 "$panel" 2>/dev/null || break
    sleep 0.1
done
grep -q '^fingerprint' "$log" || { echo "the demo panel did not start (port 18643 busy?):"; cat "$log"; exit 1; }
# this computer's preferences only: Configure opens, in Classic blue and English
(umask 077; printf 'lang = "en"\n\n[appearance]\ntheme = "classic-blue"\n' >"$cfg/desktop.toml")
export GDK_BACKEND=x11 GDK_SCALE=2 LIBGL_ALWAYS_SOFTWARE=1
dbus-run-session -- "$bin" --config "$cfg/desktop.toml" >"$cfg/app.log" 2>&1 &
app=$!
win=""
for _ in $(seq 1 100); do
    win=$(xdotool search --name "^AD Manager$" 2>/dev/null | head -n 1) && [ -n "$win" ] && break
    sleep 0.1
done
[ -n "$win" ] || { cat "$cfg/app.log"; exit 1; }
xdotool windowmove --sync "$win" 0 0 windowsize --sync "$win" 2560 1600
focus() { xdotool windowactivate --sync "$win" 2>/dev/null || xdotool windowfocus --sync "$win"; }
shot() { sleep "$2"; import -window root "$out/desktop-$1.png"; }
# clicks are in the window's logical pixels (1280×800), doubled for the 2× screen
click() { xdotool mousemove "$(($1 * 2))" "$(($2 * 2))" click 1; }
shot 01-configure 3
focus
xdotool type --delay 10 "https://localhost:18643"
xdotool key Tab ctrl+a
xdotool type --delay 10 administrator
xdotool key Tab
xdotool type --delay 10 'Secret#1'
xdotool key Return            # Test: the self-signed certificate is shown
shot 02-certificate 3
focus
xdotool key Return            # Trust this certificate: tested, Save focused
sleep 3
focus
xdotool key Return            # Save: the app opens signed in
shot 04-home 5
focus
click 147 96                  # Users
shot 05-users 3
click 1142 54                 # sign out
shot 03-signin 3
focus
click 832 247                 # the password field
xdotool type --delay 10 'Secret#1'
xdotool key Return
sleep 3
