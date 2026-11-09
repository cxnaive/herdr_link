#!/bin/sh
# Build the complete Universal app and DMG on macOS, with no Developer ID secrets.
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"
[ "$(uname -s)" = Darwin ] || { echo 'DMG production requires macOS' >&2; exit 1; }
GO=${GO_BIN:-go}
"$GO" tool mygo build -platform darwin/universal -sign - -skip-installer -skip-notarize
