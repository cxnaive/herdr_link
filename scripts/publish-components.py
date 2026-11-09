#!/usr/bin/env python3
"""Promote verified build-artifacts output on Actions; never build or sign locally."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import tarfile
import zipfile

REPOSITORY = "cxnaive/herder_relay_extra"
BRANCH = "feat/android-file-companion"
ARTIFACTS = {"android": "android-apks-signed", "native": "native-bundles-linux_amd64"}
ABIS = ("arm64-v8a", "armeabi-v7a", "x86_64")
ROOT = Path(__file__).resolve().parent.parent


def require(value, message):
    if not value:
        raise ValueError(message)


def command(args, **kwargs):
    result = subprocess.run(args, capture_output=True, check=False, **kwargs)
    require(result.returncode == 0, f"Command failed: {args[0]} {args[1]}")
    return result.stdout


def api(path, method="GET", data=None, missing=False):
    args = ["gh", "api", "-H", "Cache-Control: no-cache", f"repos/{REPOSITORY}/{path}", "--method", method]
    if data is not None:
        args += ["--input", "-"]
    result = subprocess.run(args, input=json.dumps(data) if data is not None else None,
                            capture_output=True, text=True, check=False)
    if missing and result.returncode and "HTTP 404" in result.stderr:
        return None
    require(result.returncode == 0, f"GitHub {method} request failed: {path.split('?')[0]}")
    return json.loads(result.stdout) if result.stdout.strip() else None


def digest(path):
    with path.open("rb") as handle:
        return hashlib.file_digest(handle, "sha256").hexdigest()


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2, ensure_ascii=False) + "\n")


def runner_guard():
    require(os.environ.get("GITHUB_ACTIONS") == "true", "Publishing runs on GitHub Actions, not this computer")
    require(os.environ.get("GITHUB_REPOSITORY") == REPOSITORY, "Wrong repository")
    require(os.environ.get("GITHUB_REF") == f"refs/heads/{BRANCH}", "Only the trusted release branch may promote builds")
    require(os.environ.get("GITHUB_EVENT_NAME") == "workflow_dispatch", "Promotion must be explicitly dispatched")


def validate_run(run, run_id):
    require(str(run.get("id")) == str(run_id), "Build run ID mismatch")
    require(run.get("status") == "completed" and run.get("conclusion") == "success", "Build must have completed successfully")
    require(run.get("event") in ("push", "workflow_dispatch"), "PR builds cannot be promoted")
    require(run.get("head_branch") == BRANCH, "Build did not come from the trusted branch")
    require(run.get("head_repository", {}).get("full_name") == REPOSITORY, "Fork build cannot be promoted")
    require(run.get("repository", {}).get("full_name") == REPOSITORY, "Wrong build repository")
    require(run.get("path", "").split("@", 1)[0] == ".github/workflows/build-artifacts.yml", "Wrong producer workflow")
    require(re.fullmatch(r"[0-9a-f]{40}", run.get("head_sha", "")), "Invalid source commit")
    return run["head_sha"]


def source_manifest(sha):
    command(["git", "merge-base", "--is-ancestor", sha, "HEAD"], cwd=ROOT)
    return json.loads(command(["git", "show", f"{sha}:VERSION.json"], cwd=ROOT))


def version(value):
    require(isinstance(value, str) and re.fullmatch(r"\d+\.\d+\.\d+(?:\+codexfix\.\d+)?", value), "Invalid component version")
    return value


def specifications(manifest, components):
    result = {}
    for key in components:
        if key == "android":
            data = manifest["android"]
            v = version(data["version"])
            require(re.fullmatch(r"\d+\.\d+\.\d+", v), "Android requires a stable numeric version")
            require(type(data["version_code"]) is int and data["version_code"] > 0, "Invalid Android versionCode")
            require(re.fullmatch(r"[0-9a-f]{64}", data["signing_certificate_sha256"]), "Invalid certificate pin")
            names = [f"Herdr-Pocket-{v}-{kind}-{abi}.apk" for kind in ("debug", "release") for abi in ABIS]
            result[key] = {"tag": f"android-v{v}", "version": v, "title": f"Herdr Pocket {v}",
                           "names": sorted(names + ["BUILD-INFO.json", "SHA256SUMS.txt"])}
        else:
            relay, gateway = version(manifest["relay"]["version"]), version(manifest["native_gateway"]["version"])
            names = [f"{name}_{v.replace('+', '-')}_linux_amd64.tar.gz" for name, v in
                     (("herdr-mobile-relay", relay), ("herdr-gateway", gateway))]
            result[key] = {"tag": f"native-v{relay.replace('+', '-')}", "version": relay,
                           "title": f"Native Relay {relay} / Gateway {gateway} (Linux amd64)",
                           "names": sorted(names + ["SHA256SUMS.txt"])}
    return result


def current_versions_match(manifest, components):
    current = json.loads((ROOT / "VERSION.json").read_text())
    for key in components:
        fields = ("android",) if key == "android" else ("relay", "native_gateway")
        for field in fields:
            require(current[field]["version"] == manifest[field]["version"], "Source version has advanced; choose the matching build")
        if key == "android":
            for field in ("version_code", "signing_certificate_sha256"):
                require(current["android"][field] == manifest["android"][field], "Android version/signing identity has changed")


def extract_zip(archive, destination, names):
    with zipfile.ZipFile(archive) as bundle:
        entries = bundle.infolist()
        require(len(entries) == len(names) and {e.filename for e in entries} == set(names), "Unexpected artifact file set")
        require(sum(e.file_size for e in entries) <= 512 * 1024 * 1024, "Artifact is too large")
        for entry in entries:
            require(Path(entry.filename).name == entry.filename and not entry.is_dir(), "Unsafe artifact path")
            require(not stat.S_ISLNK(entry.external_attr >> 16), "Artifact links are forbidden")
        bundle.extractall(destination)


def verify_checksums(directory, names):
    require({p.name for p in directory.iterdir()} == set(names), "Unexpected prepared files")
    require(all(p.is_file() and not p.is_symlink() for p in directory.iterdir()), "Prepared files must be regular files")
    recorded = {}
    for line in (directory / "SHA256SUMS.txt").read_text().splitlines():
        sha, name = line.split()
        require(name in names and name not in recorded and re.fullmatch(r"[0-9a-f]{64}", sha), "Invalid checksum entry")
        require(digest(directory / name) == sha, "Artifact file checksum mismatch")
        recorded[name] = sha
    require(set(recorded) == set(names) - {"SHA256SUMS.txt"}, "Incomplete checksum manifest")
    return {p.name: {"size": p.stat().st_size, "digest": "sha256:" + digest(p)} for p in directory.iterdir()}


def verify_android(directory, manifest, sha):
    android = manifest["android"]
    info = json.loads((directory / "BUILD-INFO.json").read_text())
    require(info["revision"] == sha and info["version"] == android["version"] and
            info["version_code"] == android["version_code"], "APK source/version mismatch")
    records = info["artifacts"]
    expected = {(kind, abi) for kind in ("debug", "release") for abi in ABIS}
    require(len(records) == 6 and {(r["build_type"], r["abi"]) for r in records} == expected, "Incomplete APK records")
    tools = Path(os.environ["ANDROID_HOME"]) / "build-tools/35.0.0"
    for record in records:
        name = f'Herdr-Pocket-{android["version"]}-{record["build_type"]}-{record["abi"]}.apk'
        require(record["file"] == name and record["signing"] == "existing-installation" and
                record["certificate_sha256"] == android["signing_certificate_sha256"], "Not an existing-installation APK")
        apk = directory / name
        require(digest(apk) == record["sha256"], "APK record checksum mismatch")
        report = command([str(tools / "apksigner"), "verify", "--print-certs", str(apk)], text=True)
        require(re.findall(r"^Signer #\d+ certificate SHA-256 digest: ([0-9a-f]+)$", report, re.M) ==
                [android["signing_certificate_sha256"]], "Wrong APK certificate")
        badging = command([str(tools / "aapt"), "dump", "badging", str(apk)], text=True)
        first = badging.splitlines()[0]
        require("name='dev.herdr.companion'" in first and f"versionName='{android['version']}'" in first and
                f"versionCode='{android['version_code']}'" in first, "Wrong APK manifest")
        require(re.search(r"^native-code: '" + re.escape(record["abi"]) + r"'\s*$", badging, re.M), "Wrong APK ABI")


def verify_native(directory, manifest, sha):
    for component, field in (("herdr-mobile-relay", "relay"), ("herdr-gateway", "native_gateway")):
        v = manifest[field]["version"]
        name = f"{component}_{v.replace('+', '-')}_linux_amd64"
        with tarfile.open(directory / (name + ".tar.gz")) as bundle:
            members = bundle.getmembers()
            require(sum(m.size for m in members) <= 512 * 1024 * 1024, "Native bundle is too large")
            require(all((m.isfile() or m.isdir()) and not m.name.startswith("/") and
                        ".." not in Path(m.name).parts for m in members), "Unsafe native archive")
            require(len([m for m in members if m.name == name + "/BUILD-INFO.json"]) == 1, "Invalid native manifest")
            info = json.load(bundle.extractfile(name + "/BUILD-INFO.json"))
            require(info["revision"] == sha and info["version"] == v and info["component"] == component and
                    info["target"] == "linux/amd64" and info["cgo_enabled"] is False, "Wrong native build source")
    # Executable smoke tests run only in the read-only job, with tokens removed.
    environment = {k: v for k, v in os.environ.items() if k not in ("GH_TOKEN", "GITHUB_TOKEN", "ACTIONS_RUNTIME_TOKEN")}
    command(["python3", str(ROOT / "scripts/check-native-release.py"), str(directory)], env=environment)


def prepare(run_id, components, output):
    require(re.fullmatch(r"[1-9]\d*", run_id), "Build run ID must be a positive integer")
    sha = validate_run(api(f"actions/runs/{run_id}"), run_id)
    manifest = source_manifest(sha)
    current_versions_match(manifest, components)
    specs = specifications(manifest, components)
    require(not output.exists(), "Refusing to replace prepared output")
    output.mkdir(parents=True)
    artifacts = api(f"actions/runs/{run_id}/artifacts?per_page=100")["artifacts"]
    plan = {"schema": 1, "repository": REPOSITORY, "build_run_id": int(run_id), "source": sha,
            "manifest": manifest, "components": {}}
    for key, spec in specs.items():
        candidates = [a for a in artifacts if a["name"] == ARTIFACTS[key]]
        require(len(candidates) == 1 and not candidates[0]["expired"], "Missing, duplicated or expired CI artifact")
        artifact = candidates[0]
        require(re.fullmatch(r"sha256:[0-9a-f]{64}", artifact.get("digest", "")), "Missing Actions archive digest")
        archive = output / (key + ".zip")
        with archive.open("wb") as handle:
            result = subprocess.run(["gh", "api", f"repos/{REPOSITORY}/actions/artifacts/{artifact['id']}/zip"],
                                    stdout=handle, stderr=subprocess.PIPE, check=False)
        require(result.returncode == 0 and "sha256:" + digest(archive) == artifact["digest"], "Actions ZIP digest mismatch")
        directory = output / key
        extract_zip(archive, directory, spec["names"])
        files = verify_checksums(directory, spec["names"])
        if key == "android":
            verify_android(directory, manifest, sha)
        else:
            verify_native(directory, manifest, sha)
        require(files == verify_checksums(directory, spec["names"]), "Files changed during verification")
        plan["components"][key] = {**spec, "files": files, "artifact_id": artifact["id"], "archive_digest": artifact["digest"]}
        archive.unlink()  # Only this job's verified transfer copy; prepared files stay intact.
    write_json(output / "plan.json", plan)
    inspect(plan)
    print(f"Verified CI run {run_id}, source {sha}; no build, signing or release mutation performed")


def load_plan(directory):
    plan = json.loads((directory / "plan.json").read_text())
    require(plan.get("schema") == 1 and plan.get("repository") == REPOSITORY, "Wrong promotion plan")
    require(type(plan.get("build_run_id")) is int and plan["build_run_id"] > 0, "Invalid plan run ID")
    components = plan["components"]
    require(bool(components) and set(components) <= set(ARTIFACTS), "Unknown components")
    sha = validate_run(api(f"actions/runs/{plan['build_run_id']}"), plan["build_run_id"])
    require(sha == plan["source"] and source_manifest(sha) == plan["manifest"], "Plan source changed")
    current_versions_match(plan["manifest"], components)
    for key, expected in specifications(plan["manifest"], components).items():
        spec = components[key]
        require(all(spec[field] == expected[field] for field in expected), "Altered component plan")
        require(verify_checksums(directory / key, spec["names"]) == spec["files"], "Prepared asset changed")
    return plan


def tag_commit(tag):
    ref = api(f"git/ref/tags/{tag}", missing=True)
    if ref is None:
        return None
    obj = ref["object"]
    while obj["type"] == "tag":
        obj = api("git/tags/" + obj["sha"])["object"]
    require(obj["type"] == "commit", "Tag does not identify a commit")
    return obj["sha"]


def marker(plan):
    return f"<!-- component-ci: run={plan['build_run_id']} source={plan['source']} -->"


def check_release(release, spec, sha, ownership, partial=False):
    require(release["tag_name"] == spec["tag"] and release["target_commitish"] == sha, "Release source conflict; bump the version")
    require(not release["prerelease"], "Existing release type differs")
    if release["draft"]:
        require(ownership in (release.get("body") or ""), "Refusing to adopt an unrelated draft")
    actual = {a["name"]: a for a in release["assets"]}
    require(len(actual) == len(release["assets"]) and set(actual) <= set(spec["files"]), "Unexpected release assets")
    if not partial:
        require(set(actual) == set(spec["files"]), "Incomplete existing release")
    for name, asset in actual.items():
        require(asset["state"] == "uploaded" and all(asset[k] == spec["files"][name][k] for k in ("size", "digest")),
                "Existing asset differs; refusing overwrite")


def inspect(plan):
    releases = api("releases?per_page=100")
    result = {}
    for key, spec in plan["components"].items():
        matches = [r for r in releases if r["tag_name"] == spec["tag"]]
        require(len(matches) <= 1, "Duplicated release tag")
        existing = matches[0] if matches else None
        commit = tag_commit(spec["tag"])
        require(commit in (None, plan["source"]), "Existing tag points to different source; bump the version")
        if existing:
            check_release(existing, spec, plan["source"], marker(plan), partial=existing["draft"])
            require(existing["draft"] or commit == plan["source"], "Published release tag is missing")
        if key == "android" and existing is None:
            latest = api("releases/latest", missing=True)
            if latest and latest["tag_name"].startswith("android-v"):
                current = tuple(map(int, latest["tag_name"].removeprefix("android-v").split(".")))
                candidate = tuple(map(int, spec["version"].split(".")))
                require(candidate > current, "Refusing to downgrade Android Latest")
                old_sha = tag_commit(latest["tag_name"])
                old = source_manifest(old_sha)["android"]
                require(plan["manifest"]["android"]["version_code"] > old["version_code"], "Android versionCode must increase")
        result[key] = existing
    return result


def notes(plan, key):
    spec = plan["components"][key]
    body = [f"# {spec['title']}", marker(plan), "",
            f"直接复用成功的 [CI 构建 {plan['build_run_id']}](https://github.com/{REPOSITORY}/actions/runs/{plan['build_run_id']})，在 GitHub Actions 内发布；没有本机构建、中转或重新签名。",
            f"源码 / 标签提交：`{plan['source']}`。",
            f"[此版本更新记录](https://github.com/{REPOSITORY}/blob/{plan['source']}/CHANGELOG.md)", ""]
    if key == "android":
        body += [f"versionCode：{plan['manifest']['android']['version_code']}。ARM64 手机选择 `Herdr-Pocket-{spec['version']}-release-arm64-v8a.apk`。",
                 "三种 ABI 的 Debug/Release APK 均沿用原 Pocket 证书。直接覆盖安装，不要先卸载；保留配对和草稿。",
                 f"证书 SHA-256：`{plan['manifest']['android']['signing_certificate_sha256']}`。"]
    else:
        body += ["完整 Linux amd64 Relay/Gateway 安装包，包含 Web、配置/systemd 模板、说明及 BUILD-INFO。",
                 "升级保留现有配置、身份与设备库；不重复初始化。仅重启需要升级的组件，不重启 Herdr/Agent。发布动作不部署服务。"]
    checks = api(f"actions/runs?head_sha={plan['source']}&per_page=100")["workflow_runs"]
    checks = [r for r in checks if r.get("path", "").split("@", 1)[0] == ".github/workflows/check.yml"]
    state = max(checks, key=lambda r: r["id"]).get("conclusion") if checks else None
    body += ["", "验证：Actions ZIP digest、逐文件校验和、精确源码/版本；APK 原证书与 ABI；Native 隔离启动和配对冒烟。",
             f"独立 check：`{state or '未取得通过结果'}`（可能仅含相关组件检查）。构建成功不等同深度检查、网页/PWA 或用户真机验收完成。",
             "旧 Release、标签与附件不覆盖。", "", "附件：", *[f"- `{name}`" for name in spec["names"]], ""]
    return "\n".join(body)


def promote(directory, publish):
    plan = load_plan(directory)
    existing = inspect(plan)  # Preflight every component before changing anything.
    if not publish:
        print("DRY RUN: verified release/tag collision guards; no releases, tags, assets or repository files changed")
        return
    for key, spec in plan["components"].items():
        release = existing[key]
        if release is None:
            release = api("releases", "POST", {"tag_name": spec["tag"], "target_commitish": plan["source"],
                          "name": spec["title"], "body": notes(plan, key), "draft": True, "prerelease": False, "make_latest": "false"})
        if release["draft"]:
            uploaded = {a["name"] for a in release["assets"]}
            missing = [str(directory / key / name) for name in spec["names"] if name not in uploaded]
            if missing:
                command(["gh", "release", "upload", spec["tag"], "--repo", REPOSITORY, *missing])
            release = api(f"releases/{release['id']}")
            check_release(release, spec, plan["source"], marker(plan))
        existing[key] = release
    # Both drafts are complete before either is published. Native never takes Latest.
    for key in sorted(existing, reverse=True):
        release = existing[key]
        if release["draft"]:
            api(f"releases/{release['id']}", "PATCH", {"draft": False, "make_latest": "true" if key == "android" else "false"})
    receipt = {"schema": 1, "source": plan["source"], "build_run_id": plan["build_run_id"], "components": {}}
    for key, spec in plan["components"].items():
        release = api(f"releases/{existing[key]['id']}")
        require(not release["draft"] and tag_commit(spec["tag"]) == plan["source"], "Publication did not finish")
        check_release(release, spec, plan["source"], marker(plan))
        receipt["components"][key] = {"tag": spec["tag"], "url": release["html_url"], "assets": spec["files"]}
    write_json(directory / "published.json", receipt)
    print("Published or verified existing exact CI releases; old assets were not overwritten")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("prepare", "promote", "docs"))
    parser.add_argument("directory", type=Path)
    parser.add_argument("--build-run-id")
    parser.add_argument("--components", choices=("both", "android", "native"), default="both")
    parser.add_argument("--publish", action="store_true")
    args = parser.parse_args()
    runner_guard()
    require("project" not in json.loads((ROOT / "VERSION.json").read_text()), "Legacy component releases are retired; use project_release.py")
    directory = args.directory.resolve()
    if args.mode == "prepare":
        prepare(args.build_run_id or "", list(ARTIFACTS) if args.components == "both" else [args.components], directory)
    elif args.mode == "promote":
        promote(directory, args.publish)
    else:
        from component_release_docs import update_docs
        receipt = json.loads((directory / "published.json").read_text())
        plan = load_plan(directory)
        require(receipt["source"] == plan["source"] and receipt["build_run_id"] == plan["build_run_id"], "Receipt source mismatch")
        releases = inspect(plan)
        require(all(r and not r["draft"] for r in releases.values()), "Docs require published releases")
        update_docs(ROOT, plan, receipt)


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, OSError) as error:
        raise SystemExit(f"Component release: {error}") from None
