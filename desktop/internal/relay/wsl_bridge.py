"""Private local Relay discovery and byte transport, run by the Windows client."""
import json
import os
import pathlib
import pwd
import select
import socket
import stat
import struct
import sys


def bounded(path, size):
    with open(path, "rb") as stream:
        data = stream.read(size + 1)
    if len(data) > size:
        raise ValueError("oversized metadata")
    return data


def birth(pid):
    data = bounded(f"/proc/{pid}/stat", 8192).decode()
    value = data[data.rindex(")") + 1:].split()[19]
    if not value.isdecimal() or int(value) == 0:
        raise ValueError("invalid process")
    return value


def inspect(pid):
    base = f"/proc/{pid}"
    if os.stat(base).st_uid != os.geteuid():
        raise ValueError("different user")
    started = birth(pid)
    exe = os.readlink(base + "/exe")
    if pathlib.PurePosixPath(exe.removesuffix(" (deleted)")).name != "herdr-mobile-relay":
        raise ValueError("not Relay")
    keys = {"HOME", "XDG_CONFIG_HOME", "HERDR_RELAY_ENV", "HERDR_PLUGIN_CONFIG_DIR", "HERDR_RELAY_HOST", "HERDR_RELAY_PORT"}
    env = {}
    for entry in bounded(base + "/environ", 1024 * 1024).split(b"\0"):
        key, sep, value = entry.partition(b"=")
        if sep and key.decode(errors="replace") in keys:
            env[key.decode()] = value.decode()
    config = env.get("XDG_CONFIG_HOME") or os.path.join(env.get("HOME", ""), ".config")
    runtime = env.get("HERDR_PLUGIN_CONFIG_DIR") or os.path.join(config, "herdr-mobile-relay")
    if env.get("HERDR_RELAY_ENV"):
        runtime = os.path.dirname(env["HERDR_RELAY_ENV"])
    if not os.path.isabs(runtime):
        raise ValueError("relative runtime")
    host = env.get("HERDR_RELAY_HOST", "")
    if host in ("", "localhost", "127.0.0.1", "0.0.0.0"):
        host = "127.0.0.1"
    elif host in ("::", "::1"):
        host = "::1"
    else:
        raise ValueError("not loopback")
    port = int(env.get("HERDR_RELAY_PORT") or "8375")
    if not 1 <= port <= 65535 or started != birth(pid) or exe != os.readlink(base + "/exe"):
        raise ValueError("changed process")
    return (pid, started, os.path.join(runtime, "tcp-pair", "admin.sock"), host, port)


def private(path):
    p = pathlib.PurePosixPath(path)
    if not p.is_absolute() or str(p) != path or ".." in p.parts or p.name != "admin.sock" or p.parent.name != "tcp-pair":
        raise ValueError("unsafe socket path")
    uid = os.geteuid()
    info = os.lstat(path)
    if not stat.S_ISSOCK(info.st_mode) or info.st_mode & 0o077 or info.st_uid != uid:
        raise ValueError("unsafe socket")
    boundary = False
    for parent in reversed(p.parents):
        current = os.lstat(parent)
        if not stat.S_ISDIR(current.st_mode) or current.st_uid not in (uid, 0):
            raise ValueError("unsafe ancestor")
        if parent == p.parent and (current.st_uid != uid or current.st_mode & 0o077):
            raise ValueError("public pairing directory")
        if not boundary and current.st_mode & 0o022 and not (current.st_uid == 0 and current.st_mode & stat.S_ISVTX):
            raise ValueError("replaceable ancestor")
        if current.st_uid == uid and not current.st_mode & 0o077:
            boundary = True
    return (info.st_dev, info.st_ino)


def connect_admin(path):
    identity = private(path)
    conn = socket.socket(socket.AF_UNIX)
    try:
        conn.settimeout(3)
        conn.connect(path)
        pid, uid, _ = struct.unpack("3i", conn.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, 12))
        if uid != os.geteuid():
            raise ValueError("different peer user")
        process = inspect(pid)
        if process[2] != path or identity != private(path):
            raise ValueError("changed socket")
        return conn, process, identity
    except Exception:
        conn.close()
        raise


def discover():
    found = {}
    for name in os.listdir("/proc"):
        if not name.isdecimal():
            continue
        try:
            process = inspect(int(name))
            conn, peer, _ = connect_admin(process[2])
            conn.close()
            if peer == process:
                found[peer[2]] = {"socket": peer[2], "address": address(peer), "uid": os.geteuid(), "user": pwd.getpwuid(os.geteuid()).pw_name}
            if len(found) >= 32:
                break
        except (OSError, ValueError, IndexError, KeyError):
            continue
    return list(found.values())


def address(process):
    host = f"[{process[3]}]" if ":" in process[3] else process[3]
    return f"{host}:{process[4]}"


def emit(value):
    sys.stdout.buffer.write(json.dumps(value, ensure_ascii=True).encode() + b"\n")
    sys.stdout.buffer.flush()


def run(request):
    action = request["action"]
    if action == "discover":
        emit({"relays": discover()})
        return
    if request["uid"] != os.geteuid() or request["user"] != pwd.getpwuid(os.geteuid()).pw_name:
        raise ValueError("WSL user changed")
    path = request["socket"]
    conn, process, identity = connect_admin(path)
    with conn:
        if action == "pair":
            name = request["name"]
            if not isinstance(name, str) or len(name) > 48 or len(name.encode()) > 80 or any(c in name for c in "\r\n\0"):
                raise ValueError("invalid name")
            conn.sendall(json.dumps({"action": "create", "name": name}).encode() + b"\n")
            with conn.makefile("rb") as stream:
                data = stream.readline(16385)
            if len(data) > 16384:
                raise ValueError("oversized response")
            reply = json.loads(data)
            if reply.get("error") or not reply.get("link") or inspect(process[0]) != process or private(path) != identity:
                raise ValueError("pairing failed")
            emit({"link": reply["link"]})
            return
        if action != "connect":
            raise ValueError("unknown action")
    # This connection stays inside the selected distro; Windows localhost and
    # WSL network forwarding settings cannot redirect it to another Relay.
    with socket.create_connection((process[3], process[4]), timeout=5) as remote:
        check, after, final = connect_admin(path)
        check.close()
        if process != after or identity != final:
            raise ValueError("Relay changed during connection")
        emit({"address": address(process)})
        remote.settimeout(None)
        while True:
            ready, _, _ = select.select([remote, sys.stdin.buffer], [], [])
            for source in ready:
                data = remote.recv(65536) if source is remote else os.read(sys.stdin.fileno(), 65536)
                if not data:
                    return
                if source is remote:
                    sys.stdout.buffer.write(data)
                    sys.stdout.buffer.flush()
                else:
                    remote.sendall(data)


if __name__ == "__main__":
    try:
        # Read only the request line. Do not buffer any following tunnel bytes.
        request = bytearray()
        while len(request) <= 16384:
            char = os.read(sys.stdin.fileno(), 1)
            if char == b"\n":
                break
            if not char:
                raise ValueError("missing request")
            request.extend(char)
        if len(request) > 16384:
            raise ValueError("oversized request")
        run(json.loads(request))
    except Exception:
        # Invitations, environments and arbitrary exception text stay private.
        sys.exit(1)
