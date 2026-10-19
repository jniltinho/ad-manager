#!/usr/bin/env bash
# Visual regression of the themes against a base commit (theme-base 4.5), meant for the pinned Playwright container
# (mcr.microsoft.com/playwright, the version of frontend/package-lock.json): the base commit's build records the
# baseline and the current build is compared with it, both in the same Chromium, so no image is versioned. The API
# replies come from the versioned HARs (frontend/tests/visual/har).
#
#   scripts/visual-ci.sh <base commit>
set -euo pipefail
BASE=${1:?usage: $0 <base commit>}
root=$(git rev-parse --show-toplevel)
base=$(mktemp -d)
git -C "$root" worktree add --detach "$base" "$BASE"
trap 'git -C "$root" worktree remove --force "$base"' EXIT
(cd "$base/frontend" && npm ci --no-audit --no-fund && npm run build)
cd "$root/frontend"
npm ci --no-audit --no-fund
npm run build
export PW_CHANNEL=chromium
DIST="$base/web/dist" BASELINE=1 npx playwright test
npx playwright test
