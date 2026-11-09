#!/bin/sh
# Run from an extracted native gateway bundle, as root. Preserves an existing env.
set -eu
cd "$(dirname "$0")"
[ "$(id -u)" = 0 ] || { echo 'Run as root' >&2; exit 1; }
[ -f herdr-gateway ] && [ -f herdr-tcp-gateway.service ]
# Derive the install directory from the verified bundle, never a stale template version.
version=$(python3 - <<'PYVERSION'
import hashlib, json, pathlib, re
info = json.loads(pathlib.Path("BUILD-INFO.json").read_text())
version = info["version"]
if (info.get("component") != "herdr-gateway" or info.get("target") != "linux/amd64"
        or not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+(?:[+.-][A-Za-z0-9.-]+)?", version)
        or hashlib.sha256(pathlib.Path("herdr-gateway").read_bytes()).hexdigest() != info.get("binary_sha256")):
    raise SystemExit("Invalid Gateway bundle metadata or checksum")
print(version.replace("+", "-"))
PYVERSION
)
release="/opt/herdr-tcp-gateway/releases/$version"
if [ -e "$release/herdr-gateway" ] && ! cmp -s herdr-gateway "$release/herdr-gateway"; then
  echo 'This version directory contains a different binary; use a new version directory.' >&2
  exit 1
fi
if [ ! -f /etc/herdr-tcp-gateway/relay.env ]; then
  : "${HERDR_GATEWAY_ALLOWED_RELAY_IDS:?Set your computer relay ID before first install}"
  printf '%s\n' "$HERDR_GATEWAY_ALLOWED_RELAY_IDS" | grep -Eq '^[A-Za-z0-9_-]{22}(,[A-Za-z0-9_-]{22})*$' || { echo 'Invalid relay allow-list' >&2; exit 1; }
fi
id herdr-tcp >/dev/null 2>&1 || useradd --system --home-dir /var/lib/herdr-tcp-gateway --no-create-home --shell /usr/sbin/nologin herdr-tcp
install -d -m 0755 "$release"
install -d -m 0700 /etc/herdr-tcp-gateway
install -d -m 0700 -o herdr-tcp -g herdr-tcp /var/lib/herdr-tcp-gateway
if [ ! -f /etc/herdr-tcp-gateway/relay.env ]; then
  umask 077
  token=$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')
  sed -e "s/REPLACE_WITH_RANDOM_64_HEX_CHARACTERS/$token/" \
      -e "s/REPLACE_WITH_COMPUTER_RELAY_ID/$HERDR_GATEWAY_ALLOWED_RELAY_IDS/" \
      relay.env.example > /etc/herdr-tcp-gateway/relay.env
fi
install -m 0755 herdr-gateway "$release"/herdr-gateway.new
mv -f "$release"/herdr-gateway.new "$release"/herdr-gateway
install -m 0644 herdr-tcp-gateway.service /etc/systemd/system/herdr-tcp-gateway.service
ln -sfn "releases/$version" /opt/herdr-tcp-gateway/current.new
mv -Tf /opt/herdr-tcp-gateway/current.new /opt/herdr-tcp-gateway/current
systemctl daemon-reload
systemctl enable herdr-tcp-gateway.service
systemctl restart herdr-tcp-gateway.service
printf '%s\n' 'Gateway installed. Allow inbound TCP 8377 in the cloud security group if necessary.' \
  'Verify computer access in the management console, or configure the legacy registration secret securely on each Relay; never on a client.'
