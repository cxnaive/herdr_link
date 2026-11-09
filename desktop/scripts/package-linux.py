#!/usr/bin/env python3
"""Verify and collect the MyGo Linux packages; never build, upload or install."""
import argparse
import hashlib
import io
import json
import os
import posixpath
from pathlib import Path, PurePosixPath
import shutil
import subprocess
import tarfile

ROOT = Path(__file__).resolve().parents[1]


def require(ok, message):
    if not ok:
        raise ValueError(message)


def digest(path):
    with path.open("rb") as f:
        return hashlib.file_digest(f, "sha256").hexdigest()


def elf_amd64(path):
    with path.open("rb") as f:
        header = f.read(64)
    require(len(header) == 64 and header[:6] == b"\x7fELF\x02\x01" and
            int.from_bytes(header[18:20], "little") == 62, f"Not a Linux x86_64 ELF: {path.name}")


def member_digest(bundle, name, executable=False):
    members = [m for m in bundle.getmembers() if str(PurePosixPath(m.name)) == name]
    require(len(members) == 1 and members[0].isfile(), f"Missing or unsafe bundled file: {name}")
    require(not executable or members[0].mode & 0o111, f"Bundled executable lost its execute permission: {name}")
    with bundle.extractfile(members[0]) as f:
        return hashlib.file_digest(f, "sha256").hexdigest()


def verify(source, config):
    slug, version = config["slug"], config["version"]
    executable, library = source / slug, source / "libghostty-vt.so"
    for path in (executable, library):
        require(path.is_file() and not path.is_symlink(), f"Missing built file: {path.name}")
        elf_amd64(path)
    require(os.access(executable, os.X_OK), "Executable lost its execute permission")
    native = json.loads((ROOT / "third_party/mygo-terminal/mygo-plugin.json").read_text())
    pin = next(lib["files"]["linux-amd64"] for lib in native["libraries"] if lib["name"] == "libghostty-vt")
    require(digest(library) == pin["sha256"], "Terminal library differs from the pinned Linux build")
    archive = source / f"{slug}-{version}-linux-amd64.tar.gz"
    deb = source / f"{slug}_{version}_amd64.deb"
    with tarfile.open(archive) as bundle:
        for m in bundle.getmembers():
            p = PurePosixPath(m.name)
            require(not p.is_absolute() and ".." not in p.parts and m.isfile(), "Unsafe portable archive entry")
        for path in (executable, library):
            require(member_digest(bundle, path.name, path == executable) == digest(path), "Portable archive contains stale binaries")
        require(member_digest(bundle, f"{slug}.desktop") == digest(source / f"{slug}.desktop"), "Desktop entry mismatch")
    for field, expected in (("Package", slug), ("Version", version), ("Architecture", "amd64")):
        value = subprocess.check_output(["dpkg-deb", "--field", str(deb), field], text=True).strip()
        require(value == expected, f"Wrong Debian {field}: {value}")
    depends = subprocess.check_output(["dpkg-deb", "--field", str(deb), "Depends"], text=True)
    for dependency in ("libgtk-3-0", "libwebkit2gtk-4.1-0", "libsecret-1-0"):
        require(dependency in depends, f"Missing Debian dependency: {dependency}")
    data = subprocess.check_output(["dpkg-deb", "--fsys-tarfile", str(deb)])
    with tarfile.open(fileobj=io.BytesIO(data)) as bundle:
        for path in (executable, library):
            require(member_digest(bundle, f"opt/{slug}/{path.name}", path == executable) == digest(path), "Debian package contains stale binaries")
        links = [m for m in bundle.getmembers() if str(PurePosixPath(m.name)) == f"usr/bin/{slug}"]
        require(len(links) == 1 and links[0].issym() and posixpath.normpath(posixpath.join("/usr/bin", links[0].linkname)) == f"/opt/{slug}/{slug}", "Missing installed command")
    installer = source / "install.sh"
    require(installer.is_file() and not installer.is_symlink(), "Missing user-local installer")
    return [archive, deb, installer], {p.name: digest(p) for p in (executable, library)}


def package(source, output):
    config = json.loads((ROOT / "mygo.json").read_text())
    files, binaries = verify(source, config)
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    dirty = bool(subprocess.check_output(["git", "status", "--porcelain", "--untracked-files=normal", "--", "."], cwd=ROOT))
    # Local previews can be tested without committing; CI assets must be source-bound.
    require(os.environ.get("GITHUB_ACTIONS") != "true" or not dirty, "CI source changed during the build")
    output.mkdir(parents=True, exist_ok=True)
    records = []
    for path in files:
        shutil.copy2(path, output / path.name)
        records.append({"file": path.name, "bytes": path.stat().st_size, "sha256": digest(path)})
    info = {"schema": 1, "version": config["version"], "platform": "linux/amd64", "revision": revision,
            "source_dirty": dirty, "run_id": os.environ.get("GITHUB_RUN_ID"), "event": os.environ.get("GITHUB_EVENT_NAME"),
            "signed": False, "binaries": binaries, "files": records}
    metadata = output / "BUILD-INFO.json"
    metadata.write_text(json.dumps(info, indent=2) + "\n")
    sums = [f"{record['sha256']}  {record['file']}" for record in records]
    sums.append(f"{digest(metadata)}  BUILD-INFO.json")
    (output / "SHA256SUMS.txt").write_text("\n".join(sums) + "\n")
    print(json.dumps(info, indent=2))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, default=ROOT / "build/linux-amd64")
    parser.add_argument("--output", type=Path, default=ROOT / "build/artifacts-linux-amd64")
    args = parser.parse_args()
    package(args.source.resolve(), args.output.resolve())
