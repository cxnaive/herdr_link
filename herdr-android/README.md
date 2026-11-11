# Herdr Link · Android

连接多台 Herdr 电脑的原生 Android 客户端。手机通过 Gateway 连接电脑 Relay；每台电脑独立配对，按电脑、工作空间和 Agent 展示。

系统结构与服务端部署见[项目首页](../README.md)和[快速开始](../QUICKSTART.md)。

<!-- android-downloads:start -->
## 安装

**[Herdr Link v0.3.2 · Android](https://github.com/cxnaive/herdr_link/releases/tag/v0.3.2)** · Android 9 及以上

| 设备 | APK |
| --- | --- |
| 大多数手机 / 平板 | [arm64-v8a](https://github.com/cxnaive/herdr_link/releases/download/v0.3.2/herdr-link-android-0.3.2-arm64-v8a.apk) |
| 32 位 ARM | [armeabi-v7a](https://github.com/cxnaive/herdr_link/releases/download/v0.3.2/herdr-link-android-0.3.2-armeabi-v7a.apk) |
| x86_64 设备 / 模拟器 | [x86_64](https://github.com/cxnaive/herdr_link/releases/download/v0.3.2/herdr-link-android-0.3.2-x86_64.apk) |

直接覆盖安装，**不要卸载旧版**。沿用原 Pocket 包名与签名，保留配对和草稿；本地自行签名的 APK 可能无法覆盖安装。Debug APK 仅留在 CI，不放入正式下载列表。
<!-- android-downloads:end -->

## 首次连接

1. 在运行 Herdr 的电脑上，通过该 Relay 的 `run.sh tcp-pair '我的手机'` 创建单设备邀请。
2. 在 App 中选择“添加电脑”，扫码或粘贴 **`herdr-tcp://`** 邀请。
3. 填写电脑名称，选择工作空间和 Agent。

电脑名称是此客户端的备注，离线也可修改，不改变 Relay ID 或配对。连接第二台电脑时重复流程；不要把 Gateway 的 `herdr-gateway://` 接入码当成手机邀请。

## 日常使用

- **对话**：Markdown、历史分页、连续工具调用折叠、审批和问答；支持缓存与断线重连。
- **终端**：查看输出、输入文本及快捷按键。对话身份无法确认时，终端仍是独立的操作入口，权限以界面提示为准。
- **Agent**：在指定工作空间创建、重命名、停止和重启；额外启动参数与初始提示分开填写。
- **文件**：浏览当前工作区，预览图片 / PDF / Markdown，上传、下载和分享。默认不访问工作区外；扩大只读范围需控制者明确授权。
- **状态**：显示可确认的运行状态和本轮用时；未知不等于已完成。

工作区搜索只过滤已加载的目录条目。历史图片依赖原文件仍然存在且位于允许范围内，不是永久截图归档。

## 主题配色（0.3.3）

在 **设备 → 主题配色** 中选择配色，预览后点击“应用”：

- **薄荷深色**：原来的默认深色界面。
- **午夜蓝**：深蓝背景与冷蓝强调色。
- **石墨紫**：石墨灰底与柔和紫色。
- **云白蓝**：明亮白底与经典蓝色。
- **暖纸棕**：暖白纸色与棕色。

配色只保存在本机，重新打开 App 后仍然生效；取消预览不会修改设置。列表、对话、输入框、文件查看界面和弹窗随配色切换，终端输出保留深色以保证 ANSI 颜色可读。应用配色立即生效，不会重启 App、断开连接或清除配对、草稿、待确认发送与常驻监控状态，也不会更改远端终端配置。

## 后台提醒

在“常驻监控 / 完成提醒”中开启监控并授予通知权限，可测试通知及调整通道声音。通知可打开对应会话，也可停止监控。

监控依赖手机常驻连接，**不是云推送**。系统省电、强行停止或断网可能导致延迟或漏报；不要仅凭没有提醒判断 Agent 是否已完成。覆盖安装不会重置系统通知通道的用户设置。

详见[后台监控](BACKGROUND-MONITORING.md)。

## 构建

需要 **JDK 21、Android SDK Platform 35、Build Tools 35.0.0**。使用仓库自带的 Gradle Wrapper，首次构建需要联网下载依赖。

```sh
cd herdr-android
export JAVA_HOME=/path/to/jdk-21
export ANDROID_HOME=/path/to/android-sdk
./gradlew --no-daemon testDebugUnitTest assembleDebug assembleDebugAndroidTest lintDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

APK 也可以复制到手机手动安装。已有发布版使用固定签名；本地默认签名可能不同，先确认兼容性，不要直接卸载旧版导致配对和草稿丢失。

不要提交 `local.properties`、构建缓存、签名私钥或设备状态。Release 附件只使用 CI 构建和签名，不手工上传本地 APK。

## Pi / Oh My Pi 对话输入

正常状态下的 Pi / OMP 对话输入使用 Relay 的 `submit_prompt`，由 Herdr 处理，不依赖可选的 Pi bridge。若 Herdr 拒绝当前状态的输入，保留草稿并显示错误，不自动改用终端或重发。

等待审批／问题时，先处理对应交互，或显式切换“终端”手动输入。`get_pi_prompt_capability` / `send_pi_prompt_unconfirmed` 是“Pi 阻塞、输入类型未知”时的专用桥接接口，不是 Pi / OMP 的通用发送接口；Android 不使用此特殊路径。Pi / OMP 的附件入口仍保持禁用。 新版审批与提问使用独立结构化操作；HTML 表单逐字段操作、最后单独提交。实际支持范围与测试依据见[上游自动验证](../docs/pi-omp-current-support.md)，不能仅凭存在通用回答按钮就认为已完成适配。

## 进一步了解

[文件访问](../docs/file-access.md) · [电脑与工作空间分组](AGENT-GROUPS.md) · [验证说明](VALIDATION.md) · [更新记录](CHANGELOG.md) · [第三方许可](THIRD_PARTY.md)
