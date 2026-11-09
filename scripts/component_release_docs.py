"""Update published-version records and bounded README download sections."""
import json
import re

REPOSITORY = "cxnaive/herder_relay_extra"


def replace_block(text, key, body):
    start, end = f"<!-- {key}:start -->", f"<!-- {key}:end -->"
    if text.count(start) != 1 or text.count(end) != 1:
        raise ValueError(f"Missing or duplicated {key} markers; refusing broad README replacement")
    before, rest = text.split(start, 1)
    _, after = rest.split(end, 1)
    return before + start + "\n" + body.rstrip() + "\n" + end + after


def android_block(metadata, heading="#### 下载 Android APK"):
    release = metadata["android_release"]
    v, tag = release["version"], release["release_tag"]
    base = f"https://github.com/{REPOSITORY}/releases"
    rows = []
    for label, abi in (("大多数手机 / 平板", "arm64-v8a"), ("32 位 ARM 设备", "armeabi-v7a"), ("x86_64 设备 / 模拟器", "x86_64")):
        rows.append(f"| {label} | [{abi}]({base}/download/{tag}/Herdr-Pocket-{v}-release-{abi}.apk) |")
    return f'''{heading}

**[Herdr Pocket {v}]({base}/tag/{tag})** · Android 9 及以上

| 设备 | APK |
| --- | --- |
{chr(10).join(rows)}

更新时直接覆盖安装，**不要卸载旧版**，以保留配对与草稿。
发布包沿用固定签名证书；自行构建的 APK 可能无法覆盖安装。
校验文件、其他构建与版本说明见 Release 页面。'''


def native_block(metadata):
    release = metadata["native_release"]
    base = f"https://github.com/{REPOSITORY}/releases"
    rows = "\n".join(f"- [`{release[field]}`]({base}/download/{release['tag']}/{release[field]})"
                     for field in ("relay_artifact", "gateway_artifact", "checksums"))
    preview = ""
    for field, component, binary in (("relay_artifact", "relay", "herdr-mobile-relay"), ("gateway_artifact", "native_gateway", "herdr-gateway")):
        expected = f"{binary}_{metadata[component]['version'].replace('+', '-')}_linux_amd64.tar.gz"
        if release[field] != expected:
            preview = f"\n\n当前源码包含尚未发布的更新。需要新功能时，可从 [CI 预览构建](https://github.com/{REPOSITORY}/actions/workflows/build-artifacts.yml) 的成功运行中下载 `native-bundles-linux_amd64`，并核对包内版本；CI 附件有保留期限。"
            break
    return f'''### 服务端安装包

已发布的 **[{release['tag']}]({base}/tag/{release['tag']})** · Linux x86_64 完整部署包

{rows}

电脑安装 Relay，公网服务器安装 Gateway。安装、配对与保留数据升级见[部署指南](docs/native-deployment.md)。
ARM64 / macOS 的 CI 产物为独立二进制，不是上述完整部署包。{preview}'''


def update_docs(root, plan, receipt):
    if set(receipt["components"]) != set(plan["components"]):
        raise ValueError("Incomplete publication receipt")
    path = root / "VERSION.json"
    metadata = json.loads(path.read_text())
    previous = json.loads(path.read_text())
    for key, spec in plan["components"].items():
        record = receipt["components"][key]
        if record["tag"] != spec["tag"] or record["assets"] != spec["files"]:
            raise ValueError("Publication receipt does not match verified assets")
        if key == "android":
            a = plan["manifest"]["android"]
            metadata["android_release"] = {
                "version": a["version"], "version_code": a["version_code"], "release_tag": spec["tag"],
                "artifact": f"Herdr-Pocket-{a['version']}-release-arm64-v8a.apk",
                "release_source_commit": plan["source"], "release_build_run": plan["build_run_id"],
                "release_artifacts": spec["names"],
            }
        else:
            names = spec["names"]
            metadata["native_release"] = {
                "tag": spec["tag"], "targets": ["linux/amd64"],
                "relay_artifact": next(n for n in names if n.startswith("herdr-mobile-relay_")),
                "gateway_artifact": next(n for n in names if n.startswith("herdr-gateway_")),
                "checksums": "SHA256SUMS.txt", "release_source_commit": plan["source"],
                "release_build_run": plan["build_run_id"],
            }
    changes = {}
    readme = (root / "README.md").read_text()
    readme = replace_block(readme, "android-downloads", android_block(metadata))
    readme = replace_block(readme, "native-downloads", native_block(metadata))
    versions = {
        "电脑 Relay": f"`{metadata['relay']['version']}`",
        "原生 TCP Gateway": f"`{metadata['native_gateway']['version']}`",
        "Herdr Pocket Android": f"`{metadata['android']['version']}` / versionCode `{metadata['android']['version_code']}`",
    }
    for label, value in versions.items():
        readme = re.sub(r"(?m)^(\| " + re.escape(label) + r" \| )[^|]+(\|)", lambda m: m[1] + value + " " + m[2], readme)
    for binary, field in (("herdr-mobile-relay", "relay"), ("herdr-gateway", "native_gateway")):
        pattern = r"(-ldflags '-s -w -X main.version=)[^ ]+( -X main.revision=source-build' \\\n  -o bin/" + binary + ")"
        readme = re.sub(pattern, lambda m: m[1] + metadata[field]["version"] + m[2], readme)
    changes[root / "README.md"] = readme
    child = root / "herdr-android/README.md"
    changes[child] = replace_block(child.read_text(), "android-downloads", android_block(metadata, heading="## 安装"))
    guide = root / "docs/native-deployment.md"
    text = guide.read_text()
    for field in ("tag", "relay_artifact", "gateway_artifact"):
        text = text.replace(previous["native_release"][field], metadata["native_release"][field])
    text = text.replace(previous["android_release"]["release_tag"], metadata["android_release"]["release_tag"])
    text = text.replace(previous["android_release"]["artifact"], metadata["android_release"]["artifact"])
    # Installed directory names are archive names without the .tar.gz suffix.
    for field in ("relay_artifact", "gateway_artifact"):
        text = text.replace(previous["native_release"][field].removesuffix(".tar.gz"), metadata["native_release"][field].removesuffix(".tar.gz"))
    changes[guide] = text
    changes[path] = json.dumps(metadata, indent=2, ensure_ascii=False) + "\n"
    # Validate all markers/records before changing any repository file.
    for target, text in changes.items():
        target.write_text(text)
