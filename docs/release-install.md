# Herdr Link @VERSION@ 安装说明

每台运行 Herdr 的电脑安装 Relay，公网服务器安装 Gateway，手机或桌面安装 Client。所有下载属于同一项目版本；运行时服务名和应用标识保留，便于已有安装升级。

## 校验

从同一个 Release 下载所需文件与 `SHA256SUMS.txt`：

```sh
sha256sum --ignore-missing -c SHA256SUMS.txt
```

Windows 可用 PowerShell `Get-FileHash 文件名 -Algorithm SHA256` 与清单对照。

## 选择 Linux 架构

运行 `uname -m`：`x86_64` 选择 `amd64`，`aarch64` 选择 `arm64`。Relay、Gateway 和 Linux 桌面均提供这两种架构；请勿混用安装包。

以下命令以 amd64 为例，ARM64 系统将文件名和解压目录中的 `amd64` 换成 `arm64`。

## Gateway / Relay（Linux x86_64 / ARM64）

```sh
tar -xzf herdr-link-gateway-@VERSION@-linux-amd64.tar.gz
tar -xzf herdr-link-relay-@VERSION@-linux-amd64.tar.gz
```

归档顶层目录保留运行时组件名称：`herdr-gateway_@VERSION@_linux_amd64` 与 `herdr-mobile-relay_@VERSION@_linux_amd64`。按各包的 `DEPLOYMENT.md` 与 `GATEWAY-MANAGEMENT.md` 安装。

Gateway 只需开放业务端口 TCP 8377，管理页通过 SSH 转发。Relay 与 Herdr 使用同一普通用户，首次通过 `run.sh gateway-join` 接入，再用 `run.sh tcp-pair '设备名'` 生成客户端邀请。

已有安装保留配置、主密钥、设备库与 Gateway 管理库；只重启对应服务，不重启 Herdr 或 Agent。不重复初始化、不删除旧设备数据。

## Android

普通手机安装 `herdr-link-android-@VERSION@-arm64-v8a.apk`；其他架构按附件名选择。Android 9 及以上。直接覆盖原 Pocket，保留包名、签名与数据；无需卸载。

## Windows

解压 `herdr-link-desktop-@VERSION@-windows-amd64.zip`，运行 `Herdr Desktop.exe`，保持 `ghostty-vt.dll` 在同一目录。程序未签名，PDF 预览需要系统 WebView2。

## Linux 桌面

Debian / Ubuntu：

```sh
sudo apt install ./herdr-link-desktop-@VERSION@-linux-amd64.deb
herdr-desktop
```

免 root 用户安装：

```sh
sh herdr-link-desktop-@VERSION@-linux-amd64-install.sh herdr-link-desktop-@VERSION@-linux-amd64.tar.gz
~/.local/bin/herdr-desktop
```

需图形桌面、GTK 3、WebKitGTK 4.1、libsecret 和已解锁的 Secret Service。安装器仍使用原 `herdr-desktop` 命令与数据目录。卸载程序可运行同一安装脚本加 `--uninstall`，保留配对数据。

## 配对

在 Client 中“添加电脑”，粘贴 Relay 生成的 `herdr-tcp://` 邀请。每个 Client 独立配对。Gateway 的 `herdr-gateway://` 接入码是给电脑 Relay 的，不要填入 Client。

Linux 同用户的本机 Relay 可用“查找本机 Relay”连接。macOS 当前没有签名／公证安装包。
