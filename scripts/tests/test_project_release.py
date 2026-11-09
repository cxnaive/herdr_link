"""Unified release tests use temporary fake bytes, never real credentials or GitHub writes."""
import copy
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import zipfile

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
import project_release as P
import project_release_docs as D

SHA = 'a' * 40
RUN = 123
CERT = 'b' * 64
MANIFEST = {'project': {'name': 'Herdr Link', 'version': '0.3.0'},
            'relay': {'version': '0.3.0'}, 'native_gateway': {'version': '0.3.0'},
            'desktop': {'version': '0.3.0'},
            'android': {'version': '0.3.0', 'version_code': 35, 'signing_certificate_sha256': CERT}}


def sums(directory):
    (directory / 'SHA256SUMS.txt').write_text(''.join(f'{P.digest(p)}  {p.name}\n' for p in sorted(directory.iterdir()) if p.name != 'SHA256SUMS.txt'))


def native(path, binary):
    raw = b'fake native bytes'
    info = {'version': '0.3.0', 'revision': SHA, 'component': binary, 'target': 'linux/amd64', 'cgo_enabled': False, 'binary_sha256': hashlib.sha256(raw).hexdigest()}
    with tarfile.open(path, 'w:gz') as z:
        for name, value in [('BUILD-INFO.json', json.dumps(info).encode()), (binary, raw)]:
            member = tarfile.TarInfo('package/' + name); member.size = len(value)
            z.addfile(member, io.BytesIO(value))


class ProjectReleaseTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(); self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        (self.root / 'VERSION.json').write_text(json.dumps(MANIFEST))
        (self.root / 'desktop').mkdir(); (self.root / 'desktop/mygo.json').write_text('{"version":"0.3.0"}')
        (self.root / 'docs').mkdir(); (self.root / 'docs/release-install.md').write_text('Install @VERSION@')
        self.inputs = self.root / 'inputs'; self.inputs.mkdir()
        android = self.inputs / 'android-apks-signed'; android.mkdir()
        records = []
        for kind in ['debug', 'release']:
            for abi in P.V.ABIS:
                name = f'Herdr-Pocket-0.3.0-{kind}-{abi}.apk'; (android / name).write_bytes(name.encode())
                records.append({'file': name, 'sha256': P.digest(android / name), 'build_type': kind, 'abi': abi, 'signing': 'existing-installation', 'certificate_sha256': CERT})
        (android / 'BUILD-INFO.json').write_text(json.dumps({'version': '0.3.0', 'revision': SHA, 'version_code': 35, 'artifacts': records})); sums(android)
        nd = self.inputs / 'native-bundles-linux_amd64'; nd.mkdir()
        for binary in ['herdr-mobile-relay', 'herdr-gateway']: native(nd / f'{binary}_0.3.0_linux_amd64.tar.gz', binary)
        sums(nd)
        ld = self.inputs / 'herdr-desktop-linux-amd64-5'; ld.mkdir()
        records = []
        for name in ['herdr-desktop-0.3.0-linux-amd64.tar.gz', 'herdr-desktop_0.3.0_amd64.deb', 'install.sh']:
            (ld / name).write_bytes(name.encode()); records.append({'file': name, 'sha256': P.digest(ld / name)})
        (ld / 'BUILD-INFO.json').write_text(json.dumps({'version': '0.3.0', 'revision': SHA, 'run_id': str(RUN), 'signed': False, 'platform': 'linux/amd64', 'files': records})); sums(ld)
        wd = self.inputs / 'herdr-desktop-windows-amd64-5'; wd.mkdir()
        records = [{'file': name, 'sha256': hashlib.sha256(name.encode()).hexdigest(), 'bytes': len(name)} for name in ['Herdr Desktop.exe', 'ghostty-vt.dll']]
        info = {'version': '0.3.0', 'revision': SHA, 'run_id': str(RUN), 'platform': 'windows/amd64', 'signed': False, 'files': records}
        with zipfile.ZipFile(wd / 'Herdr-Desktop-0.3.0-windows-amd64.zip', 'w') as z:
            for r in records: z.writestr(r['file'], r['file'])
            z.writestr('BUILD-INFO.json', json.dumps(info)); z.writestr('SHA256SUMS.txt', '')
        sums(wd)

    def assemble(self):
        with patch.object(P, 'ROOT', self.root), patch.dict(os.environ, {'GITHUB_SHA': SHA, 'GITHUB_RUN_ID': str(RUN)}), patch.object(P.subprocess, 'check_output', return_value=SHA):
            P.assemble(self.inputs, self.root / 'out')
        return self.root / 'out'

    def test_repository_rename_keeps_release_and_signing_guards_exact(self):
        self.assertEqual(P.V.REPOSITORY, 'cxnaive/herdr_link')
        self.assertEqual(D.REPO, P.V.REPOSITORY)
        self.assertEqual(json.loads((ROOT / 'VERSION.json').read_text())['repository'], P.V.REPOSITORY)
        for path in ('.github/workflows/build-artifacts.yml', '.github/workflows/publish-components.yml'):
            workflow = (ROOT / path).read_text()
            self.assertIn("github.repository == 'cxnaive/herdr_link'", workflow)
            self.assertNotIn('cxnaive/herder_relay_extra', workflow)
        env = {'GITHUB_ACTIONS': 'true', 'GITHUB_REPOSITORY': P.V.REPOSITORY,
               'GITHUB_REF': 'refs/heads/' + P.V.BRANCH, 'GITHUB_EVENT_NAME': 'workflow_dispatch'}
        with patch.dict(os.environ, env, clear=True):
            P.V.runner_guard()
        for repository in ('cxnaive/herder_link', 'cxnaive/herder_relay_extra', 'other/fork'):
            with patch.dict(os.environ, {**env, 'GITHUB_REPOSITORY': repository}, clear=True), self.assertRaises(ValueError):
                P.V.runner_guard()
        blocks = D.download_blocks({'version': '0.3.0', 'tag': 'v0.3.0'})
        for text in blocks.values():
            self.assertIn('https://github.com/cxnaive/herdr_link/releases/', text)
            self.assertNotIn('herder_relay_extra', text)

    def test_one_version_rejects_temporary_suffix_and_component_mismatch(self):
        self.assertEqual(P.project_version(MANIFEST), '0.3.0')
        for field in ['project', 'relay', 'android', 'desktop', 'native_gateway']:
            value = copy.deepcopy(MANIFEST); value[field]['version'] = '0.23.5+codexfix.20'
            with self.assertRaises(ValueError): P.project_version(value)

    def test_assemble_exact_bytes_release_only_and_checksums(self):
        out = self.assemble()
        self.assertEqual(set(p.name for p in out.iterdir()), set(P.file_names('0.3.0')))
        self.assertEqual(len(P.file_names('0.3.0')), 12)
        self.assertFalse(any('debug' in p.name or 'codexfix' in p.name for p in out.iterdir()))
        self.assertEqual((out / P.assets('0.3.0')['android-arm64-v8a']).read_bytes(), (self.inputs / 'android-apks-signed/Herdr-Pocket-0.3.0-release-arm64-v8a.apk').read_bytes())
        self.assertEqual((out / 'INSTALL.md').read_text(), 'Install 0.3.0')
        P.verify_bundle(out, MANIFEST, SHA, RUN)
        with self.assertRaises(ValueError): self.assemble()

    def test_rejects_mixed_source_run_and_dirty_metadata(self):
        for key, value in [('revision', 'c' * 40), ('run_id', '999'), ('source_dirty', True)]:
            p = self.inputs / 'herdr-desktop-linux-amd64-5/BUILD-INFO.json'; before = p.read_text()
            data = json.loads(before); data[key] = value; p.write_text(json.dumps(data)); sums(p.parent)
            with self.subTest(key=key), self.assertRaises(ValueError): self.assemble()
            self.assertFalse((self.root / 'out').exists())
            p.write_text(before); sums(p.parent)

    def test_rejects_substituted_apk_certificate(self):
        p = self.inputs / 'android-apks-signed/BUILD-INFO.json'; info = json.loads(p.read_text())
        for r in info['artifacts']: r['certificate_sha256'] = 'f' * 64
        p.write_text(json.dumps(info)); sums(p.parent)
        with self.assertRaises(ValueError): self.assemble()

    def test_rejects_changed_prepared_file(self):
        out = self.assemble(); (out / P.assets('0.3.0')['relay']).write_bytes(b'replaced')
        with self.assertRaises(ValueError): P.verify_bundle(out, MANIFEST, SHA, RUN)

    def test_native_archive_rejects_traversal(self):
        p = self.root / 'unsafe.tar.gz'
        with tarfile.open(p, 'w:gz') as z:
            m = tarfile.TarInfo('../escape'); m.size = 1; z.addfile(m, io.BytesIO(b'x'))
        with self.assertRaises(ValueError): P.native_info(p, 'herdr-gateway', '0.3.0', SHA)

    def test_docs_update_is_atomic_idempotent_and_preserves_gallery_location(self):
        gallery = '<!-- pocket-preview:start --><table>1x4</table><!-- pocket-preview:end -->'
        blocks = '\n'.join(f'<!-- {key}:start -->old<!-- {key}:end -->' for key in ['native-downloads', 'android-downloads', 'desktop-downloads'])
        (self.root / 'README.md').write_text('# System\n## Clients\n' + gallery + '\n' + blocks)
        (self.root / 'herdr-android').mkdir(); (self.root / 'herdr-android/README.md').write_text(blocks)
        (self.root / 'desktop/README.md').write_text(blocks)
        plan = {'manifest': MANIFEST, 'source': SHA, 'build_run_id': RUN}
        receipt = {'tag': 'v0.3.0', 'release_id': 4, 'assets': {}}
        D.update(self.root, plan, receipt); before = (self.root / 'README.md').read_text()
        self.assertIn('## Clients\n' + gallery, before)
        self.assertNotIn('codexfix', before)
        self.assertIn('herdr-link-desktop-0.3.0-windows-amd64.zip', before)
        D.update(self.root, plan, receipt); self.assertEqual(before, (self.root / 'README.md').read_text())
        version_before = (self.root / 'VERSION.json').read_bytes()
        (self.root / 'desktop/README.md').write_text('no markers')
        with self.assertRaises(ValueError): D.update(self.root, plan, receipt)
        self.assertEqual(version_before, (self.root / 'VERSION.json').read_bytes())

    def test_dry_run_never_mutates_release(self):
        with patch.object(P, 'load_plan', return_value={}), patch.object(P, 'inspect', return_value=None), patch.object(P, 'api') as api:
            P.promote(self.root, False)
            api.assert_not_called()

    def test_project_upgrade_must_preserve_certificate_and_increase_code(self):
        plan = {'manifest': MANIFEST, 'components': {'project': P.release_spec(MANIFEST)}}
        old = copy.deepcopy(MANIFEST); old.pop('project'); old['android']['version_code'] = 34
        with patch.object(P, 'api', return_value={'tag_name': 'android-v0.2.31'}), patch.object(P.V, 'tag_commit', return_value=SHA), patch.object(P.V, 'source_manifest', return_value=old), patch.object(P.V, 'inspect', return_value={'project': None}):
            self.assertIsNone(P.inspect(plan))
            old['android']['version_code'] = 35
            with self.assertRaises(ValueError): P.inspect(plan)
            old['android']['version_code'] = 34; old['android']['signing_certificate_sha256'] = 'c' * 64
            with self.assertRaises(ValueError): P.inspect(plan)

    def test_promote_publishes_draft_only_after_full_validation(self):
        spec = P.release_spec(MANIFEST); spec['files'] = {'fixture': {'size': 1, 'digest': 'sha256:' + 'd' * 64}}; spec['names'] = ['fixture']
        plan = {'manifest': MANIFEST, 'components': {'project': spec}, 'source': SHA, 'build_run_id': RUN, 'notes': '# User release notes'}
        events = []
        release = {'id': 4, 'draft': True, 'assets': [], 'tag_name': spec['tag']}
        def api(path, method='GET', data=None, **kwargs):
            events.append((method, path))
            if method == 'PATCH': release['draft'] = False
            return copy.deepcopy(release)
        with patch.object(P, 'load_plan', return_value=plan), patch.object(P, 'inspect', return_value=None), patch.object(P, 'api', side_effect=api), patch.object(P.V, 'check_release') as validate, patch.object(P.V, 'tag_commit', return_value=SHA), patch.object(P.V, 'command') as upload:
            P.promote(self.root, True)
            self.assertEqual(upload.call_args.args[0][:3], ['gh', 'release', 'upload'])
            self.assertNotIn('--clobber', upload.call_args.args[0])
            self.assertEqual(validate.call_count, 2)
            self.assertTrue((self.root / 'published.json').exists())
            self.assertFalse(any(method == 'DELETE' for method, _ in events))
            self.assertEqual(events[0], ('POST', 'releases'))

    def test_workflow_assembles_on_builder_and_never_builds_on_publisher(self):
        producer = (ROOT / '.github/workflows/build-artifacts.yml').read_text()
        self.assertIn('inputs.components == \'release\'', producer)
        self.assertIn('needs: [native-bundles, android-sign, desktop-linux-release, desktop-windows-release]', producer)
        self.assertIn('project_release.py assemble', producer)
        workflow = (ROOT / '.github/workflows/publish-components.yml').read_text()
        self.assertIn('project_release.py prepare', workflow)
        self.assertIn('project_release.py promote dist/promotion --publish', workflow)
        for forbidden in ['go build', 'gradlew', '--clobber', 'release delete', 'keystore']:
            self.assertNotIn(forbidden, workflow)


if __name__ == '__main__': unittest.main()
