Unicode true

####
## 自定义 NSIS 安装器（覆盖升级加固版）—— 安装包逻辑的权威来源。
##
## 为什么不用 wails 生成的 project.nsi：
##   `wails build -nsis` 每次 bootstrap 都会**重写** project.nsi 和 wails_tools.nsh，
##   直接改它们会被下次生成覆盖。本文件由 scripts/build-win.ps1 手动交给 makensis，
##   wails 不感知它，因此可安全承载自定义升级逻辑。
##
## 依赖：仍然 !include wails_tools.nsh（由 wails 生成，勿手改）复用其宏
##   （wails.files / webview2runtime / writeUninstaller / checkArchitecture 等）。
##
## 所有 INFO_* / PRODUCT_EXECUTABLE / UNINST_KEY_NAME / WAILS_INSTALL_SCOPE /
## REQUEST_EXECUTION_LEVEL / ARG_WAILS_*_BINARY 宏由 build-win.ps1 用 -D 注入
## （wails_tools.nsh 里每个默认值都是 !ifndef 守护，命令行 -D 一定优先）。
##
## 本文件相比 project.nsi 新增/加固：
##   W-1：升级前用 nsExec 静默结束运行中的客户端（不弹控制台黑框），避免 exe 占用导致覆盖失败
##   W-2：升级=「先静默卸载旧版再装」重装模型，杜绝旧文件残留 / 卸载器自覆盖竞争
##   W-3：InstallDirRegKey + 补写 InstallLocation，记住并复用上次安装目录，
##        不再回落默认目录产生两份安装
##   W-4：WebView2 未就绪时给出明确安装指引（引导器离线失败不再静默）
####

!include "MUI.nsh"
!include "LogicLib.nsh"
!include "x64.nsh"

# 复用 wails 生成的宏（必须最后 include，因为它依赖上面几个库）
!include "wails_tools.nsh"

# 版本信息必须是四段
VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

ManifestDPIAware true

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
!define MUI_FINISHPAGE_NOAUTOCLOSE
!define MUI_ABORTWARNING

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe"

# 默认安装目录（hive/路径随 scope）
!ifdef WAILS_INSTALL_SCOPE
  !if "${WAILS_INSTALL_SCOPE}" == "user"
    InstallDir "$LOCALAPPDATA\Programs\${INFO_PRODUCTNAME}"
  !else
    InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
  !endif
!else
  InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
!endif

# W-3：从注册表回填上次安装目录（hive 随 scope）
!ifdef WAILS_INSTALL_SCOPE
  !if "${WAILS_INSTALL_SCOPE}" == "user"
    InstallDirRegKey HKCU "${UNINST_KEY}" "InstallLocation"
  !else
    InstallDirRegKey HKLM "${UNINST_KEY}" "InstallLocation"
  !endif
!else
  InstallDirRegKey HKLM "${UNINST_KEY}" "InstallLocation"
!endif

ShowInstDetails show

Var PrevLoc      # 旧版安装目录（HKLM/HKCU 的 InstallLocation）
Var Wv2Pv        # WebView2 运行时版本（注册表 pv 值），空 = 未就绪

Function .onInit
   !insertmacro wails.checkArchitecture

   SetRegView 64
   !ifdef WAILS_INSTALL_SCOPE
     !if "${WAILS_INSTALL_SCOPE}" == "user"
       ReadRegStr $PrevLoc HKCU "${UNINST_KEY}" "InstallLocation"
     !else
       ReadRegStr $PrevLoc HKLM "${UNINST_KEY}" "InstallLocation"
     !endif
   !else
     ReadRegStr $PrevLoc HKLM "${UNINST_KEY}" "InstallLocation"
   !endif

   # W-3：装到过非默认目录时，沿用旧目录，不再回落硬编码默认值
   ${If} $PrevLoc != ""
     StrCpy $INSTDIR $PrevLoc
   ${EndIf}

   # W-1 + W-2：存在旧版卸载器 → 先关闭进程再原地静默卸载旧版，随后走全新安装
   ${If} $PrevLoc != ""
   ${AndIf} ${FileExists} "$PrevLoc\uninstall.exe"
     # 审核 M#6：taskkill /F 会绕过客户端「未保存改动」守卫。交互模式先明确告知并给退出机会；
     # 静默安装（/S，脚本/CI 自动化路径）保持强杀不阻断。
     ${IfNot} ${Silent}
       MessageBox MB_OKCANCEL|MB_ICONEXCLAMATION '检测到旧版本，安装将关闭并覆盖它。$\n$\n正在运行的客户端若有未保存的修改会丢失；建议先保存并退出后选“取消”，再重新运行安装。$\n$\n仍要继续安装吗？' IDOK p1_kill_old
       Abort
     ${EndIf}
     p1_kill_old:
     DetailPrint "检测到旧版本，正在关闭进程并静默卸载…"
     # 结束运行中的客户端（PRODUCT_EXECUTABLE 无空格，如 api-doc-client.exe）。
     # 用 nsExec 而不是 ExecWait：taskkill 是控制台程序，ExecWait 会闪一个黑色控制台窗口；
     # nsExec 会隐藏子进程控制台并把输出写进安装日志。
     nsExec::ExecToLog '"$WINDIR\System32\taskkill.exe" /IM "${PRODUCT_EXECUTABLE}" /F'
     Pop $0
     # _?=<dir> 让卸载器原地运行、不复制到临时目录；nsExec 会等它彻底结束再返回；
     # 必须是命令行最后一个参数：卸载器把 _?= 之后的整段（含空格）当目录解析。
     nsExec::ExecToLog '"$PrevLoc\uninstall.exe" /S _?=$PrevLoc'
     Pop $0
     # 给文件系统删除被占用句柄留一点时间
     Sleep 800
   ${EndIf}
FunctionEnd

# W-4：WebView2 就绪复检。wails 的 webview2runtime 宏靠在线引导器安装，离线/失败时是静默的，
# 用户只会看到一个打不开的空窗口。这里装后再查一次注册表，仍缺失就给出明确指引。
# 静默安装（/S）不打断：写日志 + 设错误码，交由脚本/CI 判定。
Function CheckWebView2Ready
  SetRegView 64
  ReadRegStr $Wv2Pv HKLM "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
  ${If} $Wv2Pv == ""
    ReadRegStr $Wv2Pv HKLM "Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
  ${EndIf}
  ${If} $Wv2Pv == ""
    ReadRegStr $Wv2Pv HKCU "Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
  ${EndIf}
  ${If} $Wv2Pv != ""
    Return
  ${EndIf}
  ; 走到这里 = WebView2 未就绪（多半是引导器离线下载失败）
  IfSilent 0 show_wv2_msgbox
    DetailPrint "WebView2 Runtime 未就绪：请联网后重试，或手动安装 https://developer.microsoft.com/microsoft-edge/webview2/"
    SetErrorLevel 16388
    Return
  show_wv2_msgbox:
    MessageBox MB_OK|MB_ICONEXCLAMATION "未能确认 Microsoft Edge WebView2 运行时已安装（安装时的自动下载需要联网）。$\r$\n$\r$\n本程序依赖 WebView2 显示界面，请先安装它再启动：$\r$\nhttps://developer.microsoft.com/microsoft-edge/webview2/$\r$\n$\r$\n（Windows 10/11 一般已随 Edge 自带；若确认已装，多为离线导致下载失败，联网后重试即可。）"
FunctionEnd

Section
    !insertmacro wails.setShellContext

    !insertmacro wails.webview2runtime

    # 装完引导器后复检 WebView2 是否真的就绪，缺失则提示用户去装（详见 CheckWebView2Ready）
    Call CheckWebView2Ready

    SetOutPath $INSTDIR

    !insertmacro wails.files

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    !insertmacro wails.writeUninstaller

    # W-3：补写 InstallLocation（wails 的 writeUninstaller 没写这个字段，
    # 没有它 InstallDirRegKey 回填、以及本次 .onInit 读旧目录都失效）
    SetRegView 64
    !ifdef WAILS_INSTALL_SCOPE
      !if "${WAILS_INSTALL_SCOPE}" == "user"
        WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
      !else
        WriteRegStr HKLM "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
      !endif
    !else
        WriteRegStr HKLM "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
    !endif
SectionEnd

Section "uninstall"
    !insertmacro wails.setShellContext

    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}" # Remove the WebView2 DataPath

    RMDir /r $INSTDIR

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    !insertmacro wails.deleteUninstaller
SectionEnd
