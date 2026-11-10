#!/bin/sh
# Opt-in verification against pinned, published UI components. No real model/tools/Herdr.
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
GO=$(command -v "${GO_BIN:-go}")
BUN=$(command -v "${BUN_BIN:-bun}")
WORK=$(mktemp -d "${TMPDIR:-/tmp}/herdr-pi-omp.XXXXXXXX")
printf 'Upstream contract work/results: %s\n' "$WORK"
mkdir -p "$WORK/runtime" "$WORK/home/.pi/agent" "$WORK/cache"
cp "$ROOT/tests/upstream-pi-omp/package.json" "$ROOT/tests/upstream-pi-omp/bun.lock" "$WORK/runtime/"
# Only this dependency step needs network. Never install globally or run dependency postinstall hooks.
env -i PATH="$PATH" HOME="$WORK/home" BUN_INSTALL_CACHE_DIR="$WORK/cache" \
    "$BUN" install --cwd "$WORK/runtime" --frozen-lockfile --ignore-scripts
(cd "$ROOT" && "$GO" test -c -o "$WORK/coordinator.test" ./internal/coordinator)
env -i PATH="$PATH" HOME="$WORK/home" PI_CODING_AGENT_DIR="$WORK/home/.pi/agent" \
    PI_OMP_RUNTIME="$WORK/runtime" PI_OMP_WORK="$WORK" PI_OMP_GO_TEST="$WORK/coordinator.test" \
    PI_OMP_REQUIRE_ALL="${PI_OMP_REQUIRE_ALL:-1}" \
    "$BUN" "$ROOT/tests/pi-omp-upstream.mjs"
