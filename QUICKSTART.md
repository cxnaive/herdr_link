# 快速开始

目标：让手机或桌面客户端连接运行 Herdr 的电脑。每台电脑部署一个 Relay，公网服务器部署一个 Gateway。

## 已有正在运行的 Relay

在该电脑上，以运行 Herdr 的同一用户执行：

```sh
~/.local/share/herdr-pocket-relay/current/run.sh tcp-pair '我的手机'
```

命令通过正在运行的 Relay 创建限时、单设备邀请。**不要再启动第二个 Relay，也不要重新初始化配置。**
如果原来的安装路径不同，使用实际的 `run.sh`，并指定服务正在使用的配置：

```sh
HERDR_RELAY_ENV=/绝对路径/relay.env /实际安装目录/run.sh tcp-pair '我的桌面'
```

在客户端选择“添加电脑”，扫码或粘贴 `herdr-tcp://` 邀请，填写电脑名称。每个新客户端应单独生成邀请。

Linux 客户端连接同一用户的本机 Relay 时，可直接选择“查找本机 Relay → 连接本机”，不需要复制邀请。

## 首次部署

### 1. 准备组件

- 运行 Agent 的电脑：已安装并运行 Herdr，Relay 使用同一个普通用户。
- 公网服务器：安装 Gateway，开放 **TCP 8377**；SSH 另作维护。
- 手机或桌面：安装客户端，见 [README 下载区](README.md#下载-android-apk) 和 [桌面安装](desktop/README.md)。

安装包与校验方法见[部署指南](docs/native-deployment.md)。Gateway 管理式接入需要 Herdr Link `0.3.0` 或更新版本；下载时核对版本，不要拿不含此功能的旧发布包执行新命令。

### 2. 部署 Gateway

按 [Gateway 首次安装](docs/gateway-management.md#首次安装-gateway)安装并初始化管理员密钥。
通过 SSH 转发打开管理页，生成一个以电脑名称标记的接入码。

### 3. 接入电脑

按[电脑 Relay 安装](docs/native-deployment.md#安装电脑-relay)准备配置和用户服务。接入时执行：

```sh
~/.local/share/herdr-pocket-relay/current/run.sh gateway-join
# 按提示粘贴 herdr-gateway:// 接入码，然后按 Enter。
systemctl --user enable --now herdr-mobile-relay.service
```

Gateway 接入码用于给电脑签发独立注册凭据，**不能粘贴到手机 App**。旧 Relay 迁移须用管理页中该电脑的“重新接入”，见[迁移已有电脑](docs/gateway-management.md#迁移已有电脑)。

### 4. 配对客户端

确认 Relay 健康后，执行本页开头的 `tcp-pair`，再在客户端添加电脑。以后连接其他电脑时，重复接入和配对流程即可。

## 确认连接正常

```sh
systemctl --user is-active herdr-mobile-relay.service
curl -fsS http://127.0.0.1:8375/healthz
```

应看到 `readiness: "ready"`、`gateway.registered: true`，并确认客户端能列出该电脑的工作空间和 Agent。若服务使用自定义端口，替换上面的 `8375`。

- 注册失败：检查 Gateway 地址、电脑是否被禁用／撤销、连接数量限制。
- 没有 Agent：检查 Relay 用户、Herdr 可执行文件和 socket 路径。
- 历史无法确认：先查看会话绑定诊断，不要删除设备库或重新配对来“修复”历史。
- 更新版本：按[升级步骤](docs/native-deployment.md#升级已有安装)保留现有配置与配对。

更多诊断见[部署指南](docs/native-deployment.md#排障)。旧版 Gateway 仍可使用[静态白名单接入](docs/native-deployment.md#旧版静态白名单接入)。
