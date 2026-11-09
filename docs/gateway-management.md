# Gateway 管理

管理页用于管理**哪些电脑可以接入 Gateway**。它不读取对话正文、不安装远端 Relay，也不负责启动或停止 Agent。

需要 Herdr Link **0.3.0** 及以上的 Gateway 和 Relay。下载时核对版本，见 [服务端安装包](../README.md#服务端安装包)。

## 首次安装 Gateway

以下用于新的 Linux x86_64 服务器，使用解压后的完整 Gateway 包。已有安装不要覆盖配置，先看[迁移](#迁移已有电脑)。

### 安装文件与用户

在解压目录中执行，需要 Python 3 和 sudo：

```sh
set -eu
version=$(python3 -c 'import json; print(json.load(open("BUILD-INFO.json"))["version"].replace("+", "-"))')
target="/opt/herdr-tcp-gateway/releases/$version"
test -f ./herdr-gateway && test -f ./herdr-tcp-gateway.service
sudo test ! -e /etc/herdr-tcp-gateway/relay.env
sudo test ! -e "$target"
getent passwd herdr-tcp >/dev/null || sudo useradd --system \
  --home-dir /var/lib/herdr-tcp-gateway --no-create-home --shell /usr/sbin/nologin herdr-tcp
sudo install -d -m 0755 /opt/herdr-tcp-gateway/releases
sudo cp -a "$PWD" "$target"
sudo install -m 0644 ./herdr-tcp-gateway.service /etc/systemd/system/
sudo install -d -m 0700 /etc/herdr-tcp-gateway
sudo install -d -m 0700 -o herdr-tcp -g herdr-tcp /var/lib/herdr-tcp-gateway
sudo ln -s "$target" /opt/herdr-tcp-gateway/current
```

### 初始化管理配置

将下面的 `PUBLIC_HOST` 改为公网 IP 或域名。先建立 0600 配置，再编辑；不需要为新 Gateway 设置共享注册密钥或静态白名单。

```sh
sudo install -m 0600 /dev/null /etc/herdr-tcp-gateway/relay.env
sudoedit /etc/herdr-tcp-gateway/relay.env
```

填入：

```dotenv
HERDR_GATEWAY_TCP_ADDR=0.0.0.0:8377
HERDR_GATEWAY_ADDR=127.0.0.1:18377
HERDR_GATEWAY_STUN_ADDR=
HERDR_GATEWAY_ADMIN_STATE=/var/lib/herdr-tcp-gateway/management
HERDR_GATEWAY_ADMIN_ADDR=127.0.0.1:18378
HERDR_GATEWAY_ADMIN_ORIGIN=http://127.0.0.1:18378
HERDR_GATEWAY_PUBLIC_ENDPOINT=tcp://PUBLIC_HOST:8377
HERDR_GATEWAY_MAX_RELAYS=16
HERDR_GATEWAY_MAX_CLIENTS=32
HERDR_GATEWAY_MAX_CLIENTS_PER_RELAY=8
HERDR_GATEWAY_MONTHLY_BYTES=0
HERDR_GATEWAY_STATE=/var/lib/herdr-tcp-gateway/quota.json
```

初始化必须使用 **Gateway 服务用户**，否则状态文件的所属用户不匹配：

```sh
sudo -u herdr-tcp /opt/herdr-tcp-gateway/current/herdr-gateway admin-init \
  --state-dir /var/lib/herdr-tcp-gateway/management
# 将输出的管理员密钥保存到密码管理器；只显示这一次。
sudo systemctl daemon-reload
sudo systemctl enable --now herdr-tcp-gateway.service
curl -fsS http://127.0.0.1:18377/healthz
```

## 打开管理页

服务器安全组只开放业务端口 **TCP 8377**，保留 SSH 管理通道，不开放 `18377` / `18378`。

在使用浏览器的电脑上执行：

```sh
ssh -N -L 18378:127.0.0.1:18378 user@PUBLIC_HOST
```

浏览器打开 `http://127.0.0.1:18378`，使用管理员密钥登录。页面地址必须与配置的 Origin 一致；不要随意换成 `localhost` 或其他本地端口。

若部署 HTTPS 反向代理，将 `HERDR_GATEWAY_ADMIN_ORIGIN` 设为精确的 HTTPS 源地址，例如 `https://gateway-admin.example.com`，代理转发到本机 `127.0.0.1:18378` 并保留正确 Host。后端不允许绑定公网地址。

## 增加电脑

1. 在管理页填写电脑名称并生成接入码，默认 **10 分钟**有效。
2. 将完整接入码安全地交给该电脑的操作者。
3. 电脑使用已安装 Relay 的同一用户和配置运行：

   ```sh
   ~/.local/share/herdr-pocket-relay/current/run.sh gateway-join
   # 在提示后粘贴接入码；不要把它作为命令行参数。
   systemctl --user restart herdr-mobile-relay.service
   ```

4. 确认管理页显示电脑在线。首次安装尚未创建服务时，先完成[用户服务安装](native-deployment.md#安装电脑-relay)。
5. 再由 Relay 生成 `herdr-tcp://` 客户端邀请，供手机或桌面配对。

注册交换在原有 TCP 端口上使用 TLS 1.3，校验接入码中的证书指纹。接入码包含短期秘密，勿粘贴到公共日志、issue 或截图。

## 迁移已有电脑

1. 升级 Gateway 与两端所需 Relay 二进制，**保留现有环境配置、Relay 主密钥及设备库**。
2. 停止 Gateway，以服务用户运行 `admin-init`，向原环境文件追加管理配置。**保留原共享注册密钥及完整白名单**，首次启用时将白名单导入管理库。
3. 启动 Gateway，确认原电脑仍在线。它们暂时显示“原有注册凭据”。
4. 在对应电脑那一行点“重新接入”，生成**绑定原 Relay ID**的接入码。不要在“新增电脑”中创建另一条身份。
5. 在该电脑运行同一配置下的 `gateway-join`，只重启 Relay。成功后显示“独立接入凭据”，原手机／桌面授权保留。

已有插件安装可能使用别的配置位置，必须显式指定原配置：

```sh
HERDR_RELAY_ENV=/绝对路径/原relay.env /新版本目录/run.sh gateway-join
```

管理库只导入旧白名单一次。启用后修改环境变量白名单不会覆盖管理操作，也不会恢复已撤销的凭据。

## 操作含义

| 操作 | 结果 |
| --- | --- |
| 重命名 | 修改 Gateway 中的管理备注，不更换 Relay ID，不同步覆盖客户端自己的备注 |
| 禁用 | 立即断开该电脑及其 Gateway 客户端连接，拒绝重连；不停止 Agent |
| 恢复 | 允许被禁用的电脑用原凭据重连 |
| 撤销凭据 | 原注册凭据失效，必须重新接入；仅点“恢复”无效 |
| 重新接入 | 为相同 Relay ID 签发新凭据；兑换后旧凭据失效，客户端配对不变 |
| 取消接入码 | 取消待使用的代码；若已兑换但尚未注册，会撤销该次待确认凭据 |
| 调整容量 | 对后续连接生效，不强制踢掉超出新限制的已有连接 |

电脑上限控制在线 Relay 数；客户端上限控制连接数，不是历史配对设备总数。本月流量按 UTC 月份计数，是 Gateway 转发量，不等同于云厂商账单。`HERDR_GATEWAY_MONTHLY_BYTES` 是服务端配置项，管理页不提供修改此配额的入口。

**Gateway 禁用仅控制经过本 Gateway 的连接**，不会撤销 Relay 自身的设备授权，也不阻断 Linux 客户端的本机直连。需要撤销某个客户端时，应在 Relay 的设备管理中撤销。

## 备份与恢复

- 私有状态：`HERDR_GATEWAY_ADMIN_STATE` 下的 `access.db`，包含电脑凭据、管理密钥摘要、TLS 证书、容量设置和审计记录。
- **停止 Gateway 后再复制整个状态目录**；同时备份环境配置和配额文件。备份含敏感凭据，应加密存放。
- 恢复时保持目录 0700、文件 0600，并归 Gateway 服务用户所有。不要以空状态启动替代原库；否则会丢失管理记录、凭据及证书指纹。
- 忘记管理员密钥时，停止 Gateway，再以服务用户执行 `admin-reset --state-dir ...`，保存新密钥后启动。此操作保留电脑凭据和证书。

```sh
sudo systemctl stop herdr-tcp-gateway.service
sudo -u herdr-tcp /opt/herdr-tcp-gateway/current/herdr-gateway admin-reset \
  --state-dir /var/lib/herdr-tcp-gateway/management
sudo systemctl start herdr-tcp-gateway.service
```

管理数据库不允许多个 Gateway 同时写入。服务重启后管理员需要重新登录，电脑凭据与设置保持不变。
