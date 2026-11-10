#!/usr/bin/env python3
"""Assemble and publish one Herdr Link release from a single trusted CI run."""
import argparse
import importlib.util
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
import tarfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]
_spec = importlib.util.spec_from_file_location("release_validation", ROOT / "scripts/publish-components.py")
V = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(V)
require, digest, api, write_json = V.require, V.digest, V.api, V.write_json
ARTIFACT = "herdr-link-release"


def project_version(manifest):
    project = manifest["project"]
    version = project["version"]
    require(project["name"] == "Herdr Link" and re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", version), "Invalid project identity/version")
    require(all(manifest[k]["version"] == version for k in ("relay", "native_gateway", "android", "desktop")), "All components must use the project version")
    require(type(manifest['android']['version_code']) is int and manifest['android']['version_code'] > 34, "Android versionCode must preserve the existing upgrade path")
    require(re.fullmatch(r"[0-9a-f]{64}", manifest['android']['signing_certificate_sha256']), "Missing Android certificate")
    return version


def assets(version):
    prefix = f"herdr-link-"
    return {
        **{f"android-{abi}": f"{prefix}android-{version}-{abi}.apk" for abi in V.ABIS},
        "relay": f"{prefix}relay-{version}-linux-amd64.tar.gz",
        "relay-windows": f"{prefix}relay-{version}-windows-amd64.zip",
        "gateway": f"{prefix}gateway-{version}-linux-amd64.tar.gz",
        "windows": f"{prefix}desktop-{version}-windows-amd64.zip",
        "macos": f"{prefix}desktop-{version}-macos-universal.dmg",
        "linux-tar": f"{prefix}desktop-{version}-linux-amd64.tar.gz",
        "linux-deb": f"{prefix}desktop-{version}-linux-amd64.deb",
        "linux-install": f"{prefix}desktop-{version}-linux-amd64-install.sh",
        "relay-arm64": f"{prefix}relay-{version}-linux-arm64.tar.gz",
        "gateway-arm64": f"{prefix}gateway-{version}-linux-arm64.tar.gz",
        "linux-arm64-tar": f"{prefix}desktop-{version}-linux-arm64.tar.gz",
        "linux-arm64-deb": f"{prefix}desktop-{version}-linux-arm64.deb",
        "linux-arm64-install": f"{prefix}desktop-{version}-linux-arm64-install.sh",
    }


def file_names(version):
    return sorted([*assets(version).values(), "INSTALL.md", "BUILD-INFO.json", "SHA256SUMS.txt"])


def one_directory(root, pattern):
    matches = [p for p in root.glob(pattern) if p.is_dir() and not p.is_symlink()]
    require(len(matches) == 1, f"Missing or ambiguous producer: {pattern}")
    return matches[0]


def provenance(info, version, sha, run_id=None):
    require(info["version"] == version and info["revision"] == sha, "Mixed source or component versions")
    require(not info.get("source_dirty", False), "Dirty source is not releasable")
    if run_id is not None:
        require(str(info["run_id"]) == str(run_id), "Mixed producer run IDs")


def native_info(path, component, version, sha, arch="amd64"):
    with tarfile.open(path) as bundle:
        members = bundle.getmembers()
        require(len(members) < 10000 and sum(m.size for m in members) < 256 * 1024 * 1024, "Oversized native package")
        require(len({m.name for m in members}) == len(members), "Duplicate archive members")
        for m in members:
            p = PurePosixPath(m.name)
            require(not p.is_absolute() and '..' not in p.parts and (m.isfile() or m.isdir()), "Unsafe native archive")
        records = [m for m in members if PurePosixPath(m.name).name == 'BUILD-INFO.json']
        require(len(records) == 1, "Ambiguous native metadata")
        info = json.load(bundle.extractfile(records[0]))
        provenance(info, version, sha)
        require(info['component'] == component and info['target'] == f'linux/{arch}' and info['cgo_enabled'] is False, "Wrong native target")
        binary = str(PurePosixPath(records[0].name).parent / component)
        with bundle.extractfile(binary) as stream:
            header = stream.read(64)
            require(len(header) == 64 and header[:6] == b'\x7fELF\x02\x01' and
                    int.from_bytes(header[18:20], 'little') == {'amd64': 62, 'arm64': 183}[arch], "Wrong native ELF architecture")
            stream.seek(0)
            require(V.hashlib.file_digest(stream, 'sha256').hexdigest() == info['binary_sha256'], "Native binary mismatch")
        return info


def windows_info(path, version, sha, run_id):
    with zipfile.ZipFile(path) as bundle:
        names = {'Herdr Desktop.exe', 'ghostty-vt.dll', 'BUILD-INFO.json', 'SHA256SUMS.txt'}
        require(set(bundle.namelist()) == names and len(bundle.namelist()) == 4, "Unexpected Windows ZIP")
        require(sum(i.file_size for i in bundle.infolist()) < 512 * 1024 * 1024, "Oversized Windows ZIP")
        info = json.loads(bundle.read('BUILD-INFO.json'))
        provenance(info, version, sha, run_id)
        require(info['platform'] == 'windows/amd64' and info['signed'] is False, "Wrong desktop target")
        require({r['file'] for r in info['files']} == {'Herdr Desktop.exe', 'ghostty-vt.dll'} and len(info['files']) == 2, "Missing Windows binaries")
        for record in info['files']:
            data = bundle.read(record['file'])
            require(len(data) == record['bytes'] and V.hashlib.sha256(data).hexdigest() == record['sha256'], "Windows binary mismatch")
        return info


def windows_relay_info(path, version, sha, run_id):
    prefix = f'herdr-link-relay-{version}-windows-amd64/'
    with zipfile.ZipFile(path) as bundle:
        members = bundle.infolist()
        require(len(members) < 10000 and sum(m.file_size for m in members) < 512 * 1024 * 1024, "Oversized Windows Relay ZIP")
        names = set()
        files = set()
        for member in members:
            name = member.filename
            p = PurePosixPath(name)
            require(name.startswith(prefix) and not p.is_absolute() and '..' not in p.parts and
                    str(p) == name.rstrip('/') and not any(c in name for c in '\\:*?"<>|') and
                    all(part.rstrip(' .') == part for part in p.parts), "Unsafe Windows Relay archive path")
            require(name.rstrip('/').casefold() not in names, "Duplicate Windows Relay archive member")
            names.add(name.rstrip('/').casefold())
            mode = (member.external_attr >> 16) & 0o170000
            require(mode in (0, 0o040000 if member.is_dir() else 0o100000), "Unsafe Windows Relay archive member")
            if not member.is_dir(): files.add(name[len(prefix):])
        required = {'herdr-mobile-relay.exe', 'install.ps1', 'run.ps1', 'LICENSE', 'VERSION.json',
                    'README.md', 'THIRD-PARTY-go-winio.txt', 'BUILD-INFO.json',
                    'web/index.html', 'web/release.json', 'web/version.json'}
        require(required <= files, "Incomplete Windows Relay package")
        info = json.loads(bundle.read(prefix + 'BUILD-INFO.json'))
        provenance(info, version, sha, run_id)
        require(info['component'] == 'herdr-mobile-relay' and info['target'] == 'windows/amd64' and
                info['format'] == 'windows-relay-v1', "Wrong Windows Relay target")
        require(info['source_dirty'] is False and info['signed'] is False and
                info['upstream_plugin_auto_update'] is False and info['windows_service'] is False, "Wrong Windows Relay build policy")
        records = info['files']
        require({r['file'] for r in records} == files - {'BUILD-INFO.json'} and len(records) == len(files) - 1,
                "Incomplete Windows Relay file manifest")
        for record in records:
            data = bundle.read(prefix + record['file'])
            require(len(data) == record['bytes'] and V.hashlib.sha256(data).hexdigest() == record['sha256'], "Windows Relay file mismatch")
        binary = bundle.read(prefix + 'herdr-mobile-relay.exe')
        require(len(binary) >= 64 and binary[:2] == b'MZ', "Invalid Windows Relay PE")
        offset = int.from_bytes(binary[60:64], 'little')
        require(offset >= 64 and offset + 6 <= len(binary) and binary[offset:offset + 6] == b'PE\0\0\x64\x86', "Wrong Windows Relay PE architecture")
        require(V.hashlib.sha256(binary).hexdigest() == info['binary_sha256'], "Windows Relay binary mismatch")
        require(project_version(json.loads(bundle.read(prefix + 'VERSION.json'))) == version, "Windows Relay manifest version mismatch")
        return info


def validate_macos(path, info, version, sha, run_id):
    provenance(info, version, sha, run_id)
    require(info['platform'] == 'darwin/universal' and info['architectures'] == ['arm64', 'x86_64'], "Wrong macOS target")
    require(info['signed'] is False and info['signing'] == 'ad-hoc' and info['notarized'] is False, "Wrong macOS signing declaration")
    require(info['source_dirty'] is False and info['minimum_macos'] == '12.0', "Wrong macOS build policy")
    require(len(info['files']) == 1 and info['files'][0]['file'] == f'herdr-desktop-{version}.dmg', "Unexpected macOS files")
    record = info['files'][0]
    require(path.stat().st_size == record['bytes'] and digest(path) == record['sha256'], "macOS provenance mismatch")
    with path.open('rb') as stream:
        require(path.stat().st_size >= 512, "Truncated DMG")
        stream.seek(-512, 2)
        require(stream.read(4) == b'koly', "Not a UDIF disk image")
    app = info['app_files']
    for name in ('Contents/Info.plist', 'Contents/MacOS/Herdr Desktop', 'Contents/Resources/libghostty-vt.dylib'):
        require(name in app, "Missing macOS app metadata")
    for name, record in app.items():
        member = PurePosixPath(name)
        require(not member.is_absolute() and '..' not in member.parts and str(member) == name, "Unsafe macOS app path")
        require(type(record['size']) is int and record['size'] > 0 and re.fullmatch(r'[0-9a-f]{64}', record['sha256']) and type(record['executable']) is bool, "Invalid macOS app record")
    require(app['Contents/MacOS/Herdr Desktop']['executable'], "Mac executable permission missing")
    return info


def macos_info(directory, version, sha, run_id):
    name = f'herdr-desktop-{version}.dmg'
    V.verify_checksums(directory, [name, 'BUILD-INFO.json', 'SHA256SUMS.txt'])
    info = json.loads((directory / 'BUILD-INFO.json').read_text())
    return validate_macos(directory / name, info, version, sha, run_id)


def assemble(inputs, output):
    manifest = json.loads((ROOT / 'VERSION.json').read_text())
    version = project_version(manifest)
    sha = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    require(sha == os.environ['GITHUB_SHA'], "Checkout differs from build SHA")
    run_id = int(os.environ['GITHUB_RUN_ID'])
    require(json.loads((ROOT / 'desktop/mygo.json').read_text())['version'] == version, "Desktop version mismatch")
    require(not output.exists(), "Refusing to replace release output")
    sources = {}
    android = one_directory(inputs, 'android-apks-signed')
    android_names = V.specifications(manifest, ['android'])['android']['names']
    V.verify_checksums(android, android_names)
    ai = json.loads((android / 'BUILD-INFO.json').read_text())
    provenance(ai, version, sha)
    require(ai['version_code'] == manifest['android']['version_code'], "Android upgrade code mismatch")
    require(len(ai['artifacts']) == 6, "Incomplete signed APK producer")
    for abi in V.ABIS:
        name = f'Herdr-Pocket-{version}-release-{abi}.apk'
        records = [r for r in ai['artifacts'] if r['file'] == name]
        require(len(records) == 1, "Missing release APK")
        r = records[0]
        require(r['build_type'] == 'release' and r['abi'] == abi and r['signing'] == 'existing-installation' and r['certificate_sha256'] == manifest['android']['signing_certificate_sha256'], "Wrong APK identity/signing")
        require(digest(android / name) == r['sha256'], "APK digest mismatch")
        sources['android-' + abi] = android / name
    native_records, linux_records = {}, {}
    for arch in ('amd64', 'arm64'):
        native = one_directory(inputs, f'native-bundles-linux_{arch}')
        native_names = {f'{binary}_{version}_linux_{arch}.tar.gz' for binary in ('herdr-mobile-relay', 'herdr-gateway')}
        V.verify_checksums(native, native_names | {'SHA256SUMS.txt'})
        for component, binary in [('relay', 'herdr-mobile-relay'), ('gateway', 'herdr-gateway')]:
            key = component if arch == 'amd64' else component + '-' + arch
            sources[key] = native / f'{binary}_{version}_linux_{arch}.tar.gz'
            native_records[key] = native_info(sources[key], binary, version, sha, arch)
        linux = one_directory(inputs, f'herdr-desktop-linux-{arch}-*')
        li = json.loads((linux / 'BUILD-INFO.json').read_text())
        provenance(li, version, sha, run_id)
        require(li['platform'] == f'linux/{arch}' and li['signed'] is False, "Wrong Linux platform")
        expected_linux = {f'herdr-desktop-{version}-linux-{arch}.tar.gz', f'herdr-desktop_{version}_{arch}.deb', 'install.sh'}
        require({r['file'] for r in li['files']} == expected_linux and len(li['files']) == 3, "Unexpected Linux files")
        V.verify_checksums(linux, expected_linux | {'SHA256SUMS.txt', 'BUILD-INFO.json'})
        for record in li['files']:
            require(digest(linux / record['file']) == record['sha256'], "Linux provenance mismatch")
        prefix = 'linux' if arch == 'amd64' else 'linux-' + arch
        sources.update({prefix + '-tar': linux / f'herdr-desktop-{version}-linux-{arch}.tar.gz',
                        prefix + '-deb': linux / f'herdr-desktop_{version}_{arch}.deb', prefix + '-install': linux / 'install.sh'})
        linux_records[arch] = li
    windows = one_directory(inputs, 'herdr-desktop-windows-amd64-*')
    sources['windows'] = windows / f'Herdr-Desktop-{version}-windows-amd64.zip'
    V.verify_checksums(windows, [sources['windows'].name, 'SHA256SUMS.txt'])
    wi = windows_info(sources['windows'], version, sha, run_id)
    windows_relay = one_directory(inputs, 'herdr-relay-windows-amd64-*')
    sources['relay-windows'] = windows_relay / assets(version)['relay-windows']
    V.verify_checksums(windows_relay, [sources['relay-windows'].name, 'SHA256SUMS.txt'])
    wri = windows_relay_info(sources['relay-windows'], version, sha, run_id)
    macos = one_directory(inputs, 'herdr-desktop-macos-universal-*')
    mi = macos_info(macos, version, sha, run_id)
    sources['macos'] = macos / f'herdr-desktop-{version}.dmg'
    output.mkdir(parents=True)
    records = {}
    for key, name in assets(version).items():
        shutil.copyfile(sources[key], output / name)
        records[name] = {'component': key, 'original_file': sources[key].name, 'size': (output / name).stat().st_size, 'sha256': digest(output / name)}
    install = (ROOT / 'docs/release-install.md').read_text().replace('@VERSION@', version)
    (output / 'INSTALL.md').write_text(install)
    info = {'schema': 2, 'project': manifest['project'], 'version': version, 'revision': sha, 'build_run_id': run_id, 'artifacts': records,
            'producers': {'android': ai, 'native': native_records, 'linux': linux_records, 'windows': wi, 'macos': mi, 'relay-windows': wri}}
    write_json(output / 'BUILD-INFO.json', info)
    (output / 'SHA256SUMS.txt').write_text(''.join(f'{digest(p)}  {p.name}\n' for p in sorted(output.iterdir())))
    verify_bundle(output, manifest, sha, run_id)
    print(f'Assembled {len(file_names(version))} release files from CI run {run_id}; binaries were not rebuilt or re-signed.')


def verify_bundle(directory, manifest, sha, run_id, inspect_apks=False):
    version = project_version(manifest)
    files = V.verify_checksums(directory, file_names(version))
    info = json.loads((directory / 'BUILD-INFO.json').read_text())
    require(info['schema'] == 2 and info['project'] == manifest['project'] and info['revision'] == sha and info['build_run_id'] == int(run_id), "Wrong release bundle identity")
    require(set(info['artifacts']) == set(assets(version).values()), "Wrong project file set")
    for name, r in info['artifacts'].items():
        require(files[name] == {'size': r['size'], 'digest': 'sha256:' + r['sha256']}, "Project metadata mismatch")
    validate_macos(directory / assets(version)['macos'], info['producers']['macos'], version, sha, run_id)
    wri = windows_relay_info(directory / assets(version)['relay-windows'], version, sha, run_id)
    require(wri == info['producers']['relay-windows'], "Windows Relay producer metadata mismatch")
    if inspect_apks:
        tools = Path(os.environ['ANDROID_HOME']) / 'build-tools/35.0.0'
        for abi in V.ABIS:
            apk = directory / assets(version)['android-' + abi]
            report = V.command([str(tools / 'apksigner'), 'verify', '--print-certs', str(apk)], text=True)
            require(re.findall(r'^Signer #\d+ certificate SHA-256 digest: ([0-9a-f]+)$', report, re.M) == [manifest['android']['signing_certificate_sha256']], "APK signing certificate mismatch")
            badging = V.command([str(tools / 'aapt'), 'dump', 'badging', str(apk)], text=True)
            require(f"versionName='{version}'" in badging and f"versionCode='{manifest['android']['version_code']}'" in badging and "name='dev.herdr.companion'" in badging, "APK package or upgrade identity mismatch")
            with zipfile.ZipFile(apk) as z:
                require({n.split('/')[1] for n in z.namelist() if n.startswith('lib/') and n.endswith('.so')} == {abi}, "APK ABI mismatch")
    return files


def current_manifest(sha):
    source = V.source_manifest(sha)
    current = json.loads((ROOT / 'VERSION.json').read_text())
    require(project_version(source) == project_version(current), "Project version has advanced; select the matching CI run")
    require(source['android'] == current['android'], "Android identity advanced")
    return source


def release_spec(manifest):
    version = project_version(manifest)
    return {'tag': 'v' + version, 'title': 'Herdr Link v' + version, 'version': version, 'names': file_names(version)}


def prepare(run_id, output):
    require(re.fullmatch(r'[1-9][0-9]*', run_id or ''), "Invalid run ID")
    sha = V.validate_run(api(f'actions/runs/{run_id}'), run_id)
    manifest = current_manifest(sha)
    records = api(f'actions/runs/{run_id}/artifacts?per_page=100')['artifacts']
    matches = [r for r in records if r['name'] == ARTIFACT and not r['expired']]
    require(len(matches) == 1 and re.fullmatch(r'sha256:[0-9a-f]{64}', matches[0].get('digest', '')), "Missing project release artifact or digest")
    require(matches[0].get('workflow_run', {}).get('head_sha') == sha, "Artifact source mismatch")
    require(not output.exists(), "Refusing to replace promotion directory")
    output.mkdir(parents=True)
    archive = output / 'transfer.zip'
    with archive.open('wb') as stream:
        subprocess.run(['gh', 'api', f"repos/{V.REPOSITORY}/actions/artifacts/{matches[0]['id']}/zip"], stdout=stream, check=True)
    require('sha256:' + digest(archive) == matches[0]['digest'], "Actions artifact digest mismatch")
    V.extract_zip(archive, output / 'files', file_names(project_version(manifest)))
    files = verify_bundle(output / 'files', manifest, sha, int(run_id), inspect_apks=True)
    archive.unlink()
    spec = release_spec(manifest)
    plan = {'schema': 2, 'repository': V.REPOSITORY, 'source': sha, 'build_run_id': int(run_id), 'manifest': manifest,
            'components': {'project': {**spec, 'files': files}}, 'artifact_id': matches[0]['id'], 'archive_digest': matches[0]['digest']}
    notes = V.command(['git', 'show', f"{sha}:docs/releases/{spec['version']}.md"], cwd=ROOT, text=True)
    plan['notes'] = notes
    write_json(output / 'plan.json', plan)
    inspect(plan)
    print('Verified exact source, artifact digest, all files, APK certificate, ABI, package and upgrade version; no release mutation.')


def load_plan(directory):
    plan = json.loads((directory / 'plan.json').read_text())
    require(plan['schema'] == 2 and plan['repository'] == V.REPOSITORY, "Wrong publication plan")
    sha = V.validate_run(api(f"actions/runs/{plan['build_run_id']}"), str(plan['build_run_id']))
    require(sha == plan['source'] and current_manifest(sha) == plan['manifest'], "Plan source changed")
    require(set(plan['components']) == {'project'}, "Not a unified release")
    spec = plan['components']['project']
    require(all(spec[k] == value for k, value in release_spec(plan['manifest']).items()), "Altered release specification")
    require(verify_bundle(directory / 'files', plan['manifest'], sha, plan['build_run_id']) == spec['files'], "Prepared bytes changed")
    expected_notes = V.command(['git', 'show', f"{sha}:docs/releases/{spec['version']}.md"], cwd=ROOT, text=True)
    require(plan['notes'] == expected_notes, "Release notes differ from source")
    return plan


def inspect(plan):
    spec = plan['components']['project']
    latest = api('releases/latest', missing=True)
    if latest and latest['tag_name'] != spec['tag']:
        latest_sha = V.tag_commit(latest['tag_name'])
        old = V.source_manifest(latest_sha)
        require(plan['manifest']['android']['version_code'] > old['android']['version_code'], "Android versionCode must increase")
        require(plan['manifest']['android']['signing_certificate_sha256'] == old['android']['signing_certificate_sha256'], "Android upgrade certificate changed")
        if 'project' in old:
            require(tuple(map(int, spec['version'].split('.'))) > tuple(map(int, project_version(old).split('.'))), "Refusing project version downgrade")
    return V.inspect(plan)['project']


def promote(directory, publish):
    plan = load_plan(directory)
    existing = inspect(plan)
    if not publish:
        print('DRY RUN: all release/tag/upgrade guards passed; nothing published or deleted.')
        return
    spec = plan['components']['project']
    body = plan['notes'] + f"\n\n## 构建来源\n\n[CI run {plan['build_run_id']}](https://github.com/{V.REPOSITORY}/actions/runs/{plan['build_run_id']}) · 源码 `{plan['source']}`\n\n" + V.marker(plan)
    if existing is None:
        existing = api('releases', 'POST', {'tag_name': spec['tag'], 'target_commitish': plan['source'], 'name': spec['title'], 'body': body, 'draft': True, 'prerelease': False, 'make_latest': 'false'})
    if existing['draft']:
        uploaded = {a['name'] for a in existing['assets']}
        pending = [str(directory / 'files' / name) for name in spec['names'] if name not in uploaded]
        if pending:
            V.command(['gh', 'release', 'upload', spec['tag'], *pending, '--repo', V.REPOSITORY])
        existing = api(f"releases/{existing['id']}")
        V.check_release(existing, spec, plan['source'], V.marker(plan))
        api(f"releases/{existing['id']}", 'PATCH', {'draft': False, 'make_latest': 'true'})
    existing = api(f"releases/{existing['id']}")
    V.check_release(existing, spec, plan['source'], V.marker(plan))
    require(not existing['draft'] and V.tag_commit(spec['tag']) == plan['source'], "Publication incomplete")
    require(api('releases/latest')['id'] == existing['id'], "New release is not Latest")
    write_json(directory / 'published.json', {'schema': 2, 'source': plan['source'], 'build_run_id': plan['build_run_id'], 'release_id': existing['id'], 'tag': spec['tag'], 'assets': spec['files']})
    print('Published exact CI assets as one Herdr Link release; old releases were not deleted.')


def update_docs(directory):
    plan = load_plan(directory)
    receipt = json.loads((directory / 'published.json').read_text())
    spec = plan['components']['project']
    require(receipt['source'] == plan['source'] and receipt['build_run_id'] == plan['build_run_id'] and receipt['tag'] == spec['tag'] and receipt['assets'] == spec['files'], "Publication receipt mismatch")
    existing = inspect(plan)
    require(existing and not existing['draft'] and existing['id'] == receipt['release_id'], "Docs require a complete release")
    from project_release_docs import update
    update(ROOT, plan, receipt)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=['assemble', 'prepare', 'promote', 'docs'])
    parser.add_argument('directory', type=Path)
    parser.add_argument('output', type=Path, nargs='?')
    parser.add_argument('--build-run-id')
    parser.add_argument('--publish', action='store_true')
    args = parser.parse_args()
    if args.mode == 'assemble':
        require(os.environ.get('GITHUB_ACTIONS') == 'true' and os.environ.get('GITHUB_REPOSITORY') == V.REPOSITORY, "Release assembly runs only on CI")
        require(args.output is not None, "Output required")
        assemble(args.directory, args.output)
        return
    V.runner_guard()
    if args.mode == 'prepare': prepare(args.build_run_id, args.directory)
    elif args.mode == 'promote': promote(args.directory, args.publish)
    else: update_docs(args.directory)


if __name__ == '__main__':
    try: main()
    except (ValueError, KeyError, OSError, subprocess.CalledProcessError) as error:
        raise SystemExit(f'Herdr Link release: {error}') from None
