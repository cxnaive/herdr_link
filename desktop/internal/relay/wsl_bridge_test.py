"""Exercise the embedded WSL helper without touching user instances or keys."""
import importlib.util
import json
import os
from pathlib import Path
import socket
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("bridge", Path(__file__).with_name("wsl_bridge.py"))
B = importlib.util.module_from_spec(spec)
spec.loader.exec_module(B)


class BridgeTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="herdr-wsl-proof-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.path = self.root / "tcp-pair/admin.sock"
        self.path.parent.mkdir(mode=0o700)
        self.server = socket.socket(socket.AF_UNIX)
        self.addCleanup(self.server.close)
        self.server.bind(str(self.path)); self.server.listen()
        self.path.chmod(0o600)
        self.process = (os.getpid(), "123", str(self.path), "127.0.0.1", 8375)

    def test_private_socket_and_ancestors(self):
        self.assertTrue(B.private(str(self.path)))
        for path, mode in ((self.path, 0o666), (self.path.parent, 0o755), (self.root, 0o777)):
            previous = path.stat().st_mode & 0o777
            path.chmod(mode)
            with self.subTest(path=path), self.assertRaises(ValueError): B.private(str(self.path))
            path.chmod(previous)
        alias = self.root / "alias"
        alias.symlink_to(self.path.parent)
        with self.assertRaises(ValueError): B.private(str(alias / "admin.sock"))
        for path in ("relative/tcp-pair/admin.sock", str(self.root / "../tcp-pair/admin.sock")):
            with self.assertRaises(ValueError): B.private(path)

    def test_discovery_uses_peer_proof_and_does_not_send_request(self):
        with patch.object(B, "inspect", return_value=self.process):
            conn, peer, _ = B.connect_admin(str(self.path)); conn.close()
        with self.server.accept()[0] as conn:
            self.assertEqual(conn.recv(1), b"")
        self.assertEqual(peer, self.process)

    def test_wrong_process_and_replaced_socket_are_rejected(self):
        wrong = (*self.process[:2], str(self.root / "other.sock"), *self.process[3:])
        with patch.object(B, "inspect", return_value=wrong), self.assertRaises(ValueError):
            B.connect_admin(str(self.path))
        with patch.object(B, "inspect", return_value=self.process), patch.object(B, "private", side_effect=[(1, 2), (1, 3)]), self.assertRaises(ValueError):
            B.connect_admin(str(self.path))

    def test_nonrelay_process_is_rejected(self):
        with self.assertRaises(ValueError): B.inspect(os.getpid())

    def test_wsl_user_change_is_rejected_before_opening_socket(self):
        with patch.object(B, "connect_admin") as connect, self.assertRaises(ValueError):
            B.run({"action": "pair", "uid": os.geteuid() + 1, "user": "changed", "socket": str(self.path), "name": "test"})
        connect.assert_not_called()

    def test_process_environment_only_selects_local_runtime(self):
        env = b"HOME=/home/alice\0HERDR_RELAY_ENV=/private/relay.env\0HERDR_RELAY_PORT=12345\0SECRET=do-not-return\0"
        def read(path, _):
            if path.endswith("/environ"): return env
            raise AssertionError("unexpected file read")
        with patch.object(B, "birth", return_value="123"), patch.object(B.os, "readlink", return_value="/bin/herdr-mobile-relay"), patch.object(B, "bounded", side_effect=read):
            process = B.inspect(os.getpid())
            self.assertEqual(process[2:], ("/private/tcp-pair/admin.sock", "127.0.0.1", 12345))
            self.assertNotIn("SECRET", json.dumps(process))
            for extra in (b"HERDR_RELAY_HOST=remote.example\0", b"HERDR_RELAY_PORT=0\0", b"HERDR_RELAY_ENV=relative/relay.env\0"):
                with patch.object(B, "bounded", return_value=env + extra), self.assertRaises(ValueError): B.inspect(os.getpid())


if __name__ == "__main__": unittest.main()
