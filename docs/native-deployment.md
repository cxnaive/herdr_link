# Relay / Gateway 安装与升级

本文面向 Linux x86_64 的完整部署包。Gateway 安装在公网服务器，Relay 安装在运行 Herdr 的电脑，使用与 Herdr / Agent 相同的普通用户。

管理式接入要求 Relay **0.23.5+codexfix.20**、Gateway **0.23.5+codexfix.7** 或更新版本。当前已发布包与源码可能不同，请核对 [README 下载区](../README.md#服务端安装包) 和包内 `BUILD-INFO.json`。旧版仍可使用本文的[静态白名单方式](#旧版静态白名单接入)。

## 准备环境

- 电脑：已运行 Herdr，需要 POSIX shell、Python 3.8+、systemd 用户服务。
- 公网服务器：需要 systemd 和 Python 3；Gateway 不需要安装 Herdr、Codex 或 Android SDK。
- 使用预编译包不需要 Go、Node 或 Android SDK。ARM64 / macOS 的 CI 独立二进制不包含本文的 Linux 安装模板。

| 端口 | 用途 | 公网开放 |
| --- | --- | --- |
| Gateway TCP 8377 | Relay / 客户端接入、加密注册交换 | 是 |
| Gateway HTTP 18377 | 健康检查 | 否，回环 |
| Gateway HTTP 18378 | 管理页 | 否，回环 + SSH 转发 |
| Relay HTTP 8375 | 本机 API | 否，回环 |
| Relay UDP 8376 | Herdr 事件 | 否，回环 |
| SSH | 服务器维护与管理页转发 | 按自己的管理策略 |

## 下载与校验

从本仓库 Release 下载所需 Relay / Gateway 归档及同一版本的 `SHA256SUMS.txt`。需要尚未发布的管理功能时，选择 `build-artifacts` 成功运行中的 **`native-bundles-linux_amd64`**，不是 `native-binaries-*`。

```sh
mkdir -p "$HOME/Downloads/herdr-native"
cd "$HOME/Downloads/herdr-native"
# 将完整部署包与 SHA256SUMS.txt 放入此目录。
sha256sum --ignore-missing -c SHA256SUMS.txt
# 确认待安装文件显示 OK，再解压对应 .tar.gz。
```

从可信来源下载；校验和只证明文件完整性。每个完整包包含二进制、`BUILD-INFO.json`、配置及 systemd 模板，Relay 包还包含 `web/`。不要单独复制 Relay 二进制而遗漏静态资源。

## 安装 Gateway

新建管理式 Gateway 的步骤见 [Gateway 首次安装](gateway-management.md#首次安装-gateway)。先安装 Gateway、保存管理员密钥，再在管理页为电脑生成接入码。

不要把管理端口直接开放到公网。需要不经 SSH 的浏览器入口时，使用 HTTPS 反向代理，并设置精确的 `HERDR_GATEWAY_ADMIN_ORIGIN`；后端仍绑定回环地址。

## 安装电脑 Relay

以下只适用于**首次安装**。已有服务请直接看[升级](#升级已有安装)，不要覆盖其配置或重新初始化身份。

### 1. 固定安装目录

在解压后的 Relay 包内执行。`name` 来自实际目录名；保留包的全部内容：

```sh
set -eu
name=$(basename "$PWD")
base="$HOME/.local/share/herdr-pocket-relay"
test -f ./herdr-mobile-relay && test -f ./run.sh && test -d ./web
test ! -e "$base/current" && test ! -L "$base/current"
mkdir -p "$base/releases"
test ! -e "$base/releases/$name"
cp -a "$PWD" "$base/releases/$name"
ln -s "releases/$name" "$base/current"
```

### 2. 初始化配置并接入

`PUBLIC_HOST` 替换为 Gateway 的公网地址。Herdr 不在 PATH 或使用非默认 socket 时，向初始化工具传入 `--herdr-bin /绝对路径/herdr`、`--socket /绝对路径/herdr.sock`。

```sh
base="$HOME/.local/share/herdr-pocket-relay"
python3 "$base/current/init-config.py" --gateway tcp://PUBLIC_HOST:8377
"$base/current/run.sh" gateway-join
# 按提示粘贴管理页生成的 herdr-gateway:// 接入码。
```

初始化工具仅创建不存在的配置，生成 Relay 主密钥，输出**公开 Relay ID**。默认私有目录 `~/.config/herdr-mobile-relay/` 权限为 0700，配置文件为 0600。

`gateway-join` 保存独立 Gateway 凭据，不更换已有 Relay 身份。初始化配置中旧注册密钥占位符可保留；成功接入后使用 `gateway-access.json` 中的凭据。

### 3. 启动用户服务

```sh
mkdir -p "$HOME/.config/systemd/user"
# 首次安装；已有自定义 service 不要覆盖。
test ! -e "$HOME/.config/systemd/user/herdr-mobile-relay.service"
cp "$HOME/.local/share/herdr-pocket-relay/current/herdr-mobile-relay.service" \
  "$HOME/.config/systemd/user/"
systemctl --user daemon-reload
systemctl --user enable --now herdr-mobile-relay.service
curl -fsS http://127.0.0.1:8375/healthz
```

需要退出登录后继续运行时，可按系统管理策略启用该用户的 lingering：`loginctl enable-linger "$USER"`，可能需要管理员权限。不要因此把 Relay 改成 root 服务。

### 4. 配对手机或桌面

```sh
~/.local/share/herdr-pocket-relay/current/run.sh tcp-pair '我的手机'
```

在客户端“添加电脑”中扫码或粘贴 `herdr-tcp://` 邀请，并设置电脑名称。每个客户端单独生成邀请；不要把主密钥或 Gateway 注册凭据填写到客户端。

## 路径与状态

| 位置 | 内容 | 升级时 |
| --- | --- | --- |
| `~/.local/share/herdr-pocket-relay/releases/` | Relay 版本目录 | 新版本放新目录 |
| `~/.local/share/herdr-pocket-relay/current` | 使用中的版本链接 | 切换链接 |
| `~/.config/herdr-mobile-relay/relay.env` | Relay 主密钥、Herdr 路径与设置 | 保留 |
| 同目录 `gateway-access.json` | 管理式 Gateway 凭据 | 保留且保持私有 |
| 同目录 `device-auth/` | 客户端授权 | 保留 |
| `/etc/herdr-tcp-gateway/relay.env` | Gateway 环境配置 | 保留 |
| `/var/lib/herdr-tcp-gateway/management/` | 电脑清单、凭据、管理密钥摘要、证书与设置 | 完整备份 |

已有 Herdr 插件安装的 runtime 可能不在上述默认路径。以服务的 **`HERDR_RELAY_ENV` / `HERDR_PLUGIN_CONFIG_DIR`** 为准；CLI 必须使用同一 runtime，不能只因为路径不同就创建第二套身份。

## 升级已有安装

### Relay

1. 查看 `systemctl --user cat herdr-mobile-relay.service`，确认可执行文件、配置路径和所属用户。
2. 将新包校验后解压到**新的版本目录**。保留完整包及旧版本，不覆盖正在运行的二进制。
3. 只停止 Relay：`systemctl --user stop herdr-mobile-relay.service`。此时备份私有配置和设备库，避免复制正在写入的状态。
4. 将 `current` 切到新目录；已有自定义 service / override 则更新其可执行路径，不要用模板覆盖。
5. 若改了服务单元，执行 `systemctl --user daemon-reload`，然后 `systemctl --user start herdr-mobile-relay.service`。
6. 检查版本、注册状态、客户端重连和 Agent 列表。

**不重启 Herdr 或 Agent，不重新运行 `init-config.py`，不删除主密钥或设备库。** 保持身份和授权状态时，客户端无需重新配对。

### Gateway

将新包放进 `/opt/herdr-tcp-gateway/releases/` 的新目录。备份配置，停止 Gateway 后一致备份管理状态和配额文件，切换 `current`，再启动 Gateway。连接会短暂中断并重连。

已有静态白名单 Gateway 第一次启用管理功能时，请先按[迁移说明](gateway-management.md#迁移已有电脑)导入原有电脑。管理状态启用后是权限的权威来源，修改旧环境白名单不会重新授权被禁用／撤销的电脑。

不自动回滚。需要人工恢复旧版本时，先确认它支持当前凭据与状态格式；旧版 Gateway / Relay 无法直接使用新管理凭据。不要用过期设备库覆盖新增授权。

## 旧版静态白名单接入

旧版本没有管理页或 `gateway-join`，但仍能连接客户端：

1. 在电脑运行 `init-config.py --gateway tcp://PUBLIC_HOST:8377`，记下公开 Relay ID。
2. 在服务器解压 Gateway 包后，运行 `sudo env HERDR_GATEWAY_ALLOWED_RELAY_IDS='公开RelayID' sh ./install.sh`。
3. 安全地将服务器配置中的 `HERDR_GATEWAY_REGISTRATION_TOKEN` 填入电脑的私有 `relay.env`。不要传给手机或提交 Git。
4. 按上面的用户服务步骤启动 Relay，再用 `tcp-pair` 配对客户端。

增加电脑需修改 Gateway 的 `HERDR_GATEWAY_ALLOWED_RELAY_IDS`、容量设置并重启 Gateway。新版本建议迁移到独立电脑凭据，无需在多台电脑共享注册秘密。

## 排障

```sh
# 运行 Herdr 的电脑
systemctl --user status herdr-mobile-relay.service --no-pager
journalctl --user -u herdr-mobile-relay.service -n 40 --no-pager
curl -fsS http://127.0.0.1:8375/healthz

# Gateway 服务器
systemctl status herdr-tcp-gateway.service --no-pager
journalctl -u herdr-tcp-gateway.service -n 40 --no-pager
curl -fsS http://127.0.0.1:18377/healthz
```

| 现象 | 检查 |
| --- | --- |
| `gateway.registered=false` | 地址／防火墙，电脑禁用或撤销状态，容量；旧版再检查共享秘密和白名单 |
| 注册成功但没有 Agent | Relay 与 Herdr 是否同用户，Herdr 可执行文件、socket、`inventory.state` |
| `tcp-pair` 找不到运行实例 | 服务是否启动，CLI 与服务的配置路径是否一致 |
| 历史不能确认或用时未知 | 查看会话绑定诊断；不能据此删除配对或猜选另一个 session |
| 管理页拒绝访问 | SSH 转发是否使用相同端口；访问地址是否与配置的 Origin 精确一致 |
| 本地 Gateway 凭据权限错误 | 配置文件必须由 Relay 用户拥有且权限为 0600；不要删文件来绕过错误 |
| 端口占用 | 检查旧服务／新包是否重复启动；不要误杀 Herdr 或 Agent |

日志、配对二维码和配置可能包含敏感信息，对外提供前先脱敏。
