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
"$GO" tool mygo build -platform linux/amd64 -skip-installer -skip-dmg -skip-notarize
