#!/bin/sh
# Package the local native-TCP extensions, not the upstream plugin updater format.
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"
[ "$#" = 1 ] || { echo 'usage: scripts/package-native-release.sh NEW_OUTPUT_DIRECTORY' >&2; exit 2; }
for tool in go python3 git tar sha256sum; do
    command -v "$tool" >/dev/null || { echo "Missing build tool: $tool" >&2; exit 1; }
done
ARCH=$(go env GOHOSTARCH)
case "$(go env GOHOSTOS)/$ARCH" in
    linux/amd64|linux/arm64) ;;
    *) echo 'Build and smoke-test on Linux amd64 or arm64.' >&2; exit 1 ;;
esac
REVISION=$(git rev-parse HEAD)
if ! git diff --quiet || ! git diff --cached --quiet || [ -n "$(git ls-files --others --exclude-standard)" ]; then
    [ "${NATIVE_ALLOW_DIRTY:-0}" = 1 ] || { echo 'Commit changes before release packaging (NATIVE_ALLOW_DIRTY=1 is for local tests only).' >&2; exit 1; }
    REVISION=$REVISION-dirty
fi
[ ! -e "$1" ] || { echo 'Refusing to replace an existing output directory.' >&2; exit 1; }
mkdir -p "$1"
OUTPUT=$(CDPATH='' cd -- "$1" && pwd)
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT HUP INT TERM
EPOCH=$(git log -1 --format=%ct)
export CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" GOAMD64=v1 GOTOOLCHAIN=local
for component in relay gateway; do
    if [ "$component" = relay ]; then
        key=relay; binary=herdr-mobile-relay; templates=deploy/native-relay
    else
        key=native_gateway; binary=herdr-gateway; templates=deploy/native-tcp
    fi
    VERSION=$(python3 -c 'import json,sys; print(json.load(open("VERSION.json"))[sys.argv[1]]["version"])' "$key")
    NAME="${binary}_$(printf '%s' "$VERSION" | tr + -)_linux_${ARCH}"
    STAGE="$WORK/$NAME"
    mkdir "$STAGE"
    go build -mod=readonly -trimpath -ldflags "-s -w -X main.version=$VERSION -X main.revision=$REVISION" -o "$STAGE/$binary" "./cmd/$binary"
    cp LICENSE VERSION.json "$STAGE/"
    cp docs/native-deployment.md "$STAGE/DEPLOYMENT.md"
    cp docs/gateway-management.md "$STAGE/GATEWAY-MANAGEMENT.md"
    cp docs/native-tcp.md "$STAGE/NATIVE-TCP.md"
    cp docs/workspace-directory.md "$STAGE/WORKSPACE-DIRECTORY.md"
    cp docs/file-access.md "$STAGE/FILE-ACCESS.md"
    cp docs/conversation-recovery.md "$STAGE/CONVERSATION-RECOVERY.md"
    cp "$templates"/*.sh "$templates"/*.service "$templates/relay.env.example" "$STAGE/"
    if [ "$component" = relay ]; then
        cp "$templates/init-config.py" "$STAGE/"
        cp docs/primary-session-binding.md "$STAGE/PRIMARY-SESSION-BINDING.md"
        # Copy the committed web bundle verbatim; native Android does not use it.
        cp -R web "$STAGE/web"
    fi
    python3 - "$STAGE" "$binary" "$VERSION" "$REVISION" "$ARCH" <<'PY'
import hashlib,json,pathlib,subprocess,sys
stage,binary,version,revision,arch=sys.argv[1:]
path=pathlib.Path(stage)
info={"component":binary,"version":version,"revision":revision,"target":f"linux/{arch}","cgo_enabled":False,"go_version":subprocess.check_output(["go","version"],text=True).strip(),"binary_sha256":hashlib.sha256((path/binary).read_bytes()).hexdigest(),"format":"native-tcp-manual-install-v1","upstream_plugin_auto_update":False}
if binary=="herdr-mobile-relay":
    info["web_source_version"]=json.loads((path/"web/version.json").read_text())["version"]
(path/"BUILD-INFO.json").write_text(json.dumps(info,indent=2)+"\n")
# Keep the two installation guides navigable after flattening them into a bundle.
for name in ("DEPLOYMENT.md", "GATEWAY-MANAGEMENT.md"):
    guide = path / name
    text = guide.read_text()
    text = text.replace("](gateway-management.md", "](GATEWAY-MANAGEMENT.md")
    text = text.replace("](native-deployment.md", "](DEPLOYMENT.md")
    text = text.replace("](../README.md", "](https://github.com/cxnaive/herdr_link/blob/feat/android-file-companion/README.md")
    guide.write_text(text)

PY
    tar --sort=name --mtime="@$EPOCH" --owner=0 --group=0 --numeric-owner -C "$WORK" -czf "$OUTPUT/$NAME.tar.gz" "$NAME"
done
(cd "$OUTPUT" && sha256sum ./*.tar.gz | sed 's@  ./@  @' > SHA256SUMS.txt)
printf 'Native Linux %s archives: %s\nSource revision: %s\n' "$ARCH" "$OUTPUT" "$REVISION"
