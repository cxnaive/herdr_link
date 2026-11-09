import importlib.util
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("ci_changes", Path(__file__).resolve().parents[1] / "ci-changes.py")
CI = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(CI)


class ComponentSelectionTest(unittest.TestCase):
    def test_desktop_changes_do_not_start_android_or_server_builds(self):
        selected = CI.classify(["desktop/view.go", "desktop/go.sum"])
        self.assertEqual([k for k, v in selected.items() if v], ["desktop"])

    def test_each_desktop_workflow_selects_only_desktop(self):
        for platform in ('windows', 'linux', 'macos'):
            with self.subTest(platform=platform):
                selected = CI.classify([f'.github/workflows/desktop-{platform}.yml'])
                self.assertEqual([k for k, v in selected.items() if v], ['desktop'])

    def test_docs_do_not_start_product_builds(self):
        self.assertFalse(any(CI.classify(["README.md", "desktop/ANDROID_PARITY.md", "docs/native-ci.md"]).values()))

    def test_shared_protocol_checks_both_ends(self):
        selected = CI.classify(["internal/protocol/protocol.go"])
        self.assertTrue(selected["go"] and selected["native"] and selected["desktop"])
        self.assertFalse(selected["android"])

    def test_android_only_change(self):
        selected = CI.classify(["herdr-android/app/src/main/java/dev/herdr/companion/NativeAgents.kt"])
        self.assertEqual([k for k, v in selected.items() if v], ["android"])

    def test_multi_commit_push_and_rename_use_both_paths(self):
        selected = CI.classify(["internal/app/old.go", "desktop/new.go", "frontend/src/view.svelte"])
        self.assertTrue(all(selected[k] for k in ("go", "native", "desktop", "frontend")))

    def test_invalid_or_missing_history_fails_open(self):
        self.assertIsNone(CI.changed_paths({"before": "0" * 40, "after": "a" * 40}, "push"))
        with patch.object(subprocess, "check_output", side_effect=subprocess.CalledProcessError(128, "git")):
            self.assertIsNone(CI.changed_paths({"before": "b" * 40, "after": "a" * 40}, "push"))

    def test_pull_request_uses_merge_base_without_shell_interpolation(self):
        with patch.object(subprocess, "check_output", return_value=b"desktop/a b.go\0") as diff:
            paths = CI.changed_paths({"pull_request": {"base": {"sha": "a" * 40}, "head": {"sha": "b" * 40}}}, "pull_request")
            self.assertEqual(paths, ["desktop/a b.go"])
            self.assertIn("a" * 40 + "..." + "b" * 40, diff.call_args.args[0])


if __name__ == "__main__":
    unittest.main()
