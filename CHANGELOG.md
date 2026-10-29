# api-doc-go-client 更新日志

dev:
当前版本 0.1.4-dev
prod:
当前版本 0.1.0

> 版本口径：`prod` 与 `wails.json` 的 `info.productVersion` 一致（发布时抬升）；
> `dev` 为开发线版本，每新增一条日期条目 patch +1。维护规则见 `AGENTS.md`。

## 2026-10-29  0.1.4-dev

- Markdown 编辑器（文档条目 / 接口说明 / 生成文档弹窗）支持**本地上传与粘贴图片**：PNG / JPEG / GIF / WebP、单张 ≤5MB、按文件头校验（SVG 等拒收），按 sha256 存入 `<集合>/assets/`（同图自动去重），正文引用 `/doc-assets/<sha256>`，预览离线可直接显示。
- 新增文档图片只读路由 `/doc-assets/<sha256>`：桌面壳 `AssetServer` 与 devserver 共用同一处理器，`Content-Type` 只认文件头嗅探、带 `nosniff` 与长缓存；只接受 64 位 hex 文件名，防路径穿越。
- MCP 新增 `save_doc_image`（工具数 29 → 30）：传图片 base64，返回可直接写进 Markdown 的 URL，AI 可为文档或接口说明配图；同样走文件头校验与 5MB 上限。
- 同步支持图片：推送前自动把正文里的 `/doc-assets/<sha256>` 换回服务端 URL（首次按需上传，映射存 `.sync.json`，改文字不重复上传）；拉取后自动下载 `/uploads/*`、`/api/file/<hash>` 到本地 `assets/` 并改写成本地引用。文档条目与接口说明（`request.docs`）都覆盖，外链图片原样保留。注意：按服务端现有约定，同步后的图片位于 `/uploads` 公开静态路径（与 Web 编辑器一致），敏感截图请勿入库。
- 已知限制：**导出**（Markdown / HTML）仍不打包图片；冲突时手动选择「采用服务器版本」的说明文档，其图片要等下一轮同步才会本地化。

## 2026-10-28  0.1.3-dev

- MCP 搜索升级：`search_requests` 支持整词 `exactMatch`、`fields` 限定范围、命中片段 `snippet` 与 `includeHash`；新增 `search_docs`（文档正文按行命中，返回 `L行号`）；新增 `get_request_details`（一次 1–10 个 uid 的批量详情）。
- MCP 读取升级：`get_request_detail` 支持 `sections` 分段；详情 hash 输出完整值，可直接回传 `ifMatch`。
- MCP 写入更安全：`update_request` / `update_doc` 支持 `replaces` 局部替换（`old` 找不到整单拒绝、不落盘），成功响应附「改动行 ±1」落盘确认；`ifMatch` 冲突时报错并给出当前完整 hash，不写入。
- MCP 长文档分段读取：`get_doc` 新增 `outline` 大纲与 `startLine` + `maxLines` 区间（行号仅供阅读，写回时不要带；全文超长只提示不截断）；`get_request_detail` 同步支持 `docsOutline` / `docsStartLine` / `docsMaxLines`；`search_docs` 的行号可直接接续精读。
- 集合树行右键菜单：新增「复制 ID」「复制 MCP 指令」（请求附 12 位短 hash，文档用 `get_doc` 同款；与悬停「···」共用选项），并修复菜单在选中、点击外部、按 Esc、滚动、鼠标移开后不消失的问题。
- 修复打开 MCP 设置时凭空提示「配置已修改」：来源白名单空值在前后端分别是 `null` / `[]`，脏检查误判；现统一归一。
- 修复 Linux 下窗口缩小后无法拖动边框调整大小：外壳为画阴影让出 32px 留白，可见边框离窗口外沿太远，Wails 自带边缘检测（外沿 6px）命中不到；现在可见边框内侧新增 6px 缩放热区，按下直接把缩放交回窗口管理器（最大化时自动隐藏）。
- 文档与回归：工具数 27 → 29，使用手册 / 设计文档 / 优化清单同步；新增 MCP 优化、长文档分段、树复制/右键菜单等自动化用例。

## 2026-10-27  0.1.2-dev

- 集合树支持自定义拖动排序：集合、目录、接口、文档均可拖动，落点支持 before / after / inside，顺序按 `seq` 写回文件（集合根顺序、`folder.yml`、请求 `info.seq`、文档 frontmatter `seq`）。

## 2026-10-26  0.1.1-dev

- 响应字段生成改为读取「当前响应」：按钮改为「从当前响应生成」，直接取右侧正在展示的响应（实时或已保存示例）；响应为空时填充，已有数据需选择「覆盖」才会替换；同时修复响应类型偶尔回退为 `application/json` 的问题。
- PAT 本地加密落盘（`.sync.json` 仅保存密文，密钥在用户配置目录），拆分超大前端组件并接入 ESLint。
- 安装包与托盘产品名统一为 `api-doc-go`（需要空格处使用 `Api Doc Go`）。
- 界面修复：编辑区高度占满、页内查找焦点跟随、生成数据表格确认框宽度、覆盖生成时写入响应体、字段表格行区滚动等。
- 补充客户端保存 / 取消流程的自动化测试。

## 2026-09-28 ~ 2026-10-25  0.1.0

初始开发期（当前正式版基线），主要能力：

- 工程与门禁：Wails v2 + Vue3/TS 骨架，集合为纯文本文件（文件即真相源）；`scripts/check.sh` 门禁（gofmt/build/vet/test/`-race`/vue-tsc/ESLint/i18n）、CI，以及 deb / NSIS 安装包与覆盖升级隔离。
- 集合与索引：目录 / 请求 / 文档树、回收站（预览 / 还原 / 彻底删除）、集合 YAML 原子写盘、文件监听与外部改动回填、全文搜索与命令面板。
- 请求执行：HTTP（重定向 / Cookie / 超时 / 代理 / 历史）、gRPC（unary 与服务器流式）、断言与脚本、集合运行器（桌面面板 + CLI/JUnit）。
- 变量与环境：环境文件、变量默认值语法、敏感变量掩码、同步口令与系统密钥链。
- 响应与文档：响应字段表格、响应类型徽标、接口文档生成，Markdown / HTML / OpenAPI / Postman 导出，契约校验。
- MCP 服务：内嵌与独立 server（stdio/HTTP）、令牌鉴权、工作目录白名单、风险工具交互授权与调用审计、文档条目工具。
- 双向同步：PAT 加密、增量拉取 / 推送、冲突副本、文档与示例同步、多工作目录。
- 界面：工作台两栏布局、多页签、暗色主题与缩放、中英双语、移动端适配、托盘驻留与单实例。
