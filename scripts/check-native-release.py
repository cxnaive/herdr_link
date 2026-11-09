#!/usr/bin/env python3
"""Verify native bundles and exercise them using only temporary loopback services."""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import re
import secrets
import shlex
import socket
import stat
import struct
import subprocess
import tarfile
import tempfile
import time
from urllib.parse import parse_qs, urlsplit
from urllib.request import ProxyHandler, build_opener


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def unused_port(kind=socket.SOCK_STREAM):
    with socket.socket(socket.AF_INET, kind) as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def wait_health(port, predicate, processes):
    opener = build_opener(ProxyHandler({}))
    for _ in range(160):
        require(all(p.poll() is None for p in processes), "Temporary service exited; inspect the build locally")
        try:
            with opener.open(f"http://127.0.0.1:{port}/healthz", timeout=1) as response:
                value = json.load(response)
            if predicate(value):
                return value
        except OSError:
            pass
        time.sleep(0.1)
    raise RuntimeError("Temporary loopback service did not become healthy")


def receive(sock, size):
    value = b""
    while len(value) < size:
        part = sock.recv(size - len(value))
        require(bool(part), "Incomplete native TCP probe frame")
        value += part
    return value


def extract(archive, work, arch="amd64"):
    with tarfile.open(archive) as bundle:
        for member in bundle.getmembers():
            path = PurePosixPath(member.name)
            require(not path.is_absolute() and ".." not in path.parts, "Unsafe archive path")
            require(member.isfile() or member.isdir(), "Archive must not contain links or special files")
        bundle.extractall(work)
    directory = work / archive.name[:-7]
    info = json.loads((directory / "BUILD-INFO.json").read_text())
    require(info["target"] == f"linux/{arch}" and not info["cgo_enabled"], "Wrong build target")
    binary = directory / info["component"]
    require(hashlib.sha256(binary.read_bytes()).hexdigest() == info["binary_sha256"], "Binary digest mismatch")
    header = binary.read_bytes()[:64]
    require(len(header) == 64 and header[:6] == b"\x7fELF\x02\x01" and
            int.from_bytes(header[18:20], "little") == {"amd64": 62, "arm64": 183}[arch], "Wrong ELF architecture")
    require(os.access(binary, os.X_OK), "Binary lost its executable bit")
    return directory, info


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path)
    args = parser.parse_args()
    arch = {"x86_64": "amd64", "aarch64": "arm64"}.get(platform.machine())
    require(platform.system() == "Linux" and arch, "Run on Linux amd64 or arm64")
    archives = []
    for line in (args.directory / "SHA256SUMS.txt").read_text().splitlines():
        digest, name = line.split()
        require(Path(name).name == name and name.endswith(".tar.gz"), "Invalid checksum file name")
        path = args.directory / name
        require(hashlib.sha256(path.read_bytes()).hexdigest() == digest, "Archive checksum mismatch")
        archives.append(path)
    require(len(archives) == 2, "Expected Relay and Gateway archives")
    with tempfile.TemporaryDirectory(prefix="herdr-native-smoke-") as temporary:
        work = Path(temporary)
        bundles = [extract(p, work, arch) for p in archives]
        relay, relay_info = next(b for b in bundles if b[1]["component"] == "herdr-mobile-relay")
        gateway, gateway_info = next(b for b in bundles if b[1]["component"] == "herdr-gateway")
        require(relay_info["revision"] == gateway_info["revision"], "Mixed source revisions")
        for bundle in (relay, gateway):
            for script in bundle.glob("*.sh"):
                subprocess.run(["sh", "-n", str(script)], check=True)
        for bundle in (relay, gateway):
            data = subprocess.check_output(["readelf", "-l", str(bundle / json.loads((bundle / "BUILD-INFO.json").read_text())["component"])], text=True)
            require("INTERP" not in data, "Executable needs a dynamic loader")
        home = work / "home"; home.mkdir(mode=0o700)
        env = {"HOME": str(home), "PATH": "/usr/local/bin:/usr/bin:/bin", "LANG": "C.UTF-8",
               "XDG_CONFIG_HOME": str(home / "config"), "XDG_CACHE_HOME": str(home / "cache"), "XDG_DATA_HOME": str(home / "data")}
        fake = work / "no-real-herdr"
        fake.write_text("#!/bin/sh\nexit 1\n"); fake.chmod(0o755)
        tcp, http, relay_port = unused_port(), unused_port(), unused_port()
        while len({tcp, http, relay_port}) != 3:
            tcp, http, relay_port = unused_port(), unused_port(), unused_port()
        endpoint = f"tcp://127.0.0.1:{tcp}"
        config = home / "config/herdr-mobile-relay/relay.env"
        args_init = ["python3", str(relay / "init-config.py"), "--gateway", endpoint, "--config", str(config), "--herdr-bin", str(fake), "--socket", str(work / "no-herdr.sock")]
        initialized = subprocess.check_output(args_init, env=env, text=True)
        public_id = re.search(r"Public relay ID: ([A-Za-z0-9_-]{22})", initialized).group(1)
        original = config.read_bytes()
        require(stat.S_IMODE(config.stat().st_mode) == 0o600 and stat.S_IMODE(config.parent.stat().st_mode) == 0o700, "Unsafe config permissions")
        values = dict(line.split("=", 1) for line in config.read_text().splitlines() if line and not line.startswith("#"))
        require(len(shlex.split(values["HERDR_RELAY_TOKEN"])[0]) == 32, "Wrong master key size")
        require(values["HERDR_RELAY_TOKEN"] not in initialized, "Initializer printed a secret")
        require(subprocess.run(args_init, env=env, capture_output=True).returncode != 0 and config.read_bytes() == original, "Reinitialization overwrote config")
        env["HERDR_RELAY_ENV"] = str(config)
        require(subprocess.run([str(relay / "run.sh"), "serve"], env=env, capture_output=True).returncode != 0, "Placeholder registration secret accepted")
        for endpoint_bad in ["http://example.invalid:8377", "tcp://example.invalid", "tcp://user:secret@example.invalid:8377", "tcp://example.invalid:8377/path"]:
            bad = args_init.copy(); bad[bad.index("--gateway") + 1] = endpoint_bad
            require(subprocess.run(bad, env=env, capture_output=True).returncode != 0 and config.read_bytes() == original, "Invalid gateway config accepted")
        unsafe = home / "public-config"; unsafe.mkdir(mode=0o755)
        linked = home / "linked-config"; linked.symlink_to(config.parent, target_is_directory=True)
        for parent in (unsafe, linked):
            bad = args_init.copy(); bad[bad.index("--config") + 1] = str(parent / "new.env")
            require(subprocess.run(bad, env=env, capture_output=True).returncode != 0, "Unsafe config directory accepted")
            require(not (parent / "new.env").exists(), "Unsafe initializer wrote a config")
        secret = secrets.token_hex(32)
        text = config.read_text().replace("REPLACE_WITH_GATEWAY_REGISTRATION_SECRET", secret)
        text = text.replace("HERDR_RELAY_PORT=8375", f"HERDR_RELAY_PORT={relay_port}").replace("HERDR_RELAY_PLUGIN_PORT=8376", f"HERDR_RELAY_PLUGIN_PORT={unused_port(socket.SOCK_DGRAM)}")
        config.write_text(text)
        version = json.loads(subprocess.check_output([str(relay / "herdr-mobile-relay"), "version", "--json"], env=env))
        require(version["version"] == relay_info["version"] and version["revision"] == relay_info["revision"], "Relay build identity mismatch")
        gw_env = dict(env, HERDR_GATEWAY_TCP_ADDR=f"127.0.0.1:{tcp}", HERDR_GATEWAY_ADDR=f"127.0.0.1:{http}", HERDR_GATEWAY_STUN_ADDR="",
                      HERDR_GATEWAY_REGISTRATION_TOKEN=secret, HERDR_GATEWAY_ALLOWED_RELAY_IDS=public_id, HERDR_GATEWAY_STATE=str(work / "quota.json"))
        processes = []
        with (work / "gateway.log").open("w") as gw_log, (work / "relay.log").open("w") as relay_log:
            try:
                gateway_process = subprocess.Popen([str(gateway / "herdr-gateway")], env=gw_env, stdout=gw_log, stderr=gw_log)
                processes.append(gateway_process)
                health = wait_health(http, lambda h: h.get("ok"), processes)
                require(health["version"] == gateway_info["version"] and health["revision"] == gateway_info["revision"] and health["stun_port"] == 0, "Gateway build/transport mismatch")
                with socket.create_connection(("127.0.0.1", tcp), timeout=3) as sock:
                    sock.sendall(b"HRDRTCP1\x03")
                    kind, length = struct.unpack("!BI", receive(sock, 5))
                    require(kind == 1 and length < 1024, "Invalid native health response")
                    require(json.loads(receive(sock, length)) == {"type": "tcp_health", "proto": 1, "status": "ok"}, "Native probe failed")
                processes.append(subprocess.Popen([str(relay / "run.sh"), "serve"], env=env, stdout=relay_log, stderr=relay_log))
                health = wait_health(relay_port, lambda h: h.get("gateway", {}).get("registered"), processes)
                require(health["gateway"]["relay_id"] == public_id, "Initializer Relay ID disagrees with Go runtime")
                require(health["gateway"]["forced_relay"] and not health["gateway"]["direct"], "Native mode unexpectedly enables direct transport")
                require(health.get("bundle_hash"), "Packaged web bundle failed to load")
                wait_health(http, lambda h: h.get("relays") == 1, processes)
                result = subprocess.check_output([str(relay / "run.sh"), "tcp-pair", "Fixture phone"], env=env, text=True, timeout=10)
                link = next(line for line in result.splitlines() if line.startswith("herdr-tcp://"))
                uri = urlsplit(link); params = parse_qs(uri.fragment)
                require(uri.netloc == f"127.0.0.1:{tcp}" and params["relay_id"] == [public_id], "Pairing targets wrong Gateway/Relay")
                require(all(k in params for k in ("invite", "invite_version", "invite_expires", "setup")), "Missing single-device invitation")
                require(values["HERDR_RELAY_TOKEN"] not in link, "Master key leaked in invitation")
                require(config.read_text() == text, "Runtime rewrote private config")
                require(stat.S_IMODE((config.parent / "tcp-pair/admin.sock").stat().st_mode) == 0o600, "Unsafe pairing socket")
            finally:
                for proc in reversed(processes):
                    proc.terminate()
                    try:
                        proc.wait(timeout=15)
                    except subprocess.TimeoutExpired:
                        proc.kill(); proc.wait(timeout=5)
        print(f"PASS: checksums, static Linux {arch} binaries, private config/init rejection, isolated Gateway/TCP/Relay registration and one-device pairing")
        print("Source:", relay_info["revision"])
        print("No production service, credential, real Herdr socket or phone was used; systemd itself was not installed.")


if __name__ == "__main__":
    main()
