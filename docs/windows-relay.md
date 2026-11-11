# Windows Relay（CI 预览）

将 **Windows 原生 Herdr** 中的终端与 Agent 接入已有 Gateway。不是把 WSL 的 Relay 指向 Windows socket，也不要求管理员权限。使用与 Herdr 相同的 Windows 用户；优先运行普通权限的 Herdr 和 Relay。

## 获取

在仓库 Actions 的 **relay-windows** 成功运行中下载 `herdr-relay-windows-amd64-<运行编号>`，核对提交与 `BUILD-INFO.json`，验证 `SHA256SUMS.txt` 后解压其中的 `herdr-link-relay-<版本>-windows-amd64.zip`。保留 `web` 和脚本，不要只复制 EXE。CI 失败不会上传可分发包。

Windows Relay 已接入统一构建：手动运行 `build-artifacts` 并选择 `release` 时，与其他平台从同一提交构建、测试和归档。是否已公开下载以 README 下载区为准；构建成功不会自动创建 Release。

要求 Windows 10/11 x86_64、原生 Herdr（CI 固定验证 0.9.5）、PowerShell 7。Herdr 的完整 Windows 包包含 app-local ConPTY，不能只复制 `herdr.exe`。Relay 与 Agent 共用用户权限，不以管理员身份绕过权限错误。

## 初始化和连接

在已解压目录打开 PowerShell 7：

```powershell
./install.ps1
./run.ps1 gateway-join
# 按提示粘贴 Gateway 管理页为这台电脑生成的限时接入码。
./run.ps1 serve
```

需要先让 PowerShell 能通过 PATH 找到 `herdr.exe`。默认配置保存在 `%LOCALAPPDATA%\HerdrLink\relay\windows-relay.json`，Herdr API 默认使用 `%APPDATA%\herdr\herdr.sock`。如果使用命名 session 或 `XDG_CONFIG_HOME`，将 `socket_path` 改为对应的**原始路径**；不要填 `\\.\pipe\...`，Relay 按 Herdr 的规则映射命名管道。

配置目录使用当前用户、SYSTEM 和 Administrators 可访问的受保护 ACL；不依赖 Windows 无效的 POSIX 权限位。`gateway-access.json` 与设备数据库含私密凭据，不上传仓库、不分享给客户端。重复安装不会重建身份或覆盖配置。

若启动报 `bind: An attempt was made to access a socket in a way forbidden by its access permissions`，先用 `netsh interface ipv4 show excludedportrange protocol=tcp` 检查系统保留端口。Hyper-V / WSL 等环境可能保留包含 `8375–8377` 的范围，即使没有进程监听也无法绑定。将 `windows-relay.json` 的 `port`、`plugin_port` 改为可用端口（例如 `28375`、`28376`）；本机测试 Gateway 可使用 `127.0.0.1:28377`。无需管理员权限，也不要删除系统保留范围。

源码工作区从旧版本升级后，若健康日志报告 `file digest does not match its descriptor`，检查 `git ls-files --eol -- web`：`web/**` 必须保留提交的精确字节。旧 checkout 留下的 CRLF 转换不会因新增 `.gitattributes` 自动消失；核对没有本地内容改动后恢复受影响文件的提交字节，或使用全新 checkout / 完整 CI ZIP。不要改发布摘要来匹配损坏的文件。

Relay 保持运行，再开一个 PowerShell：

```powershell
./run.ps1 tcp-pair '我的手机'
```

客户端粘贴生成的 `herdr-tcp://` 邀请，填写电脑名称。Windows、Linux、macOS、Android 客户端均通过现有 Gateway / E2EE 协议连接。

同一台 Windows 电脑上的客户端也可以使用“添加电脑 → 查找本机 Relay → 连接本机”，直接连接当前 Windows 用户的 Relay，无需复制邀请或经过 Gateway。需同步更新客户端和 Relay。发现过程不创建邀请，选择后才授权；原有设备身份、草稿与角色保持不变。Windows 客户端也支持连接已运行的 WSL 中的 Relay，要求发行版内有 Python 3.9+，详见[本机连接](../desktop/README.md#windows--linux-连接本机-relay)。

这是**前台运行预览**，尚未安装 Windows Service／计划任务；关闭 Relay 进程会断开客户端，不会关闭 Herdr 的终端。不要同时启动两个使用同一配置的 Relay。

## 功能边界

- 命名管道 API、事件订阅、工作空间与 Agent／纯终端清单；Agent 释放后仍保留终端入口。
- Android 终端读取、文字与按键；桌面协议 22 原生终端流。接入、输入和重连仍校验设备凭据、目标身份和 generation。
- 标准 Herdr agent kind 的启动和生命周期命令经原生 Herdr 执行；自定义 POSIX profile 命令拒绝执行，不猜测 PowerShell/cmd 引号规则。也可直接在终端启动命令。
- **Windows x64 Codex 会话绑定**：核对同用户原生进程、创建时间、父子关系及工作目录。共享运行时用内核确认的本进程回环监听端口关联会话；内嵌模式用本进程持有的文件及排他读限制确认写锁，再核对标题 ID 与历史元数据。支持结构化历史、按会话发送、审批和运行状态；`/new`、进程替换或证据变化后旧请求失效。不会按目录、最新日志或另一个 Codex 的会话猜选。
- **模型与思考强度**：共享运行时支持读取及修改，并核对生效值。`--no-daemon` 或标题 `-c` 参数触发的内嵌运行时没有外部设置接口，需在 Codex 终端修改，与 Linux 的限制相同；历史、审批和终端仍可使用。当前实机验证为 Codex CLI 0.167.0 / Herdr 0.9.5；未知架构、进程读取失败、锁归属不唯一时保留未确认状态。
- 非 Codex Agent 继续使用 Herdr 的上报与已有解析器；每种 Agent 的 Windows 安装路径／历史格式仍需分别验证。
- Pi Unix 本地命令桥、直接 PTY ioctl 尺寸租约、Unix reload/bootstrap 信号、插件自更新及托管 Web 部署不提供 Windows 支持。桌面终端尺寸由 Herdr 的终端协议处理。

## 测试和升级

Ubuntu 可以交叉编译并验证平台无关协议以及 Linux 回归，不能替代 Windows 原生执行。`relay-windows.yml` 使用 Windows 2025 runner 验证命名管道、ACL、进程树取消，安装固定摘要的真实 Herdr，并启动隔离 Gateway／Relay，通过真实桌面客户端协议验证注册、配对、终端读写、Agent 释放、重连和关闭面板。

测试使用临时目录和随机回环端口，不访问线上 Gateway 或用户 Agent，不要求模型 API 密钥。无头 CI 不等于人工确认中文输入法、真实屏幕体验和所有 Agent；这些仍需实机验证。

Windows CI 还覆盖原生进程参数读取、PID 重用、Node 启动器、监听端口归属、文件锁的独占/共享/未锁定/局部锁及并发检查。可显式设置 `HERDR_CODEX_LIVE_TEST_PANE`、`HERDR_BIN`、`HERDR_SOCKET_PATH` 后运行 `go test -run TestWindowsCodexLiveProcessProof -v ./internal/app`，只读验证指定测试会话；默认不访问现有 Agent。

升级时只停止 **Relay**，将新的 CI ZIP 解压到新目录，用同一默认配置目录运行 `run.ps1 serve`。保留原配置与设备库，无需重新接入或配对。不要在 UI 中尝试 Unix 插件自动更新。
