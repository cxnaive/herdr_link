# Relay 安装模板

Relay 运行在 Herdr / Agent 所属的普通用户下。完整步骤见[部署指南](../../docs/native-deployment.md)，Gateway 接入与迁移见[管理指南](../../docs/gateway-management.md)。

| 文件 / 命令 | 用途 |
| --- | --- |
| `init-config.py` | 首次创建私有配置；不覆盖已有身份，需 Python 3.8+ |
| `run.sh gateway-join` | 使用接入码获得独立 Gateway 注册凭据 |
| `run.sh serve` | 加载同一配置并运行 Relay |
| `run.sh tcp-pair '设备名'` | 向运行中的 Relay 申请客户端配对邀请 |
| `herdr-mobile-relay.service` | 首次安装的 systemd 用户服务模板 |
| `relay.env.example` | 配置字段示例，不含可用秘密 |

完整部署包内还提供 `DEPLOYMENT.md`；本源码目录没有编译好的 Relay，不能直接运行服务。

已有安装请保留主密钥、runtime 和设备库，CLI 使用与服务相同的 `HERDR_RELAY_ENV`。不要覆盖自定义服务，不要以 root 代替 Agent 用户，不要为了升级重启 Herdr / Agent。
