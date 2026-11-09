# Herdr Link · Android

连接多台 Herdr 电脑的原生 Android 客户端。手机通过 Gateway 连接电脑 Relay；每台电脑独立配对，按电脑、工作空间和 Agent 展示。

系统结构与服务端部署见[项目首页](../README.md)和[快速开始](../QUICKSTART.md)。

<!-- android-downloads:start -->
## 安装

**[Herdr Pocket 0.2.31](https://github.com/cxnaive/herder_relay_extra/releases/tag/android-v0.2.31)** · Android 9 及以上

| 设备 | APK |
| --- | --- |
| 大多数手机 / 平板 | [arm64-v8a](https://github.com/cxnaive/herder_relay_extra/releases/download/android-v0.2.31/Herdr-Pocket-0.2.31-release-arm64-v8a.apk) |
| 32 位 ARM 设备 | [armeabi-v7a](https://github.com/cxnaive/herder_relay_extra/releases/download/android-v0.2.31/Herdr-Pocket-0.2.31-release-armeabi-v7a.apk) |
| x86_64 设备 / 模拟器 | [x86_64](https://github.com/cxnaive/herder_relay_extra/releases/download/android-v0.2.31/Herdr-Pocket-0.2.31-release-x86_64.apk) |

更新时直接覆盖安装，**不要卸载旧版**，以保留配对与草稿。
发布包沿用固定签名证书；自行构建的 APK 可能无法覆盖安装。
校验文件、其他构建与版本说明见 Release 页面。
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

## 进一步了解

[文件访问](../docs/file-access.md) · [电脑与工作空间分组](AGENT-GROUPS.md) · [验证说明](VALIDATION.md) · [更新记录](CHANGELOG.md) · [第三方许可](THIRD_PARTY.md)
