# 打包流程：Linux · x86(x86_64) · Deepin 25 —— deb 安装包

> 状态：**已在 Deepin 25 实跑打通（2026-10-19，v0.1.0 双包产出）** —— `scripts/build-deb.sh`
> 一条命令完成 构建 → deb 树 → 图标缩放 → `dpkg-deb`。CI 接入等见文末「待实施清单」。
>
> 目录层级约定：`build/<系统>/<cpu架构>/<os>/`。
> 本目录对应 **linux + x86_64 + deepin25**，dpkg 架构为 **amd64**；arm64 见 `../../arm/deepin25/README.md`。

## 0. 快速开始

```bash
# 构建机需：Go 1.26+ + Wails CLI + Node + libgtk-3-dev libwebkit2gtk-4.0-dev（或 4.1）
#           + dpkg-deb + ImageMagick（magick 或 convert）+ binutils（readelf）
# Go/Node 由 vfox 管理时，先激活：eval "$(vfox activate bash)"
scripts/build-deb.sh                 # 默认 amd64，正式 + dev 各出一包
scripts/build-deb.sh arm64           # 交叉打 arm64（需 aarch64 工具链，见 arm README）
VERSION=1.2.3 scripts/build-deb.sh   # 覆盖版本号
SKIP_BUILD=1 scripts/build-deb.sh    # 复用 build/bin 现有二进制（需 prod/dev 两份都在）
```

产物：`build/bin/api-doc-client_<版本>_<arch>.deb` 与 `build/bin/api-doc-client-dev_<版本>_<arch>.deb`。

安装与验证：

```bash
sudo apt install ./build/bin/api-doc-client_<版本>_amd64.deb
gtk-launch api-doc-client        # 必须从入口启动，不要 /usr/bin 裸跑（否则 dock 匹配不到）
```

版本号优先级：`VERSION` 环境变量 > `wails.json info.productVersion`（当前 `0.1.0`）> `0.0.0-dev`。
脚本会做 dpkg 版本合法性校验：`0.1.0-dev` 这类「修订段含非数字」的版本会自动规范化为
`0.1.0~dev`（`~` 排序低于正式版，语义=预发布）；必须以数字开头。

## 1. 目标产物

```
api-doc-client_<version>_<arch>.deb            # 正式版（arch=amd64|arm64）
api-doc-client-dev_<version>_<arch>.deb        # 开发版（可与正式版并存安装）
```

安装后布局：

| 路径 | 内容 |
|---|---|
| `/usr/bin/api-doc-client` | 正式版可执行文件（**固定文件名，见 §4 的 WM_CLASS 约定**） |
| `/usr/bin/api-doc-client-dev` | 开发版可执行文件 |
| `/usr/share/applications/api-doc-client{,-dev}.desktop` | 应用入口 |
| `/usr/share/icons/hicolor/<尺寸>/apps/api-doc-client{,-dev}.png` | 主题图标（16/32/48/64/128/256/512） |

## 2. 前置条件（构建机）

- Go 1.26+、Wails CLI（`wails build`）、Node（前端构建）
- GTK3 / WebKit2GTK 开发头文件（Wails Linux 前端为 cgo，必须有）：
  `libgtk-3-dev libwebkit2gtk-4.0-dev`（Deepin 25 实测为 4.0；若无 4.0 头文件可改 `-tags webkit2_41`
  构建 4.1；Depends 由脚本按实际 soname 生成）
- 打包工具：`dpkg-deb`；图标缩放：ImageMagick（`magick` 或 `convert`）；
  依赖探测：`binutils` 的 `readelf`（gcc 工具链通常已带）
- x86_64 为**本机原生构建**，无需交叉编译环境

> 脚本当前**未暴露 `webkit2_41` 标签**（默认按 4.0 构建）。若构建机只有 4.1 头文件，先手动：
> `wails build -tags webkit2_41 -o api-doc-client` 与 `wails build -tags "devbuild webkit2_41" -o api-doc-client-dev`，
> 再 `SKIP_BUILD=1 scripts/build-deb.sh` 打包。打包时脚本会用 `readelf` 按实际 soname 生成精确 `Depends`，无需改 control。

## 3. 打包流程

### 步骤 1：构建二进制

```bash
# 正式版（build 标签会内嵌 frontend/dist，需先构建前端）
wails build -platform linux/amd64 -clean -o api-doc-client

# 开发版（devbuild 标签：开发图标 + 开发版程序名，见 internal/appicon、internal/devicon）
wails build -platform linux/amd64 -tags devbuild -o api-doc-client-dev
```

> ⚠️ 两个坑（2026-10-19 实跑踩过）：
> 1. **不要用 `-tags dev`**：Wails v2.16 库内部保留 `dev`/`production` 标签（`internal/app/app_dev.go` 等），
>    传 `dev` 会把库内两份互斥实现同时激活，报 `App.Run already declared`。项目自有标签统一用 `devbuild`
>    （源文件里写作 `dev || devbuild`，兼容 `wails dev` 自动注入的 `dev`）。
> 2. **第二次构建不要带 `-clean`**：`-clean` 会清空 `build/bin`，把先构建好的正式版二进制删掉。

产物落在 `build/bin/`（已 gitignore），打包时拷入 deb 树。`SKIP_BUILD=1` 时脚本兼容旧产物名
`api-doc-client-dev-linux-amd64`（会复制成 `api-doc-client-dev`）。

### 步骤 2：组织 deb 目录树

```
pkg/
  DEBIAN/
    control
    postinst
    postrm        # 卸载后刷新缓存（不删用户配置）
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
Depends: libgtk-3-0, libwebkit2gtk-4.0-37
Installed-Size: <KiB>
Description: AI-native, local-first API client
 Markdown 风格接口文档协作工具的桌面客户端。
```

> Depends 由脚本用 `readelf -d` 读取二进制 DT_NEEDED **自动生成精确项**（prod/dev 必须一致，否则报错），
> 不再固定写交替依赖。默认构建（无 webkit 标签）链 `libwebkit2gtk-4.0.so.37` →
> `libwebkit2gtk-4.0-37`；`-tags webkit2_41` 则链 4.1 → `libwebkit2gtk-4.1-0`。
> 也可构建后手动 `ldd build/bin/api-doc-client | grep -i webkit` 复核。

### 步骤 4：`.desktop`（关键，决定任务栏图标）

```ini
[Desktop Entry]
Type=Application
Name=API Doc Client
GenericName=API Client
Comment=AI-native、local-first 的 API 客户端
Exec=/usr/bin/api-doc-client
Icon=api-doc-client
Terminal=false
StartupWMClass=api-doc-client
Categories=Development;Utility;
StartupNotify=true
```

要点：
- `Icon=` 用**主题名** `api-doc-client`（对应 §1 的 hicolor 路径），不要写绝对路径；
- `StartupWMClass` **必须与窗口真实 `WM_CLASS` 匹配**，否则 DDE dock 匹配不到应用，只显示默认图标。
  当前实现里 `WM_CLASS` 来自**可执行文件名**（GTK 的 `gdk_set_progname` 链路），Wails 的
  `linux.Options.ProgramName`（只调 `g_set_prgname`）对它无效——已在 deepin25 实测确认。
- **采用「固定二进制名」方案（方式 1）**：安装名固定为 `api-doc-client` / `api-doc-client-dev`，
  `StartupWMClass` 相应写同名（脚本即按 `StartupWMClass=$base` 生成）。这样 dev 与 prod 的 WM_CLASS
  不同，dock 不会并组。
- 方式 2（`gdk_set_program_class` cgo 垫片）可让二进制随便改名也匹配，但**当前未采用**（无必要）。

### 步骤 5：图标多尺寸

源文件 `build/appicon.png`（1024×1024）缩放为 16/32/48/64/128/256/512 后分别装入
`/usr/share/icons/hicolor/<尺寸>/apps/api-doc-client.png`；dev 图标用
`internal/appicon/appicon_dev.png` 同法处理，命名为 `api-doc-client-dev.png`（缺失时回落正式图标）。

### 步骤 6：`DEBIAN/postinst` / `postrm`

脚本生成的 `postinst` 刷新桌面数据库与图标缓存，失败**打 warning 但不阻断安装**；`postrm` 同样刷新
桌面数据库（不删用户配置）：

```sh
#!/bin/sh
update-desktop-database -q /usr/share/applications 2>/dev/null || echo "warning: update-desktop-database 刷新失败（不影响安装）" >&2
gtk-update-icon-cache -q -t -f /usr/share/icons/hicolor 2>/dev/null || echo "warning: gtk-update-icon-cache 刷新失败（不影响安装）" >&2
```

### 步骤 7：打包与安装

```bash
chmod 755 pkg/DEBIAN/postinst pkg/DEBIAN/postrm
dpkg-deb --build --root-owner-group pkg api-doc-client_<version>_amd64.deb

# 安装
sudo apt install ./api-doc-client_<version>_amd64.deb   # 或 sudo dpkg -i
```

### 步骤 8：安装后验证

```bash
gtk-launch api-doc-client                 # 从入口启动（不要用 /usr/bin 裸跑）
xprop -id $(xdotool search --class api-doc-client | head -1) WM_CLASS _NET_WM_ICON
```

- `WM_CLASS` 应等于 `.desktop` 的 `StartupWMClass`（当前=二进制名 `api-doc-client`）；
- `_NET_WM_ICON` 应为 `Icon (N x N)`（Wails 会设置，图标本体不缺；dock 取图标靠的是入口匹配）；
- **已运行中的旧窗口要重启应用**才会按新入口重新匹配；
- Deepin 25 当前会话为 X11；若后续切 Wayland，窗口图标只认 `.desktop` 入口，本文档的入口匹配要求依然成立。

## 4. 与 `build/linux/` 现有文件的关系

`build/linux/{api-doc-client,api-doc-client-dev}.desktop` 与 `install.sh` 是**本地免安装入口**
（Exec/Icon 用项目绝对路径，装到 `~/.local/share/applications/`），**独立维护，打包脚本不会修改它们**。
`scripts/build-deb.sh` 只在 deb 内部生成系统级入口（`/usr/share/applications/*.desktop`，Exec 指 `/usr/bin`、
Icon 用主题名），与本地的这两个文件互不影响。

本地入口用法：`build/linux/install.sh`（装 prod+dev 两个入口；`--prod` / `--dev` 单独装），
Exec/Icon 使用项目绝对路径，移动项目目录后重跑一次即可刷新。

## 5. 待实施清单

1. ✅ 已实现（脚本）：`scripts/build-deb.sh` 一条命令完成 构建 → deb 树 → 图标缩放 → `dpkg-deb` 打包
   （prod/dev 各一个包），**2026-10-19 已在 Deepin 25 实跑产出 v0.1.0 双包**
2. ✅ 已采用「固定二进制名」方案：安装名固定 `api-doc-client{,-dev}`，脚本生成 `StartupWMClass=$base`；
   方式 2（`gdk_set_program_class` cgo 垫片）未做，当前不需要
3. ✅ 已并入脚本（ImageMagick 缩放 16–512 七档，dev 图标自动回落正式图标）
4. ✅ 已定（脚本）：prod `api-doc-client_<version>_<arch>.deb` / dev `api-doc-client-dev_…`，
   版本取 `VERSION` 环境变量 > `wails.json info.productVersion`（当前 `0.1.0`）> `0.0.0-dev`，
   非法修订段自动 `-`→`~`
5. ⬜ CI 接入（`.github/workflows`）与发布流程
6. ⬜ `nfpm` 配置替代手工 deb 树（便于后续多架构/多发行版）
7. ✅ `Depends` 已按实际链接的 soname 自动生成（`readelf -d`，prod/dev 一致校验），不再写交替依赖
