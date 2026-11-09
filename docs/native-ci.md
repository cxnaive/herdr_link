# Native build CI and Android signing

The `build-artifacts` workflow selects changed components on pushes to the default
branch and pull requests. Desktop-only and documentation-only changes skip the
Android and Relay builds. Manual dispatch selects `all`, `native`, or `android`.
Selected builds produce Relay/Gateway for Linux/macOS amd64/arm64 or Herdr Pocket
for three Android ABIs. Existing Android signing and publication checks remain in
place. This workflow does not deploy or publish releases. Android/native build
artifacts expire after **one day**; Windows desktop ZIPs and Linux desktop packages expire after **seven days**.

## 日常 CI 与深度检查

- `check` 按改动范围运行 Go 格式/vet/测试、Shell 与打包脚本测试、或前端 lint/类型检查/单测/build。只改桌面 UI 时不启动这些服务端/网页任务。
- 完整竞态检测、journald 验收、浏览器、多平台发布包、手机测试分片与 Android/iOS 模拟器改为 **Actions → check → Run workflow → full=true**。测试内容和发布验收标准没有删减。
- 新提交取消相同分支/PR 尚未完成的旧 CI，保留最新提交。正式发布依然使用完成且成功的精确构建；上游 `v*` 发布要求成功的 `check · deep`。
- `build-artifacts` 自动构建发生变化的组件；同时发布 Android 与 Native 时，可手动选择 `all` 从同一提交生成两类产物。
- Windows 客户端由独立的 `desktop-windows` 自动测试并打包；Linux x86_64 使用 `desktop-linux`，验证包内终端库、GTK/PDF/独立钥匙环，以及无 Gateway 的真实 Relay 本机配对/重连后生成 `.deb` 和用户安装压缩包。两者不发布或部署，下载入口见 [桌面 CI](../desktop/README.md#自动构建)。

## Release 发布：只走 GitHub Actions

**以后 Android / Native Release 一律使用 `publish-components`，不在本机编译、重签或下载后再上传。**
`build-artifacts` 负责构建和固定签名，`publish-components` 复用一次已经成功的构建。
它不会重新跑 Gradle/Go 构建，也不读取签名私钥；大文件只在 GitHub runner 和 GitHub 之间传输。
每次 push 仍只构建，不会擅自发布新版本。

1. 修改需要发布的组件版本，commit/push 原分支，等对应 `build-artifacts` 成功；需同时取得两个组件时手动选择 `components=all`。
2. 在 **Actions → publish-components → Run workflow** 填入这个构建的 run ID。
3. 先保持 `publish=false` 进行只读验证；确认成功后，用同一 run ID 设 `publish=true` 发布。
   两个组件都变更选 `both`，只更新 App 选 `android`，只更新服务端选 `native`。

也可仅在本机发起 CI（不会在本机传 APK）：

```bash
gh workflow run publish-components.yml --ref feat/android-file-companion \
  -f build_run_id=RUN_ID -f components=both -f publish=false
# 上面的只读演练成功后，再明确发布：
gh workflow run publish-components.yml --ref feat/android-file-companion \
  -f build_run_id=RUN_ID -f components=both -f publish=true
```

- 只接受本仓库可信分支的成功 `build-artifacts` push/manual run；拒绝 PR、fork、失败/未完成的构建及过期产物。
- 只读 job 核对官方 Actions ZIP digest、SHA-256、精确源码/版本、六个 APK 的 ABI/原证书，并对 Native 完整包做隔离启动/配对测试。
- 后续独立 job 才持有 Release 写权限；不执行归档中的程序、不构建、不签名。两个组件先完整上传并核验草稿，再发布；Android 为 Latest，Native 不抢 Latest。
- 标签必须指向构建的精确 commit。同名已发布版本只允许同源码、同文件、同散列的幂等检查，**不覆盖旧附件、不移动标签、不重传已存在文件**。版本冲突必须先升版本号。
- 发布完成后，同一 CI 自动更新 `VERSION.json`、根 README/Android README 的下载区，以及部署指南，并提交到现有分支。README 开头的 1×4 截图区保持原样。
- 文档更新失败会让发布 job 报错，不会默默算作完成。若并发提交使 push 无法 fast-forward，重新运行同一发布即可核验已有 Release 并补文档，绝不 force-push。
- 发布不会 SSH、部署或重启 Relay/Gateway/Herdr/Agent；独立完整 `check` 的状态会写入发布说明，不把构建通过冒充设备验收全绿。
- `android-*` / `native-*` 是本 fork 的组件 Release；上游 `v*` 插件 Release 和网页升级基线仍走原有流程。

## Relay / Gateway artifacts

- `native-bundles-linux_amd64`: installable Relay and Gateway tarballs plus
  `SHA256SUMS.txt`, produced by `scripts/package-native-release.sh`. Includes the
  Relay web bundle, config/systemd templates, initialization tools, deployment
  notes and per-component `BUILD-INFO.json` with source revision and binary hash.
- Before upload, `scripts/check-native-release.py` checks checksums, static ELF
  binaries, build metadata and starts both components in an isolated loopback
  smoke test, including pairing. It never touches a live Relay, Gateway or Herdr.
- `native-binaries-linux_amd64`, `native-binaries-linux_arm64`,
  `native-binaries-darwin_amd64`, `native-binaries-darwin_arm64`: zip files of the
  two executables, build metadata and hashes only. They are **not** complete
  deployment bundles. Linux/WSL alone advertises primary-session-binding V1;
  macOS builds do not imply equivalent process-proof support.

Download these from **Actions → build-artifacts → successful run → Artifacts**.
Use the exact commit you intend to deploy. For a fresh Linux amd64 installation,
prefer the full bundle and follow `docs/native-deployment.md`. Preserve existing
configuration and pairing data on upgrades. A green artifact job is not a claim
that all repository checks have passed.

`VERSION.json` records source versions separately from the previously published
`android_release` and `native_release` provenance. A source version bump does not
create a Release or rewrite historical asset names.

## Promoting CI output to a GitHub Release

Actions artifacts are not Release attachments. A release must explicitly copy a
successful run's **`android-apks-signed`** files into a new Release; no local APK
build is necessary. Preserve the CI bytes, manifest,
and checksums, and point the release tag at that run's exact source commit.
Never substitute `android-apks-ci` or an unrelated run's artifacts.

`android-v0.2.24` and `native-v0.23.5-codexfix.12` were promoted from successful
run `41811754288`, commit `e5efde31c469eb51a109428b2e2a8cec25fe29cd`.
The Android release preserves all six signed APKs, BUILD-INFO and checksums;
the native release preserves the full Linux amd64 Relay/Gateway archives and
checksums. Archive digests and all release asset digests were checked before
publication. Android remains Latest; Gateway stays at codexfix.6.

This is the component-specific `android-*` / `native-*` release lane, not an
upstream `v*` plugin promotion. Its package format is not a compatible input to
`tests/mobile/baselines.json`; the existing upstream web upgrade baselines are
unchanged. Successful component builds do not certify the whole `check` workflow
or Android/iOS Appium upgrade acceptance. See `herdr-android/VALIDATION.md`.

`android-v0.2.23` was promoted from run `41778780951`, commit
`2789f9165741e3249f983a2e1c95dfa82177f5d8`. It includes all six APKs plus
`BUILD-INFO.json` and `SHA256SUMS.txt`. The Release attachments remain available
independently of the one-day Actions artifact retention. The build workflow still
does not publish a Release on every push.

## Which APK can update an existing installation?

| Artifact | Contents | Existing Pocket installation |
| --- | --- | --- |
| `android-apks-ci` | Debug APKs ending in `-ci.apk`, Release APKs ending in `-unsigned.apk` | Not an upgrade package: Debug uses the runner's temporary key; Release is unsigned. |
| `android-apks-signed` | Debug and Release APKs, `BUILD-INFO.json`, `SHA256SUMS.txt` | Both build types use the existing Pocket certificate. Select the matching device ABI and do not downgrade the versionCode. |

The fixed signing job runs only for a **push or manual workflow dispatch on
`feat/android-file-companion` in `cxnaive/herder_relay_extra`**. This is the
existing default branch, not an additional signing branch. PRs, other branches,
tags, and forks do not run that job or receive its secrets. Routine `check` runs
only affected components; full validation is an explicit manual dispatch.

The expected certificate SHA-256 is recorded in `VERSION.json`:

```text
fe1ddf4d04f46d45168d53ca25dd2b379f242b6cd9b23169c9717258cf5232b9
```

This preserves the existing **debug certificate** used by released Pocket APKs;
a Release *build type* does not change that signing identity or turn it into a
Play Store release. Do not generate a replacement key to fix a CI failure, and
do not uninstall an existing app merely to install a differently signed build:
that can lose pairing and drafts.

## Secret storage and verification

The GitHub Actions environment **`android-signing`** must allow deployments only
from the branch `feat/android-file-companion` (no wildcard or tag policy). Its
four encrypted environment secrets are:

- `HERDR_ANDROID_KEYSTORE_BASE64`: base64 of the existing signing keystore.
- `HERDR_ANDROID_KEYSTORE_PASSWORD`: its store password.
- `HERDR_ANDROID_KEY_ALIAS`: the existing key alias.
- `HERDR_ANDROID_KEY_PASSWORD`: that key's password.

Never commit these values, put them in repository variables, or upload the
keystore as an artifact. Retain a private backup of the original key separately.
The job's branch/event restriction and the environment's deployment policy must
both remain in place; do not replace them with `pull_request_target` or sign
artifacts taken from another workflow run.

Gradle builds and tests **without** the fixed key. A separate, environment-bound
job downloads APKs from the same run and then:

1. Checks all six input APKs, their versions, ABIs, and recorded hashes.
2. Decodes the keystore in a private temporary directory outside the checkout.
3. Signs every Debug and Release APK with `apksigner`, using password environment
   variables rather than password command arguments.
4. Verifies each APK and requires exactly the expected signing certificate.
5. Publishes the complete directory only after every APK passes, then removes
   the temporary key. An error does not fall back to a new Debug key or publish
   a partially signed artifact set.

`BUILD-INFO.json` records the commit, APK hashes, build types, ABIs, and public
certificate fingerprint. `SHA256SUMS.txt` covers the APKs and that manifest.
Neither contains a private key or password.

## Mobile device CI source consistency

The web/PWA device lane uses a separate Appium harness under `tests/mobile/`.
Its launchers, pinned producer manifests, runtime helpers, evidence readers and
unit registry must match `.github/actions/mobile-device-run/action.yml`; copying
an older source snapshot must not leave newer workflows pointing at missing files.

The November 4, 2026 failures in check run `41785144588` were setup failures:
the Android emulator lifetime observer and the iOS Appium integration had been
removed while the composite action still invoked them. The compatible harness
is restored from `d07403b246a2220c3447dcd84084d7d717140361`, the source revision of
the unchanged device workflow. The fork's `0.23.4` / `0.23.3` upgrade baselines,
default-branch triggers and fixed-signing isolation checks are retained.

`make mobile-composite-check` now also checks literal repository script
entrypoints in composite run steps, including both platform branches. Missing
scripts fail the common host check before emulator/simulator provisioning.
This does not skip the device tests, weaken product assertions or turn failed
qualification into success. Local fixture/installer checks do not replace the
hosted Android and iOS upgrade runs.

## Local build and tests

The wrapper is pinned to **Gradle 8.14.6** with its official distribution SHA-256.
This keeps the build on the maintained 8.x patch line while retaining Android
Gradle Plugin 8.13.0, Kotlin Gradle Plugin 2.2.20, and JDK 21. The wrapper JAR and
launch scripts are generated together, not just the distribution URL.

`setup-gradle` classifies every previous-major release as “out of date”, including
maintained 8.14.x patches. Its informational notice can therefore remain with
8.14.6; this is not a build failure. Moving to Gradle 9.x is a separate plugin
compatibility migration, not a reason to disable that notice or blindly set
`gradle-version: current` in CI.

A normal local `assembleDebug` still produces the universal `app-debug.apk`.
CI opts into per-ABI builds explicitly:

```bash
cd herdr-android
./gradlew -PsplitApks=true testDebugUnitTest assembleDebug assembleRelease assembleDebugAndroidTest lintDebug
cd ..
python3 -m unittest discover -s scripts/tests -p test_android_artifacts.py
python3 scripts/android-artifacts.py collect herdr-android/app/build/outputs/apk dist/android-ci
```

Release signing stays in the protected CI signing job. Local development builds
are not promotion inputs. Offline packaging/signing tests use synthetic files
and a fake signer, never a personal keystore.
