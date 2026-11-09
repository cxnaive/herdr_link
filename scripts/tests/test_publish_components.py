"""Offline promotion tests: no network, real keys, release writes or server access."""
import copy
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import zipfile

ROOT = Path(__file__).resolve().parents[2]

def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module

P = load("publisher", ROOT / "scripts/publish-components.py")
D = load("release_docs", ROOT / "scripts/component_release_docs.py")
SHA = "a" * 40
CERT = "b" * 64
MANIFEST = {
    "android": {"version": "0.2.24", "version_code": 27, "signing_certificate_sha256": CERT},
    "relay": {"version": "0.23.5+codexfix.12"}, "native_gateway": {"version": "0.23.5+codexfix.6"},
    "android_release": {"version": "0.2.23", "version_code": 26, "release_tag": "android-v0.2.23",
                        "artifact": "Herdr-Pocket-0.2.23-release-arm64-v8a.apk", "release_source_commit": "c" * 40,
                        "release_build_run": 99, "release_artifacts": []},
    "native_release": {"tag": "native-v0.23.5-codexfix.11", "targets": ["linux/amd64"],
                       "relay_artifact": "herdr-mobile-relay_0.23.5-codexfix.11_linux_amd64.tar.gz",
                       "gateway_artifact": "herdr-gateway_0.23.5-codexfix.6_linux_amd64.tar.gz",
                       "checksums": "SHA256SUMS.txt", "release_source_commit": "c" * 40, "release_build_run": 99},
}

def build_run():
    return {"id": 123, "status": "completed", "conclusion": "success", "event": "push", "head_branch": P.BRANCH,
            "head_repository": {"full_name": P.REPOSITORY}, "repository": {"full_name": P.REPOSITORY},
            "path": ".github/workflows/build-artifacts.yml", "head_sha": SHA}


def plan():
    value = {"schema": 1, "repository": P.REPOSITORY, "build_run_id": 123, "source": SHA,
             "manifest": copy.deepcopy(MANIFEST), "components": P.specifications(MANIFEST, ("android", "native"))}
    for spec in value["components"].values():
        spec["files"] = {name: {"size": 1, "digest": "sha256:" + "d" * 64} for name in spec["names"]}
    return value


def release(value, key, draft=False):
    spec = value["components"][key]
    return {"id": 1 if key == "android" else 2, "tag_name": spec["tag"], "target_commitish": SHA,
            "draft": draft, "prerelease": False, "body": P.marker(value) if draft else "Previously published human release notes",
            "html_url": "https://example.invalid/release", "assets":
            [{"name": name, "state": "uploaded", **record} for name, record in spec["files"].items()]}


class PublisherTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)

    def test_trusted_successful_run(self):
        for event in ("push", "workflow_dispatch"):
            run = build_run(); run["event"] = event
            self.assertEqual(P.validate_run(run, "123"), SHA)

    def test_rejects_pr_fork_wrong_branch_workflow_status_and_source(self):
        mutations = {"event": "pull_request", "head_branch": "other", "path": ".github/workflows/other.yml",
                     "conclusion": "failure", "status": "in_progress", "head_sha": "bad", "id": 124,
                     "head_repository": {"full_name": "other/fork"}, "repository": {"full_name": "other/fork"}}
        for field, bad in mutations.items():
            with self.subTest(field=field):
                run = build_run(); run[field] = bad
                with self.assertRaises(ValueError): P.validate_run(run, "123")

    def test_refuses_local_or_untrusted_publication_context(self):
        valid = {"GITHUB_ACTIONS": "true", "GITHUB_REPOSITORY": P.REPOSITORY,
                 "GITHUB_REF": "refs/heads/" + P.BRANCH, "GITHUB_EVENT_NAME": "workflow_dispatch"}
        with patch.dict(os.environ, valid, clear=True): P.runner_guard()
        for field in valid:
            env = {**valid, field: "wrong"}
            with patch.dict(os.environ, env, clear=True), self.assertRaises(ValueError): P.runner_guard()

    def test_versions_and_exact_file_sets(self):
        specs = P.specifications(MANIFEST, ("android", "native"))
        self.assertEqual(len(specs["android"]["names"]), 8)
        self.assertEqual(len(specs["native"]["names"]), 3)
        self.assertEqual(specs["native"]["tag"], "native-v0.23.5-codexfix.12")
        for bad in ("../../file", "0.2.24\n", "v0.2.24", "0.2.24; echo bad"):
            with self.assertRaises(ValueError): P.version(bad)
        for field, bad in (("version_code", 0), ("version_code", True), ("signing_certificate_sha256", "bad")):
            v = copy.deepcopy(MANIFEST); v["android"][field] = bad
            with self.assertRaises(ValueError): P.specifications(v, ("android",))

    def test_rejects_build_version_after_source_has_advanced(self):
        v = copy.deepcopy(MANIFEST); v["android"]["version_code"] = 28
        (self.root / "VERSION.json").write_text(json.dumps(v))
        with patch.object(P, "ROOT", self.root), self.assertRaises(ValueError): P.current_versions_match(MANIFEST, ("android",))

    def test_zip_rejects_extra_files_traversal_symlinks_and_duplicates(self):
        for mode in ("extra", "traversal", "symlink", "duplicate"):
            path = self.root / (mode + ".zip")
            with zipfile.ZipFile(path, "w") as z:
                info = zipfile.ZipInfo("../apk" if mode == "traversal" else "apk")
                if mode == "symlink": info.external_attr = 0o120777 << 16
                z.writestr(info, b"x")
                if mode in ("extra", "duplicate"): z.writestr("extra" if mode == "extra" else "apk", b"x")
            with self.subTest(mode=mode), self.assertRaises(ValueError):
                P.extract_zip(path, self.root / mode, ["../apk" if mode == "traversal" else "apk"])

    def test_checksums_reject_corruption_omission_and_extra_files(self):
        (self.root / "apk").write_bytes(b"x")
        sums = self.root / "SHA256SUMS.txt"
        valid = hashlib.sha256(b"x").hexdigest() + "  apk\n"
        sums.write_text(valid)
        self.assertEqual(P.verify_checksums(self.root, ["apk", sums.name])["apk"]["size"], 1)
        for data in ("", valid + valid, "0" * 64 + "  apk\n"):
            sums.write_text(data)
            with self.assertRaises(ValueError): P.verify_checksums(self.root, ["apk", sums.name])
        sums.write_text(valid); (self.root / "extra").write_text("x")
        with self.assertRaises(ValueError): P.verify_checksums(self.root, ["apk", sums.name])

    def android_fixture(self):
        records = []
        for kind in ("debug", "release"):
            for abi in P.ABIS:
                name = f"Herdr-Pocket-0.2.24-{kind}-{abi}.apk"
                (self.root / name).write_bytes(b"synthetic")
                records.append({"file": name, "abi": abi, "build_type": kind, "signing": "existing-installation",
                                "certificate_sha256": CERT, "sha256": hashlib.sha256(b"synthetic").hexdigest()})
        info = {"revision": SHA, "version": "0.2.24", "version_code": 27, "artifacts": records}
        (self.root / "BUILD-INFO.json").write_text(json.dumps(info))
        return info

    @staticmethod
    def inspector(args, **kwargs):
        if Path(args[0]).name == "apksigner": return "Signer #1 certificate SHA-256 digest: " + CERT + "\n"
        name = Path(args[-1]).name
        abi = next(a for a in P.ABIS if a in name)
        return f"package: name='dev.herdr.companion' versionName='0.2.24' versionCode='27'\nnative-code: '{abi}'\n"

    def test_android_inspects_every_signature_and_abi(self):
        self.android_fixture()
        with patch.dict(os.environ, {"ANDROID_HOME": "/synthetic"}), patch.object(P, "command", side_effect=self.inspector) as cmd:
            P.verify_android(self.root, MANIFEST, SHA)
            self.assertEqual(cmd.call_count, 12)

    def test_android_rejects_wrong_source_version_record_signature_or_actual_certificate(self):
        for field, bad in (("revision", "c" * 40), ("version_code", 26)):
            info = self.android_fixture(); info[field] = bad
            (self.root / "BUILD-INFO.json").write_text(json.dumps(info))
            with self.assertRaises(ValueError): P.verify_android(self.root, MANIFEST, SHA)
        info = self.android_fixture(); info["artifacts"][0]["signing"] = "ci-debug"
        (self.root / "BUILD-INFO.json").write_text(json.dumps(info))
        with patch.dict(os.environ, {"ANDROID_HOME": "/synthetic"}), self.assertRaises(ValueError): P.verify_android(self.root, MANIFEST, SHA)
        self.android_fixture()
        with patch.dict(os.environ, {"ANDROID_HOME": "/synthetic"}), patch.object(P, "command", return_value="wrong certificate"), self.assertRaises(ValueError):
            P.verify_android(self.root, MANIFEST, SHA)

    def test_native_rejects_wrong_revision_before_execution(self):
        name = "herdr-mobile-relay_0.23.5-codexfix.12_linux_amd64"
        info = {"revision": "c" * 40, "version": MANIFEST["relay"]["version"], "component": "herdr-mobile-relay",
                "target": "linux/amd64", "cgo_enabled": False}
        data = json.dumps(info).encode()
        with tarfile.open(self.root / (name + ".tar.gz"), "w:gz") as t:
            member = tarfile.TarInfo(name + "/BUILD-INFO.json"); member.size = len(data); t.addfile(member, io.BytesIO(data))
        with patch.object(P, "command") as cmd, self.assertRaises(ValueError): P.verify_native(self.root, MANIFEST, SHA)
        cmd.assert_not_called()

    def test_release_conflicts_fail_closed(self):
        value = plan(); spec = value["components"]["android"]
        original = release(value, "android")
        P.check_release(original, spec, SHA, P.marker(value))
        mutations = (("target_commitish", "b" * 40), ("prerelease", True), ("tag_name", "other"), ("assets", []))
        for key, bad in mutations:
            changed = copy.deepcopy(original); changed[key] = bad
            with self.assertRaises(ValueError): P.check_release(changed, spec, SHA, P.marker(value))
        changed = copy.deepcopy(original); changed["assets"][0]["digest"] = "sha256:" + "0" * 64
        with self.assertRaises(ValueError): P.check_release(changed, spec, SHA, P.marker(value))

    def test_only_owned_drafts_may_be_resumed(self):
        value = plan(); r = release(value, "android", draft=True); r["assets"] = []
        P.check_release(r, value["components"]["android"], SHA, P.marker(value), partial=True)
        r["body"] = "unrelated manual draft"
        with self.assertRaises(ValueError): P.check_release(r, value["components"]["android"], SHA, P.marker(value), partial=True)

    def test_existing_tag_conflict_prevents_all_release_writes(self):
        value = plan()
        with patch.object(P, "api", return_value=[]), patch.object(P, "tag_commit", return_value="c" * 40), self.assertRaises(ValueError):
            P.inspect(value)

    def test_dry_run_never_writes_or_creates_receipt(self):
        with patch.object(P, "load_plan", return_value=plan()), patch.object(P, "inspect", return_value={}), patch.object(P, "api") as api:
            P.promote(self.root, False)
        api.assert_not_called(); self.assertFalse((self.root / "published.json").exists())

    def test_existing_published_release_retry_is_read_only(self):
        value = plan(); existing = {key: release(value, key) for key in value["components"]}
        def api(path, method="GET", **kwargs):
            self.assertEqual(method, "GET")
            return next(r for r in existing.values() if path == f"releases/{r['id']}")
        with patch.object(P, "load_plan", return_value=value), patch.object(P, "inspect", return_value=existing), patch.object(P, "api", side_effect=api), patch.object(P, "tag_commit", return_value=SHA), patch.object(P, "command") as cmd:
            P.promote(self.root, True)
        cmd.assert_not_called()
        self.assertEqual(json.loads((self.root / "published.json").read_text())["source"], SHA)

    def test_all_uploads_verified_before_any_publication_and_android_latest_only(self):
        value = plan(); releases = {}; events = []
        def api(path, method="GET", data=None, **kwargs):
            events.append((method, path, data))
            if method == "POST":
                key = next(k for k, s in value["components"].items() if s["tag"] == data["tag_name"])
                r = release(value, key, draft=True); r["assets"] = []; releases[r["id"]] = r; return copy.deepcopy(r)
            r = releases[int(path.split("/")[-1])]
            if method == "PATCH": r["draft"] = data["draft"]
            return copy.deepcopy(r)
        def upload(args, **kwargs):
            self.assertEqual(args[:3], ["gh", "release", "upload"])
            key = next(k for k, s in value["components"].items() if s["tag"] == args[3])
            r = release(value, key, draft=True); releases[r["id"]] = r; events.append(("UPLOAD", key, None))
        with patch.object(P, "load_plan", return_value=value), patch.object(P, "inspect", return_value={"android": None, "native": None}), patch.object(P, "notes", return_value=P.marker(value)), patch.object(P, "api", side_effect=api), patch.object(P, "tag_commit", return_value=SHA), patch.object(P, "command", side_effect=upload):
            P.promote(self.root, True)
        first_publish = next(i for i, e in enumerate(events) if e[0] == "PATCH")
        self.assertEqual(sum(e[0] == "UPLOAD" for e in events[:first_publish]), 2)
        self.assertEqual([e[2]["make_latest"] for e in events if e[0] == "PATCH"], ["false", "true"])

    def docs_fixture(self):
        (self.root / "herdr-android").mkdir(); (self.root / "docs").mkdir()
        (self.root / "VERSION.json").write_text(json.dumps(MANIFEST))
        gallery = '<!-- pocket-preview:start --><table>one by four unchanged</table><!-- pocket-preview:end -->\n'
        system = '# System\n## Architecture\n```mermaid\nflowchart LR\n Relay --> Gateway --> Clients\n```\n## Clients\n### Android\n'
        blocks = '<!-- android-downloads:start -->old<!-- android-downloads:end -->\n<!-- native-downloads:start -->old<!-- native-downloads:end -->\n'
        (self.root / "README.md").write_text(system + gallery + blocks + 'User prose stays\n')
        (self.root / "herdr-android/README.md").write_text(blocks)
        (self.root / "docs/native-deployment.md").write_text(MANIFEST["native_release"]["tag"] + '\n' + MANIFEST["native_release"]["relay_artifact"])
        value = plan()
        receipt = {"components": {k: {"tag": s["tag"], "assets": s["files"]} for k, s in value["components"].items()}}
        return value, receipt, gallery

    def test_documentation_is_automatic_idempotent_and_preserves_gallery(self):
        value, receipt, gallery = self.docs_fixture()
        D.update_docs(self.root, value, receipt)
        first = (self.root / "README.md").read_text()
        self.assertTrue(first.startswith('# System\n## Architecture'))
        self.assertIn('## Clients\n### Android\n' + gallery, first)
        self.assertIn('User prose stays', first)
        self.assertIn('## 安装\n', (self.root / 'herdr-android/README.md').read_text())
        self.assertIn('Herdr-Pocket-0.2.24-release-arm64-v8a.apk', first)
        self.assertIn('native-v0.23.5-codexfix.12', first)
        D.update_docs(self.root, value, receipt)
        self.assertEqual((self.root / "README.md").read_text(), first)
        meta = json.loads((self.root / "VERSION.json").read_text())
        self.assertEqual(meta["android_release"]["release_source_commit"], SHA)

    def test_download_blocks_are_user_facing_and_distinguish_preview(self):
        android = D.android_block(MANIFEST)
        native = D.native_block(MANIFEST)
        self.assertNotIn("发布来源", android + native)
        self.assertNotIn("真机验收", android + native)
        self.assertLess(len(android), 1500)
        self.assertLess(len(native), 1500)
        current = copy.deepcopy(MANIFEST)
        current["relay"]["version"] = "0.23.5+codexfix.99"
        self.assertIn("尚未发布", D.native_block(current))
        current["native_release"]["relay_artifact"] = "herdr-mobile-relay_0.23.5-codexfix.99_linux_amd64.tar.gz"
        self.assertNotIn("尚未发布", D.native_block(current))

    def test_missing_readme_marker_changes_no_files(self):
        value, receipt, _ = self.docs_fixture()
        p = self.root / "herdr-android/README.md"; p.write_text("no markers")
        before = (self.root / "VERSION.json").read_bytes()
        with self.assertRaises(ValueError): D.update_docs(self.root, value, receipt)
        self.assertEqual((self.root / "VERSION.json").read_bytes(), before)

    def test_incomplete_receipt_changes_no_files(self):
        value, receipt, _ = self.docs_fixture(); receipt["components"].pop("native")
        with self.assertRaises(ValueError): D.update_docs(self.root, value, receipt)

    def test_workflow_has_separate_read_only_verification_and_write_promotion(self):
        text = (ROOT / ".github/workflows/publish-components.yml").read_text()
        read, write = text.split('\n  publish:\n', 1)
        self.assertNotIn('contents: write', read)
        self.assertIn('contents: write', write)
        self.assertIn("if: inputs.publish && needs.verify.result == 'success'", write)
        self.assertIn('promote dist/promotion --publish', write)
        self.assertNotIn('prepare dist/promotion', write)
        self.assertIn('scripts/publish-components.py docs dist/promotion', write)
        self.assertIn('git push origin HEAD:feat/android-file-companion', write)
        for forbidden in ('gradlew', 'go build', '--clobber', '--force', 'keystore', 'pull_request_target'):
            self.assertNotIn(forbidden, text)
        self.assertNotIn('${{ inputs.build_run_id }}', text.split('run: python3 scripts/publish-components.py prepare', 1)[1].split('\n', 1)[0])


if __name__ == "__main__":
    unittest.main()
