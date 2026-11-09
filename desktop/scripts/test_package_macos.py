#!/usr/bin/env python3
"""Platform-independent checks for Mac packaging; Apple commands are mocked here."""
from contextlib import nullcontext
import importlib.util
import os
from pathlib import Path
import plistlib
import shutil
import struct
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('mac_package', Path(__file__).with_name('package-macos.py'))
P = importlib.util.module_from_spec(spec); spec.loader.exec_module(P)
CONFIG = {'name': 'Herdr Desktop', 'identifier': 'dev.herdr.desktop', 'version': '0.3.2', 'macos': {'minimumSystemVersion': '12.0'}}


def fat(cpus=(0x01000007, 0x0100000c)):
    result = struct.pack('>II', 0xcafebabe, len(cpus))
    offset = 8 + 20 * len(cpus)
    for cpu in cpus:
        result += struct.pack('>IIIII', cpu, 0, offset, 32, 0); offset += 32
    for cpu in cpus:
        result += struct.pack('<II', 0xfeedfacf, cpu) + b'\0' * 24
    return result


class PackageTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(); self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.app = self.root / 'Herdr Desktop.app'
        self.exe = self.app / 'Contents/MacOS/Herdr Desktop'
        self.lib = self.app / 'Contents/Resources/libghostty-vt.dylib'
        for path in (self.exe, self.lib):
            path.parent.mkdir(parents=True, exist_ok=True); path.write_bytes(fat()); path.chmod(0o755)
        info = {'CFBundleIdentifier': CONFIG['identifier'], 'CFBundleShortVersionString': CONFIG['version'],
                'CFBundleVersion': CONFIG['version'], 'CFBundleExecutable': CONFIG['name'], 'LSMinimumSystemVersion': '12.0'}
        (self.app / 'Contents/Info.plist').write_bytes(plistlib.dumps(info))
        self.commands = patch.object(P, 'command', return_value='Signature=adhoc\n').start()
        self.addCleanup(patch.stopall)

    def test_verifies_bundle_identity_both_arches_and_signatures(self):
        records = P.verify_app(self.app, CONFIG)
        self.assertTrue(records['Contents/MacOS/Herdr Desktop']['executable'])
        self.assertIn(('codesign', '--verify', '--deep', '--strict', self.app), [c.args for c in self.commands.call_args_list])

    def test_rejects_missing_duplicate_and_mislabeled_architectures(self):
        for value in (fat((0x01000007,)), fat((0x01000007, 0x01000007)), b'not-mach-o', fat()[:-32], fat()[:48] + b'\0' * 64):
            self.lib.write_bytes(value)
            with self.subTest(value=value[:16]), self.assertRaises(ValueError):
                P.verify_app(self.app, CONFIG)

    def test_rejects_overlapping_or_out_of_bounds_slices(self):
        for offset in (48, 9999):
            value = bytearray(fat()); value[36:40] = struct.pack('>I', offset)
            self.lib.write_bytes(value)
            with self.assertRaises(ValueError): P.universal(self.lib)

    def test_rejects_wrong_version_missing_library_and_lost_permissions(self):
        with self.assertRaisesRegex(ValueError, 'CFBundleShortVersionString'):
            P.verify_app(self.app, {**CONFIG, 'version': '0.9.9'})
        self.exe.chmod(0o644)
        with self.assertRaisesRegex(ValueError, 'permission'): P.verify_app(self.app, CONFIG)
        self.exe.chmod(0o755); self.lib.unlink()
        with self.assertRaises(FileNotFoundError): P.verify_app(self.app, CONFIG)

    def test_rejects_symlink_and_wrong_signing_claim(self):
        (self.app / 'escape').symlink_to('/tmp')
        with self.assertRaisesRegex(ValueError, 'symlink'): P.verify_app(self.app, CONFIG)
        (self.app / 'escape').unlink()
        self.commands.return_value = 'Authority=Developer ID Application: Someone\n'
        with self.assertRaisesRegex(ValueError, 'ad-hoc'): P.verify_app(self.app, CONFIG)

    def test_mounted_dmg_must_match_all_app_bytes_and_permissions(self):
        volume = self.root / 'volume'; volume.mkdir()
        copied = volume / self.app.name
        shutil.copytree(self.app, copied)
        (volume / 'Applications').symlink_to('/Applications')
        dmg = self.root / 'app.dmg'; dmg.write_bytes(b'fixture')
        records = P.verify_app(self.app, CONFIG)
        with patch.object(P, 'mounted', return_value=nullcontext(volume)):
            P.verify_dmg(dmg, CONFIG, records)
        (copied / 'Contents/Resources/extra').write_text('substituted')
        with patch.object(P, 'mounted', return_value=nullcontext(volume)), self.assertRaisesRegex(ValueError, 'different app'):
            P.verify_dmg(dmg, CONFIG, records)

    def test_dmg_mount_is_always_detached_on_verification_failure(self):
        with self.assertRaisesRegex(RuntimeError, 'fixture'):
            with P.mounted(self.root / 'app.dmg'):
                raise RuntimeError('fixture')
        self.assertEqual(self.commands.call_args.args[:2], ('hdiutil', 'detach'))


if __name__ == '__main__': unittest.main()
