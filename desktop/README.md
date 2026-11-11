# Herdr Link · Desktop

Windows / Linux / macOS 原生桌面客户端，使用同一套应用代码。通过 Relay 连接多台 Herdr 电脑，提供对话、审批、交互终端和工作区文件面板。

当前版本 **0.3.2 开发预览**。系统结构与服务端安装见[项目首页](../README.md)。

<!-- desktop-downloads:start -->
## 下载

**[Herdr Link v0.3.3 · Desktop](https://github.com/cxnaive/herdr_link/releases/tag/v0.3.3)**

| 系统 | 安装方式 |
| --- | --- |
| Windows x86_64 | [便携 ZIP](https://github.com/cxnaive/herdr_link/releases/download/v0.3.3/herdr-link-desktop-0.3.3-windows-amd64.zip)，解压运行 `Herdr Desktop.exe` |
| Debian / Ubuntu x86_64 | [Debian 包](https://github.com/cxnaive/herdr_link/releases/download/v0.3.3/herdr-link-desktop-0.3.3-linux-amd64.deb)，使用 `sudo apt install ./文件名.deb` |
| Linux x86_64 用户安装 | [归档](https://github.com/cxnaive/herdr_link/releases/download/v0.3.3/herdr-link-desktop-0.3.3-linux-amd64.tar.gz) + [安装脚本](https://github.com/cxnaive/herdr_link/releases/download/v0.3.3/herdr-link-desktop-0.3.3-linux-amd64-install.sh) |
| Debian / Ubuntu ARM64 | [Debian 包](https://github.com/cxnaive/herdr_link/releases/download/v0.3.3/herdr-link-desktop-0.3.3-linux-arm64.deb)，使用 `sudo apt install ./文件名.deb` |
| Linux ARM64 用户安装 | [归档](https://github.com/cxnaive/herdr_link/releases/download/v0.3.3/herdr-link-desktop-0.3.3-linux-arm64.tar.gz) + [安装脚本](https://github.com/cxnaive/herdr_link/releases/download/v0.3.3/herdr-link-desktop-0.3.3-linux-arm64-install.sh) |
| macOS 12+ · Apple Silicon / Intel | [Universal DMG](https://github.com/cxnaive/herdr_link/releases/download/v0.3.3/herdr-link-desktop-0.3.3-macos-universal.dmg)，将 App 拖入 Applications |

Windows / Linux 桌面未签名；macOS 为 ad-hoc 签名预览包，未经 Developer ID 签名或 Apple 公证，首次打开可能需在系统设置中确认；Linux 需要 GTK 3、WebKitGTK 4.1、libsecret 和已解锁的 Secret Service。各端共享项目版本，不表示所有平台已完成同等范围的验证。
<!-- desktop-downloads:end -->

## Windows

从上方统一 Release 下载 Windows 便携 ZIP，按同一 Release 的校验和核对后解压。

运行 `Herdr Desktop.exe`，保持 `ghostty-vt.dll` 与程序在同一目录。当前是未签名便携包；PDF 预览需要系统 WebView2。

## Linux x86_64 / ARM64

支持 amd64 与 arm64。运行 `uname -m`：`x86_64` 选择 amd64，`aarch64` 选择 arm64。以下安装命令以 amd64 为例，ARM64 用户替换文件名中的架构；源码构建脚本自动选择本机架构。

从上方统一 Release 下载 Linux 安装文件和 `SHA256SUMS.txt`，执行 `sha256sum --ignore-missing -c SHA256SUMS.txt`。

**Debian / Ubuntu：**

```sh
sudo apt install ./herdr-link-desktop-0.3.3-linux-amd64.deb
herdr-desktop
```

**免 root 用户安装：**

```sh
# 安装脚本和归档放在同一目录。
sh herdr-link-desktop-0.3.3-linux-amd64-install.sh herdr-link-desktop-0.3.3-linux-amd64.tar.gz
~/.local/bin/herdr-desktop
# 卸载程序但保留配对与草稿：sh herdr-link-desktop-0.3.3-linux-amd64-install.sh --uninstall
```

需要 **GTK 3、WebKitGTK 4.1、libsecret 和已解锁的 Secret Service**，例如 GNOME Keyring。`.deb` 声明库依赖，钥匙环仍需在桌面会话中运行；KWallet / KeePassXC 需启用 Secret Service。

缺少钥匙环时不会降级为明文保存。纯 SSH、无图形会话的服务器不适合运行本客户端。Linux CI 验证环境为 Ubuntu 24.04 x86_64 / ARM64；其他发行版和不同桌面环境不视为已验证。

## macOS

0.3.2 增加 **macOS 12+ Universal DMG 预览包**，同一应用包含 Apple Silicon 和 Intel。正式发布完成后使用上方下载区；日常构建在 `desktop-macos` Actions 的 `herdr-desktop-macos-universal-<运行编号>` 附件中，保留 7 天。

1. 核对同一发布的 `SHA256SUMS.txt`：Mac 可用 `shasum -a 256 文件名.dmg` 对照。
2. 打开 DMG，将完整的 `Herdr Desktop.app` 拖到 **Applications（应用程序）**，弹出磁盘映像后启动；无需安装 Go 或 Xcode。
3. 这是 **ad-hoc 签名、未使用 Developer ID、未经 Apple 公证**的预览包。首次打开若被阻止，仅在确认下载来源可信且校验一致后，按系统提示进入“系统设置 → 隐私与安全性 → 仍要打开”。企业管理策略可能禁止例外；不要关闭全局安全检查。
4. 每个客户端用自己的 `herdr-tcp://` 邀请配对；安装包不包含任何用户凭据。覆盖升级前退出旧 App，保留 Keychain 与应用数据。

CI 在 macOS 15 的 Apple Silicon / Intel 原生 runner 上运行测试，并安装验证**同一 DMG**的 App 启动、终端和 PDF；最低系统版本声明为 12，旧系统及完整人工交互仍需额外验收。此包是远程客户端，不代表 Mac 本机 Relay 发现或 Codex 进程绑定获得支持。

## 配对与使用

1. 在电脑上使用对应 Relay 的 `run.sh tcp-pair '我的桌面'` 生成新邀请。
2. 在客户端点击“添加电脑”，粘贴 `herdr-tcp://` 邀请并命名。
3. 左侧选择电脑、项目与 Agent；中间切换“对话 / 终端”，右侧打开工作区文件。

电脑与项目支持展开／折叠。可管理 Agent、发送消息、处理审批、查看 Markdown / 工具调用、上传附件及下载文件；操作权限由 Relay 的设备角色和当前目标验证决定。

配对信息与草稿加密保存在用户配置目录的 `Herdr Desktop/state.enc`。加密密钥保存在 Windows Credential Manager、macOS Keychain 或 Linux Secret Service。不要导入手机或 Relay 主密钥替代独立配对。

### Windows / Linux 连接本机 Relay

“添加电脑 → 查找本机 Relay → 连接本机”可以发现当前用户的本机实例。首次空列表会查找，选择实例后才配对。

Windows 客户端同时列出 **Windows 原生 Relay** 和**已运行的 WSL 发行版中的 Relay**，并标明来源。原生 Relay 需更新到支持本机发现的版本，客户端与 Relay 使用同一 Windows 用户。WSL 使用各发行版的默认 Linux 用户发现实例；发行版内需安装 Python 3.9+，无需额外 Python 包。只扫描已运行的发行版，不负责启动 WSL 或 Relay。

本机连接走回环 E2EE WebSocket，不经过 Gateway。Windows 原生通过核对服务进程及用户的命名管道申请独立设备授权；Linux / WSL 核对私有配对 socket 和进程身份。不读取 `relay.env` 或设备库，不提取主密钥。已配对同一 Relay 时保留电脑身份、草稿与角色。

WSL 连接通过隐藏的 `wsl.exe` 辅助进程在选定发行版内建立回环通道，不依赖 Windows 到 WSL 的 localhost 转发、WSL IP 或额外监听端口。多个发行版使用相同端口也不会互相覆盖。保存的连接绑定发行版、Linux 用户与 Relay 路径；Relay 重启后复用原凭据，WSL 停止时提示离线。

需要同一用户、可读取其 `/proc` 元数据、可信私有配对 socket 和回环地址服务。容器跨用户 / PID 空间、其他用户的 Relay 或未提供配对接口的版本不会自动连接；仍可使用远程邀请。凭据被撤销后不会静默重新授权。

### 对话历史与阅读位置

首次打开对话默认定位到最近消息；显示缓存后同步新历史，若仍在底部就继续跟随最新消息。主动向上滚动阅读、展开工具详情或加载更早消息时保留位置，不被后台刷新拉回底部。

“回到最新”跳到当前已加载对话末尾，并清除消息搜索过滤；不重新下载历史，离线时也能使用。历史恢复深度与阅读位置是两回事：恢复此前加载过的长历史，不代表默认跳到最早消息。

### 终端与外观

桌面交互终端需要此分支的 Relay，以及支持私有终端协议 **22** 的 Herdr。客户端检查 `desktop_terminal_v1` 能力；附加／断开终端不会结束 Agent，也不会强行夺取其他控制者的输入权。

侧栏底部 **外观 → 应用主题** 可切换“跟随系统、柔和浅色、柔和深色”。0.3.3 还提供与 Android 同款的 **薄荷深色、午夜蓝、石墨紫、云白蓝、暖纸棕**：前三套为深色，后两套为浅色，Windows / Linux / macOS 共用。

选择会立即生效并保存在本机；不重连、不清除草稿、待确认发送或操作状态，也不改远端配置。原有选择保持不变，默认仍跟随系统。

**终端配色** 独立选择“柔和深色、柔和浅色、跟随应用”，默认深色。终端对比度增强与 Codex 浅色背景柔化只影响本机显示，不修改远端输出。

默认文件范围是当前 Agent 工作区；控制者可明确授权更大的只读范围，详见[文件访问](../docs/file-access.md)。

## 源码构建

需要 **Go 1.27.3**；MyGo **v0.8.1** 和终端库版本固定，构建工具负责下载并验证终端库。

**Windows（PowerShell）：**

```powershell
cd desktop
./scripts/build.ps1 -Platform windows/amd64
& './build/windows-amd64/Herdr Desktop.exe'
```

**Linux：**

Ubuntu 24.04 的运行依赖可先安装（其他发行版包名可能不同）：

```sh
sudo apt install libgtk-3-0t64 libwebkit2gtk-4.1-0 libsecret-1-0 gnome-keyring
```

```sh
cd desktop
./scripts/build-linux.sh
python3 scripts/package-linux.py
# 安装包：build/artifacts-linux-amd64/ 或 build/artifacts-linux-arm64/
```

**macOS universal：**

```sh
cd desktop
./scripts/build-macos.sh
python3 scripts/package-macos.py
```

对生成的可执行文件加 `--demo` 可使用虚构数据查看界面，不读取真实配对。构建输出位于 `desktop/build/`；运行时保留同目录资源。

在 Mac 上运行上述脚本会生成 `build/darwin-universal/Herdr Desktop.app` 和 `.dmg`；收集后的 DMG、元数据与校验和位于 `build/artifacts-macos-universal/`。构建时使用 ad-hoc 签名，不需要 Developer ID 密钥，也不进行公证。

### 测试

在 `desktop/` 中先构建终端库，再运行 `go test ./...` 和 `go vet ./...`。Linux 可指定：

```sh
MYGO_GHOSTTY_VT="$PWD/build/linux-$(go env GOHOSTARCH)/libghostty-vt.so" go test ./...
python3 scripts/test_package_linux.py
./scripts/smoke-linux.sh
```

图形 smoke test 还需 Xvfb、D-Bus、GNOME Keyring、xdotool 和 ImageMagick，依赖配置见 [Linux CI](../.github/workflows/desktop-linux.yml)。CI 下载包为预览产物，保留时间有限；构建不会自动发布 Release 或部署 Relay。
