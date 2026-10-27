#!/bin/sh
# Build "AD Manager.app" around a universal binary and zip it. Runs on macOS (sips, iconutil, codesign and
# ditto come with the system); ad-hoc signed unless MACOS_SIGN_IDENTITY names a Developer ID.
#
# Usage: packaging/macos/bundle.sh <binary> <version> <out-dir>
set -eu

bin=${1:?binary path}
version=${2:?version}
out=${3:?output directory}
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

[ -f "$bin" ] || { echo "binary not found: $bin" >&2; exit 1; }
mkdir -p "$out"
out=$(CDPATH= cd -- "$out" && pwd)
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
app="$stage/AD Manager.app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp "$bin" "$app/Contents/MacOS/ad-manager-desktop"
chmod +x "$app/Contents/MacOS/ad-manager-desktop"
sed "s/__VERSION__/$version/g" "$here/Info.plist" > "$app/Contents/Info.plist"

set_dir=$(mktemp -d)/icon.iconset
mkdir -p "$set_dir"
for s in 16 32 128 256 512; do
    sips -z "$s" "$s" "$here/../icon.png" --out "$set_dir/icon_${s}x${s}.png" >/dev/null
    sips -z $((s * 2)) $((s * 2)) "$here/../icon.png" --out "$set_dir/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns "$set_dir" -o "$app/Contents/Resources/icon.icns"

codesign --force --deep --sign "${MACOS_SIGN_IDENTITY:--}" "$app"
codesign --verify --deep --strict "$app"
(cd "$stage" && ditto -c -k --keepParent "AD Manager.app" "$out/ad-manager-desktop_${version}_macos_universal.zip")
echo "$out/ad-manager-desktop_${version}_macos_universal.zip"
