# 开发与测试

本项目包含根目录的 Go Relay / Gateway、`desktop/` Go 桌面客户端、`herdr-android/` Kotlin 客户端，以及 Relay 的 Web 客户端资源。

用户安装请看 [README](../README.md) 和[部署指南](native-deployment.md)。工具链版本以 `go.mod`、Gradle Wrapper 和 CI 配置为准。

## Relay / Gateway

需要 Go **1.27.3**：

```sh
go test -p 1 ./...
go vet ./...
CGO_ENABLED=0 go build -trimpath -o bin/herdr-mobile-relay ./cmd/herdr-mobile-relay
CGO_ENABLED=0 go build -trimpath -o bin/herdr-gateway ./cmd/herdr-gateway
```

Linux amd64 本地部署包验证（用于开发，不上传为 Release）：

```sh
# 在干净、已提交的源码中运行；输出目录必须不存在。
scripts/package-native-release.sh /tmp/herdr-native-test
python3 scripts/check-native-release.py /tmp/herdr-native-test
```

包中保留 committed `web/`。不要手工编辑生成文件；仅修改 Relay / Gateway 不需要重建网页。Web 变更按根目录 [AGENTS.md](../AGENTS.md) 的构建要求处理。

## 客户端

- [Android 编译与安装](../herdr-android/README.md#构建)：JDK 21、SDK 35、Build Tools 35.0.0。
- [Windows / Linux / macOS 桌面构建](../desktop/README.md#源码构建)：独立 Go module，MyGo 与终端库版本固定。
- Web 客户端：`frontend/`，使用仓库指定的 Bun / Node，见根目录 Makefile。

## CI 与发布

日常检查按组件选择。完整 race、浏览器和设备资格验证手动触发，见 [CI 与发布](native-ci.md)。

所有 Release 附件均来自可信的成功 CI 构建。发布使用 `publish-components.yml`：先只读预演，再发布**同一 build run**；不上传本地编译或重新签名的附件。发布器根据回执更新下载区和 `VERSION.json`。

下载区的模板是 `scripts/component_release_docs.py`。修改 README 时保留下载区和四图画廊标记；画廊位于 Clients / Android 小节，发布器不得移动它。

```sh
python3 -m unittest discover -s scripts/tests -p 'test_*.py'
```

## 技术设计

- [原生 TCP 与加密边界](native-tcp.md)
- [Codex 主会话定位](primary-session-binding.md)
- [会话诊断接口](session-binding-diagnostics.md)
- [Agent 管理](agent-management.md)
- [运行状态和计时](agent-run-state.md)
- [文件访问](file-access.md)

提交 issue 时说明客户端与 Relay / Gateway 版本、操作步骤和脱敏诊断；不要附带注册密钥、完整邀请、设备库或私人对话记录。

Web 开发及运行时实现细节见[原有开发文档](development.md)。
