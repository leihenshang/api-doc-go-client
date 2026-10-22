# 打包流程：Windows · x86(x86_64) · Win10+ —— NSIS 安装包

> 状态：**脚本与手写 `installer.nsi` 已落地，并已在 Windows 实跑产出 v0.1.0 安装包（含覆盖升级）；
> arm64 未实跑**。安装包逻辑的权威来源是
> `build/windows/installer/installer.nsi`（入库、wails 不感知），不是 `wails build -nsis` 生成的 `project.nsi`。
>
> 目录层级约定：`build/<系统>/<cpu架构>/<os>/`。
> 本目录对应 **windows + x86_64 + Win10 及以上**（安装器校验架构与系统）。
> arm64 走同一套脚本（`-Arch arm64`），产物名里的架构段会自动变。

## 1. 目标产物

```
api-doc-client_<version>_<arch>-setup.exe        # 正式版（arch=amd64|arm64）
api-doc-client-dev_<version>_<arch>-setup.exe    # 开发版（-Dev 时产出，可与正式版并存安装）
```

默认安装布局（`machine` 范围，需管理员）：

| 路径 | 内容 |
|---|---|
| `C:\Program Files\api-doc-go\API Doc Client\api-doc-client.exe` | 正式版可执行文件 |
| `…\API Doc Client\uninstall.exe` | 卸载器 |
| 开始菜单 / 桌面 `API Doc Client.lnk` | 快捷方式 |
| `HKLM\…\Uninstall\api-doc-go.ApiDocClient` | 卸载信息（「程序和功能」里那条），含 `InstallLocation` |

开发版把 `API Doc Client` 换成 `API Doc Client Dev`、exe 换成 `api-doc-client-dev.exe`、
注册表键换成 `api-doc-go.ApiDocClientDev` —— **三处都不同才能并存**，少一处就会互相覆盖。

`user` 范围（`-InstallScope user`）装到 `%LOCALAPPDATA%\Programs\API Doc Client`，注册表落 `HKCU`，不弹 UAC。
**machine 与 user 是两个独立安装**（不同目录 / 不同注册表 hive），升级时请固定同一 `-InstallScope`。

> 安装器会从注册表 `InstallLocation` 回填上次安装目录（W-3）：装过非默认目录的机器，升级不会另起一份。

## 2. 前置条件（构建机）

- **Windows 10/11**（原生构建；交叉编译见 §6）
- Go 1.26+、Wails CLI（`wails build`）、Node（前端构建）
- **NSIS**：<https://nsis.sourceforge.io/Download>
  - **不要求 `makensis` 在 PATH**：脚本先查 PATH，再回落到默认安装目录（`Program Files (x86)\NSIS` /
    `Program Files\NSIS`），拿到绝对路径再调用。
  - ⚠️ 完全没装 NSIS 时脚本开头会直接报错（`wails build -nsis` 缺 makensis 时只打印 warning 就返回成功，
    会得到「构建通过但没有安装包」的假象）。
  - ⚠️ `installer.nsi` 含中文注释，**必须保持 UTF-8 带 BOM**：缺 BOM 时 makensis 在中文 Windows 上可能报错/乱码。
- WebView2 Runtime：**不需要预装**。Wails 把微软引导器内嵌在二进制里，首次 bootstrap 写到
  `build/windows/installer/tmp/MicrosoftEdgeWebview2Setup.exe`（离线可用），安装时检测目标机、缺了才装。

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

版本号优先级：`-Version` 参数 > `wails.json` 的 `info.productVersion`（当前 `0.1.0`）> `0.0.0-dev`。
**必须是 `x.y.z` 三段数字** —— NSIS 的 `VIProductVersion` 会在后面补 `.0` 凑四段，
带 `-dev` 之类后缀会直接报错，脚本已提前拦下。

## 4. 打包流程

### 步骤 1：构建 exe

```powershell
wails build -platform windows/amd64 -clean -o api-doc-client.exe          # 正式版
wails build -platform windows/amd64 -tags devbuild -o api-doc-client-dev.exe   # 开发版
```

> ⚠️ 三个坑（实跑踩过）：
> 1. **不要用 `-tags dev`**：`dev` 是 Wails v2.16 库内部保留标签（`internal/app/app_dev.go`），
>    传它会把库内两份互斥实现同时激活，报 `App.Run already declared`。项目自有标签统一用 `devbuild`。
> 2. **第二次构建不要带 `-clean`**：`-clean` 会清空 `build\bin`，把先构建好的正式版 exe 删掉。
> 3. **`-o` 必须显式带 `.exe`**：wails 只在「未传 `-o`」时才自动补后缀，传了就原样使用。
>
> 脚本用「`wails_tools.nsh` 或 WebView2 引导器任一缺失」判断是否需要带 `-nsis` 做一次 bootstrap；
> 两者齐全时只跑普通 `wails build`，并删除 wails 顺手产出的默认命名安装包。

### 步骤 2：NSIS 工程从哪来

| 文件 | 作用 | 入库？ |
|---|---|---|
| `installer.nsi` | **安装包主工程（权威来源）**：页面、快捷方式、卸载、覆盖升级加固 | ✅ 入库，手写维护 |
| `wails_tools.nsh` | 架构检查、WebView2 安装、写卸载注册表等宏（由 `wails build -nsis` 生成） | ❌ 不入库（`.gitignore`；wails 生成，勿手改） |
| `tmp/MicrosoftEdgeWebview2Setup.exe` | WebView2 引导器（~2MB） | ❌ 已 gitignore，每次由 wails 重写 |
| `project.nsi` | wails 生成的默认工程 | **不使用**（每次 bootstrap 会被重写，避免在该文件上做定制） |

> 为什么不用 `project.nsi`：`wails build -nsis` 每次 bootstrap 都会重写 `project.nsi` / `wails_tools.nsh`，
> 直接改会被覆盖。`installer.nsi` 只 `!include "wails_tools.nsh"` 复用宏，自身不受 wails 影响，
> 因此可安全承载自定义升级逻辑。升级 Wails 后若需重生成宏库：删掉 `wails_tools.nsh`（及 `tmp/`）再构建一次即可。
>
> 新克隆仓库后第一次必须**不带** `-SkipBuild` 跑，否则宏库/引导器不存在，makensis 会失败
> （脚本已提前拦下并给出提示）。

`installer.nsi` 相比默认工程新增的加固：

| 编号 | 行为 |
|---|---|
| W-1 | 升级前用 `nsExec` 静默执行 `taskkill` 关闭运行中的客户端（不闪控制台黑框），避免 exe 占用导致覆盖失败 |
| W-2 | 升级 = 「先静默卸载旧版再装」重装模型（`uninstall.exe /S _?=<dir>`），杜绝旧文件残留 |
| W-3 | `InstallDirRegKey` + 补写 `InstallLocation`，记住并复用上次安装目录 |
| W-4 | WebView2 未就绪时给出明确指引；静默安装写 `errorlevel 16388` 不阻断脚本 |

### 步骤 3：makensis 打包（脚本自己调，不经过 wails）

`wails_tools.nsh` / `installer.nsi` 的宏都用 `!ifndef` 守护，**命令行 `-D` 一定优先**。脚本注入：

| 宏 | 正式版 | 开发版 | 决定什么 |
|---|---|---|---|
| `INFO_PROJECTNAME` | `api-doc-client` | `api-doc-client-dev` | `OutFile` 名（两者不撞） |
| `INFO_PRODUCTNAME` | `API Doc Client` | `API Doc Client Dev` | 安装目录、快捷方式、卸载显示名 |
| `PRODUCT_EXECUTABLE` | `api-doc-client.exe` | `api-doc-client-dev.exe` | 装完的 exe 名 |
| `UNINST_KEY_NAME` | `api-doc-go.ApiDocClient` | `api-doc-go.ApiDocClientDev` | 卸载注册表键 |
| `INFO_PRODUCTVERSION` / `INFO_COPYRIGHT` | `$Version` / `Copyright (C) 2026 api-doc-go` | 同左 | 版本信息 |
| `INFO_COMPANYNAME` | `api-doc-go` | `api-doc-go` | `machine` 范围下的安装目录上一层 |
| `ARG_WAILS_AMD64_BINARY` / `ARG_WAILS_ARM64_BINARY` | 实际二进制路径 | 同左 | 架构与源文件 |
| `WAILS_INSTALL_SCOPE` / `REQUEST_EXECUTION_LEVEL` | 仅 `-InstallScope user` 时注入 | 同左 | 安装范围与 UAC |

> `PRODUCT_EXECUTABLE` 必须显式覆盖：默认值是 `${INFO_PROJECTNAME}.exe`，
> 而 `wails.json` 的 `name` 是 `api-doc-go-client`，不覆盖就会装出 `api-doc-go-client.exe`，
> 与 Linux 侧的 `/usr/bin/api-doc-client` 对不上。

makensis 必须在 `build/windows/installer/` 目录里执行 —— `installer.nsi` 全用相对路径
（`!include "wails_tools.nsh"`、`File "tmp\…"`、`MUI_ICON "..\icon.ico"`、`OutFile "..\..\bin\…"`）。

产物先落在 `build/bin/<INFO_PROJECTNAME>-<arch>-installer.exe`，脚本再 `Move-Item` 成 §1 的规范名。

### 步骤 4：安装、升级与验证

```powershell
.\build\bin\api-doc-client_0.1.0_amd64-setup.exe        # 交互安装
.\build\bin\api-doc-client_0.1.0_amd64-setup.exe /S     # 静默安装
```

覆盖升级（检测到旧版卸载器时）：

1. 交互模式先弹确认（提示未保存修改会丢失，取消即中止）；
2. 以隐藏控制台方式执行 `taskkill /IM <PRODUCT_EXECUTABLE> /F` 关闭运行中的客户端（`nsExec`，不弹黑框）；
3. 以隐藏控制台方式执行 `uninstall.exe /S _?=<旧目录>` 原地静默卸载旧版；
4. 按 `InstallLocation` 回填的目录全新安装（静默安装 `/S` 跳过第 1 步确认）。

> 从**未写 `InstallLocation` 的旧安装包**（早期 wails 默认工程）升级时，首次不会被自动卸载；
> 请先手动卸载旧版再安装，之后的新版本即可走自动覆盖升级。

## 5. 验证清单

- [ ] 不带 `-SkipBuild` 跑一次：`installer.nsi`（入库）存在，且 bootstrap 后生成了 `wails_tools.nsh`、
      `tmp/MicrosoftEdgeWebview2Setup.exe`
- [ ] 产物名与 §1 一致，大小合理（正式版 exe ≈ 36–37MB；NSIS 压缩后安装包约 16MB，**不必大于 exe**）
- [ ] 安装后 exe 名是 `api-doc-client.exe`（**不是** `api-doc-go-client.exe`）
- [ ] 开始菜单 + 桌面快捷方式能启动，图标是 `build/windows/icon.ico`
- [ ] 「程序和功能」里显示 `API Doc Client` + 正确版本号
- [ ] **覆盖升级**：旧版运行中时安装新版 → 先确认 → 进程被关闭 → 旧版被静默卸载 → 新版装到同一目录，
      无旧文件残留、任务栏不出现两个图标
- [ ] 自定义安装目录后再次升级 → 仍装到原目录（`InstallLocation` 生效）
- [ ] `-Dev` 时：正式版与开发版能**同时装、同时跑、各自独立卸载**
- [ ] `-InstallScope user` 时不弹 UAC，装到 `%LOCALAPPDATA%\Programs\API Doc Client`
- [ ] 卸载后安装目录、快捷方式、注册表键都清干净
- [ ] 目标机没有 WebView2 Runtime 时：交互模式给出安装指引；`/S` 返回 `16388`
- [ ] 托盘：开启「任务栏驻留」并选择「放入托盘」后关窗隐藏、**双击托盘图标**能唤出主窗口、右键菜单正常

## 6. 从 Linux 交叉编译（可选，未验证）

```bash
sudo apt install gcc-mingw-w64-x86-64 nsis     # 交叉编译器 + makensis
# 首次带 -nsis：生成 wails_tools.nsh 与 WebView2 引导器（后续可去掉）
wails build -platform windows/amd64 -nsis -o api-doc-client.exe
# 之后照 §4 步骤 3 手动调 makensis（注意路径分隔符要换成反斜杠）
```

本机（Deepin 25）实测**没有** mingw 工具链，`wails build -platform windows/amd64` 会失败，
所以脚本按「在 Windows 上原生跑」设计。交叉编译这条路只作备选，未验证。

## 7. 待实施清单

1. ✅ 已实现（脚本）：`scripts/build-win.ps1` 一条命令完成 构建 → NSIS 打包 → 规范化命名（`-Dev` 时 prod/dev 各一个）
2. ✅ 已在 Windows 实跑产出 v0.1.0 安装包，并修正：`makensis` 默认目录探测、`-o` 显式 `.exe`、
   手写 `installer.nsi` 承载覆盖升级（W-1〜W-4）；换构建机请按 §5 复验
3. ⬜ 代码签名：`installer.nsi` 里可加 `!finalize 'signtool --file "%1"'`，配好证书即可
   （不签名的话 SmartScreen 会拦一次）
4. ⬜ CI 接入（`.github/workflows`，windows-latest + `choco install nsis`）
5. ⬜ 自动更新（Wails v2 无内置方案；需要的话考虑 `go-update`/自建检查接口）
6. ⬜ `arm64` 实跑验证（脚本已支持 `-Arch arm64`，但没在 arm64 Windows 上跑过）

## 8. 与其它平台文档的关系

- Linux deb（x86）：`build/linux/x86/deepin25/README.md`
- Linux deb（arm）：`build/linux/arm/deepin25/README.md`
- 本地免安装入口（开发用）：`build/linux/install.sh`
- 两个平台的可执行文件名统一为 `api-doc-client` / `api-doc-client-dev`，
  这是任务栏图标匹配的前提（Linux 靠 WM_CLASS，Windows 靠 AppUserModelID/进程名），
  改名前请先读 Linux 那份 README 的 **§3 步骤 4（WM_CLASS 约定）**。
