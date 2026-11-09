# Gateway 安装模板

Gateway 部署在公网服务器，只转发加密应用数据，不需要安装 Herdr 或 Agent。

- **新管理式安装**：按[Gateway 管理指南](../../docs/gateway-management.md)初始化私有状态，使用限时接入码授权各台电脑。
- **旧版静态白名单安装**：`install.sh` 从解压后的 Gateway 部署包运行，首次要求 `HERDR_GATEWAY_ALLOWED_RELAY_IDS`，生成共享注册秘密。它不会自动启用管理页；需要 Python 3 来核对包内版本和二进制校验和，同版本但不同二进制会拒绝覆盖。
- **升级**：保留原环境与私有状态，见[安装与升级](../../docs/native-deployment.md)。

源码此目录不含二进制。安装时可使用 root，但服务由专用非登录用户 `herdr-tcp` 运行。公网开放 TCP 8377，健康接口与管理页保持回环监听；模板不会修改云安全组。

`relay.env.example` 提供配置字段，`herdr-tcp-gateway.service` 提供 systemd 模板。不要把实际配置、管理密钥或电脑注册凭据提交到仓库。
