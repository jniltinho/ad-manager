#!/bin/sh
# Native smoke of the Linux build under a private Xvfb (xvfb-run -a): start the real window against a test panel,
# drive it with xdotool and save a screenshot of each step in $SHOTS. A smoke build (-tags smoke) then runs its own
# checks and prints "SMOKE PASS"; anything else fails the run.
#   ADM_PANEL_URL, ADM_LIVE_PASSWORD_FILE (0600, typed by xdotool, never printed), ADM_LIVE_USER (administrator).
#   ADM_PANEL_FINGERPRINT: with it the app starts configured (a private desktop.toml) and signs in on the sign-in
#   screen; without it the run goes through Configure (Test, Trust this certificate, Save).
# Test panels only: never point it at a production panel.
set -eu
if [ -z "${ADM_SMOKE_IN_XVFB:-}" ]; then
    ADM_SMOKE_IN_XVFB=1 exec xvfb-run -a -s "-screen 0 1440x900x24 -nolisten tcp" "$0" "$@"
fi
bin=${1:-bin/linux/ad-manager-desktop}
shots=${SHOTS:-.cache/smoke}
cfg=$(mktemp -d)
mkdir -p "$shots"
app=""
trap 'if [ -n "$app" ]; then kill "$app" 2>/dev/null || true; fi; rm -rf "$cfg"' EXIT
chmod 700 "$cfg"
if [ -n "${ADM_PANEL_FINGERPRINT:-}" ]; then
    (umask 077; printf '[panel]\nurl = "%s"\nfingerprint = "%s"\nuser = "%s"\n' "$ADM_PANEL_URL" "$ADM_PANEL_FINGERPRINT" \
        "${ADM_LIVE_USER:-administrator}" > "$cfg/desktop.toml")
fi
export GDK_BACKEND=x11 LIBGL_ALWAYS_SOFTWARE=1 ADM_SMOKE_PANEL=1
start=$(date +%s%N)
dbus-run-session -- "$bin" --config "$cfg/desktop.toml" >"$shots/app.log" 2>&1 &
app=$!
win=""
for _ in $(seq 1 100); do
    win=$(xdotool search --name "^AD Manager$" 2>/dev/null | head -n 1) && [ -n "$win" ] && break
    sleep 0.1
done
[ -n "$win" ] || { echo "no window"; cat "$shots/app.log"; exit 1; }
echo "window after $(( ($(date +%s%N) - start) / 1000000 )) ms"
focus() { xdotool windowactivate --sync "$win" 2>/dev/null || xdotool windowfocus --sync "$win"; }
sleep 3
if [ -z "${ADM_PANEL_FINGERPRINT:-}" ]; then
    import -window root "$shots/01-configure.png"
    focus
    xdotool type --delay 10 "$ADM_PANEL_URL"
    xdotool key Tab ctrl+a
    xdotool type --delay 10 "${ADM_LIVE_USER:-administrator}"
    xdotool key Tab
    xdotool type --delay 10 --file "$ADM_LIVE_PASSWORD_FILE"
    xdotool key Return            # Test: the certificate is shown, its Trust button focused
    sleep 3
    import -window root "$shots/02-certificate.png"
    focus
    xdotool key Return            # Trust this certificate: tests again, Save focused
    sleep 4
    import -window root "$shots/03-tested.png"
    focus
    xdotool key Return            # Save: the app reloads signed in
    sleep 5
    import -window root "$shots/04-home.png"
else
    import -window root "$shots/01-signin.png"
    focus
    xdotool key ctrl+a
    xdotool type --delay 10 "${ADM_LIVE_USER:-administrator}"
    xdotool key Tab
    xdotool type --delay 10 --file "$ADM_LIVE_PASSWORD_FILE"
    xdotool key Return
    sleep 5
    import -window root "$shots/02-home.png"
fi
if [ -n "${ADM_SMOKE_DIR:-}" ]; then
    wait "$app" || true
    app=""
    grep -a "^SMOKE" "$shots/app.log" || true
    grep -aq "^SMOKE PASS" "$shots/app.log" || { echo "smoke failed"; exit 1; }
fi
