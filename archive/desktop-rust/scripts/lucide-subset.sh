#!/bin/sh
# Rebuilds crates/core/assets/fonts/lucide.ttf with the icons of crates/ui/src/icons.rs, from lucide-static at the
# version the web panel uses (frontend's lucide-vue-next). Needs npm and python3 with fonttools.
set -eu
cd "$(dirname "$0")/.."
version=$(node -p "require('../frontend/node_modules/lucide-vue-next/package.json').version")
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
(cd "$work" && npm pack "lucide-static@$version" --silent >/dev/null && tar xzf lucide-static-*.tgz)
codes=$(sed -n 's/.*"\\u{\([0-9a-f]*\)}".*/U+\1/p' crates/ui/src/icons.rs | paste -sd, -)
python3 -m fontTools.subset "$work/package/font/lucide.ttf" --unicodes="$codes" \
	--output-file=crates/core/assets/fonts/lucide.ttf --no-hinting --layout-features=''
cp "$work/package/LICENSE" crates/core/assets/fonts/LICENSE-lucide.txt
echo "lucide.ttf: $(echo "$codes" | tr ',' '\n' | wc -l) icons from lucide-static $version"
