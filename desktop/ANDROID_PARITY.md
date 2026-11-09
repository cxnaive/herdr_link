# Android 与桌面端功能核对

核对日期：2026-11-07。以 Kotlin 客户端的实际入口、协议和权限检查为依据。“已接入”表示桌面已有实现和本地测试，不表示操作过用户的真实 Agent 或完成 macOS 实机验收。

## 已接入

| 功能 | 桌面入口与行为 | 实现 |
| --- | --- | --- |
| 电脑 → 项目 → Agent | 两级独立折叠、保存状态、搜索临时展开，服务端名称与排序 | `sidebar.go` |
| 名称与 ID | 标签名称优先；详情显示会话 ID，管理菜单复制 ID | `view.go`、`management.go` |
| 新建 Agent | 项目旁「＋」/项目右键：提供方、名称、初始任务、额外 argv | `management.go` |
| Codex 默认参数 | 显示 Relay 公布的默认参数；客户端只提交额外 argv。Relay 自动附加 `-c tui.terminal_title=["thread-id","thread-name"]`，重启沿用且不重复 | `internal/relay/management.go`、根模块 `internal/coordinator/launch_args.go` |
| Agent 管理 | Agent 右键/顶部「管理」：改名、停止、重启、清空、排序；停止/重启/清空先确认目标 | `management.go` |
| 模型/思考强度 | 动态读取 Codex 模型目录及设置版本，保存后验证有效值，未知结果重新读取 | `management.go` |
| 审批 | 对话卡片显示请求/命令/选项；携带精确目标、事件 ID 和审批指纹 | `interaction.go` |
| 结构化问答 | 单选、多选、自定义答案、上一题、进一步讨论；问题变化时清除旧表单 | `interaction.go` |
| 附件 | 「＋ 附件」选择文件、粘贴图片/文件，资源管理器/Finder 拖入；分块进度、散列校验、取消 | `attachments.go`、`internal/relay/upload.go` |
| 附件草稿 | 引用和有效期加密保存，随提示发送；每次最多 8 个、单个 20 MiB、总计 50 MiB | `storage.go` |
| 可靠操作 | 写入前保存待核对标记；明确拒绝可重试，超时/断线不自动重发；创建按项目防重复，Agent 操作按身份互斥 | `management.go`、`internal/relay/outcome.go` |
| 增量消息 | 根据订阅 ID、版本和序号合并更新，漏序/重置后重新取页，无订阅时回退读取 | `history.go` |
| 长历史恢复 | 重连/重新打开后跨页补齐到上次阅读深度；支持加载全部、实际页数/消息进度、暂停继续和过期游标恢复，无 2,000 条截断 | `history_loader.go` |
| 最近历史缓存 | 加密缓存最近 8 个会话，每个最多 300 条/1 MiB，并保存阅读边界和全部加载意图；更早正文联网自动恢复 | `history.go` |
| 历史加载 | 转圈、整理历史的实际进度、分页、取消等待、保留当前内容 | `controller.go`、`view.go` |
| 状态和用时 | 选中 Agent 每 5 秒读取可靠状态；超过 15 秒、断线或身份变化后停止推算 | `run_state.go` |
| 工具调用 | 连续事件分组，组/单次调用两级折叠，查看输入输出 | `timeline.go` |
| 通知 | 应用运行期间，所有连接的 Agent 完成/待处理事件触发系统通知；单独静音，点击进入会话 | `tools_panel.go` |
| 指令和搜索 | 斜杠指令列表、已加载对话/工具内容搜索、原生终端查找和前后定位、复制最新回复 | `tools_panel.go`、`view.go` |
| Git、诊断、活动 | 逐文件 diff、只读绑定诊断、电脑活动记录 | `tools_panel.go` |
| 项目 | 电脑菜单添加；项目右键改名、排序、关闭；支持空项目 | `projects.go` |
| Worktree | 列出、打开、创建和移除；删除确认具体目录，始终不使用 force | `projects.go` |
| 已配对电脑 | 本地改名、移除、刷新、重连；移除清理该电脑的本地凭据/草稿/缓存，远端继续运行 | `projects.go` |
| 配对设备 | 查看设备/角色、改名、撤销授权、生成只读或控制者邀请并复制 | `devices.go` |
| 文件列表 | 分页、筛选、隐藏文件开关、路径输入和上下级导航 | `files.go` |
| 工作区外只读访问 | 电脑菜单/文件面板的「文件访问范围」由控制者明确开启；当前设备授权保存在 Relay，可返回 `/` 浏览和下载；关闭即取消读取并清理预览 | `file_access.go`、`internal/relay/file_scope.go` |
| 文件引用 | Codex citation、绝对/相对路径 Markdown 链接及图片均使用当前已授权范围 | `citations.go`、`media.go` |
| 预览 | Markdown、源码、图片、内嵌 PDF；Markdown 相对链接/标题锚点，图片放大 | `files.go`、`media.go` |
| 下载 | 系统保存对话框、原文件分块下载、版本/散列校验、进度和取消 | `internal/relay/files.go` |
| 原生终端 | 键盘、鼠标、中文、复制和尺寸同步，连接当前远端 Agent | `internal/relay/terminal.go` |
| 桌面外观 | 应用与终端独立深浅色、跟随系统/应用，本机保存；终端默认柔和深色并校正低对比文字；浅色 Codex 深灰背景可柔化，其他 Agent 保持原背景，切换不重连 | `appearance.go` |

## 仍未对齐与明确限制

| 优先级 | 差距 | 当前边界 |
| --- | --- | --- |
| P1 | WSS、多网关、WebRTC | 当前仍是单 TCP 网关和 `herdr-tcp://` 邀请；需单独完成传输、邀请解析和重连验证 |
| P1 | Pi/OMP 专用投递 | 未接 `get_pi_prompt_capability` / `send_pi_prompt_unconfirmed` 确认流程，请使用原生终端；这两类 Agent 的附件入口禁用 |
| P1 | 更多文件引用与定位 | 尚未从工具输出/裸路径提取文件卡片，未接 PDF 引用页码和源码行号定位 |
| P1 | 后台常驻 | 已有运行期间的事件通知；尚无托盘常驻/关闭窗口后监控，也不声称后台精确计时 |
| P2 | 阅读体验 | 未持久化阅读位置、跨文件前进后退和系统分享；放大/下载已有 |
| P2 | Relay 运维 | 尚无检查更新、Pi 集成开关的图形设置页 |
| P2 | 语音、扫码 | 尚无本机 TTS、二维码图片识别；支持粘贴邀请 |
| 服务端限制 | 独立 Codex 设置 | `--no-daemon` 不公开外部设置端点时显示 Relay 原因，需在终端修改 |
| 服务端限制 | 编辑文件、续传 | Relay 未公开 `workspace_file_write`，Android 的禁用入口不算可用功能；两端均无断点续传 |
| 平台验证 | macOS | 0.3.2 增加 Universal DMG 与 macOS 15 Apple Silicon / Intel 原生 CI：同一 DMG 的安装、启动、终端、Keychain 与 PDF 检查；系统通知、文件选择/拖放/剪贴板、旧系统和完整人工交互仍需实机验收 |

## 桌面布局与验证

- 管理操作使用电脑/项目/Agent 菜单和原生对话框；审批、问答留在对话区域。
- 附件使用系统文件选择、拖放和粘贴。终端直接接收键盘鼠标，不复制手机的补充按键栏、快照分页和独立输入框。
- 远端操作以服务端精确目标为准，不以名称推断身份；只读角色不能提交写操作。
- 测试覆盖过期目标、未知写入持久化、默认 argv 隔离、管理菜单、问答指纹、分块校验/取消、加密上传回复路由、增量漏序、缓存边界和电脑数据隔离。无窗口截图检查布局，未自动操作用户真实 Agent。
- 新增 4,260 条/71 页历史恢复、缓存外阅读边界、整理游标续读、快照过期、分页循环、暂停与普通刷新不重复全量读取测试；授权范围覆盖所有文件接口，撤销后取消请求并拒绝晚到内容。若 Relay 源记录本身损坏或缺失，仍明确显示服务端不完整提示。

Android 依据：`AgentManagement.kt`、`AgentManagementDialogs.kt`、`AgentLaunchArguments.kt`、`AgentProtocol.kt`、`PrimarySessionBinding.kt`、`NativeAgents.kt`、`MainActivity.kt`。创建、管理、提问、上传和订阅均按现有接口接入。
