<#
.SYNOPSIS
  Windows 打包：出 exe（正式/开发），可选 NSIS 安装包。与 scripts/build-deb.sh 对称。

.DESCRIPTION
  产物统一落在 build\bin\：
    api-doc-client.exe                正式版
    api-doc-client-dev.exe            开发版（标题栏带 dev 标记）
    api-doc-client-amd64-installer.exe  NSIS 安装包（-Nsis 时）

  版本号只认一个来源（与 scripts/check-versions.sh 同口径）：
    wails.json info.productVersion = frontend/package.json version = frontend/src/lib/version.ts APP_VERSION
  传 -Version 会像 scripts/release.sh 那样把三处一起改；不传则校验一致性，不一致直接停。

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File scripts\build-win.ps1
  powershell -ExecutionPolicy Bypass -File scripts\build-win.ps1 -Nsis
  powershell -ExecutionPolicy Bypass -File scripts\build-win.ps1 -Version 0.2.0 -Nsis -Sign
#>
param(
    [string]$Version = "",
    [switch]$Nsis,
    [switch]$SkipFrontend,
    [switch]$DevOnly,
    [switch]$ProdOnly
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root

# 发布构建必须用 go.mod 里锁定的 share 版本，不能受开发者本地 go.work 影响：
# 本地 share 副本落后时，会出现「Linux 能编、Windows 报字段不存在」这种诡异差异。
$env:GOWORK = 'off'
Write-Host "[build-win] GOWORK=off（使用 go.mod 锁定的 api-doc-go-share 版本）" -ForegroundColor DarkGray

function Info($m) { Write-Host "[build-win] $m" -ForegroundColor Cyan }
function Fail($m) { Write-Host "[build-win] $m" -ForegroundColor Red; exit 1 }

# ---- 1) 版本号：改或校验 ----------------------------------------------------
$WailsJson = Join-Path $Root 'wails.json'
$PkgJson = Join-Path $Root 'frontend/package.json'
$VerTs = Join-Path $Root 'frontend/src/lib/version.ts'

$wv = (Get-Content $WailsJson -Raw | ConvertFrom-Json).info.productVersion
$pv = (Get-Content $PkgJson -Raw | ConvertFrom-Json).version
$tv = [regex]::Match((Get-Content $VerTs -Raw), "APP_VERSION\s*=\s*'v?([^']+)'").Groups[1].Value

if ($Version) {
    if ($Version -notmatch '^\d+\.\d+\.\d+$') { Fail "版本号须形如 0.2.0" }
    (Get-Content $WailsJson -Raw) -replace '("productVersion"\s*:\s*")[^"]*(")', "`$1$Version`$2" | Set-Content $WailsJson -NoNewline
    (Get-Content $PkgJson -Raw) -replace '("version"\s*:\s*")[^"]*(")', "`$1$Version`$2" | Set-Content $PkgJson -NoNewline
    (Get-Content $VerTs -Raw) -replace "(APP_VERSION\s*=\s*')v?[^']*(')", "`$1v$Version`$2" | Set-Content $VerTs -NoNewline
    $wv = $Version; $pv = $Version; $tv = $Version
    Info "已把三处版本号改为 $Version（记得提交）"
}

if ($wv -ne $pv -or $wv -ne $tv) {
    Fail "版本号不一致：wails.json=$wv, package.json=$pv, version.ts=$tv —— 用 -Version 或 scripts/release.sh 统一"
}
Info "版本：$wv"

# ---- 2) 工具链 ---------------------------------------------------------------
foreach ($t in @('go', 'node', 'npm', 'wails')) {
    if (-not (Get-Command $t -ErrorAction SilentlyContinue)) {
        Fail "缺少 $t：$(if ($t -eq 'wails') { 'go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.1' } else { '请先安装并加入 PATH' })"
    }
}
if ($Nsis -and -not (Get-Command 'makensis' -ErrorAction SilentlyContinue)) {
    Fail "缺少 makensis（NSIS）：winget install NSIS.NSIS，或去掉 -Nsis 只出 exe"
}
Info ("go=" + (go version))
Info ("wails=" + (wails version 2>$null | Select-Object -First 1))

# ---- 3) 前端 ----------------------------------------------------------------
if (-not $SkipFrontend) {
    if (-not (Test-Path (Join-Path $Root 'frontend/node_modules'))) {
        Info "安装前端依赖（npm ci）"
        Push-Location (Join-Path $Root 'frontend'); try { npm ci } finally { Pop-Location }
    }
    Info "构建前端"
    Push-Location (Join-Path $Root 'frontend'); try { npm run build } finally { Pop-Location }
}

# ---- 4) 构建 ----------------------------------------------------------------
$Bin = Join-Path $Root 'build\bin'
New-Item -ItemType Directory -Force -Path $Bin | Out-Null

if (-not $DevOnly) {
    Info "构建正式版 exe"
    wails build -platform windows/amd64 -clean -o api-doc-client
    if ($LASTEXITCODE -ne 0) { Fail "正式版构建失败" }
}
if (-not $ProdOnly) {
    Info "构建开发版 exe"
    wails build -platform windows/amd64 -clean -tags devbuild -o api-doc-client-dev
    if ($LASTEXITCODE -ne 0) { Fail "开发版构建失败" }
}
if ($Nsis) {
    Info "构建 NSIS 安装包"
    wails build -platform windows/amd64 -clean -nsis
    if ($LASTEXITCODE -ne 0) { Fail "NSIS 安装包构建失败（先跑 wails doctor 看缺什么）" }
}

# ---- 5) 产物校验 ------------------------------------------------------------
$exes = Get-ChildItem $Bin -Filter *.exe | Sort-Object LastWriteTime
if (-not $exes) { Fail "构建结束但 build\bin 下没有 exe" }

Write-Host ''
Info '产物：'
foreach ($f in $exes) {
    $vi = $f.VersionInfo
    $ver = if ($vi.FileVersion) { $vi.FileVersion } else { '(无资源版本)' }
    Write-Host ("  {0,-46} {1,7:N1} MB  版本 {2}" -f $f.Name, ($f.Length / 1MB), $ver)
}
Write-Host ''
Info "安装包（如有）：双击 $Bin\api-doc-client-amd64-installer.exe"
Info "免安装：直接运行 $Bin\api-doc-client.exe（需 WebView2 Runtime）"
