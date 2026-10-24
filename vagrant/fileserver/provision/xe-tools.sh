#!/usr/bin/env bash
# xe-guest-utilities for XCP-ng (inert on VirtualBox): the xenserver GitHub release, version and SHA-256 pinned.
set -euo pipefail
V=${XE_VERSION:-10.0.0}
SHA256=${XE_SHA256:-62fa81534a6a0e762ffe40e91fe1e2f3f4850a4270304928eb4aff647a914b13}
f=xe-guest-utilities_${V}-1_amd64.deb
dpkg -s xe-guest-utilities 2>/dev/null | grep -q "^Version: ${V}-1$" && { echo "xe-guest-utilities already ${V}-1"; exit 0; }
tmp=$(mktemp -d)
curl -fsSL --retry 3 -o "$tmp/$f" "https://github.com/xenserver/xe-guest-utilities/releases/download/v${V}/$f"
echo "$SHA256  $tmp/$f" | sha256sum -c -
DEBIAN_FRONTEND=noninteractive apt-get install -y -qq "$tmp/$f" >/dev/null
rm -rf "$tmp"
dpkg -s xe-guest-utilities | grep -E '^(Status|Version)'
