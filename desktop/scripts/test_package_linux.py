import hashlib
import importlib.util
import io
from pathlib import Path
import tarfile
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("package_linux", Path(__file__).with_name("package-linux.py"))
package = importlib.util.module_from_spec(spec)
spec.loader.exec_module(package)


class LinuxPackageTests(unittest.TestCase):
    def test_elf_rejects_wrong_architecture_and_truncation(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "binary"
            header = bytearray(64)
            header[:6] = b"\x7fELF\x02\x01"
            header[18:20] = (62).to_bytes(2, "little")
            path.write_bytes(header)
            package.elf_amd64(path)
            header[18:20] = (183).to_bytes(2, "little")
            path.write_bytes(header)
            with self.assertRaises(ValueError):
                package.elf_amd64(path)
            path.write_bytes(b"MZ")
            with self.assertRaises(ValueError):
                package.elf_amd64(path)

    def test_archive_requires_one_regular_matching_file(self):
        for kind in ("regular", "link", "duplicate", "missing", "not-executable"):
            with self.subTest(kind=kind):
                buffer = io.BytesIO()
                with tarfile.open(fileobj=buffer, mode="w") as bundle:
                    if kind != "missing":
                        entry = tarfile.TarInfo("./opt/herdr-desktop/libghostty-vt.so")
                        if kind == "link":
                            entry.type = tarfile.SYMTYPE
                            entry.linkname = "/tmp/untrusted.so"
                            bundle.addfile(entry)
                        else:
                            entry.size = 4
                            bundle.addfile(entry, io.BytesIO(b"test"))
                            if kind == "duplicate":
                                bundle.addfile(entry, io.BytesIO(b"evil"))
                buffer.seek(0)
                with tarfile.open(fileobj=buffer) as bundle:
                    if kind == "regular":
                        self.assertEqual(hashlib.sha256(b"test").hexdigest(), package.member_digest(bundle, "opt/herdr-desktop/libghostty-vt.so"))
                    else:
                        with self.assertRaises(ValueError):
                            package.member_digest(bundle, "opt/herdr-desktop/libghostty-vt.so", executable=kind == "not-executable")


if __name__ == "__main__":
    unittest.main()
