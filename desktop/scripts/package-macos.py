#!/usr/bin/env python3
"""Verify the signed app and its DMG, then collect exact CI bytes without re-signing."""
import argparse
from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path
import plistlib
import shutil
import struct
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
ARCHES = {0x01000007: 'x86_64', 0x0100000c: 'arm64'}


def require(ok, message):
    if not ok:
        raise ValueError(message)


def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def command(*args):
    return subprocess.check_output([str(a) for a in args], stderr=subprocess.STDOUT, text=True)


def universal(path):
    """Check both real Mach-O slices, not just the filename or fat header labels."""
    with path.open('rb') as stream:
        magic = stream.read(4)
        require(magic in (b'\xca\xfe\xba\xbe', b'\xca\xfe\xba\xbf'), 'Expected Universal Mach-O: ' + path.name)
        require(stream.read(4) == struct.pack('>I', 2), 'Universal must contain exactly two architectures')
        size = 32 if magic[-1] == 0xbf else 20
        entries = stream.read(2 * size)
        require(len(entries) == 2 * size, 'Truncated fat header')
        found, ranges = set(), []
        for index in range(2):
            entry = entries[index * size:(index + 1) * size]
            cpu, _, offset, length = struct.unpack('>IIQQ' if size == 32 else '>IIII', entry[:24 if size == 32 else 16])
            require(cpu in ARCHES and ARCHES[cpu] not in found, 'Wrong or duplicate architecture')
            require(offset >= 8 + 2 * size and length >= 32 and offset + length <= path.stat().st_size, 'Invalid Mach-O slice bounds')
            require(all(offset + length <= start or offset >= end for start, end in ranges), 'Overlapping Mach-O slices')
            stream.seek(offset)
            header = stream.read(8)
            require(header[:4] == b'\xcf\xfa\xed\xfe' and struct.unpack('<I', header[4:])[0] == cpu, 'Fat header differs from Mach-O slice')
            found.add(ARCHES[cpu]); ranges.append((offset, offset + length))
        require(found == set(ARCHES.values()), 'Missing Mac architecture')


def app_files(app):
    require(app.is_dir() and not app.is_symlink(), 'Missing app bundle')
    records = {}
    for path in sorted(app.rglob('*')):
        require(not path.is_symlink(), 'Unexpected app symlink: ' + str(path))
        require(path.is_dir() or path.is_file(), 'Special file in app')
        if path.is_file():
            records[path.relative_to(app).as_posix()] = {
                'size': path.stat().st_size, 'sha256': digest(path),
                'executable': bool(path.stat().st_mode & 0o111),
            }
    return records


def verify_app(app, config):
    records = app_files(app)
    with (app / 'Contents/Info.plist').open('rb') as stream:
        info = plistlib.load(stream)
    for key, expected in {
        'CFBundleIdentifier': config['identifier'], 'CFBundleShortVersionString': config['version'],
        'CFBundleVersion': config['version'], 'CFBundleExecutable': config['name'],
        'LSMinimumSystemVersion': config['macos']['minimumSystemVersion'],
    }.items():
        require(info.get(key) == expected, 'Wrong app ' + key)
    executable = app / 'Contents/MacOS' / config['name']
    library = app / 'Contents/Resources/libghostty-vt.dylib'
    require(os.access(executable, os.X_OK), 'App lost its executable permission')
    for path in (executable, library):
        universal(path)
        command('codesign', '--verify', '--strict', path)
    command('codesign', '--verify', '--deep', '--strict', app)
    signature = command('codesign', '--display', '--verbose=4', app)
    require('Signature=adhoc' in signature and 'Authority=Developer ID' not in signature, 'Expected ad-hoc preview signature')
    require(records, 'Empty app bundle')
    return records


@contextmanager
def mounted(dmg):
    command('hdiutil', 'verify', dmg)
    with tempfile.TemporaryDirectory(prefix='herdr-dmg-') as work:
        mount = Path(work) / 'volume'; mount.mkdir()
        command('hdiutil', 'attach', '-readonly', '-nobrowse', '-noautoopen', '-mountpoint', mount, dmg)
        try:
            yield mount
        finally:
            command('hdiutil', 'detach', mount)


def verify_dmg(dmg, config, expected):
    require(dmg.is_file() and not dmg.is_symlink(), 'Missing DMG')
    with mounted(dmg) as volume:
        app_name = config['name'] + '.app'
        require({p.name for p in volume.iterdir() if not p.name.startswith('.')} == {app_name, 'Applications'}, 'Unexpected DMG contents')
        require((volume / 'Applications').is_symlink() and os.readlink(volume / 'Applications') == '/Applications', 'Missing Applications shortcut')
        require(verify_app(volume / app_name, config) == expected, 'DMG contains different app bytes or permissions')


def package(source, output):
    config = json.loads((ROOT / 'mygo.json').read_text())
    require(not output.exists(), 'Refusing to replace existing package output')
    records = verify_app(source / (config['name'] + '.app'), config)
    dmg = source / f"{config['slug']}-{config['version']}.dmg"
    verify_dmg(dmg, config, records)
    revision = command('git', '-C', ROOT, 'rev-parse', 'HEAD').strip()
    dirty = bool(command('git', '-C', ROOT, 'status', '--porcelain', '--untracked-files=normal', '--', '.'))
    require(os.environ.get('GITHUB_ACTIONS') != 'true' or (not dirty and revision == os.environ.get('GITHUB_SHA')), 'CI source changed during build')
    info = {
        'schema': 1, 'version': config['version'], 'revision': revision,
        'source_dirty': dirty, 'run_id': os.environ.get('GITHUB_RUN_ID'),
        'event': os.environ.get('GITHUB_EVENT_NAME'), 'platform': 'darwin/universal',
        'architectures': ['arm64', 'x86_64'], 'minimum_macos': config['macos']['minimumSystemVersion'],
        'signed': False, 'signing': 'ad-hoc', 'notarized': False,
        'app_files': records,
        'files': [{'file': dmg.name, 'bytes': dmg.stat().st_size, 'sha256': digest(dmg)}],
    }
    output.mkdir(parents=True)
    shutil.copy2(dmg, output / dmg.name)
    metadata = output / 'BUILD-INFO.json'
    metadata.write_text(json.dumps(info, indent=2) + '\n')
    (output / 'SHA256SUMS.txt').write_text(f'{digest(dmg)}  {dmg.name}\n{digest(metadata)}  BUILD-INFO.json\n')
    print(json.dumps(info, indent=2))


def verify_artifact(source, install):
    """Intel CI validates and copies the same ARM-runner DMG; it never rebuilds it."""
    config = json.loads((ROOT / 'mygo.json').read_text())
    info = json.loads((source / 'BUILD-INFO.json').read_text())
    require(info['version'] == config['version'] and info['revision'] == os.environ['GITHUB_SHA'] and str(info['run_id']) == os.environ['GITHUB_RUN_ID'], 'Mac producer source/run mismatch')
    name = f"{config['slug']}-{config['version']}.dmg"
    require(len(info['files']) == 1 and info['files'][0]['file'] == name, 'Unexpected Mac artifact files')
    dmg = source / name
    require(digest(dmg) == info['files'][0]['sha256'] and dmg.stat().st_size == info['files'][0]['bytes'], 'DMG digest mismatch')
    require(not install.exists(), 'Refusing to replace installed test app')
    verify_dmg(dmg, config, info['app_files'])
    with mounted(dmg) as volume:
        command('ditto', volume / (config['name'] + '.app'), install)
    require(verify_app(install, config) == info['app_files'], 'Installed app differs from DMG')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=Path, default=ROOT / 'build/darwin-universal')
    parser.add_argument('--output', type=Path, default=ROOT / 'build/artifacts-macos-universal')
    parser.add_argument('--verify-artifact', type=Path, metavar='INSTALL_APP')
    args = parser.parse_args()
    if args.verify_artifact:
        verify_artifact(args.source.resolve(), args.verify_artifact.resolve())
    else:
        package(args.source.resolve(), args.output.resolve())
