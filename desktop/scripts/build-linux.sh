#!/bin/sh
# The Linux client uses the same Go application and pinned terminal library.
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"
if [ -n "${GO_BIN:-}" ]; then
    GO=$GO_BIN
elif [ -x ../.tools/go/bin/go ]; then
    GO="$ROOT/../.tools/go/bin/go"
else
    GO=$(command -v go)
fi
PATH="$(dirname -- "$GO"):$PATH"
export PATH
ARCH=${HERDR_LINUX_ARCH:-$("$GO" env GOHOSTARCH)}
case "$ARCH" in amd64|arm64) ;; *) echo "Unsupported Linux architecture: $ARCH" >&2; exit 2 ;; esac
"$GO" tool mygo build -platform "linux/$ARCH" -skip-installer -skip-dmg -skip-notarize
