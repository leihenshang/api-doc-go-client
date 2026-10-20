# api-doc-go-client 代码审核报告（2026-10-20，会话内审）

> 审核对象：`e0db005..75f84a9`（30 提交：托盘/NSIS 打包/同步修复系列/整包导出/文档页签等）。工作区审时干净。
> **修复状态：本报告 11 项已全部收口（2026-10-20），断点提交见各行。**

| 级别 | # | 问题 | 修复 | 断点 |
|---|---|---|---|---|
| Critical | 1 | `collectDocOps/collectExampleOps` 吞错 + 差异墓碑 → 扫描异常时可把远端整仓文档/示例批量删光 | 清单读错即返回；「磁盘仍有条目文件而清单为空」防呆（保留 P1-10 合法墓碑），4 条单测 | `ff4c47e`（fix(sync) 扫空防呆，含 3 条配套测试） |
| High | 2 | 脏文档页签不在 `savableTabs` → 退出询问漏 doc、改动静默丢失 | 口径去掉 kind 过滤 | `68c7186` |
| High | 3 | 关闭询问「不再显示」未等落盘即退出 → 勾选随机丢失 | persist 改 async，两出口等待后继续 | `68c7186` |
| Medium | 4 | `applyDelete` doc/example 本地删除失败仍清基线 → 远端文档"复活" | 仅成功时清基线 | `3f869af` |
| Medium | 5 | `HideToTray` 感知不到托盘启动失败 → Linux 无 SNI 时应用不可达 | `Manager.Ready/WaitReady` + 超时退化最小化（单测） | `3f869af` |
| Medium | 6 | NSIS 覆盖升级 `taskkill /F` 绕过未保存守卫 | 交互模式先确认（取消 Abort），静默保留强杀 | `3f869af`（本机无 makensis，未实跑） |
| Low | 7 | `TrayResident` 死字段、两套写入口径 | 标 Deprecated、quit.ts 与 SettingsDialog 统一 `!== 'quit'` | `b6d1745` |
| Low | 8 | 会话 `{uid,kind}` 化后 `forgetRequest` 字符串比较失配 | `idOf` 统一比较 | `b6d1745` |
| Low | 9 | 整包导出 `.apidocgo.json` 带请求内联凭据明文 | 导出掩码 password/token/value + `redactedAuthCount` 计数 | `b6d1745` |
| Low | 10 | MCP `update_doc` 三步非原子、失败无说明 | 失败信息回带已完成步骤与重试指引 | `b6d1745` |
| Low | 11 | DocEditor 交互件缺 testid；死 i18n 键 | 补 `doc.modeEdit/modePreview/close`；删 `settings.trayResident*` 双语键 | `b6d1745` |

**判定为无问题的疑点**（防误报）：reveal.go 无命令注入；bundle 导入无路径穿越；`.tmp-*` 原子写不触发回环；托盘 LockOSThread/atomic 正确；`Collection` 互斥补齐。
**环境性修正**（顺带）：SaveBind/gitignore mode/symlink/windowFits 三处测试按仓内惯例补平台护栏；cover-check 容忍当前平台无测试文件的包。
