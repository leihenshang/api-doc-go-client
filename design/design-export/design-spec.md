# 本地优化 API Client 设计规格（Naive UI 风格）

> 基于 Bruno 的本地化改造原型，配色遵循 Naive UI 默认主题。
> 交付图：01-main-ui.png（主界面）、02-command-palette.png（Ctrl+K 命令面板）、03-features.png（特性说明卡）、04-vertical-layout.png（竖向布局预览）。

## 1. 设计 Token

### 颜色（Naive UI 默认主题）
| Token | 值 | 用途 |
|---|---|---|
| primary | `#18a058` | 品牌色：Logo、Send 按钮、GET 标签、激活页签、离线徽章、选中态 |
| primary-tint | `#E8F5EE` | 主色浅底：激活布局按钮、选中结果行、「本地」徽章、Ctrl+K 入口底色 |
| primary-dark | `#0E7A3E` | 主色深文字：Ctrl+K 文案、「本地」徽章文字 |
| info | `#2080F0` | POST 方法标签、POST 结果图标 |
| info-tint | `#E8F0FE` | POST 选中行/方法选择器浅底 |
| info-dark | `#1060C0` | POST 方法选择器文字 |
| body-bg | `#F5F5F5` | 应用底、标题栏、状态栏、侧边栏、特性区背景 |
| surface | `#FFFFFF` | 卡片、编辑器、面板 |
| border | `#E3E3E3` | 全部描边/分隔线 |
| text-primary | `#202124` | 标题、请求名 |
| text-secondary | `#3C4043` | 正文 |
| text-tertiary | `#5F6368` | 说明文字、Git 状态 |
| text-placeholder | `#A0A4AB` | 占位符、图标灰 |
| code-key | `#0550AE` / code-string `#116329` | JSON 语法高亮 |

#### 暗色主题（派生，实现见 `frontend/src/styles/base.css` 的 `:root[data-theme='dark']`）
| Token | 暗色值 | 说明 |
|---|---|---|
| primary / primary-tint / primary-dark | `#36AD6A` / `#1B2B22` / `#7ED6A5` | 深底上主色提亮一档，保证对比度；`primary-dark` 在暗色下实际是「浅底上的主色文字」 |
| body-bg / surface / border | `#17181A` / `#1E1F22` / `#2B2D31` | 与浅色同一套语义令牌，仅换值 |
| text-primary / secondary / tertiary / placeholder | `#E6E7E9` / `#C9CCD1` / `#9AA0A6` / `#6B7075` | 三级文字与占位符 |
| code-key / string / number / boolean / null | `#79C0FF` / `#7EE787` / `#79C0FF` / `#FFA657` / `#8B949E` | 代码着色的深色配值 |
| 新增派生令牌 | `surface-2` `#202124`、`surface-3` `#26282C`、`chip` `#26282C`、`danger` `#E88080`、`warn` `#F2C97D`、`dash` `#2B2D31` | 表头/代码底、次级块、灰底小件、错误/警告、细分隔线 |
| 方法色 | GET `#36AD6A`、POST `#4098FC`、PUT `#F2C97D`、DELETE `#E88080`、PATCH `#B98AFF` | HTTP 方法语义色同样随主题切换 |

> 主题为「浅色 ↔ 深色」两态（不含跟随系统）；根元素 `data-theme` 由前端写入，`color-scheme` 同步切换以带动原生控件与滚动条。

### 字体
- UI 中文：Noto Sans SC（Regular 400 / Medium 500 / SemiBold 600 / Bold 700）
- 代码（URL、参数、JSON）：JetBrains Mono
- 字号：标题 20 / 正文 13 / 说明 12 / 小字 11 / 徽章 9-10

### 圆角与投影
- 控件 6px、卡片 8-10px、命令面板卡片 12px、徽章全圆（胶囊）
- 浮层卡片：`0 16px 48px -8px rgba(0,0,0,0.25)`

## 2. 画板一：主界面（1440×900）

```
App
├─ 标题栏 (h44, bg #F5F5F5)：Logo(纸飞机 icon) + "Bruno Local" | 窗口控制(最小化/最大化/关闭)
├─ 工具栏 (h48, 白底)：左侧 Collection 切换(projectetA) + [离线可用·绿徽章] + [已同步 main·灰徽章]
│                      右侧 环境切换(No Environment) + [Ctrl+K 本地搜索·绿底入口]
├─ 主体 (横向三栏)
│  ├─ 侧边栏 (w260, bg #F5F5F5)：请求搜索框 → 目录树(clxone-docs / projectetA[本地] / 用户 /
│  │   GET 用户-列表 / POST 用户-删除[选中·蓝底]) → 底部「本地存储」说明卡
│  ├─ 主内容区 (白底, 纵向)
│  │  ├─ 请求标签栏 (h38)：Collection | GET 用户-列表 | POST 用户-删除(激活) | +
│  │  ├─ 请求地址栏 (h50)：POST 方法选择器(蓝底) + {{baseUrl}}/api/user/delete + 格式化 + Send(绿)
│  │  └─ 编辑器区域 (横向分栏)
│  │     ├─ 请求面板：Params(激活·绿下划线)/Body/Headers/Auth/Vars/Script/Tests
│  │     │   + Query 参数表格(Name/Value/Description) + Path 标签 + Bulk Edit(绿链接)
│  │     ├─ 分隔区 (w28)：中央浮着「布局切换」胶囊按钮(横向图标激活·绿底 / 竖向图标灰)
│  │     └─ 响应面板 (w470)：响应头(响应 · 200 OK 绿徽章 · 12 ms · 1.2 KB) + 着色 JSON
└─ 状态栏 (h28)：左"本地 Collection · 12 个请求 · 3 个环境 · 纯文本存储"
                 右"离线优先 · 数据 100% 存于本机 · v2.0"
```

## 3. 画板二：Ctrl+K 命令面板（1200×760，深色遮罩 45%）

居中白卡 640×440，圆角 12，大投影：
- 搜索输入行：绿搜索图标 + 输入「搜索请求、环境、脚本…」+ ESC 键帽
- 范围筛选行：「全部」(绿激活) / 请求 / 环境 / 命令
- 结果列表：POST 用户-删除(选中·绿底, POST 图标蓝) / GET 用户-列表 / 环境·Production / 打开本地 Collection 目录，每行右侧灰色路径
- 底栏：「本地索引 · 毫秒级响应 · 无需联网」

## 4. 画板三：本地优先·六大特性（1200 宽，3×2 网格）

灰底容器 + 6 张白卡(376 宽, 圆角 10, 边框 #E3E3E3)：纯文本存储 / 离线优先 / Git 友好 / 毫秒级本地搜索 / 快捷键全覆盖 / 隐私安全，图标统一主色绿。

## 5. 画板四：竖向布局预览（980×920）

编辑器区域改为纵向：请求面板(上, flex:1) → 28px 分隔条 → 响应面板(下, h340)。
切换按钮横排 (54×26)，激活态为「竖向」图标绿底。此画板与画板一的切换按钮互为对照，演示横向/竖向两种请求-响应布局。

## 6. 交互与实现要点
1. 布局切换按钮：点击在横向(左右)/竖向(上下)分栏间切换，激活图标用 primary 色、非激活用灰。
2. Ctrl+K：全局快捷键唤起命令面板，ESC 关闭；范围筛选可切换激活 chip。
3. 方法语义色：GET=primary 绿（读），POST=info 蓝（写），取自 Naive UI 调色板。
4. 「本地缓存徽章」已在响应头中移除，不实现。
5. 批量编辑：Bulk Edit 链接切换为多行 key:value 文本编辑模式（见画板四）。
6. 侧边栏选中请求行使用 info-tint 浅蓝底 + primary 色 POST 标签。
7. 环境变量占位 `{{baseUrl}}` 需支持渲染高亮。
