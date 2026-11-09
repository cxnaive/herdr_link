#!/bin/sh
# Uses the owner's private config for both serve and tcp-pair; never starts Herdr.
set -eu
HERE=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
HERDR_RELAY_ENV=${HERDR_RELAY_ENV:-${XDG_CONFIG_HOME:-"$HOME/.config"}/herdr-mobile-relay/relay.env}
if [ -f "$HERDR_RELAY_ENV" ]; then
    set -a
    . "$HERDR_RELAY_ENV"
    set +a
elif [ "${1:-serve}" != gateway-join ] && [ ! -f "$(dirname -- "$HERDR_RELAY_ENV")/gateway-access.json" ]; then
    echo "Missing private config; run this wrapper with gateway-join first." >&2
    exit 1
fi
[ "${1:-serve}" = gateway-join ] || [ -f "$(dirname -- "$HERDR_RELAY_ENV")/gateway-access.json" ] || [ "${HERDR_GATEWAY_REGISTRATION_TOKEN:-}" != REPLACE_WITH_GATEWAY_REGISTRATION_SECRET ] || {
    echo 'Fill the Gateway registration secret in relay.env before starting.' >&2; exit 1;
}
export HERDR_RELAY_ENV
export HERDR_WEB_ROOT="$HERE/web"
exec "$HERE/herdr-mobile-relay" "$@"
