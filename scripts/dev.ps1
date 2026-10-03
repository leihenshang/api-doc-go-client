<#
.SYNOPSIS
  启动 wails dev，并在 Ctrl+C / 退出时确保「一次 Ctrl+C 结束全部」。

.DESCRIPTION
  为什么需要这个包装器：`wails dev` 在 Windows 上的进程树清理不可靠 ——
  实测把 CLI 连同整棵树杀掉之后，它拉起的应用进程（api-doc-client-dev.exe，仍占 34115）
  与前端 vite（node，仍占 vite 端口）都还活着，终端提示符也不回来，
  看起来就是「Ctrl+C 停不掉开发服务」。

  本脚本的清理分四步（逐层兜底）：
    1. 结束 wails CLI 及其子进程（taskkill /T）；
    2. 按进程名兜底：wails.exe、api-doc-client*.exe；
    3. 按端口兜底：wails devserver(34115/34116) 与 vite(5173/5273-5275)——
       只杀「进程名属于 node/wails/api-doc-*」的持有者，避免误杀你别的 dev server；
    4. 复查端口并打印结果，不留糊涂账。

.EXAMPLE
  ./scripts/dev.ps1
  ./scripts/dev.ps1 -noreload
#>
[CmdletBinding()]
param(
  # 透传给 wails dev 的参数
  [Parameter(ValueFromRemainingArguments = $true)]
  [string[]]$WailsArgs = @()
)

# 注意：这里必须是 Continue —— 原生命令（taskkill）把「进程已不存在」写进 stderr 时，
# 在 Stop 模式下会变成终止错误，把 finally 里的清扫步骤直接打断（实测踩过）。
$ErrorActionPreference = 'Continue'
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

function Write-Info($msg) { Write-Host "[dev] $msg" -ForegroundColor Cyan }
function Write-Warn2($msg) { Write-Host "[dev] $msg" -ForegroundColor Yellow }

# 兜底目标：进程名（wails CLI 与应用）
$targetNames = @('wails', 'api-doc-client', 'api-doc-client-dev')
# 兜底目标：端口（wails devserver 与 vite）。vite 的端口随配置浮动，这里覆盖常见几个。
$targetPorts = @(34115, 34116, 5173, 5273, 5274, 5275, 5276)
# 允许按端口杀的进程名（避免误杀无关服务，例如你自己在跑的别的 dev server）
$portKillNames = @('node', 'wails', 'api-doc-client', 'api-doc-client-dev')

function Invoke-Kill {
  param([int]$TargetPid, [string]$Why)
  if ($TargetPid -le 0) { return }
  $p = Get-Process -Id $TargetPid -ErrorAction SilentlyContinue
  if (-not $p) { return }
  Write-Warn2 ("结束 {0} (pid {1}) — {2}" -f $p.ProcessName, $TargetPid, $Why)
  & taskkill /F /T /PID $TargetPid 2>&1 | Out-Null
}

function Stop-DevTree {
  param([int]$CliPid)

  # 1) CLI 及其子进程
  if ($CliPid -gt 0) {
    Write-Info "结束 wails CLI (pid $CliPid) 及其子进程…"
    Invoke-Kill -TargetPid $CliPid -Why 'wails dev 主进程'
  }
  Start-Sleep -Milliseconds 400

  # 2) 按名字兜底（CLI 自报的清理不可靠，app 不在它的树里）
  foreach ($name in $targetNames) {
    foreach ($p in @(Get-Process -Name $name -ErrorAction SilentlyContinue)) {
      Invoke-Kill -TargetPid $p.Id -Why ('残留进程 ' + $name)
    }
  }
  Start-Sleep -Milliseconds 300

  # 3) 按端口兜底（vite 之类名字不固定，但端口固定）
  foreach ($port in $targetPorts) {
    $conns = @(Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue)
    foreach ($c in $conns) {
      $proc = Get-Process -Id $c.OwningProcess -ErrorAction SilentlyContinue
      if (-not $proc) { continue }
      if ($portKillNames -notcontains $proc.ProcessName) {
        Write-Warn2 ("端口 {0} 被无关进程 {1} (pid {2}) 占用，未结束（如需请手动处理）" -f $port, $proc.ProcessName, $proc.Id)
        continue
      }
      Invoke-Kill -TargetPid $proc.Id -Why ('占用端口 ' + $port)
    }
  }
  Start-Sleep -Milliseconds 300

  # 4) 复查并报告
  $busy = @()
  foreach ($port in $targetPorts) {
    $hit = @(Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue)
    foreach ($h in $hit) {
      $busy += ('{0} (pid {1})' -f $port, $h.OwningProcess)
    }
  }
  if ($busy.Count -gt 0) {
    Write-Warn2 ("端口仍被占用: {0}" -f ($busy -join ', '))
  } else {
    Write-Info '已全部停止（端口已释放）'
  }
}

$cli = $null
try {
  Write-Info ("启动 wails dev {0}" -f ($WailsArgs -join ' '))
  # Start-Process 拿到可强杀的 pid；-NoNewWindow 让日志直接打到当前终端
  $cli = Start-Process -FilePath 'wails' -ArgumentList (@('dev') + $WailsArgs) -NoNewWindow -PassThru
  Write-Info ("wails CLI pid = {0}（按 Ctrl+C 结束全部；再按一次强制退出）" -f $cli.Id)
  $cli.WaitForExit()
  Write-Info ("wails dev 已退出（exit={0}）" -f $cli.ExitCode)
}
catch {
  Write-Warn2 ("启动/运行出错: {0}" -f $_.Exception.Message)
}
finally {
  if ($cli) { Stop-DevTree -CliPid $cli.Id } else { Stop-DevTree -CliPid 0 }
}
