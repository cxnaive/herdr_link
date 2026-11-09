#!/bin/sh
# Actual GTK startup, private Secret Service, and screenshots of demo data only.
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"
ARCH=${HERDR_LINUX_ARCH:-$(go env GOHOSTARCH)}
case "$ARCH" in amd64|arm64) ;; *) echo "Unsupported Linux architecture: $ARCH" >&2; exit 2 ;; esac
BUNDLE="$ROOT/build/linux-$ARCH"
VERSION=$(python3 -c 'import json; print(json.load(open("mygo.json"))["version"])')
ARCHIVE="$BUNDLE/herdr-desktop-$VERSION-linux-$ARCH.tar.gz"
OUTPUT="$ROOT/build/linux-smoke"
mkdir -p "$OUTPUT"
if [ "${1:-}" != --isolated ]; then
    for tool in xvfb-run dbus-run-session gnome-keyring-daemon xdotool import; do
        command -v "$tool" >/dev/null || { echo "Missing smoke-test tool: $tool" >&2; exit 1; }
    done
    GOCACHE=$(go env GOCACHE)
    GOMODCACHE=$(go env GOMODCACHE)
    export GOCACHE GOMODCACHE
    TEMP=$(mktemp -d)
    trap 'rm -rf -- "$TEMP"' EXIT HUP INT TERM
    export HOME="$TEMP/home" XDG_CONFIG_HOME="$TEMP/config" XDG_DATA_HOME="$TEMP/data" XDG_CACHE_HOME="$TEMP/cache" XDG_RUNTIME_DIR="$TEMP/run"
    mkdir -m 700 -p "$HOME" "$XDG_CONFIG_HOME" "$XDG_DATA_HOME" "$XDG_CACHE_HOME" "$XDG_RUNTIME_DIR"
    # Set the private directories before creating the bus: activated services
    # must inherit them too, not the user's real desktop configuration.
    xvfb-run -a -s '-screen 0 1440x1000x24 -nolisten tcp' dbus-run-session -- "$ROOT/scripts/smoke-linux.sh" --isolated
    exit
fi
APP_PID=
cleanup() {
    if [ -n "$APP_PID" ]; then kill "$APP_PID" 2>/dev/null || true; wait "$APP_PID" 2>/dev/null || true; fi
}
trap cleanup EXIT HUP INT TERM
export LIBGL_ALWAYS_SOFTWARE=1 GDK_BACKEND=x11
export MYGO_GHOSTTY_VT="$BUNDLE/libghostty-vt.so"
# A new bus and home prevent reading or unlocking the user's real keyring.
printf '%s\n' 'herdr-test-only' | gnome-keyring-daemon --unlock --components=secrets > "$OUTPUT/keyring.log" 2>&1
HERDR_LINUX_INTEGRATION=1 go test -count=1 -timeout=45s -run '^TestLinuxSecretServiceRoundTrip$' .
sh "$BUNDLE/install.sh" "$ARCHIVE" > "$OUTPUT/install.log" 2>&1
cmp "$BUNDLE/herdr-desktop" "$HOME/.local/herdr-desktop.app/herdr-desktop"
cmp "$BUNDLE/libghostty-vt.so" "$HOME/.local/herdr-desktop.app/libghostty-vt.so"
[ -L "$HOME/.local/bin/herdr-desktop" ]
[ -f "$XDG_DATA_HOME/applications/herdr-desktop.desktop" ]
"$HOME/.local/bin/herdr-desktop" --demo > "$OUTPUT/startup.log" 2>&1 &
APP_PID=$!
WINDOW=$(timeout 25s xdotool search --sync --onlyvisible --name '^Herdr Desktop$' | head -n 1)
[ -n "$WINDOW" ]
xdotool windowfocus --sync "$WINDOW"
sleep 2
kill -0 "$APP_PID"
import -window "$WINDOW" "$OUTPUT/conversation.png"
xdotool key --window "$WINDOW" ctrl+2
sleep 1
import -window "$WINDOW" "$OUTPUT/terminal.png"
kill -0 "$APP_PID"
go run ./internal/pdfview/smoke "$OUTPUT/pdf.png" > "$OUTPUT/pdf.log" 2>&1
[ -s "$OUTPUT/pdf.png" ]
# Capturing a window is not proof of interaction tests; these are visual artifacts.
[ -s "$OUTPUT/conversation.png" ] && [ -s "$OUTPUT/terminal.png" ]
kill "$APP_PID"
wait "$APP_PID" 2>/dev/null || true
APP_PID=
mkdir -p "$XDG_CONFIG_HOME/Herdr Desktop"
printf '%s' 'keep-synthetic-user-data' > "$XDG_CONFIG_HOME/Herdr Desktop/state.enc"
sh "$BUNDLE/install.sh" --uninstall >> "$OUTPUT/install.log" 2>&1
[ ! -e "$HOME/.local/bin/herdr-desktop" ]
[ "$(cat "$XDG_CONFIG_HOME/Herdr Desktop/state.enc")" = 'keep-synthetic-user-data' ]
printf 'Linux GTK startup, PDF rendering and isolated Secret Service passed. Screenshots: %s\n' "$OUTPUT"
