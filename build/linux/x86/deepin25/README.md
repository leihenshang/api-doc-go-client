# 打包流程：Linux · x86(x86_64) · Deepin 25 —— deb 安装包

> 状态：**打包脚本已实现** —— `scripts/build-deb.sh`（构建 → deb 树 → 图标缩放 → dpkg-deb，prod/dev 各一包）；
> 脚本尚待在 Linux 构建机首次实跑验证（Windows 侧仅做过 `bash -n` 语法校验）。CI 接入等见文末「待实施清单」。
>
> 目录层级约定：`build/<系统>/<cpu架构>/<os>/`。
> 本目录对应 **linux + x86_64 + deepin25**，dpkg 架构为 **amd64**。

## 1. 目标产物

```
api-doc-client_<version>_amd64.deb            # 正式版
api-doc-client-dev_<version>_amd64.deb        # 开发版（可与正式版并存安装）
```

安装后布局：

| 路径 | 内容 |
|---|---|
| `/usr/bin/api-doc-client` | 正式版可执行文件（**固定文件名，见 §4 的 WM_CLASS 约定**） |
| `/usr/bin/api-doc-client-dev` | 开发版可执行文件 |
| `/usr/share/applications/api-doc-client{,-dev}.desktop` | 应用入口 |
| `/usr/share/icons/hicolor/<尺寸>/apps/api-doc-client{,-dev}.png` | 主题图标（16/32/48/64/128/256/512） |

## 2. 前置条件（构建机）

- Go 工具链 + Wails CLI（`wails build`）、Node（前端构建）
- GTK3 / WebKit2GTK 开发头文件（Wails Linux 前端为 cgo，必须有）：
  `libgtk-3-dev libwebkit2gtk-4.1-dev`（按项目实际 webkit 版本调整）
- 打包工具：`dpkg-deb`；图标缩放：ImageMagick（`convert`）或 `rsvg-convert`/`sharp`
- x86_64 为**本机原生构建**，无需交叉编译环境

## 3. 打包流程

### 步骤 1：构建二进制

```bash
# 正式版（build 标签会内嵌 frontend/dist，需先构建前端）
wails build -platform linux/amd64 -clean -o api-doc-client

# 开发版（dev 标签：开发图标 + 开发版程序名，见 internal/appicon、internal/devicon）
wails build -platform linux/amd64 -tags dev -clean -o api-doc-client-dev
```

产物落在 `build/bin/`（已 gitignore），打包时拷入 deb 树。

### 步骤 2：组织 deb 目录树

```
pkg/
  DEBIAN/
    control
    postinst
    postrm        # 可选：卸载清理
  usr/
    bin/api-doc-client{,-dev}
    share/applications/api-doc-client{,-dev}.desktop
    share/icons/hicolor/<尺寸>/apps/api-doc-client{,-dev}.png
```

### 步骤 3：`DEBIAN/control`

```
Package: api-doc-client
Version: <version>
Architecture: amd64
Maintainer: api-doc-go
Section: utils
Priority: optional
Depends: libgtk-3-0, libwebkit2gtk-4.1-0
Description: AI-native, local-first API client
 Markdown 风格接口文档协作工具的桌面客户端（Postman/Bruno 风格）。
```

### 步骤 4：`.desktop`（关键，决定任务栏图标）

```ini
[Desktop Entry]
Type=Application
Name=API Doc Client
GenericName=API Client
Comment=AI-native、local-first 的 API 客户端（Postman/Bruno 风格）
Exec=/usr/bin/api-doc-client
Icon=api-doc-client
Terminal=false
StartupWMClass=ApiDocClient
Categories=Development;Utility;
StartupNotify=true
```

要点：
- `Icon=` 用**主题名** `api-doc-client`（对应 §1 的 hicolor 路径），不要写绝对路径；
- `StartupWMClass` **必须与窗口真实 `WM_CLASS` 匹配**，否则 DDE dock 匹配不到应用，只显示默认图标。
  当前实现里 `WM_CLASS` 来自**可执行文件名**（GTK 的 `gdk_set_progname` 链路），Wails 的
  `linux.Options.ProgramName`（只调 `g_set_prgname`）对它无效——已在 deepin25 实测确认。
  因此两种落地方式二选一：
  1. **固定二进制名**：安装名固定为 `api-doc-client` / `api-doc-client-dev`，
     `StartupWMClass` 相应写 `api-doc-client` / `api-doc-client-dev`（与 basename 一致）；
  2. **固定 WM_CLASS（推荐）**：`wails.Run` 前用 cgo 调 `gdk_set_program_class("ApiDocClient")`
     （dev 用 `ApiDocClientDev`），之后二进制随便改名，`StartupWMClass` 都能匹配。
- dev 入口必须与 prod 用不同 `StartupWMClass`，否则两个窗口在 dock 会被并成一个分组。

### 步骤 5：图标多尺寸

源文件 `build/appicon.png`（1024×1024）缩放为 16/32/48/64/128/256/512 后分别装入
`/usr/share/icons/hicolor/<尺寸>/apps/api-doc-client.png`；dev 图标用
`internal/appicon/appicon_dev.png` 同法处理，命名为 `api-doc-client-dev.png`。

### 步骤 6：`DEBIAN/postinst`

```sh
#!/bin/sh
set -e
update-desktop-database -q /usr/share/applications || true
gtk-update-icon-cache -q -t -f /usr/share/icons/hicolor || true
```

（`postrm` 可选：`update-desktop-database` 即可，不删用户配置。）

### 步骤 7：打包与安装

```bash
chmod 755 pkg/DEBIAN/postinst
dpkg-deb --build --root-owner-group pkg api-doc-client_<version>_amd64.deb

# 安装
sudo apt install ./api-doc-client_<version>_amd64.deb   # 或 sudo dpkg -i
```

### 步骤 8：安装后验证

```bash
gtk-launch api-doc-client                 # 从入口启动（不要用 /usr/bin 裸跑）
xprop -id $(xdotool search --class ApiDocClient | head -1) WM_CLASS _NET_WM_ICON
```

- `WM_CLASS` 应等于 `.desktop` 的 `StartupWMClass`；
- `_NET_WM_ICON` 应为 `Icon (N x N)`（Wails 会设置，图标本体不缺；dock 取图标靠的是入口匹配）；
- **已运行中的旧窗口要重启应用**才会按新入口重新匹配；
- Deepin 25 当前会话为 X11；若后续切 Wayland，窗口图标只认 `.desktop` 入口，本文档的入口匹配要求依然成立。

## 4. 与 `build/linux/` 现有文件的关系

`build/linux/{api-doc-client,api-doc-client-dev}.desktop` 与 `install.sh` 是**本地免安装入口**
（Exec/Icon 用项目绝对路径，装到 `~/.local/share/applications/`）。deb 版本应改为系统路径
（`/usr/bin` + 主题名图标），两者可共用同一份模板，由脚本生成。

## 5. 待实施清单（先不做）

1. ✅ 已实现（脚本）：`scripts/build-deb.sh` 一条命令完成 构建 → deb 树 → 图标缩放 → `dpkg-deb` 打包（prod/dev 各一个包）
2. `gdk_set_program_class` cgo 垫片（§4 方式 2）及 `StartupWMClass` 的相应收敛
3. ✅ 已并入脚本（ImageMagick 缩放 16–512 七档，dev 图标自动回落正式图标）
4. ✅ 已定（脚本）：prod `api-doc-client_<version>_<arch>.deb` / dev `api-doc-client-dev_…`，版本取 `VERSION` 环境变量 > `wails.json info.productVersion` > `0.0.0-dev`
5. CI 接入（`.github/workflows`）与发布流程
6. `nfpm` 配置替代手工 deb 树（便于后续多架构/多发行版）
