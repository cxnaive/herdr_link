# Herdr Link · Desktop

Windows / Linux / macOS 原生桌面客户端，使用同一套应用代码。通过 Relay 连接多台 Herdr 电脑，提供对话、审批、交互终端和工作区文件面板。

当前版本 **0.3.1 开发预览**。系统结构与服务端安装见[项目首页](../README.md)。

<!-- desktop-downloads:start -->
## 下载

**[Herdr Link v0.3.0 · Desktop](https://github.com/cxnaive/herdr_link/releases/tag/v0.3.0)**

| 系统 | 安装方式 |
| --- | --- |
| Windows x86_64 | [便携 ZIP](https://github.com/cxnaive/herdr_link/releases/download/v0.3.0/herdr-link-desktop-0.3.0-windows-amd64.zip)，解压运行 `Herdr Desktop.exe` |
| Debian / Ubuntu x86_64 | [Debian 包](https://github.com/cxnaive/herdr_link/releases/download/v0.3.0/herdr-link-desktop-0.3.0-linux-amd64.deb)，使用 `sudo apt install ./文件名.deb` |
| Linux x86_64 用户安装 | [归档](https://github.com/cxnaive/herdr_link/releases/download/v0.3.0/herdr-link-desktop-0.3.0-linux-amd64.tar.gz) + [安装脚本](https://github.com/cxnaive/herdr_link/releases/download/v0.3.0/herdr-link-desktop-0.3.0-linux-amd64-install.sh) |
| macOS | 暂无签名、公证的下载包，使用下方源码构建路径 |

桌面程序目前未签名；Linux 需要 GTK 3、WebKitGTK 4.1、libsecret 和已解锁的 Secret Service。各端共享项目版本，不表示所有平台已完成同等范围的验证。
<!-- desktop-downloads:end -->

## Windows

从上方统一 Release 下载 Windows 便携 ZIP，按同一 Release 的校验和核对后解压。

运行 `Herdr Desktop.exe`，保持 `ghostty-vt.dll` 与程序在同一目录。当前是未签名便携包；PDF 预览需要系统 WebView2。

## Linux x86_64 / ARM64

支持 amd64 与 arm64。运行 `uname -m`：`x86_64` 选择 amd64，`aarch64` 选择 arm64。以下安装命令以 amd64 为例，ARM64 用户替换文件名中的架构；源码构建脚本自动选择本机架构。

从上方统一 Release 下载 Linux 安装文件和 `SHA256SUMS.txt`，执行 `sha256sum --ignore-missing -c SHA256SUMS.txt`。

**Debian / Ubuntu：**

```sh
sudo apt install ./herdr-link-desktop-0.3.0-linux-amd64.deb
herdr-desktop
```

**免 root 用户安装：**

```sh
# 安装脚本和归档放在同一目录。
sh herdr-link-desktop-0.3.0-linux-amd64-install.sh herdr-link-desktop-0.3.0-linux-amd64.tar.gz
~/.local/bin/herdr-desktop
# 卸载程序但保留配对与草稿：sh herdr-link-desktop-0.3.0-linux-amd64-install.sh --uninstall
```

需要 **GTK 3、WebKitGTK 4.1、libsecret 和已解锁的 Secret Service**，例如 GNOME Keyring。`.deb` 声明库依赖，钥匙环仍需在桌面会话中运行；KWallet / KeePassXC 需启用 Secret Service。

缺少钥匙环时不会降级为明文保存。纯 SSH、无图形会话的服务器不适合运行本客户端。Linux CI 验证环境为 Ubuntu 24.04 x86_64 / ARM64；其他发行版和不同桌面环境不视为已验证。

## macOS

目前需[源码构建](#源码构建)，没有已签名、公证的安装包。构建目标 `darwin/universal` 包含 Intel 和 Apple silicon；签名、公证和实际分发需在 Mac 完成。

## 配对与使用

1. 在电脑上使用对应 Relay 的 `run.sh tcp-pair '我的桌面'` 生成新邀请。
2. 在客户端点击“添加电脑”，粘贴 `herdr-tcp://` 邀请并命名。
3. 左侧选择电脑、项目与 Agent；中间切换“对话 / 终端”，右侧打开工作区文件。

电脑与项目支持展开／折叠。可管理 Agent、发送消息、处理审批、查看 Markdown / 工具调用、上传附件及下载文件；操作权限由 Relay 的设备角色和当前目标验证决定。

配对信息与草稿加密保存在用户配置目录的 `Herdr Desktop/state.enc`。加密密钥保存在 Windows Credential Manager、macOS Keychain 或 Linux Secret Service。不要导入手机或 Relay 主密钥替代独立配对。

### Linux 连接本机 Relay

“添加电脑 → 查找本机 Relay → 连接本机”可以发现同一 Linux 用户下的本机实例。首次空列表会查找，但不会自动授权。

本机连接走回环 E2EE WebSocket，不经过 Gateway。它通过 Relay 的私有配对 socket 申请独立设备授权，不读取 `relay.env` 或设备库，不提取主密钥。已配对同一 Relay 时保留电脑身份、草稿与角色。

需要同一用户、可读取其 `/proc` 元数据、可信私有配对 socket 和回环地址服务。容器跨用户 / PID 空间、其他用户的 Relay 或未提供配对接口的版本不会自动连接；仍可使用远程邀请。凭据被撤销后不会静默重新授权。

### 终端与外观

桌面交互终端需要此分支的 Relay，以及支持私有终端协议 **22** 的 Herdr。客户端检查 `desktop_terminal_v1` 能力；附加／断开终端不会结束 Agent，也不会强行夺取其他控制者的输入权。

“外观”可分别设置应用和终端的深浅色。终端对比度增强与 Codex 浅色背景柔化只影响本机显示，不修改远端输出。

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
go tool mygo build -platform darwin/universal -skip-installer -skip-dmg -skip-notarize
```

对生成的可执行文件加 `--demo` 可使用虚构数据查看界面，不读取真实配对。构建输出位于 `desktop/build/`；运行时保留同目录资源。

### 测试

在 `desktop/` 中先构建终端库，再运行 `go test ./...` 和 `go vet ./...`。Linux 可指定：

```sh
MYGO_GHOSTTY_VT="$PWD/build/linux-$(go env GOHOSTARCH)/libghostty-vt.so" go test ./...
python3 scripts/test_package_linux.py
./scripts/smoke-linux.sh
```

图形 smoke test 还需 Xvfb、D-Bus、GNOME Keyring、xdotool 和 ImageMagick，依赖配置见 [Linux CI](../.github/workflows/desktop-linux.yml)。CI 下载包为预览产物，保留时间有限；构建不会自动发布 Release 或部署 Relay。
