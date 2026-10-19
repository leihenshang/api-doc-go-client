<#
.SYNOPSIS
  Windows 安装包（NSIS）构建脚本 —— 对应 Linux 侧的 scripts/build-deb.sh。

.DESCRIPTION
  一条命令完成：wails 构建 exe → 生成/复用 NSIS 工程 → makensis 打包 → 规范化产物名。

  为什么不完全交给 `wails build -nsis`：
    1. Wails 的 NSIS 产物名固定为 `<wails.json name>-<arch>-installer.exe`，
       且安装后的 exe 名默认取 `${INFO_PROJECTNAME}.exe`（= api-doc-go-client.exe），
       与 Linux 侧的 /usr/bin/api-doc-client 不一致；
    2. 正式版与开发版要能**并存安装**，必须让 ProductName / 安装目录 / 卸载注册表键互不相同，
       而 `wails build -nsis` 没有注入这些宏的入口。
  所以：wails 只负责「构建 exe + 首次生成 NSIS 工程与 WebView2 引导器」，
  安装包本身由本脚本直接调 makensis 并用 -D 覆盖宏来产出（wails_tools.nsh 里每个宏
  都是 !ifndef 守护的，命令行 -D 一定优先）。

  ⚠️ makensis 缺失时 `wails build -nsis` **只打印一句 warning 就返回成功**，
     会得到「构建通过但没有安装包」的假象，因此本脚本开头就硬性检查。

.EXAMPLE
  ./scripts/build-win.ps1                        # amd64，仅正式版
  ./scripts/build-win.ps1 -Dev                   # 正式版 + 开发版（可并存安装）
  ./scripts/build-win.ps1 -Version 1.2.3
  ./scripts/build-win.ps1 -Arch arm64
  ./scripts/build-win.ps1 -InstallScope user     # 装到 %LOCALAPPDATA%\Programs，免管理员
  ./scripts/build-win.ps1 -SkipBuild             # 复用 build\bin 下已有 exe，只重打安装包
#>
[CmdletBinding()]
param(
  [ValidateSet('amd64', 'arm64')]
  [string]$Arch = 'amd64',

  # 版本号：显式 -Version > wails.json info.productVersion > 0.0.0-dev
  [string]$Version = '',

  # 同时产出开发版安装包（与正式版不同产品名/安装目录/卸载键，可并存）
  [switch]$Dev,

  # machine = Program Files（需管理员，默认）；user = %LOCALAPPDATA%\Programs（免提权）
  [ValidateSet('machine', 'user')]
  [string]$InstallScope = 'machine',

  # 复用 build\bin 下已有的 exe，跳过 wails 构建
  [switch]$SkipBuild
)

# 用 Continue 而非 Stop：原生命令（wails/makensis）把进度信息写进 stderr 时，
# Stop 模式会把它当终止错误，把后面的收尾步骤直接打断（dev.ps1 里踩过同一个坑）。
$ErrorActionPreference = 'Continue'
Set-StrictMode -Version Latest

$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root

$BinDir  = Join-Path $Root 'build\bin'
$NsisDir = Join-Path $Root 'build\windows\installer'
$ProjNsi = Join-Path $NsisDir 'project.nsi'
# wails build -nsis 会把内嵌的 WebView2 引导器写到这里；project.nsi 用 File 引用它。
# 它在 .gitignore 里（~2MB 二进制），所以「工程文件在、引导器不在」是新克隆的常态，必须一起判。
$Webview2Setup = Join-Path $NsisDir 'tmp\MicrosoftEdgeWebview2Setup.exe'

# ---- 0) 工具与版本号 ----

function Assert-Tool {
  param([string]$Name, [string]$Hint)
  if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
    throw "缺少 $Name。$Hint"
  }
}

Assert-Tool -Name 'makensis' -Hint '安装 NSIS（https://nsis.sourceforge.io/Download）并确保它在 PATH 里。'
if (-not $SkipBuild) {
  Assert-Tool -Name 'wails' -Hint '安装 Wails CLI：go install github.com/wailsapp/wails/v2/cmd/wails@latest'
}

if (-not $Version) {
  try {
    $wailsJson = Get-Content (Join-Path $Root 'wails.json') -Raw | ConvertFrom-Json
    $Version = $wailsJson.info.productVersion
  } catch {
    $Version = $null
  }
}
if (-not $Version) { $Version = '0.0.0-dev' }

# NSIS 的 VIProductVersion 要「三段数字」，project.nsi 会再补 .0 凑成四段
if ($Version -notmatch '^\d+\.\d+\.\d+$') {
  throw "版本号 '$Version' 不是 x.y.z 形式：NSIS 的 VIProductVersion 只接受三段数字（去掉 -dev 之类后缀再传）。"
}

Write-Host "架构=$Arch  版本=$Version  安装范围=$InstallScope  开发版=$([bool]$Dev)" -ForegroundColor Cyan

# ---- 1) 构建二进制 ----
#
# NSIS 工程或 WebView2 引导器缺失时必须带 -nsis：只有这条通道会让 wails 写出
# project.nsi / wails_tools.nsh / tmp\MicrosoftEdgeWebview2Setup.exe（引导器内嵌在 wails 里，离线可用）。
# 两者都齐全时就不带 -nsis，免得它顺手产出一个默认命名的安装包造成混淆。

if (-not $SkipBuild) {
  $needBootstrap = (-not (Test-Path $ProjNsi)) -or (-not (Test-Path $Webview2Setup))

  $wailsArgs = @('build', '-platform', "windows/$Arch", '-clean', '-o', 'api-doc-client')
  if ($needBootstrap) { $wailsArgs += '-nsis' }

  $label = '[1/3] wails 构建正式版'
  if ($needBootstrap) { $label += '（首次：同时生成 NSIS 工程与 WebView2 引导器）' }
  Write-Host "`n$label" -ForegroundColor Cyan

  & wails @wailsArgs
  if ($LASTEXITCODE -ne 0) { throw "wails build 失败（退出码 $LASTEXITCODE）" }

  # 丢掉 wails 默认命名的产物，统一用本脚本的命名
  if ($needBootstrap) {
    $defaultInstaller = Join-Path $BinDir "api-doc-go-client-$Arch-installer.exe"
    if (Test-Path $defaultInstaller) { Remove-Item $defaultInstaller -Force }
  }

  if ($Dev) {
    # 注意：这里**不能**带 -clean —— 它会清空 build\bin，把刚构建好的正式版 exe 删掉
    # （build-deb.sh 上踩过同一个坑）。
    Write-Host "`n[1/3] wails 构建开发版（-tags devbuild）" -ForegroundColor Cyan
    & wails build -platform "windows/$Arch" -tags devbuild -o api-doc-client-dev
    if ($LASTEXITCODE -ne 0) { throw "wails build（dev）失败（退出码 $LASTEXITCODE）" }
  }
} else {
  Write-Host "`n[1/3] 跳过构建（-SkipBuild）" -ForegroundColor Cyan
}

if (-not (Test-Path $ProjNsi)) {
  throw "找不到 $ProjNsi。去掉 -SkipBuild 跑一次，让 wails 生成 NSIS 工程。"
}
if (-not (Test-Path $Webview2Setup)) {
  throw "找不到 $Webview2Setup（project.nsi 会 File 引用它）。去掉 -SkipBuild 跑一次让 wails 重新写入。"
}

# ---- 2) makensis 打包 ----

$exeProd = Join-Path $BinDir 'api-doc-client.exe'
$exeDev  = Join-Path $BinDir 'api-doc-client-dev.exe'
if (-not (Test-Path $exeProd)) { throw "未找到 $exeProd，请先构建。" }
if ($Dev -and -not (Test-Path $exeDev)) { throw "未找到 $exeDev，请先构建（-Dev）。" }

# 按架构选 wails_tools.nsh 认的那个宏名；只传一个，ARCH 就由它决定
$script:BinaryDefine = if ($Arch -eq 'arm64') { 'ARG_WAILS_ARM64_BINARY' } else { 'ARG_WAILS_AMD64_BINARY' }

# 安装范围：user 要同时降 UAC 等级，否则装到 LOCALAPPDATA 还弹管理员，自相矛盾
$script:ScopeDefines = @()
if ($InstallScope -eq 'user') {
  $script:ScopeDefines += '-DWAILS_INSTALL_SCOPE=user'
  $script:ScopeDefines += '-DREQUEST_EXECUTION_LEVEL=user'
}

<#
  打一个安装包，返回最终产物路径。
  所有 -D 都在覆盖 wails_tools.nsh 里的 !ifndef 默认值：
    INFO_PROJECTNAME    → OutFile 名（build\bin\<它>-<arch>-installer.exe）
    INFO_PRODUCTNAME    → 安装目录、开始菜单/桌面快捷方式、卸载列表里的显示名
    PRODUCT_EXECUTABLE  → 安装后的 exe 名（与 Linux 的 /usr/bin/api-doc-client 对齐）
    UNINST_KEY_NAME     → 卸载注册表键；正式版与开发版必须不同，否则互相覆盖卸载信息
#>
function New-Installer {
  param(
    [Parameter(Mandatory)][string]$Binary,
    [Parameter(Mandatory)][string]$ProjectName,
    [Parameter(Mandatory)][string]$ProductName,
    [Parameter(Mandatory)][string]$Executable,
    [Parameter(Mandatory)][string]$UninstKey,
    [Parameter(Mandatory)][string]$OutName
  )

  $defines = @(
    "-D$($script:BinaryDefine)=$Binary"
    '-DINFO_COMPANYNAME=api-doc-go'
    "-DINFO_PRODUCTVERSION=$Version"
    '-DINFO_COPYRIGHT=Copyright (C) 2026 api-doc-go'
    "-DINFO_PROJECTNAME=$ProjectName"
    "-DINFO_PRODUCTNAME=$ProductName"
    "-DPRODUCT_EXECUTABLE=$Executable"
    "-DUNINST_KEY_NAME=$UninstKey"
  )
  $defines += $script:ScopeDefines

  # 必须在 installer 目录里跑：project.nsi 全用相对路径
  # （!include "wails_tools.nsh"、File "tmp\..."、MUI_ICON "..\icon.ico"、OutFile "..\..\bin\..."）
  Push-Location $NsisDir
  try {
    # 先收进变量再打印：既能拿到 stderr，也保证 $LASTEXITCODE 不被管道后续 cmdlet 干扰
    $output = & makensis @defines 'project.nsi' 2>&1
    $code = $LASTEXITCODE
  } finally {
    Pop-Location
  }
  $output | ForEach-Object { if ($null -ne $_) { Write-Host "  $_" } }
  if ($code -ne 0) { throw "makensis 打包 '$ProductName' 失败（退出码 $code）" }

  $built = Join-Path $BinDir "$ProjectName-$Arch-installer.exe"
  if (-not (Test-Path $built)) { throw "makensis 返回成功但找不到产物：$built" }

  $final = Join-Path $BinDir $OutName
  Move-Item -Force -Path $built -Destination $final
  Write-Host "  → $OutName" -ForegroundColor Green
  # 只把路径写回管道（上面的输出都走 Write-Host，不会污染返回值）
  return $final
}

Write-Host "`n[2/3] makensis 打包" -ForegroundColor Cyan
$produced = @()

# 用 hashtable splatting 传参，避免反引号续行（行尾多个空格就会静默语法错误）
$prodParams = @{
  Binary      = $exeProd
  ProjectName = 'api-doc-client'
  ProductName = 'API Doc Client'
  Executable  = 'api-doc-client.exe'
  UninstKey   = 'api-doc-go.ApiDocClient'
  OutName     = "api-doc-client_${Version}_${Arch}-setup.exe"
}
$produced += New-Installer @prodParams

if ($Dev) {
  $devParams = @{
    Binary      = $exeDev
    ProjectName = 'api-doc-client-dev'
    ProductName = 'API Doc Client Dev'
    Executable  = 'api-doc-client-dev.exe'
    UninstKey   = 'api-doc-go.ApiDocClientDev'
    OutName     = "api-doc-client-dev_${Version}_${Arch}-setup.exe"
  }
  $produced += New-Installer @devParams
}

# ---- 3) 汇总 ----

Write-Host "`n[3/3] 完成：" -ForegroundColor Green
foreach ($p in $produced) {
  $size = '{0:N1} MB' -f ((Get-Item $p).Length / 1MB)
  Write-Host "  $p  ($size)"
}
Write-Host ''
Write-Host '安装：双击上面的 -setup.exe（machine 范围会弹 UAC）。'
Write-Host '静默安装：  <安装包> /S'
Write-Host '卸载：      控制面板「程序和功能」，或安装目录下的 uninstall.exe'
Write-Host '说明：      build\windows\x86\win10\README.md'
