# 打包流程：Linux · arm(aarch64) · Deepin 25 —— deb 安装包

> 状态：**脚本已支持 `arm64` 参数（与 x86 共用同一份 deb 模板），但尚未在 arm64 上实跑**。
> 流程主体与 `../../x86/deepin25/README.md` 完全一致，本文只记录 arm64 的差异点；
> 共性步骤（deb 目录树、`.desktop`、`StartupWMClass`、图标、验证）请以上游文档为准。
>
> 目录层级约定：`build/<系统>/<cpu架构>/<os>/`。
> 本目录对应 **linux + arm64 + deepin25**，dpkg 架构为 **arm64**。

## 1. 与 x86 的差异

| 项 | x86/deepin25 | arm/deepin25（本目录） |
|---|---|---|
| 打包命令 | `scripts/build-deb.sh` | `scripts/build-deb.sh arm64` |
| Wails 目标平台 | `linux/amd64` | `linux/arm64` |
| dpkg `Architecture` | `amd64` | `arm64` |
| 构建方式 | 本机原生 | **不建议本机交叉编译**，见 §2 |
| 产物名 | `api-doc-client_<v>_amd64.deb` | `api-doc-client_<v>_arm64.deb` |
| 二进制安装名 | `/usr/bin/api-doc-client` | 同左（`WM_CLASS` 约定不变） |

`.desktop`、`StartupWMClass`、图标、依赖库名等与 x86 完全相同（脚本按 `StartupWMClass=$base` 生成，
`Depends` 为 `libgtk-3-0, libwebkit2gtk-4.1-0 | libwebkit2gtk-4.0-37` 交替依赖）。

## 2. 构建机要求（重要）

Wails 的 Linux 前端走 **cgo + GTK3/WebKit2GTK**，`GOARCH=arm64` 的本机交叉编译需要
aarch64 的 GTK/WebKit 头文件与 sysroot，实践中代价很高。推荐三选一：

1. **arm64 真机/开发板直接构建**（最稳，Deepin 25 arm64 上装好 §前置条件 的依赖后
   `scripts/build-deb.sh arm64`）；
2. **qemu-user 静态 + arm64 根文件系统**做半交叉；
3. **docker buildx（`--platform linux/arm64`）+ arm64 基础镜像**，在容器内安装 arm64 版
   `libgtk-3-dev libwebkit2gtk-4.1-dev` 后构建。

> WM_CLASS 采用「固定二进制名」方案（与上游 §4 方式 1 一致），
> 不依赖 `gdk_set_program_class` cgo 垫片，两种架构行为一致，无额外成本。

## 3. 待实施清单

1. ⬜ 选定并固化构建方式（真机 / qemu / docker buildx），并完成一次 **arm64 实跑**
   （脚本 `arm64` 参数与共享模板已就绪）
2. ✅ 产物命名与版本号规则已定：`api-doc-client{,-dev}_<version>_arm64.deb`，
   版本取 `VERSION` > `wails.json info.productVersion` > `0.0.0-dev`（非法修订段自动 `-`→`~`）
3. ⬜ CI 矩阵（x86、arm 两个 job）接入
4. ✅ 与 x86 共用同一份 deb 模板/脚本（`scripts/build-deb.sh arm64`），仅参数不同
