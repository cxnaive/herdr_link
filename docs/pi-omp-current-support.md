# Pi / Oh My Pi 当前交互支持

核验基线：2026-11-10 官方稳定版 **Pi 1.5.2 / OMP 18.18.6**。相关支持包含在 Herdr Link 0.3.3；普通发送修复需要更新 Android，结构化交互需要更新 Relay，Android 的空多选及旧弹窗目标保护也需要新版客户端。

## 版本与实现依据

| 上游 | npm 包 | 源码提交 |
| --- | --- | --- |
| [Pi v1.5.2](https://github.com/badlogic/pi-mono/tree/v1.5.2) | `@earendil-works/pi-coding-agent@1.5.2` | `b4afdddf44e059c67ccf5097e5317d913521b470` |
| [OMP v18.18.6](https://github.com/can1357/oh-my-pi/tree/v18.18.6) | `@oh-my-pi/pi-coding-agent@18.18.6` | `9058ab9d229663e229e2867824ba6df6f21e9e59` |

Pi 官方 npm scope 已迁移，旧 `@mariozechner/pi-coding-agent` 的 latest 0.73.1 不能代表当前官方版本。源码、版本和发布包 integrity 记录在 `tests/fixtures/pi-omp-upstream.json`，测试依赖及传递依赖锁在 `tests/upstream-pi-omp/`；它们不进入产品运行依赖。

主要参考：Pi 的 `ExtensionSelectorComponent`、`ExtensionInputComponent`、`interactive-mode.ts`；OMP 的 `ApprovalDialogComponent`、`AskDialogComponent`、`HookEditorComponent`、`HtmlFormDialog` 及 `tools/ask.ts`。

## 支持方式

| 场景 | 客户端行为 |
| --- | --- |
| Pi select / confirm | 显示真实选项，支持选择和取消；confirm 沿用上游 Yes/No 语义 |
| Pi 普通文本 input | 显式输入或取消，可提交空答案；替换单行输入时先移动到行尾再清空 |
| OMP 审批 | 展示 Approve / Deny、会话和文件夹范围；“Remember across sessions”明确标为仅切换，不当作批准 |
| OMP 计划审批 | 保持原审批协议，四个选项实际回调均经过验证 |
| OMP Ask | 单选、多选（Space 切换、Enter 继续）、空多选、多题及最终确认；Other 和备注先打开上游编辑器，再显式输入/提交 |
| OMP HTML 表单 | 展示当前可见原生表单，操作当前字段：选择、复选、下拉、插入文本、上一/下一字段；到 Submit 后才可单独提交 |

**HTML 表单不是被转换成另一张手机网页表单。** Relay 不猜测屏幕里未暴露的 HTML `name/value`，也不绕过 required/min/max 等上游校验。原表单持有字段身份和值；手机通过当前精确画面的语义操作驱动它。插入文本保留字段现有内容，不隐式清空多行字段或自动提交。校验失败后仍停在原生表单，应查看最新字段错误再修改。

这些控件使用现有 `answer_question` 与精确 target，加上 `stepwise` 标识。每次字段值、焦点或授权范围变化都会生成新 interaction ID，旧弹窗不能提交到新状态。Android 的单选与自定义答案互斥，空多选只对明确的新版多选开放；桌面客户端同步这一规则。

## 安全边界

- 未识别布局、默认键位契约不匹配、缺失边框、截断选项/授权说明、窗口化的 Pi 长列表不猜测回答；仍可显式使用终端。
- OMP 有草稿占用输入时不展示可自动提交的审批控件；审批等待至少 550 ms 后再次读取并核验画面，遵守上游 500 ms 防误触窗口。
- 记忆开关只改变后续授权范围，操作完成后读取新界面；不会把切换当作批准。跨会话授权选项必须由用户再次明确选择。
- 新版表单导航的 Shift+Tab 通过官方 `pane.send_input` 按键接口发送，不能变成被粘贴到字段里的转义文本；Space、Esc、Ctrl+E 使用 Herdr 支持的命名键。
- 回答前重新解析原生界面，权限、终端、generation、interaction ID 仍受既有调度检查；超时或投递不明确不重发。
- 识别为密码/密钥、或已显示掩码的输入不映射成普通答案框；不要在普通字段输入凭据。仅靠终端画面无法证明一个无内容、无提示的任意自定义字段不是密码框。任意第三方 `ui.custom`、用户自定义主题/键位，以及无法完整观察的控件不在通用支持承诺内。
- 终端解析不是上游提供的原子 compare-and-swap；电脑端同时按键仍可能导致竞争。不要把同一弹窗当作允许多人同时操作的事务接口。

## 验证结果

### 真实上游组件

24 个场景 × 深浅两种主题 × 80/120 两种列宽，共 **96 项全部通过**。调用产品真实 parser、payload validation、input planner，随后把按键与 paste-aware 文本交给上游真实 `handleInput`，核对其实际 callback；不是只比较截图。

覆盖普通/拒绝/会话/跨会话授权、四种计划选项、单选、多选、空选择、多题、自定义答案、备注、单行与多行中文、HTML 单选/复选/下拉/文本字段及独立提交。每个场景另验证上游原生参考输入和取消行为。所有内容为合成数据，没有模型请求或工具执行。

最初未修改时为 16 支持、36 未识别；仅把 `⏎` 改成 `enter` 会造成旧多选逻辑提交空选择。本轮同时迁移识别与按键语义，不采用这种文字替换捷径。

### 原版 CLI / PTY

在独立 HOME、无登录凭据、无模型、禁用自动发现扩展/上下文的原版进程中，加载唯一测试命令 `/fixture`：

- Pi 原生 select → Relay 生成 Down/Enter → 上游返回 `South`。
- OMP 原生 approval → Relay 生成 Down/Enter → 上游返回 `deny`，没有真实工具被执行。
- OMP 原生 Ask → Relay 生成 Down/Enter → 上游返回 `selectedOptions: ["South"]`。

这层测试发现并覆盖真实进程中弹窗后的默认状态栏/空编辑器布局。只允许已确认的默认尾部，不因任意新输出而恢复旧弹窗。

### Relay / Android / Desktop

- Relay 调度测试检查旧画面拒绝、Space 序列、审批等待及非法答案拒绝；新语法不改变旧版 provider 测试。
- Android 使用真实 TCP + 网关分帧 + E2EE 测试，验证 Pi 确认、OMP 审批及 HTML 字段操作发送正确的 interaction ID 和 target，空多选、中文输入、跨 Agent 旧弹窗拒绝。
- 桌面验证空多选只对 stepwise 多选开放，单动作表单仍必须明确选择。

这不是用户真机全功能验收，也没有验证所有任意 HTML、自定义插件、键位或所有 Herdr/终端版本。遇到不能识别的界面保持拒绝，不降级为猜测性审批。

## 复跑

Go 1.27.3、Bun 1.4.4；初次安装固定测试依赖需网络：

```sh
scripts/check-pi-omp-upstream.sh
```

默认严格模式：任何未支持或错答返回非零。结果位于打印的 `/tmp/herdr-pi-omp.*/report.json` 和 `frames/`。依赖只安装到临时目录，`--ignore-scripts` 禁止依赖安装脚本，组件运行使用独立 HOME/最小环境，不读个人凭据。

真实 CLI 的 Linux PTY 检查还需要独立 Python 环境中的 `pyte==0.8.2`，然后将路径指向上一步产物：

```sh
PI_OMP_RUNTIME=/tmp/herdr-pi-omp.RUN/runtime \
PI_OMP_WORK=/tmp/herdr-pi-omp.RUN \
PI_OMP_GO_TEST=/tmp/herdr-pi-omp.RUN/coordinator.test \
PI_OMP_BUN=/absolute/path/to/bun python tests/pi-omp-live.py
```

脚本会拒绝复用已有的 live 子目录，所有进程结束时清理；产物只保留测试画面和答案，便于诊断。

日常离线回归：

```sh
go test ./internal/question ./internal/coordinator -run 'CurrentPiOMP|CurrentOMP|Pi|OMP|Approval|Question'
cd herdr-android
./gradlew testDebugUnitTest --tests '*PiPromptTcpUiTest'
```
