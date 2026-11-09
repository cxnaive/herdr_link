"""Keep system-first documentation aligned with one verified project release."""
import json
from component_release_docs import replace_block

REPO = 'cxnaive/herdr_link'


def download_blocks(record):
    version, tag = record['version'], record['tag']
    base = f'https://github.com/{REPO}/releases/download/{tag}'
    page = f'https://github.com/{REPO}/releases/tag/{tag}'
    native = f'''### 服务端安装包

**[Herdr Link {tag}]({page})** · Relay 与 Gateway 使用相同项目版本

| 组件 | Linux x86_64 |
| --- | --- |
| Relay | [下载部署包]({base}/herdr-link-relay-{version}-linux-amd64.tar.gz) |
| Gateway | [下载部署包]({base}/herdr-link-gateway-{version}-linux-amd64.tar.gz) |

[校验文件]({base}/SHA256SUMS.txt) · [安装说明]({base}/INSTALL.md) · [部署指南](docs/native-deployment.md)

完整包包含可执行文件、配置和服务模板。ARM64 / macOS 服务端目前只有 CI 独立二进制，不属于本次完整部署包。'''
    android = f'''#### 下载 Android APK

**[Herdr Link {tag} · Android]({page})** · Android 9 及以上

| 设备 | APK |
| --- | --- |
| 大多数手机 / 平板 | [arm64-v8a]({base}/herdr-link-android-{version}-arm64-v8a.apk) |
| 32 位 ARM | [armeabi-v7a]({base}/herdr-link-android-{version}-armeabi-v7a.apk) |
| x86_64 设备 / 模拟器 | [x86_64]({base}/herdr-link-android-{version}-x86_64.apk) |

直接覆盖安装，**不要卸载旧版**。沿用原 Pocket 包名与签名，保留配对和草稿；本地自行签名的 APK 可能无法覆盖安装。Debug APK 仅留在 CI，不放入正式下载列表。'''
    desktop = f'''### 桌面客户端下载

**[Herdr Link {tag} · Desktop]({page})**

| 系统 | 安装方式 |
| --- | --- |
| Windows x86_64 | [便携 ZIP]({base}/herdr-link-desktop-{version}-windows-amd64.zip)，解压运行 `Herdr Desktop.exe` |
| Debian / Ubuntu x86_64 | [Debian 包]({base}/herdr-link-desktop-{version}-linux-amd64.deb)，使用 `sudo apt install ./文件名.deb` |
| Linux x86_64 用户安装 | [归档]({base}/herdr-link-desktop-{version}-linux-amd64.tar.gz) + [安装脚本]({base}/herdr-link-desktop-{version}-linux-amd64-install.sh) |
| macOS | 暂无签名、公证的下载包，使用下方源码构建路径 |

桌面程序目前未签名；Linux 需要 GTK 3、WebKitGTK 4.1、libsecret 和已解锁的 Secret Service。各端共享项目版本，不表示所有平台已完成同等范围的验证。'''
    return {'native-downloads': native, 'android-downloads': android, 'desktop-downloads': desktop}


def update(root, plan, receipt):
    metadata = json.loads((root / 'VERSION.json').read_text())
    metadata['release'] = {'version': plan['manifest']['project']['version'], 'tag': receipt['tag'], 'source_commit': plan['source'], 'build_run_id': plan['build_run_id'], 'release_id': receipt['release_id'], 'assets': receipt['assets']}
    for key in ('native_release', 'android_release', 'desktop_release', 'prior_repository_head'):
        metadata.pop(key, None)
    blocks = download_blocks(metadata['release'])
    readme = (root / 'README.md').read_text()
    for key, body in blocks.items(): readme = replace_block(readme, key, body)
    child = replace_block((root / 'herdr-android/README.md').read_text(), 'android-downloads', blocks['android-downloads'].replace('#### 下载 Android APK', '## 安装', 1))
    desktop = replace_block((root / 'desktop/README.md').read_text(), 'desktop-downloads', blocks['desktop-downloads'].replace('### 桌面客户端下载', '## 下载', 1))
    changes = {root / 'README.md': readme, root / 'herdr-android/README.md': child, root / 'desktop/README.md': desktop, root / 'VERSION.json': json.dumps(metadata, ensure_ascii=False, indent=2) + '\n'}
    for p, text in changes.items(): p.write_text(text)
