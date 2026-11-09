"""Exercise the exact, non-mutating bundle validation used by the root installer."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = (ROOT / "deploy/native-tcp/install.sh").read_text()
VALIDATOR = SCRIPT.split("<<'PYVERSION'\n", 1)[1].split("\nPYVERSION", 1)[0]


class GatewayInstallTests(unittest.TestCase):
    def validate(self, machine="x86_64", **overrides):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "herdr-gateway").write_bytes(b"fixture gateway")
            info = {"component": "herdr-gateway", "target": "linux/amd64",
                    "version": "0.23.5+codexfix.7",
                    "binary_sha256": hashlib.sha256(b"fixture gateway").hexdigest()}
            info.update(overrides)
            (root / "BUILD-INFO.json").write_text(json.dumps(info))
            program = "import platform; platform.machine = lambda: " + repr(machine) + "; platform.system = lambda: 'Linux';\n" + VALIDATOR
            return subprocess.run([sys.executable, "-c", program], cwd=root,
                                  capture_output=True, text=True)

    def test_uses_bundle_version_not_old_template(self):
        result = self.validate()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), "0.23.5-codexfix.7")
        self.assertNotIn("releases/0.23.5-codexfix.6", SCRIPT)
        self.assertIn('cmp -s herdr-gateway "$release/herdr-gateway"', SCRIPT)

    def test_arm64_installs_only_on_arm64_host(self):
        self.assertEqual(self.validate(machine="aarch64", target="linux/arm64").returncode, 0)
        self.assertNotEqual(self.validate(machine="aarch64").returncode, 0)
        self.assertNotEqual(self.validate(machine="riscv64", target="linux/riscv64").returncode, 0)

    def test_rejects_wrong_component_target_checksum_and_path(self):
        for overrides in ({"component": "herdr-mobile-relay"}, {"target": "linux/arm64"},
                          {"binary_sha256": "0" * 64}, {"version": "../../elsewhere"}):
            with self.subTest(overrides=overrides):
                self.assertNotEqual(self.validate(**overrides).returncode, 0)
