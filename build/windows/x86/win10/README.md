# 打包流程：Windows · x86(x86_64) · Win10+ —— NSIS 安装包

> 状态：**脚本已实现，尚未在 Windows 构建机实跑验证**（开发机为 Deepin 25，无 `makensis`/`pwsh`，
> 也没有 mingw 交叉工具链）。首次实跑请按文末「验证清单」逐项过一遍。
>
> 目录层级约定：`build/<系统>/<cpu架构>/<os>/`。
> 本目录对应 **windows + x86_64 + Win10 及以上**（NSIS 模板里有 `${AtLeastWin10}` 硬检查）。
> arm64 走同一套脚本（`-Arch arm64`），产物名里的架构段会自动变。

## 1. 目标产物

```
api-doc-client_<version>_amd64-setup.exe        # 正式版
api-doc-client-dev_<version>_amd64-setup.exe    # 开发版（-Dev 时产出，可与正式版并存安装）
```

安装后布局（`machine` 范围，默认）：

| 路径 | 内容 |
|---|---|
| `C:\Program Files\api-doc-go\API Doc Client\api-doc-client.exe` | 正式版可执行文件 |
| `…\API Doc Client\uninstall.exe` | 卸载器 |
| 开始菜单 / 桌面 `API Doc Client.lnk` | 快捷方式 |
| `HKLM\…\Uninstall\api-doc-go.ApiDocClient` | 卸载信息（「程序和功能」里那条） |

开发版把上面每一处的 `API Doc Client` 换成 `API Doc Client Dev`、exe 换成 `api-doc-client-dev.exe`、
注册表键换成 `api-doc-go.ApiDocClientDev` —— **三处都不同才能并存**，少一处就会互相覆盖。

`user` 范围（`-InstallScope user`）改装到 `%LOCALAPPDATA%\Programs\API Doc Client`，注册表落 `HKCU`，不弹 UAC。

## 2. 前置条件（构建机）

- **Windows 10/11**（原生构建；交叉编译见 §6）
- Go 工具链 + Wails CLI（`wails build`）+ Node（前端构建）
- **NSIS**（`makensis` 在 PATH 里）：<https://nsis.sourceforge.io/Download>
  —— ⚠️ 缺它时 `wails build -nsis` **只打印一句 warning 就返回成功**，
  你会得到「构建通过但没有安装包」的假象。脚本开头已硬性检查。
- WebView2 Runtime：**不需要预装**。Wails 把微软的引导器内嵌在自己二进制里，
  构建时写到 `build/windows/installer/tmp/MicrosoftEdgeWebview2Setup.exe`（离线可用），
  安装包运行时会检测目标机器、缺了才联网装。

## 3. 快速开始

```powershell
# 在仓库根目录（api-doc-go-client\）
./scripts/build-win.ps1                      # amd64，仅正式版
./scripts/build-win.ps1 -Dev                 # 正式版 + 开发版
./scripts/build-win.ps1 -Version 1.2.3       # 覆盖版本号
./scripts/build-win.ps1 -Arch arm64
./scripts/build-win.ps1 -InstallScope user   # 免管理员，装到 LOCALAPPDATA
./scripts/build-win.ps1 -SkipBuild           # 复用 build\bin 下已有 exe，只重打安装包
```

若执行策略拦住了脚本：

```powershell
powershell -ExecutionPolicy Bypass -File ./scripts/build-win.ps1 -Dev
```

版本号优先级：`-Version` 参数 > `wails.json` 的 `info.productVersion` > `0.0.0-dev`。
**必须是 `x.y.z` 三段数字** —— NSIS 的 `VIProductVersion` 会在后面补 `.0` 凑四段，
带 `-dev` 之类后缀会直接报错，脚本已提前拦下。

## 4. 打包流程

### 步骤 1：构建 exe

```powershell
wails build -platform windows/amd64 -clean -o api-doc-client          # 正式版
wails build -platform windows/amd64 -tags devbuild -o api-doc-client-dev   # 开发版
```

> ⚠️ 两个坑（Linux 侧实跑踩过，Windows 同理）：
> 1. **不要用 `-tags dev`**：`dev` 是 Wails v2.16 库内部保留标签（`internal/app/app_dev.go`），
>    传它会把库内两份互斥实现同时激活，报 `App.Run already declared`。项目自有标签统一用 `devbuild`。
> 2. **第二次构建不要带 `-clean`**：`-clean` 会清空 `build\bin`，把先构建好的正式版 exe 删掉。

### 步骤 2：NSIS 工程从哪来

`build/windows/installer/` 下的三个文件由 **`wails build -nsis` 首次运行时自动生成**：

| 文件 | 作用 | 入库？ |
|---|---|---|
| `project.nsi` | 安装包主工程（页面、快捷方式、卸载逻辑） | ✅ 入库，可自定义 |
| `wails_tools.nsh` | 架构检查、WebView2 安装、写卸载注册表等宏 | ✅ 入库（标注 DO NOT EDIT，升级 Wails 后重新生成） |
| `tmp/MicrosoftEdgeWebview2Setup.exe` | WebView2 引导器（~2MB） | ❌ 已 gitignore，每次由 wails 重写 |

脚本用「`project.nsi` 或引导器任一缺失」判断是否需要走一次 `-nsis` 引导；
两者都齐全时就只跑普通 `wails build`，避免 wails 顺手产出一个默认命名的安装包造成混淆。

> 新克隆仓库后第一次必须**不带** `-SkipBuild` 跑，否则引导器不存在，makensis 会在
> `File "tmp\MicrosoftEdgeWebview2Setup.exe"` 处失败（脚本已提前拦下并给出提示）。

### 步骤 3：makensis 打包（脚本自己调，不经过 wails）

`wails_tools.nsh` 里每个宏都是 `!ifndef` 守护的，**命令行 `-D` 一定优先**。脚本据此覆盖：

| 宏 | 正式版 | 开发版 | 决定什么 |
|---|---|---|---|
| `INFO_PROJECTNAME` | `api-doc-client` | `api-doc-client-dev` | `OutFile` 名（两者不撞） |
| `INFO_PRODUCTNAME` | `API Doc Client` | `API Doc Client Dev` | 安装目录、快捷方式、卸载显示名 |
| `PRODUCT_EXECUTABLE` | `api-doc-client.exe` | `api-doc-client-dev.exe` | 装完的 exe 名 |
| `UNINST_KEY_NAME` | `api-doc-go.ApiDocClient` | `api-doc-go.ApiDocClientDev` | 卸载注册表键 |
| `INFO_PRODUCTVERSION` | `$Version` | `$Version` | 版本信息 / 卸载列表里的版本 |
| `INFO_COMPANYNAME` | `api-doc-go` | `api-doc-go` | `machine` 范围下的安装目录上一层 |

> `PRODUCT_EXECUTABLE` 必须显式覆盖：它的默认值是 `${INFO_PROJECTNAME}.exe`，
> 而 `wails.json` 的 `name` 是 `api-doc-go-client`，不覆盖就会装出 `api-doc-go-client.exe`，
> 与 Linux 侧的 `/usr/bin/api-doc-client` 对不上。

makensis 必须在 `build/windows/installer/` 目录里执行 —— `project.nsi` 全用相对路径
（`!include "wails_tools.nsh"`、`File "tmp\…"`、`MUI_ICON "..\icon.ico"`、`OutFile "..\..\bin\…"`）。

产物先落在 `build/bin/<INFO_PROJECTNAME>-<arch>-installer.exe`，脚本再 `Move-Item` 成 §1 的规范名。

### 步骤 4：安装与验证

```powershell
.\build\bin\api-doc-client_0.1.0_amd64-setup.exe        # 交互安装
.\build\bin\api-doc-client_0.1.0_amd64-setup.exe /S     # 静默安装
```

## 5. 验证清单（首次实跑请逐项确认）

- [ ] `makensis` 在 PATH 里（脚本会检查，但先手动 `makensis /VERSION` 确认）
- [ ] 不带 `-SkipBuild` 跑一次，确认 `build/windows/installer/` 下生成了 `project.nsi`、
      `wails_tools.nsh`、`tmp/MicrosoftEdgeWebview2Setup.exe`
- [ ] 产物名与 §1 一致，大小合理（正式版 exe ≈ 37MB，安装包应略大于它）
- [ ] 安装后 exe 名是 `api-doc-client.exe`（**不是** `api-doc-go-client.exe`）
- [ ] 开始菜单 + 桌面快捷方式能启动，图标是 `build/windows/icon.ico`
- [ ] 「程序和功能」里显示 `API Doc Client` + 正确版本号
- [ ] `-Dev` 时：正式版与开发版能**同时装、同时跑、各自独立卸载**，任务栏不并组
- [ ] `-InstallScope user` 时不弹 UAC，装到 `%LOCALAPPDATA%\Programs\API Doc Client`
- [ ] 卸载后安装目录、快捷方式、注册表键都清干净
- [ ] 目标机没有 WebView2 Runtime 时，安装包能自动装上（可用干净的 Win10 虚拟机验证）
- [ ] 托盘：开启「驻留托盘」后关窗隐藏、**双击托盘图标**能唤出主窗口、右键菜单正常

## 6. 从 Linux 交叉编译（可选，未验证）

```bash
sudo apt install gcc-mingw-w64-x86-64 nsis     # 交叉编译器 + makensis
wails build -platform windows/amd64 -o api-doc-client
# 之后照 §4 步骤 3 手动调 makensis（注意路径分隔符要换成反斜杠）
```

本机（Deepin 25）实测**没有** mingw 工具链，`wails build -platform windows/amd64` 会失败，
所以脚本按「在 Windows 上原生跑」设计。交叉编译这条路只作备选，未验证。

## 7. 待实施清单

1. ✅ 已实现（脚本）：`scripts/build-win.ps1` 一条命令完成 构建 → NSIS 打包 → 规范化命名（prod/dev 各一个）
2. ⬜ **首次 Windows 实跑**：按 §5 逐项验证（本机无环境，脚本未实跑过）
3. ⬜ 代码签名：`project.nsi` 里已留好 `!finalize 'signtool --file "%1"'` 注释位，
      取消注释并配好证书即可（不签名的话 SmartScreen 会拦一次）
4. ⬜ CI 接入（`.github/workflows`，windows-latest + `choco install nsis`）
5. ⬜ 自动更新（Wails v2 无内置方案；需要的话考虑 `go-update`/自建检查接口）
6. ⬜ `arm64` 实跑验证（脚本已支持 `-Arch arm64`，但没在 arm64 Windows 上跑过）

## 8. 与其它平台文档的关系

- Linux deb：`build/linux/x86/deepin25/README.md`（已实跑打通）
- 本地免安装入口（开发用）：`build/linux/install.sh`
- 两个平台的可执行文件名统一为 `api-doc-client` / `api-doc-client-dev`，
  这是任务栏图标匹配的前提（Linux 靠 WM_CLASS，Windows 靠 AppUserModelID/进程名），
  改名前请先读 Linux 那份 README 的 §4。
