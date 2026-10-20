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
##   W-1：升级前 taskkill 关闭运行中的客户端，避免 exe 占用导致覆盖失败
##   W-2：升级=「先静默卸载旧版再装」重装模型，杜绝旧文件残留 / 卸载器自覆盖竞争
##   W-3：InstallDirRegKey + 补写 InstallLocation，记住并复用上次安装目录，
##        不再回落默认目录产生两份安装
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
     DetailPrint "检测到旧版本，正在关闭进程并静默卸载…"
     # 结束运行中的客户端（PRODUCT_EXECUTABLE 无空格，如 api-doc-client.exe）
     ExecWait '$WINDIR\System32\taskkill.exe /IM "${PRODUCT_EXECUTABLE}" /F' $0
     # _?=<dir> 让卸载器原地运行、不复制到临时目录，ExecWait 会等它彻底结束再返回；
     # 必须是命令行最后一个参数：卸载器把 _?= 之后的整段（含空格）当目录解析。
     ExecWait '"$PrevLoc\uninstall.exe" /S _?=$PrevLoc' $0
     # 给文件系统删除被占用句柄留一点时间
     Sleep 800
   ${EndIf}
FunctionEnd

Section
    !insertmacro wails.setShellContext

    !insertmacro wails.webview2runtime

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
