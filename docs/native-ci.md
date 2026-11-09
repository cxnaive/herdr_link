# Herdr Link 构建与发布

项目采用一个标准语义版本和一个 Release 页面。Relay、Gateway、Android、Desktop 的版本在 `VERSION.json` 中保持一致；不再发布零散的 Android / Native 修复版。

## 日常 CI

- `check.yml` 按变更组件执行 Go、Shell / 打包工具或前端检查。
- `build-artifacts.yml` 自动构建受影响的 Relay / Gateway 和 Android，不发布或部署。
- `desktop-windows.yml` / `desktop-linux.yml` 在相关变更时验证并生成平台预览包，也可被统一发布构建复用。
- 完整 race、浏览器和设备资格验证手动运行 `check.yml`，设置 `full=true`，不强制每次 push 都运行。

## 准备版本

1. `VERSION.json` 中 `project.version` 和四个组件版本统一为 `MAJOR.MINOR.PATCH`，并更新 `desktop/mygo.json`、Android `versionName`。
2. Android `versionCode` 严格递增，不更换包名、签名证书或设备数据目录。
3. 编写 `docs/releases/<版本>.md`：变化、下载选择、升级、支持范围。不要把调试日记放入发布说明。
4. 更新 `docs/release-install.md` 中通用安装步骤，版本使用 `@VERSION@` 占位符。
5. commit / push 到现有默认分支。发布不要求新建分支。

## CI 构建整套下载

```sh
gh workflow run build-artifacts.yml --ref feat/android-file-companion -f components=release
```

等待这次构建全部成功。它在**同一次 run、同一个源码提交**中构建：

- Linux x86_64 Relay / Gateway 完整包并执行隔离启动、注册和配对测试。
- Android 三种 ABI，使用现有固定签名。
- Windows 原生界面 / 终端测试与便携 ZIP。
- Linux 桌面包、GTK / 钥匙环 / PDF smoke test，以及真实本机 Relay 连接测试。

最后汇总为 `herdr-link-release` CI 附件。汇总只重命名并复制原制品字节，不编译或重新签名；发布下载统一使用 `herdr-link-<组件>-<版本>-<平台>`。Debug APK 留在 CI，不混入用户 Release。

制品包括组件包、`INSTALL.md`、`BUILD-INFO.json` 和 `SHA256SUMS.txt`。顶层元数据记录同一构建来源及各原始组件元数据。统一附件保留 7 天；常规 Android / Native 中间制品仍保留 1 天。

## 先预演，再发布同一构建

```sh
# BUILD_RUN_ID 是上一步成功的完整构建。
gh workflow run publish-components.yml --ref feat/android-file-companion \
  -f build_run_id=BUILD_RUN_ID -f publish=false

# 只读预演成功后，仍填写同一个 BUILD_RUN_ID。
gh workflow run publish-components.yml --ref feat/android-file-companion \
  -f build_run_id=BUILD_RUN_ID -f publish=true
```

Actions 中显示为 **publish-release**。预演核对可信分支／提交、CI ZIP digest、逐文件散列、APK 包名／版本／ABI／原证书及现有标签冲突。写权限 job 仅发布已验证字节，不持有签名密钥，也不执行构建或制品代码。

发布结果是 **Herdr Link vX.Y.Z**、标签 **vX.Y.Z**，包含全套下载并设为 Latest。发布器创建草稿、上传并校验全部附件后才公开。遇到已有标签或不同附件拒绝覆盖；不得 `--clobber`、移动标签或上传本地替代包。

发布后从回执自动更新根 README、Android / Desktop README 的下载区，以及 `VERSION.json` 中的统一 `release` 记录。系统框图和 Android 小节的 1×4 截图不变。**文档更新失败时，发布任务仍未完成。**

## 签名与旧安装

Android 签名只在可信分支的 `android-signing` 环境中执行，密钥不进入源码、Gradle 或上传制品。发布验证只需要公开证书指纹，不读取私钥。

继续使用四个既有环境 secrets：`HERDR_ANDROID_KEYSTORE_BASE64`、`HERDR_ANDROID_KEYSTORE_PASSWORD`、`HERDR_ANDROID_KEY_ALIAS`、`HERDR_ANDROID_KEY_PASSWORD`。不要为修复 CI 更换签名证书。

“Release APK”指构建类型，仍沿用 Pocket 原来的调试证书；不代表 Play 商店签名。桌面包目前未签名，macOS 暂无签名／公证 Release 包。安装边界必须在发布说明中如实注明。

## 旧版本清理

发布流程**不会自动删除**历史 Release。仅在用户明确要求时，先验证新 Release 与下载链接、保存旧 Release 元数据清单，再按已审核 ID 删除旧 Release 和附件。默认保留历史 Git 标签和源码；不要把清理变成每次发布都会执行的动作。

此 fork 的上游插件 `v*` 工作流已限制为上游仓库，避免与 Herdr Link 的项目标签重复发布。已有插件更新格式与本项目系统安装包不同。
