# 安装包覆盖升级：问题与改进方案（Windows / Linux）

> 范围：桌面客户端两条打包链路的「重复安装 = 覆盖升级」行为分析。
> - Windows：`scripts/build-win.ps1` → NSIS（`build/windows/installer/project.nsi` + `wails_tools.nsh`，均由 `wails build -nsis` 生成）
> - Linux：`scripts/build-deb.sh` → `.deb`（`dpkg-deb` 打包，`apt/dpkg` 安装）
>
> 结论先行：**Windows(NSIS) 的三大隐患本质是"NSIS 只是解压器、没有包管理器语义"；Linux 因走 dpkg 这三点被天然规避，但 deb 侧另有风险点**。

---

## 一、Windows（NSIS）

### 1.1 现状：能覆盖升级，但属于「原地覆盖」而非「卸载重装」

- **安装路径不含版本号**，是写死的固定目录（`project.nsi` 75–83 行）：
  - machine 范围：`C:\Program Files\api-doc-go\API Doc Client`
  - user 范围：`%LOCALAPPDATA%\Programs\API Doc Client`
- **`File` 默认覆盖**：`wails.files` 宏（`wails_tools.nsh` 100–112 行）用 `/oname=${PRODUCT_EXECUTABLE}` 写入 exe，同名直接替换。
- **卸载注册表键固定**（`UNINST_KEY_NAME`），`writeUninstaller`（114–155 行）每次 `WriteRegStr` 覆盖，`DisplayVersion` 更新为新版本。

> 只要走默认目录、同架构、正式版对正式版，双击新包即一次正常覆盖升级。

### 1.2 问题清单

| 编号 | 问题 | 触发条件 | 严重度 | 状态 |
|---|---|---|---|---|
| W-1 | **运行中的 exe 被占用导致覆盖失败**：未用 `/REBOOTOK`、未用 `MoveFileOnReboot`，`File` 覆盖被锁文件会弹 "Failed to overwrite" | 升级前没关闭客户端 | 高 | ✅ 已落实 |
| W-2 | **没有「检测旧版→先静默卸载」的 `.onInit`**（`project.nsi` 86–88 行只做架构检查）：纯原地覆盖，旧文件不清理，若版本间删/改名文件会残留孤儿文件 | 文件集合发生增删改名时 | 中 | ✅ 已落实 |
| W-3 | **未设 `InstallDirRegKey`，注册表也未写 `InstallLocation`**：安装器不记得用户上次自选目录，重装会回落到硬编码默认目录，产生两份安装、且固定 `UNINST_KEY` 被覆盖指向新目录，旧目录成卸载不到的孤儿 | 上次手动装到非默认目录 | 中 | ✅ 已落实 |
| W-4 | **machine / user 范围不是覆盖关系**：装到不同目录、写不同 hive（HKLM vs HKCU），混用产生两个独立条目 | 混用不同 `-InstallScope` | 低 | ✅ 已加提示 |
| W-5 | Dev 与正式版是不同 ProductName/目录/卸载键，**互不覆盖**（设计意图，可并存） | —— | 说明项 | ✔ 保持 |

> `wails_tools.nsh` 顶部标注 "DO NOT EDIT - Generated automatically by wails build"，改它会被下次生成覆盖；加固应放在 `project.nsi`，或用 `build-win.ps1` 注入宏 / 生成后补丁。
>
> **落地修正**：本次不直接改 `project.nsi`（会被 wails 重写），而是新增手写、纳入版本库的 **`build/windows/installer/installer.nsi`** 作为安装包逻辑权威来源，`!include` wails 生成的 `wails_tools.nsh` 复用其宏，`build-win.ps1` 改为对 `installer.nsi` 调 `makensis`。详见「五、落地实现」。

---

## 二、Linux（deb）

### 2.1 现状：dpkg 事务化升级，规避了 Windows 三大隐患

- 使用**固定安装前缀**（`/usr/bin`、`/usr/share/...`），无"用户自选目录"概念。
- dpkg 掌握旧包完整文件清单（`/var/lib/dpkg/info/api-doc-client.list`），升级按清单比对。

### 2.2 与 Windows 三大隐患的逐条对比

| Windows 隐患 | Linux 是否存在 | 原因 |
|---|---|---|
| W-1 占用导致覆盖失败 | **基本不存在** | dpkg 替换是「旧文件改名让位 + 新文件落位」，运行进程持旧 inode 不受影响，绕过 `ETXTBSY`；**新版本能装上，但需重启 app 生效**，dpkg 不会帮你重启 |
| W-2 原地覆盖残留孤儿文件 | **基本不存在** | dpkg 升级时新包中不存在的旧文件会被删除 |
| W-3 不记目录、装两份 | **不存在** | 路径固定、无目录可选 |
| W-5 dev/prod 并存 | **一致** | 独立包名 `api-doc-client` / `api-doc-client-dev`，二进制名/`.desktop`/图标主题名均不同（`build-deb.sh` 117–118 行） |

### 2.3 问题清单（deb 侧特有）

| 编号 | 问题 | 触发条件 | 严重度 | 状态 |
|---|---|---|---|---|
| L-1 | **版本非递增时体验不一致**：`apt install ./xxx.deb` 遇相同/更低版本提示"已是最新，不重装"，需 `--reinstall` 或 `dpkg -i`；`VERSION` 兜底 `0.0.0-dev`（`build-deb.sh` 27 行），不改 `wails.json` 会反复同版本被拒重装 | 版本号不严格递增 | 中 | ⚠️ 部分落实 |
| L-2 | **依赖包名写死、随发行版演进失效**：`Depends: libgtk-3-0, libwebkit2gtk-4.0-37`（84 行）中 `libwebkit2gtk-4.0-37` 在 Ubuntu 24.04 / Debian 12+ 已更名 `libwebkit2gtk-4.1-0`，跨版本 apt 可能依赖解析失败装不上 | 新发行版 / 系统升级 | 高 | ✅ 已落实 |
| L-3 | **缺 `Breaks/Replaces/Provides`**：若历史用过别的包名又装到同路径，缺 `Replaces` 会触发"试图覆盖…已在另一包中"（当前无此历史，风险低） | 包名迁移时 | 低 | ➖ 暂不处理 |
| L-4 | **postinst/postrm 用 `|| true` 吞错**（103–109 行）：升级后 `update-desktop-database`/`gtk-update-icon-cache` 失败被静默，可能导致图标/启动项没刷新 | 缓存刷新失败 | 低 | ✅ 已落实 |
| L-5 | control 缺 `Installed-Size`、`Homepage` 等字段 | —— | 说明项 | ✅ Installed-Size / ➖ Homepage 未加 |

---

## 三、改进方案

### 3.1 Windows（NSIS）

1. **让安装器记住目录**（解决 W-3）：
   - `build-win.ps1` 生成后对 `project.nsi` 追加 `InstallDirRegKey HKLM "${UNINST_KEY}" "InstallLocation"`；
   - `writeUninstaller` 需写 `InstallLocation`，但该宏在 `wails_tools.nsh`（不可手改）——改为在 `project.nsi` 主 Section 末尾补 `WriteRegStr HKLM "${UNINST_KEY}" "InstallLocation" "$INSTDIR"`（user 范围对应 HKCU）。

2. **升级前静默卸载旧版**（解决 W-2、间接缓解 W-1）：
   - 在 `.onInit` 读取旧 `QuietUninstallString`，`ExecWait` 静默卸载后再继续；注意自卸载需拷贝到临时目录（NSIS 经典 `un.removeFromInstallPath`/`CopyFiles "$EXEPATH"` 处理）。

3. **文件占用兜底**（解决 W-1）：
   - `wails.files` 生成的 File 若不便改，可在主 Section 前 `SetOverwrite try` + 关键 exe 用 `/REBOOTOK`；或检测进程名并提示关闭后重试。

4. **文档/参数提示**（缓解 W-4）：
   - `build-win.ps1` 输出与帮助里明确「machine / user 需保持一致，否则是两个独立安装」；升级统一走同一 `-InstallScope`。

> 落地方式建议：加固点集中在 `project.nsi`（wails 每次 `-nsis` 会重写它，需要 `build-win.ps1` 用生成后补丁或 `makensis -D` 注入），不要手改 `wails_tools.nsh`。

### 3.2 Linux（deb）

1. **强制版本递增**（解决 L-1）：
   - `build-deb.sh` 打包前校验 `VERSION` 为合法且严格递增的 dpkg 版本；同/低版本直接报错退出，避免产出会被 apt 拒装。

2. **依赖名兼容**（解决 L-2）：
   - 用虚拟包/交替依赖，避免锁死单一 SONAME 包名。示例：
     ```
     Depends: libgtk-3-0, libwebkit2gtk-4.1-0 | libwebkit2gtk-4.0-37
     ```
   - 或在 control 生成处按目标发行版参数化 webkit 包名。

3. **补 `Replaces`/`Provides`/`Breaks`**（解决 L-3）：
   - 若做过包名迁移再加；当前无历史可暂不加，但方案留档。

4. **缓存刷新不要静默吞错**（缓解 L-4）：
   - `postinst`/`postrm` 去掉尾部 `|| true` 或改为失败时打印告警（保留 `set -e` 语义需谨慎，命令不存在可先判 `command -v`）。

5. **补全 control 字段**（解决 L-5）：`Homepage`、`Installed-Size`（`dpkg-query`/du 估算）。

---

## 四、优先级建议

| 优先级 | 平台 | 项 | 理由 |
|---|---|---|---|
| P0 | Linux | L-2 依赖名 | 新发行版直接装不上，影响面最大 |
| P0 | Windows | W-1 占用失败 | 升级最常见的翻车点 |
| P1 | Linux | L-1 版本递增 | 频繁重装场景体验割裂 |
| P1 | Windows | W-3 目录记忆 | 误导用户装出两份 |
| P2 | Windows | W-2 静默卸载 | 文件集变化时才有残留 |
| P2 | Linux | L-4 / L-5 | 细节完善 |

---

## 五、落地实现（本次已完成）

### 5.1 Windows

新增手写、纳入版本库的安装器工程 **`build/windows/installer/installer.nsi`**，作为安装包逻辑权威来源（不再使用 wails 每次重写生成的 `project.nsi`，但仍 `!include` 其宏库 `wails_tools.nsh`）。`scripts/build-win.ps1` 改为对 `installer.nsi` 调 `makensis`。

| 问题 | 实现 |
|---|---|
| W-1 占用失败 | `.onInit` 卸载旧版前先 `taskkill /IM ${PRODUCT_EXECUTABLE} /F` 关闭运行中的客户端 |
| W-2 残留/自覆盖竞争 | `.onInit` 检测旧版 `InstallLocation` 存在且有 `uninstall.exe` → `ExecWait '"<旧目录>\uninstall.exe" /S _?=<旧目录>'` 先静默卸载（重装模型），彻底清理旧文件后再全新安装 |
| W-3 目录记忆 | 加 `InstallDirRegKey HKLM/HKCU "${UNINST_KEY}" "InstallLocation"`（hive 随 scope）；主 Section 在 `wails.writeUninstaller` 后补写 `InstallLocation`；`.onInit` 读旧 `InstallLocation` 回填 `$INSTDIR` |
| W-4 范围提示 | `build-win.ps1` 完成输出追加「machine/user 是两个独立安装，升级请固定同一 `-InstallScope`」提示 |
| 引导/bootstrap 判据 | `build-win.ps1` 的 `-nsis` 触发条件从「project.nsi 缺失」改为「`wails_tools.nsh` 或 WebView2 引导器缺失」（我们不再依赖 project.nsi） |

> ⚠️ **编码约束**：`installer.nsi` 含中文注释，**必须存为带 BOM 的 UTF-8**；无 BOM 时 makensis 按系统 ACP 解析会报 `Bad text encoding`。已用 `makensis` 实编译验证 machine 与 user 两种 scope 均通过。

### 5.2 Linux

改 `scripts/build-deb.sh`：

| 问题 | 实现 |
|---|---|
| L-1 版本 | 打包前校验 `VERSION`：非法字符/非数字开头直接报错退出；`-` 修订段含非数字（如 `0.0.0-dev`）自动规范化为 `0.0.0~dev`（dpkg 合法，`~` 表预发布）。**注**：「严格递增」需外部版本源比对，脚本仅保证合法与语义正确，未做跨历史递增强校验 |
| L-2 依赖名 | `Depends` 改交替依赖 `libgtk-3-0, libwebkit2gtk-4.1-0 \| libwebkit2gtk-4.0-37`，新发行版取 4.1、老系统回落 4.0 |
| L-4 吞错 | `postinst`/`postrm` 去掉 `\|\| true`，失败改为向 stderr 打 `warning:` 且不阻断安装/卸载 |
| L-5 字段 | control 补 `Installed-Size`（`du -sk` 估算 KiB）；`Homepage` 因主仓 URL 未确认**不臆造**，暂不加 |
| L-3 | 当前无包名迁移历史，暂不加 `Replaces/Provides`（方案留档） |

### 5.3 校验记录

- Windows：`makensis` 对 `installer.nsi` 编译 **machine/amd64、user/amd64 均成功**；`build-win.ps1` 通过 PowerShell 解析器 PARSE OK。
- Linux：`bash -n scripts/build-deb.sh` 通过；版本规范化逻辑单测：`0.1.0→0.1.0`、`0.0.0-dev→0.0.0~dev`、`0.1.0-beta.2→0.1.0~beta.2`、`2.0.0-1→2.0.0-1`、非法字符/非数字开头拒绝。
- 未跑完整 `scripts/check.sh`：本次仅改打包脚本与 NSIS 工程，未触及 Go/TS，门禁不覆盖这些文件。

---

## 附：涉及文件

- `scripts/build-win.ps1`（改：指向 installer.nsi、bootstrap 判据、范围提示）
- **`build/windows/installer/installer.nsi`**（新增，手写权威安装包工程，UTF-8 BOM）
- `build/windows/installer/project.nsi`（wails 生成，现**不再使用**）
- `build/windows/installer/wails_tools.nsh`（wails 生成，勿手改，被 installer.nsi !include）
- `scripts/build-deb.sh`（改：版本校验、依赖名、Installed-Size、缓存告警）
