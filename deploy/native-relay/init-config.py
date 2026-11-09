#!/usr/bin/env python3
"""Create a new private computer config; print only its public routing ID."""
import argparse
import base64
import hashlib
import hmac
import os
from pathlib import Path
import secrets
import shlex
import shutil
import stat
from urllib.parse import urlsplit


def relay_id(key):
    # Same HKDF-SHA256 salt/info and 16-byte output as internal/gatewaywire.
    prk = hmac.new(b"herdr-gateway-v1", key.encode("ascii"), hashlib.sha256).digest()
    raw = hmac.new(prk, b"herdr-gw-id\x01", hashlib.sha256).digest()[:16]
    return base64.urlsafe_b64encode(raw).decode("ascii").rstrip("=")


def main():
    config_home = Path(os.environ.get("XDG_CONFIG_HOME", str(Path.home() / ".config")))
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--gateway", required=True, help="tcp://PUBLIC_IP:8377")
    parser.add_argument("--config", type=Path, default=config_home / "herdr-mobile-relay/relay.env")
    parser.add_argument("--herdr-bin", default=shutil.which("herdr"))
    parser.add_argument("--socket", default=str(config_home / "herdr/herdr.sock"))
    args = parser.parse_args()
    try:
        url = urlsplit(args.gateway)
        valid = (url.scheme == "tcp" and url.hostname and url.port and not url.username
                 and not url.password and url.path in ("", "/") and not url.query and not url.fragment)
    except ValueError:
        valid = False
    if not valid:
        parser.error("gateway must be tcp://HOST:PORT without credentials, path, query or fragment")
    if not args.herdr_bin or not Path(args.herdr_bin).is_absolute() or not os.access(args.herdr_bin, os.X_OK):
        parser.error("install Herdr first, or supply --herdr-bin /absolute/path/to/herdr")
    if not Path(args.socket).is_absolute():
        parser.error("--socket must be an absolute path")
    path = args.config.expanduser().absolute()
    parent = path.parent
    parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    info = parent.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077:
        parser.error("config directory must be an owned private directory (0700); do not use a symlink")
    key = secrets.token_urlsafe(24)  # Exactly 32 printable ASCII bytes.
    values = {
        "HERDR_RELAY_TOKEN": key,
        "HERDR_GATEWAY_URL": args.gateway,
        "HERDR_GATEWAY_REGISTRATION_TOKEN": "REPLACE_WITH_GATEWAY_REGISTRATION_SECRET",
        "HERDR_RELAY_HOST": "127.0.0.1", "HERDR_RELAY_PORT": "8375", "HERDR_RELAY_PLUGIN_PORT": "8376",
        "HERDR_TRANSPORT_FORCE_RELAY": "1", "HERDR_REACHABILITY_PORT_MAPPING": "0",
        "HERDR_BIN": args.herdr_bin, "HERDR_SOCKET_PATH": args.socket,
    }
    try:
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    except FileExistsError:
        parser.error("config already exists; preserve its key/device store and follow the upgrade guide")
    with os.fdopen(fd, "w") as output:
        output.write("# Private shell config. Complete gateway-join, or set the legacy registration secret before starting.\n")
        for name, value in values.items():
            output.write(f"{name}={shlex.quote(value)}\n")
    print(f"Created private config: {path}")
    print(f"Public relay ID: {relay_id(key)}")
    print("For a managed Gateway, run the installed run.sh gateway-join with its access code.")
    print("For a legacy Gateway, allow this Relay ID and fill its registration secret in this config.")
    print("The relay master key was NOT printed; never send it to the Gateway or phone.")


if __name__ == "__main__":
    main()
