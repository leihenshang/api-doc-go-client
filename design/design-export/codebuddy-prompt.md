# CodeBuddy 复刻提示词（Vue 3 + Naive UI）

把下面这段提示词与同目录的 4 张 PNG、design-spec.md 一起发给 CodeBuddy 即可。

---

请根据附带的 4 张 UI 设计稿截图（01-main-ui / 02-command-palette / 03-features / 04-vertical-layout）和 design-spec.md 设计规格，用 **Vue 3 + TypeScript + Naive UI** 复刻这套「本地优先 API Client」桌面应用界面。

要求：

1. **严格还原布局**（对照规格第 2-5 节的结构图）：
   - 主界面：标题栏 + 工具栏（离线徽章/Git 状态/环境切换/Ctrl+K 入口）+ 左侧 Collection 目录树 + 请求标签栏 + URL 地址栏（POST 方法选择器 + Send 按钮）+ Params 参数表格 + 响应面板 + 底部状态栏。
   - Ctrl+K 命令面板：深色遮罩 + 居中白卡，搜索输入行、范围筛选 chips、结果列表（含选中态与右侧路径）、底栏。
   - 特性说明页：3×2 网格六张特性卡。
   - 请求/响应双布局：横向（左右分栏）与竖向（上下分栏）两种，通过分隔条中央的胶囊切换按钮切换（对照 01 与 04 两张图）。

2. **主题配置**：用 Naive UI 的 `NConfigProvider` + `darkTheme` 之外的默认（亮色）主题，`themeOverrides` 中设置 `common.primaryColor = '#18a058'`、`primaryHover`、`primaryPressed`、`infoColor = '#2080f0'`、`borderColor = '#e3e3e3'`、`bodyColor = '#f5f5f5'`，全部色值以 design-spec.md 的 Token 表为准。

3. **方法语义色**：GET 标签用 primary 绿（`NTag type="success"` 或自定义），POST 用 info 蓝（`type="info"`）。

4. **字体**：中文 Noto Sans SC，代码（URL、参数名、JSON、状态栏）JetBrains Mono，通过 Google Fonts 或本地字体引入。

5. **交互**：
   - Ctrl+K 唤起命令面板，ESC 关闭；筛选 chips 可切换。
   - 布局切换按钮切换请求/响应横向/竖向排列（竖向时响应面板固定高约 340px）。
   - Bulk Edit 链接在表格模式与多行 key:value 文本模式间切换。
   - 其余（发送请求、目录树展开等）先做静态/占位实现，接口留空并加 TODO 注释。

6. **交付**：单个可运行的 Vite 项目（`npm i && npm run dev` 即可启动），组件按 `AppShell / Sidebar / RequestEditor / ResponsePanel / CommandPalette / FeatureCards` 拆分；不引入 UI 之外的重量级依赖。

截图是唯一视觉真值，若截图与规格描述冲突，以截图为准。
