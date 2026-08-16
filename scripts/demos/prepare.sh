#!/usr/bin/env bash
# Stage everything the demo recordings run against.
#
# The recordings execute the real binary against the real sample application, so
# they need a build and a working directory to make a mess in. Both live in
# scripts/demos/build/, which is disposable and not tracked — delete it and run
# this again whenever a recording looks stale.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
build="$here/build"

rm -rf "$build"
mkdir -p "$build"

echo "building 3270Connect…"
(cd "$repo" && go build -o "$build/3270Connect" .)

cp "$here"/workflows/*.json "$build/"

echo "staged $build"
"$build/3270Connect" -version 2>/dev/null || true
