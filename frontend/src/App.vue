<script setup lang="ts">
// 根组件（AppShell，design-spec §2）：标题栏 → 工具栏 → 三栏主体（侧栏 / 标签+请求+响应）→ 状态栏。
// 请求区与响应区的位置由全局设置 responseLayout 决定，占比由分隔条拖动调整（松手落盘）。
import CodeGenDialog from '@/components/CodeGenDialog.vue'
import CommandPalette from '@/components/CommandPalette.vue'
import CookieDialog from '@/components/CookieDialog.vue'
import CurlImportDialog from '@/components/CurlImportDialog.vue'
import EnvManager from '@/components/EnvManager.vue'
import HistoryDialog from '@/components/HistoryDialog.vue'
import ImportDialog from '@/components/ImportDialog.vue'
import MockDialog from '@/components/MockDialog.vue'
import Overview from '@/components/Overview.vue'
import RequestBar from '@/components/RequestBar.vue'
import RequestEditor from '@/components/RequestEditor.vue'
import ResponsePanel from '@/components/ResponsePanel.vue'
import SettingsDialog from '@/components/SettingsDialog.vue'
import Sidebar from '@/components/Sidebar.vue'
import StatusBar from '@/components/StatusBar.vue'
import SyncDialog from '@/components/SyncDialog.vue'
import TabBar from '@/components/TabBar.vue'
import TitleBar from '@/components/TitleBar.vue'
import Toolbar from '@/components/Toolbar.vue'
import Welcome from '@/components/Welcome.vue'
import { api, onAppEvent } from '@/lib/ipc'
import { dialog, message } from '@/lib/notice'
import { requestQuit } from '@/lib/quit'
import { isDark, nextTheme } from '@/lib/theme'
import { savedActiveDir, savedRecentDirs, savedRootDirs, useCollectionStore } from '@/stores/collection'
import { useSettingsStore } from '@/stores/settings'
import type { Tab } from '@/stores/tabs'
import { blankGrpcRequest, useTabsStore } from '@/stores/tabs'
import type { RequestDoc, SyncStatus, TreeNode } from '@/types'
import { WarningOutline } from '@vicons/ionicons5'
import type { GlobalThemeOverrides } from 'naive-ui'
import {
  NButton,
  NConfigProvider,
  NForm,
  NFormItem,
  NIcon,
  NInput,
  NModal,
  NSpin,
  darkTheme,
  dateEnUS,
  dateZhCN,
  enUS,
  zhCN
} from 'naive-ui'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch, watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'

const coll = useCollectionStore()
const tabs = useTabsStore()
const settings = useSettingsStore()
const { t, locale } = useI18n()

const naiveLocale = computed(() => (locale.value === 'en-US' ? enUS : zhCN))
const naiveDateLocale = computed(() => (locale.value === 'en-US' ? dateEnUS : dateZhCN))

// 主题令牌取自 design-spec §1：控件圆角 6px、主色 #18a058、信息色 #2080f0、底 #f5f5f5；
// 暗色沿用同一套语义，只把底色/边框换成 base.css 里 --app-* 的深夜配值（主色提亮一档）。
const commonBase: GlobalThemeOverrides['common'] = {
  fontSize: '13px',
  borderRadius: '6px',
  borderRadiusSmall: '4px',
  fontFamily:
    "'Noto Sans SC Variable', -apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif",
}
const commonLight: GlobalThemeOverrides['common'] = {
  ...commonBase,
  primaryColor: '#18a058',
  primaryColorHover: '#36ad6a',
  primaryColorPressed: '#0c7a43',
  primaryColorSuppl: '#18a058',
  infoColor: '#2080f0',
  infoColorHover: '#4098fc',
  infoColorPressed: '#1060c0',
  borderColor: '#e3e3e3',
  bodyColor: '#f5f5f5',
  cardColor: '#ffffff',
  textColorBase: '#202124',
}
const commonDark: GlobalThemeOverrides['common'] = {
  ...commonBase,
  primaryColor: '#36ad6a',
  primaryColorHover: '#4cc47c',
  primaryColorPressed: '#2a9a5c',
  primaryColorSuppl: '#36ad6a',
  infoColor: '#4098fc',
  infoColorHover: '#5ea9ff',
  infoColorPressed: '#2a7fd4',
  borderColor: '#2b2d31',
  bodyColor: '#17181a',
  cardColor: '#1e1f22',
  textColorBase: '#e6e7e9',
}

const naiveTheme = computed(() => (isDark.value ? darkTheme : null))
const themeOverrides = computed<GlobalThemeOverrides>(() => ({
  common: isDark.value ? commonDark : commonLight,
  Card: {
    paddingMedium: '16px 20px',
    borderRadiusMedium: '10px',
  },
  DataTable: {
    thColor: isDark.value ? '#202124' : '#fafafa',
    thFontWeight: '500',
  },
  // 浮层（保存▾ 等）：naive 默认那套 `0 9px 28px` 浓投影看着像"大边框盒子"，
  // 换成 app 的轻投影令牌 + 8px 圆角；底色浅色用面板白、暗色用 surface-3（否则与面板同色，糊在一起）
  Popover: {
    borderRadius: '8px',
    boxShadow: 'var(--app-shadow-menu)',
    color: isDark.value ? 'var(--app-surface-3)' : 'var(--app-panel)',
    padding: '4px',
  },
}))

const showEnvManager = ref(false)
const showSettings = ref(false)
const showHistory = ref(false)
const showCookies = ref(false)
const showPalette = ref(false)
const showImport = ref(false)
const showCodegen = ref(false)
const showMock = ref(false)
const showSync = ref(false)
const syncStatus = ref<SyncStatus | null>(null)
let syncTimer: ReturnType<typeof setInterval> | null = null
/** 欢迎页展示用的「上次打开的目录」（多根：取最近恢复列表里的第一个）。 */
const lastDir = savedRootDirs()[0] ?? ''

// 界面缩放：作用在根元素上，弹层（teleport 到 body）也会一起缩放
watchEffect(() => {
  document.documentElement.style.zoom = String(settings.uiScale || 1)
})

const requestCount = computed(() => {
  const walk = (nodes: TreeNode[]): number =>
    nodes.reduce((n, x) => n + (x.type === 'request' ? 1 : walk(x.children ?? [])), 0)
  return walk(coll.tree)
})

// ---- 请求区 / 响应区分栏 ----
const workEl = ref<HTMLElement | null>(null)
const editorEl = ref<HTMLElement | null>(null)
const respEl = ref<HTMLElement | null>(null)
const respSize = ref(settings.responseSize)
// 拖动中不覆盖本地值，避免落盘往返把拖动位置"回跳"
let resizing = false

watch(
  () => settings.responseSize,
  (v) => {
    if (!resizing) respSize.value = v
  },
)

// 响应区尺寸：flex-basis 百分比（right 生效为宽度、bottom 生效为高度）
const respStyle = computed(() => ({ flexBasis: `${respSize.value}%` }))

function setLayout(layout: 'right' | 'bottom'): void {
  if (settings.responseLayout === layout) return
  void settings.save({ ...settings.form, responseLayout: layout })
}

// 主题切换：界面立即生效并落盘（原生窗口底色由 Go 侧 SaveSettings 同步）
async function toggleTheme(): Promise<void> {
  try {
    await settings.setTheme(nextTheme())
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

function startResize(e: PointerEvent): void {
  const box = workEl.value
  if (!box) return
  e.preventDefault()
  resizing = true
  const horizontal = settings.responseLayout === 'right'
  // 四舍五入 + 20~80 夹取，与 Go 侧 config.Normalize 的范围一致
  const clamp = (n: number): number => Math.min(80, Math.max(20, Math.round(n)))

  const move = (ev: PointerEvent): void => {
    const r = box.getBoundingClientRect()
    respSize.value = horizontal
      ? clamp(((r.right - ev.clientX) / r.width) * 100)
      : clamp(((r.bottom - ev.clientY) / r.height) * 100)
  }
  const stop = (): void => {
    resizing = false
    window.removeEventListener('pointermove', move)
    window.removeEventListener('pointerup', stop)
    window.removeEventListener('pointercancel', stop)
    document.body.classList.remove('dragging', 'col', 'row')
    void settings.save({ ...settings.form, responseSize: respSize.value })
  }

  document.body.classList.add('dragging', horizontal ? 'col' : 'row')
  window.addEventListener('pointermove', move)
  window.addEventListener('pointerup', stop)
  window.addEventListener('pointercancel', stop)
}

// ---- 打开 / 切换工作目录（多根）----

/** 恢复上限的兜底值：正常走设置项 `restoreLimit`（默认 8），设置没读到才用它。 */
const MAX_RESTORE_ROOTS = 8

/** 目录基名（跨平台：分隔符可能是 / 或 \）。 */
function dirName(dir: string): string {
  const parts = dir.split(/[\\/]/).filter(Boolean)
  return parts[parts.length - 1] ?? dir
}

/**
 * 最近打开过、当前没打开的目录：启动恢复有数量上限（设置里的「启动恢复目录数」），
 * 超出的不静默丢 —— 它们出现在工具条「最近打开」里，一点就开。
 *
 * 数据源是独立的 MRU 列表（`client.recentDirs`），不是「当前打开的根」：后者在恢复被截断时
 * 只含恢复成功的那几个，用它就等于把没恢复的目录忘了；关掉某个根时也一样。
 */
const recentDirs = computed(() => {
  const open = new Set(coll.roots.map((r) => r.info.dir.trim().toLowerCase()))
  return savedRecentDirs()
    .filter((d) => !open.has(d.trim().toLowerCase()))
    .map((d) => ({ dir: d, name: dirName(d) }))
})

/** 按目录找已打开的根（Windows 上目录大小写不敏感）。 */
function rootOfDir(dir: string): string {
  const want = dir.trim().toLowerCase()
  return coll.roots.find((r) => r.info.dir.trim().toLowerCase() === want)?.root ?? ''
}

/**
 * 打开一个工作目录：追加为新的根并置为活动根，然后恢复该根的标签会话。
 * （后端 OpenCollection 已经是「追加 + 幂等」，同一目录重复打开只会切成活动根。）
 */
async function openCollection(dir: string): Promise<void> {
  if (!dir) {
    try {
      dir = await api.pickDirectory()
    } catch {
      return
    }
  }
  if (!dir) {
    message.warning(t('welcome.pickFailed'))
    return
  }
  // 手动保存模式下打开新集合不强制落盘：先确认未保存改动（与切换工作区一致）
  const mode = await tabs.confirmLeaveUnsaved()
  if (mode === 'cancel') return
  await tabs.reset(mode === 'save')
  try {
    await coll.open(dir)
    // 恢复该根的 tab 现场（uid 仍在集合内才打开）
    await tabs.restoreSession()
    void refreshSyncStatus()
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

/**
 * 工作区切换的「保持旧详情」缓冲：switchRoot 会先清空标签再做 I/O，期间 tabs.active 变 null，
 * 若直接走 v-else 会闪一下 Overview。这里在切换期间把上一个请求详情留在屏幕，等新会话就绪再淡入 ——
 * 视觉上就是一次平滑过渡，不会先跳到概览页。
 */
const switchingRoot = ref(false)
const stuckActive = ref<Tab | null>(null)
watch(
  () => tabs.active,
  (t) => {
    if (t) stuckActive.value = t
  },
  { immediate: true },
)
/** 详情区实际渲染的页签：有活动页签用它；切根瞬间没有活动页签时沿用上一个（避免闪概览）。 */
const detailTab = computed(() => tabs.active ?? (switchingRoot.value ? stuckActive.value : null))

/** 侧栏/工具条点击某个根：切成活动根，并把标签栏换成该根的会话。 */
async function activateRoot(root: string): Promise<void> {
  if (!root || root === coll.activeRoot) return
  if (switchingRoot.value) return // 正在切换中，忽略重复点击
  switchingRoot.value = true
  try {
    await tabs.switchRoot(root) // 落盘当前根 → 切活动根 → 恢复目标根会话
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
    return
  } finally {
    switchingRoot.value = false
  }
  void refreshSyncStatus()
}

/** 关闭一个工作目录（其它根不受影响）；关掉活动根时自动切到剩余的根。 */
async function closeRoot(root: string): Promise<void> {
  const wasActive = root === coll.activeRoot
  if (wasActive) {
    // 手动保存模式下关闭活动根同样先确认未保存改动（与切换工作区一致）
    const mode = await tabs.confirmLeaveUnsaved()
    if (mode === 'cancel') return
    await tabs.reset(mode === 'save')
  }
  try {
    await coll.closeRoot(root)
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
    return
  }
  if (wasActive && coll.roots.length) await tabs.restoreSession()
  void refreshSyncStatus()
}

/**
 * 打开请求（命令面板跨目录搜索会带上 root）：
 * 目标根不是活动根时先切过去 —— 页签按根分组，别把别根的请求开到当前组的标签栏里。
 */
async function openRequestInRoot(uid: string, root?: string): Promise<void> {
  if (root && root !== coll.activeRoot) await activateRoot(root)
  await tabs.openRequest(uid)
}

/** 重载某个根（外部改动 / 手工触发）：只刷新它的树，不动其它根。 */
async function reloadRoot(root: string): Promise<void> {
  try {
    await coll.reloadRoot(root)
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

/** 在系统文件管理器中打开某个根对应的工作目录（不切活动根、不改页签）。 */
async function revealRoot(root: string): Promise<void> {
  try {
    await api.revealInFolder(root)
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

/**
 * 启动恢复：按上次打开的目录列表逐个打开，并把上次的活动根设回活动。
 * 单个目录失败（被删/无权限）只提示并跳过，不阻塞其它根。
 *
 * 只恢复前 `settings.restoreLimit` 个（默认 8）：每个根都常驻一个文件监听 + 一次索引扫描。
 * 被截断的**不静默丢** —— 明确提示还剩几个，工具条「最近打开」里一点就能补开。
 */
async function restoreRoots(): Promise<void> {
  const saved = savedRootDirs()
  const limit = Math.max(1, settings.form.restoreLimit || MAX_RESTORE_ROOTS)
  const dirs = saved.slice(0, limit)
  const wantActive = savedActiveDir()
  if (!dirs.length) return
  if (saved.length > dirs.length) {
    message.info(t('welcome.restoreTruncated', { n: dirs.length, m: saved.length - dirs.length }), {
      duration: 8000,
    })
  }
  let firstError = ''
  for (const d of dirs) {
    try {
      await coll.open(d)
    } catch (e) {
      if (!firstError) firstError = `${d}: ${e instanceof Error ? e.message : String(e)}`
    }
  }
  if (!coll.roots.length) {
    if (firstError) message.error(t('welcome.restoreFailed', { msg: firstError }))
    return
  }
  const active = rootOfDir(wantActive)
  if (active && active !== coll.activeRoot) {
    await coll.setActive(active)
  }
  await tabs.restoreSession()
  void refreshSyncStatus()
  if (firstError) message.warning(t('welcome.restoreFailed', { msg: firstError }), { duration: 8000 })
}

function pickEnv(name: string): void {
  coll.setEnv(name)
  tabs.refreshResolve()
}

// ---- 命令面板动作 ----
function onCommand(key: string): void {
  if (key === 'open-dir') void openCollection('')
  else if (key === 'new-request') newDraft()
  else if (key === 'reload') void coll.reload()
  else if (key === 'toggle-layout') setLayout(settings.responseLayout === 'right' ? 'bottom' : 'right')
  else if (key === 'toggle-theme') void toggleTheme()
  else if (key === 'history') showHistory.value = true
  else if (key === 'settings') showSettings.value = true
  else if (key === 'import') showImport.value = true
  else if (key === 'export-md') void exportDoc('markdown')
  else if (key === 'export-html') void exportDoc('html')
}

// ---- 导出文档（H10）----
async function exportDoc(format: 'markdown' | 'html'): Promise<void> {
  try {
    const path = await api.exportDoc(format, coll.name + (format === 'html' ? '.html' : '.md'))
    if (path) message.success(t('export.saved', { path }))
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

async function imported(): Promise<void> {
  await coll.reload()
  tabs.refreshResolve()
}

async function onSynced(): Promise<void> {
  await coll.reload()
  tabs.refreshResolve()
  await refreshSyncStatus()
}

function onHotkey(e: KeyboardEvent): void {
  // 命令面板打开时它自己接管键盘，避免 Ctrl+Enter 等落到编辑器
  if (showPalette.value) return
  if (!e.ctrlKey && !e.metaKey) return
  const key = e.key.toLowerCase()
  if (key === 'k') {
    e.preventDefault()
    showPalette.value = true
  } else if (key === 'enter') {
    e.preventDefault()
    if (tabs.active) void tabs.send(tabs.active.key)
  } else if (key === 'n') {
    e.preventDefault()
    newDraft()
  } else if (key === 'e') {
    e.preventDefault()
    showEnvManager.value = true
  } else if (key === 'w') {
    // Ctrl+W 关闭当前 tab（无 tab 时不拦截，避免吞掉浏览器关闭窗口）
    if (!tabs.active) return
    e.preventDefault()
    requestClose(tabs.active.key)
  } else if (key === 's') {
    // Ctrl+S：已落盘请求立即写盘；新建但未保存的草稿没有磁盘副本，走「保存请求」对话框
    e.preventDefault()
    saveActiveNow()
  } else if (key === 'z') {
    // Ctrl+Z 撤销 / Ctrl+Shift+Z（或 Ctrl+Y）重做：请求级编辑历史
    if (!tabs.active) return
    e.preventDefault()
    if (e.shiftKey) tabs.redo(tabs.active.key)
    else tabs.undo(tabs.active.key)
  } else if (key === 'y') {
    if (!tabs.active) return
    e.preventDefault()
    tabs.redo(tabs.active.key)
  }
}

onMounted(() => {
  const settingsReady = settings.load()
  window.addEventListener('keydown', onHotkey)
  // 关窗前 flush 未保存改动（自动保存模式下补上防抖未触发的最后编辑）；
  // 手动保存模式下不写盘（未保存改动本就该显式保存留档）；新建草稿没有磁盘副本，始终同步一份到 localStorage
  window.addEventListener('beforeunload', () => {
    if (settings.autoSave) void tabs.flushAll()
    tabs.saveDrafts()
  })
  // 窗口失焦：自动保存模式下立即落盘（把「编辑完没保存就切走/关窗」的丢失窗口从防抖时长压到接近零）；
  // 手动保存模式下不落盘 —— 否则切走/失焦就把改动静默写盘，既违背手动保存预期，也可能触发假冲突
  window.addEventListener('blur', () => {
    if (settings.autoSave) void tabs.flushAll()
  })
  // 外部改动（D1/D2 + G10）：干净 tab 自动重载；脏 tab 不覆盖，提示用户。
  // 事件带 root：多根并存时只重载发生改动的那一根
  onAppEvent('collection:changed', (payload) => {
    const p = payload as { root?: string } | null
    onExternalChange(typeof p?.root === 'string' ? p.root : '')
  })
  // Alt+F4 / 系统关闭被 Go 侧 WindowShouldClose 拦截后通知：前端做退出确认（与标题栏关闭一致）
  onAppEvent('app:close-requested', () => {
    requestQuit()
  })
  // 同步状态轮询（状态栏）
  syncTimer = setInterval(() => {
    void refreshSyncStatus()
  }, 5000)
  // 恢复上次打开的工作目录（多根）；单个失败只提示，不阻塞其它根。
  // 必须等设置读完再恢复：恢复上限在设置里，抢跑会拿到默认值，把超出上限的目录当成「不需要恢复」。
  void settingsReady.then(() => restoreRoots())
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onHotkey)
  if (syncTimer) clearInterval(syncTimer)
})

// ---- 同步状态（状态栏）----
async function refreshSyncStatus(): Promise<void> {
  if (!coll.ready) return
  try {
    syncStatus.value = await api.getSyncStatus()
    const s = syncStatus.value
    // 只有明确「已关联 + mirror」才进只读；本地/未关联/其他模式一律可编辑
    if (s?.linked && s.mode === 'mirror') {
      coll.syncMode = 'mirror'
    } else {
      coll.syncMode = ''
    }
    if (s?.linked) {
      void api.startAutoSync(120)
    }
  } catch {
    syncStatus.value = null
    coll.syncMode = ''
  }
}

/**
 * 外部改动（文件监听 / 同步）：事件带 root 时只处理那一根 ——
 * 非活动根只需刷新它的树（当前根的标签与编辑状态与它无关），活动根才走「回填 / 标冲突」。
 */
function onExternalChange(root: string): void {
  void (async () => {
    if (root && root !== coll.activeRoot) {
      try {
        await coll.reloadRoot(root)
      } catch {
        // 忽略：下次事件或手动重载再试
      }
      return
    }
    await coll.reload() // 树 / 环境先跟上（watchLoop 已重建索引，hash 是新的）
    await tabs.refreshResolve()
    // G10 落地：干净页签回填磁盘内容；有未保存编辑的页签标冲突（冲突条给出两个出口）
    const conflicts = await tabs.syncExternalChanges()
    if (conflicts > 0) message.warning(t('conflict.external', { n: conflicts }), { duration: 6000 })
  })()
}

onBeforeUnmount(() => window.removeEventListener('keydown', onHotkey))

// ---- 新建请求：直接开一个未落盘的空 tab；名称与分组在**关闭时**才问 ----
// n-tree-select 默认以 key 字段作为节点标识（不是 value）；根目录用 sentinel 避免空串被当成无效 key
const ROOT_KEY = '__root__'
interface FolderNode {
  label: string
  key: string
  children?: FolderNode[]
}
const folderTree = computed<FolderNode[]>(() => {
  const walk = (nodes: TreeNode[]): FolderNode[] =>
    nodes
      .filter((n) => n.type === 'folder')
      .map<FolderNode>((n) => ({
        label: n.name,
        key: n.path,
        children: n.children ? walk(n.children) : undefined,
      }))
  const rest = walk(coll.tree)
  const rootLabel = t('prompt.folderRoot')
  return rest.length ? [{ label: rootLabel, key: ROOT_KEY, children: rest }] : [{ label: rootLabel, key: ROOT_KEY }]
})

/** 新建请求（folder 为保存时的默认分组）；doc 为「导入 cURL」等预填内容。 */
function newDraft(folder = '', doc: RequestDoc | null = null): void {
  tabs.openDraft(doc, folder)
}

/** 新建 gRPC 请求（G1.1/G1.2）：同样是未落盘草稿，grpc 段全空，由请求栏与 Schema 分段补齐。 */
function newGrpcDraft(folder = ''): void {
  tabs.openDraft(blankGrpcRequest(), folder)
}

// ---- 导入 cURL（侧栏工具栏 / 分组菜单入口） ----
const showCurl = ref(false)
const curlFolder = ref('')

function openCurl(folder = ''): void {
  curlFolder.value = folder
  showCurl.value = true
}

/** 解析结果直接开成草稿 tab：与「新建」走同一条保存路径（关闭时才落盘）。 */
function onCurlImported(doc: RequestDoc, folder: string): void {
  tabs.openDraft(doc, folder)
}

// ---- 草稿保存框：关闭标签时问「保存（名称 + 分组） / 不保存 / 取消」；Ctrl+S 只保存、不关标签 ----
const draftTab = ref<Tab | null>(null)
/** true = 由 Ctrl+S 打开：保存后留在原地，「不保存」按钮不出现 */
const draftSaveOnly = ref(false)
const draftForm = ref({ name: '', folder: '' })
const savingDraft = ref(false)

/** 打开草稿保存框（saveOnly = 保存后不关页签）。 */
function openDraftDialog(tab: Tab, saveOnly: boolean): void {
  draftSaveOnly.value = saveOnly
  draftTab.value = tab
  draftForm.value = { name: draftName(tab), folder: tab.draftFolder || ROOT_KEY }
}

/** 关掉草稿保存框并复位模式。 */
function closeDraftDialog(): void {
  draftTab.value = null
  draftSaveOnly.value = false
}

// ---- 未保存询问框：已落盘请求有未保存改动时，关闭页签前先问「保存并关闭 / 不保存 / 取消」----
// 之前是直接关掉并顺手 flush 落盘：用户按了 × 却不知道改动被写进了文件（手动保存模型下更意外），
// 也没机会改成「不要这次改动」。这里把选择权交回用户。
//
// 用 Promise 而不是「记一个 key 等按钮回调」：批量关闭要**按页签顺序依次**问，逐个 await
// （关一个 → 问下一个），取消即中止整批 —— 靠 key 状态做不到「等用户答完再继续」。
type CloseChoice = 'save' | 'discard' | 'cancel'

const closeAsk = ref<{
  tab: Tab
  /** 批量关闭时的进度（用于提示「第 i / n 个」），单个关闭为 null */
  seq: { i: number; n: number } | null
  resolve: (c: CloseChoice) => void
} | null>(null)
const closeAskTab = computed(() => closeAsk.value?.tab ?? null)
const closeAskSeq = computed(() => closeAsk.value?.seq ?? null)

/** 弹询问框并等用户作答。 */
function askClose(tab: Tab, seq: { i: number; n: number } | null = null): Promise<CloseChoice> {
  return new Promise<CloseChoice>((resolve) => {
    closeAsk.value = { tab, seq, resolve }
  })
}

/** 询问框作答（按钮 / Esc / 遮罩关闭都走这里，否则 await 会一直挂着）。 */
function answerClose(choice: CloseChoice): void {
  const ask = closeAsk.value
  closeAsk.value = null
  ask?.resolve(choice)
}

/**
 * 关掉一个页签（草稿/脏页签都先问；其余直接关）。
 * 返回是否真的关掉了 —— 批量关闭据此判断「用户取消（中止整批）」还是「写盘被拒（保留页签）」。
 * `seq` 用于批量关闭时提示进度（「第 i / n 个」）。
 */
async function closeOne(key: string, seq: { i: number; n: number } | null = null): Promise<boolean> {
  const tab = tabs.tabs.find((t) => t.key === key)
  if (!tab) return true // 已经不在了（批量关闭过程中被前面的操作带走）：算处理过
  // 草稿：走保存框（需要用户提供名称+分组）
  if (tab.draft) {
    openDraftDialog(tab, false)
    return false // 保存框由对话框的保存/丢弃按钮控制关闭，这里不阻塞批量流程（草稿逐个处理）
  }
  if (!tab.dirty) {
    await tabs.close(key)
    return true
  }
  const choice = await askClose(tab, seq)
  if (choice === 'cancel') return false
  if (choice === 'discard') {
    await tabs.closeDiscard(key)
    return true
  }
  // 保存并关闭：写盘被拒（冲突 / 只读 / 磁盘错误）时页签留着，原因由 flush 自己提示
  await tabs.flush(key)
  if (tab.dirty || tab.conflict) return false
  await tabs.close(key)
  return true
}

/** 统一关闭入口：草稿弹保存框；已落盘但有未保存改动先问一句；其余直接关。 */
function requestClose(key: string): void {
  void closeOne(key)
}

/**
 * 批量关闭（关闭左侧 / 右侧 / 全部）：
 *  - 已落盘脏页签：**依次**弹「保存/丢弃/取消」；
 *  - 草稿（新建未落盘）：统一累计，最后**一次性**弹「全部丢弃 / 取消」——N 个草稿弹 N 个保存框不现实；
 *  - 用户点「取消」（任一环节）= 中止整批，剩下的一律保留。
 */
async function closeManySequential(keys: string[]): Promise<void> {
  // dirty 总数用于提示「第 i / n 个」——草稿单独处理，不计入
  const dirty = keys.filter((k) => {
    const td = tabs.tabs.find((x) => x.key === k)
    return !!td && !td.draft && td.dirty
  }).length
  let closed = 0
  let asked = 0
  const remainingDrafts: string[] = []
  for (const key of keys) {
    const tab = tabs.tabs.find((t) => t.key === key)
    if (!tab) continue
    if (tab.draft) {
      remainingDrafts.push(key)
      continue
    }
    const seq = tab.dirty ? { i: ++asked, n: dirty } : null
    if (!(await closeOne(key, seq))) {
      message.info(t('tab.closeAborted', { n: closed }))
      return
    }
    closed++
  }
  // 循环结束后：还有草稿待处理 → 批量确认「全部丢弃？」
  if (remainingDrafts.length) {
    const discarding = await askBatchDiscardDrafts(remainingDrafts.length)
    if (discarding) {
      for (const k of remainingDrafts) {
        const tab = tabs.tabs.find((t) => t.key === k)
        if (!tab) continue
        await tabs.close(k) // 草稿直接 close = 丢弃（没有磁盘副本，无需 closeDiscard）
        closed++
      }
    } else {
      // 用户取消草稿丢弃：本次已关闭的 + 剩余草稿一起保留
      // 如果 closed=0 且取消，不提示
      if (closed > 0) message.info(t('tab.closeAborted', { n: closed }))
      return
    }
  }
  if (!closed) {
    message.info(t('tab.nothingToClose'))
    return
  }
  message.success(t('tab.closedSome', { n: closed }))
}

/** 批量丢弃草稿的确认框：一次性问「有 N 个新建请求未保存，全部丢弃？」。 */
function askBatchDiscardDrafts(n: number): Promise<boolean> {
  return new Promise((resolve) => {
    const title = t('prompt.draftBatchTitle', { n })
    const content = t('prompt.draftBatchContent', { n })
    const d = dialog.warning({
      title,
      content,
      positiveText: t('prompt.draftBatchDiscard'),
      negativeText: t('common.cancel'),
      onPositiveClick: () => resolve(true),
      onNegativeClick: () => resolve(false),
      onClose: () => resolve(false),
      onMaskClick: () => resolve(false),
    })
    void d
  })
}

/** tab 栏右键菜单命令：关闭类走批量关闭（草稿自动跳过），复制新建开草稿，保存所有 = 立即 flush 全部改动。 */
async function onTabCommand(cmd: string, key: string): Promise<void> {
  const i = tabs.tabs.findIndex((t) => t.key === key)
  if (cmd === 'save-all') {
    const n = tabs.tabs.filter((t) => t.dirty && !t.draft && t.uid).length
    if (!n) {
      message.info(t('tab.nothingToSave'))
      return
    }
    await tabs.flushAll()
    message.success(t('tab.savedAll'))
    return
  }
  if (i < 0) return
  if (cmd === 'close') {
    requestClose(key)
    return
  }
  if (cmd === 'duplicate') {
    tabs.duplicate(key)
    return
  }
  const keys =
    cmd === 'close-all'
      ? tabs.tabs.map((t) => t.key)
      : cmd === 'close-left'
        ? tabs.tabs.slice(0, i).map((t) => t.key)
        : tabs.tabs.slice(i + 1).map((t) => t.key)
  await closeManySequential(keys)
}

/** Ctrl+S：已落盘请求立即写盘（flush 会清掉待触发的防抖保存）；新建草稿开保存框，保存后留在原地。 */
function saveActiveNow(): void {
  // 保存框已经开着时不再重复打开（否则会把已填的名称/分组重置）
  if (draftTab.value) return
  const tab = tabs.active
  if (!tab) return
  if (!tab.draft) {
    void tabs.flush(tab.key)
    return
  }
  openDraftDialog(tab, true)
}

/** 保存对话框的名称预填：已有名字优先，gRPC 用「方法 / 服务」名，最后退回页签标题。 */
function draftName(tab: Tab): string {
  const named = tab.request.name.trim()
  if (named) return named
  const g = tab.request.grpc
  if (g) return (g.method || g.service || '').trim() || tab.title
  return tab.title
}

/** 保存草稿（落盘到所选分组）：关闭流程顺带关页签，Ctrl+S 流程保存后留在原地继续编辑。 */
async function confirmSaveDraft(): Promise<void> {
  const tab = draftTab.value
  const name = draftForm.value.name.trim()
  if (!tab || !name) return
  // 树的根目录 key 是 sentinel，落盘前转回空串
  const folder = draftForm.value.folder === ROOT_KEY ? '' : draftForm.value.folder
  savingDraft.value = true
  try {
    await tabs.saveDraft(tab.key, folder, name)
    const keepOpen = draftSaveOnly.value
    closeDraftDialog()
    if (!keepOpen) await tabs.close(tab.key)
    message.success(t('prompt.draftSaved', { name }))
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  } finally {
    savingDraft.value = false
  }
}

/** 不保存：丢弃草稿并关闭（只在关闭流程出现）。 */
async function discardDraft(): Promise<void> {
  const tab = draftTab.value
  closeDraftDialog()
  if (tab) await tabs.discardDraft(tab.key)
}

function envSaved(): void {
  tabs.refreshResolve()
}

// 集合切换后刷新解析预览
watch(
  () => coll.uid,
  () => tabs.refreshResolve(),
)

// ---- G8 滚动位置记忆 ----
function saveScrolls(): void {
  const key = tabs.active?.key
  if (!key) return
  if (editorEl.value) tabs.saveScroll(key, 'editor', editorEl.value.scrollTop)
  if (respEl.value) tabs.saveScroll(key, 'resp', respEl.value.scrollTop)
}

function restoreScrolls(): void {
  const key = tabs.active?.key
  if (!key) return
  const pos = tabs.getScroll(key)
  if (editorEl.value) editorEl.value.scrollTop = pos.editor
  if (respEl.value) respEl.value.scrollTop = pos.resp
}

watch(
  () => tabs.activeKey,
  (next, prev) => {
    if (prev) {
      // 切走前先记下旧 tab 的位置（activeKey 已变，用 prev 找不到 DOM，故依赖 onScroll 里已存的值）
      void prev
    }
    if (next) {
      // DOM 更新后再恢复
      void nextTick(() => restoreScrolls())
    }
  },
)
</script>

<template>
  <n-config-provider :locale="naiveLocale" :date-locale="naiveDateLocale" :theme="naiveTheme"
    :theme-overrides="themeOverrides">
    <div class="app">
      <!-- 标题栏常驻：无边框窗口下即使没打开集合也要有拖动区（主题切换也放这里，未打开集合时也可用） -->
      <!-- 布局切换跟着「有没有打开的请求」走：没有请求时该按钮组不渲染 -->
      <title-bar :dark="settings.isDark" :layout="tabs.active ? settings.responseLayout : null"
        @toggle-theme="toggleTheme" @set-layout="setLayout">
        <!-- 集合 / 环境 / 本地搜索并入标题栏（原先独占一行的工具栏） -->
        <toolbar v-if="coll.ready && coll.info" :name="coll.name" :dir="coll.dir" :envs="coll.info.envs"
          :current-env="coll.currentEnv" :roots="coll.roots" :active-root="coll.activeRoot" :recent="recentDirs"
          @activate="activateRoot" @open-dir="openCollection" @open-other="openCollection('')" @reload="coll.reload()"
          @palette="showPalette = true" @manage-env="showEnvManager = true" @import="showImport = true"
          @export="exportDoc($event)" @mock="showMock = true" @sync="showSync = true" @update:currentEnv="pickEnv" />
      </title-bar>

      <n-spin :show="coll.loading">
        <template v-if="coll.ready && coll.info">
          <div class="body">
            <aside class="side">
              <sidebar :roots="coll.roots" :active-root="coll.activeRoot" :active-uid="tabs.active?.uid ?? ''"
                @activate="activateRoot" @reload-root="reloadRoot" @reveal-root="revealRoot" @close-root="closeRoot"
                @open="tabs.openRequest($event)" @new-request="newDraft" @new-grpc-request="newGrpcDraft"
                @import-curl="openCurl" />
            </aside>

            <main class="main">
              <tab-bar :tabs="tabs.tabs" :active-key="tabs.activeKey" @select="tabs.setActive($event)"
                @select-overview="tabs.setActive('')" @close="requestClose($event)" @new="newDraft()"
                @reorder="(from: number, to: number) => tabs.reorder(from, to)" @command="onTabCommand" />

              <transition name="rb-fade" mode="out-in">
                <div v-if="detailTab" :key="detailTab.key" class="detail">
                  <!-- 冲突条：磁盘文件被外部改动且本页签有未保存编辑，写盘被拒后给出的两个出口 -->
                  <div v-if="detailTab.conflict" class="conflict-bar" data-testid="req.conflict">
                    <n-icon :component="WarningOutline" :size="14" />
                    <span class="ct">{{ t('conflict.bar') }}</span>
                    <button class="cb" type="button" data-testid="conflict.reload"
                      @click="tabs.reloadFromDisk(detailTab.key)">
                      {{ t('conflict.reload') }}
                    </button>
                    <button class="cb strong" type="button" data-testid="conflict.copy"
                      @click="tabs.saveAsCopy(detailTab.key)">
                      {{ t('conflict.saveCopy') }}
                    </button>
                  </div>
                  <request-bar :tab="detailTab" @codegen="showCodegen = true" @save="saveActiveNow" />
                  <div ref="workEl" class="work" :class="settings.responseLayout">
                    <section ref="editorEl" class="editor-col" @scroll.passive="saveScrolls">
                      <request-editor :tab="detailTab" />
                    </section>

                    <!-- 纯拖动条：布局切换已移到标题栏（TitleBar 的 .layouts），中缝只留拖拽与分隔线 -->
                    <div class="splitter" role="separator" :title="t('editor.resizeHint')"
                      :aria-orientation="settings.responseLayout === 'right' ? 'vertical' : 'horizontal'"
                      @pointerdown="startResize" />

                    <section ref="respEl" class="resp-col" :style="respStyle" @scroll.passive="saveScrolls">
                      <response-panel :tab="detailTab" />
                    </section>
                  </div>
                </div>

                <overview v-else :info="coll.info" @new-request="newDraft()" />
              </transition>
            </main>
          </div>

          <status-bar :requests="requestCount" :envs="coll.info.envs.length" :sync="syncStatus"
            :saving-count="tabs.savingCount" :last-saved-at="tabs.lastSavedAt" @open-sync="showSync = true"
            @history="showHistory = true" @cookies="showCookies = true" @settings="showSettings = true" />
        </template>

        <welcome v-else :last-dir="lastDir" @open="openCollection" />
      </n-spin>

      <env-manager v-model:show="showEnvManager" @saved="envSaved" />
      <settings-dialog v-model:show="showSettings" />
      <history-dialog v-model:show="showHistory" @open="tabs.openRequest($event)" />
      <cookie-dialog v-model:show="showCookies" />
      <import-dialog v-model:show="showImport" @imported="imported" />
      <code-gen-dialog v-model:show="showCodegen" :request="tabs.active?.request ?? null" />
      <mock-dialog v-model:show="showMock" />
      <sync-dialog v-model:show="showSync" @synced="onSynced" />
      <command-palette v-model:show="showPalette" :tree="coll.tree" :envs="coll.info?.envs ?? []" :roots="coll.roots"
        :active-root="coll.activeRoot" @open-request="openRequestInRoot" @switch-env="pickEnv" @command="onCommand" />

      <!-- 导入 cURL：粘贴命令 → 解析预览 → 开成未落盘草稿 tab -->
      <curl-import-dialog v-model:show="showCurl" :folder="curlFolder" @imported="onCurlImported" />

      <!-- 未保存的新建请求：关闭时（可丢弃）或 Ctrl+S 时（只保存）在这里问名称与分组 -->
      <n-modal :show="draftTab !== null" preset="card" :title="t('prompt.saveDraftTitle')" style="width: 460px"
        @update:show="(v: boolean) => (v ? undefined : closeDraftDialog())">
        <p class="draft-hint">{{ t(draftSaveOnly ? 'prompt.saveOnlyHint' : 'prompt.saveDraftHint') }}</p>
        <n-form label-placement="left" label-width="86">
          <n-form-item :label="t('prompt.reqName')">
            <n-input v-model:value="draftForm.name" data-testid="draft.name" @keyup.enter="confirmSaveDraft" />
          </n-form-item>
          <n-form-item :label="t('prompt.folder')">
            <n-tree-select v-model:value="draftForm.folder" :options="folderTree" filterable
              data-testid="draft.folder" />
          </n-form-item>
        </n-form>
        <template #footer>
          <div class="modal-ft">
            <n-button size="small" data-testid="draft.cancel" @click="closeDraftDialog">{{ t('common.cancel')
            }}</n-button>
            <n-button v-if="!draftSaveOnly" size="small" data-testid="draft.discard" @click="discardDraft">{{
              t('prompt.discard') }}</n-button>
            <n-button size="small" type="primary" :disabled="!draftForm.name.trim()" :loading="savingDraft"
              data-testid="draft.save" @click="confirmSaveDraft">
              {{ t('common.save') }}
            </n-button>
          </div>
        </template>
      </n-modal>

      <!-- 已落盘请求有未保存改动：关页签前问一句，别静默写盘（批量关闭时按页签顺序逐个弹） -->
      <n-modal :show="closeAskTab !== null" preset="card" :title="t('prompt.unsavedTitle')" style="width: 420px"
        @update:show="(v: boolean) => (v ? undefined : answerClose('cancel'))">
        <p v-if="closeAskSeq" class="draft-hint" data-testid="closeask.seq">
          {{ t('prompt.unsavedBatch', { i: closeAskSeq.i, n: closeAskSeq.n }) }}
        </p>
        <p class="draft-hint">{{ t('prompt.unsavedHint', { name: closeAskTab?.title ?? '' }) }}</p>
        <template #footer>
          <div class="modal-ft">
            <n-button size="small" data-testid="closeask.cancel" @click="answerClose('cancel')">
              {{ t('common.cancel') }}
            </n-button>
            <n-button size="small" data-testid="closeask.discard" @click="answerClose('discard')">
              {{ t('prompt.discard') }}
            </n-button>
            <n-button size="small" type="primary" data-testid="closeask.save" @click="answerClose('save')">
              {{ t('prompt.saveAndClose') }}
            </n-button>
          </div>
        </template>
      </n-modal>
    </div>
  </n-config-provider>
</template>

<style scoped>
.app {
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--app-bg);
  /* 窗口外框圆角：外壳铺满窗口，靠 overflow 把子元素的直角裁掉；
     圆角外透出桌面（窗口透明由 main.go 的 WindowIsTranslucent 开启），
     描边让圆角在浅色桌面上也有清晰边界。 */
  border-radius: var(--app-radius-window);
  border: 1px solid var(--app-border);
  overflow: hidden;
}

/* 标题栏常驻后，n-spin 的两层容器需吃掉剩余高度（而非按 100% 高度计算） */
:deep(.n-spin-container),
:deep(.n-spin-content) {
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.body {
  flex: 1 1 auto;
  display: flex;
  min-height: 0;
}

.side {
  width: var(--app-side-w);
  flex: 0 0 auto;
  border-right: 1px solid var(--app-border);
  overflow: hidden;
  background: var(--app-sidebar);
}

.main {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  min-width: 0;
  background: var(--app-panel);
}

.detail {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  min-height: 0;
}

/* 请求详情 / 概览页之间切换（含切工作区）的淡入淡出，避免硬切造成「闪一下」 */
.rb-fade-enter-active,
.rb-fade-leave-active {
  transition: opacity 0.18s ease;
}

.rb-fade-enter-from,
.rb-fade-leave-to {
  opacity: 0;
}

.work {
  flex: 1 1 auto;
  display: flex;
  min-height: 0;
  min-width: 0;
}

.work.right {
  flex-direction: row;
}

.work.bottom {
  flex-direction: column;
}

/* 请求区自适应剩余空间；响应区尺寸由内联 flex-basis（拖动得到）决定 */
.work .editor-col {
  flex: 1 1 auto;
}

/* 保底尺寸要留小：界面缩放的 zoom 作用在 <html> 上，这里的 px 会随之放大
   （zoom 1.5 时 320px 相当于屏幕上 480px），过大会顶掉拖动得到的比例。 */
.work.right .editor-col {
  min-width: 160px;
}

.work.bottom .editor-col {
  min-height: 120px;
}

.work .resp-col {
  flex-grow: 0;
  flex-shrink: 0;
}

.work.right .resp-col {
  min-width: 160px;
}

.work.bottom .resp-col {
  min-height: 100px;
}

.editor-col,
.resp-col {
  display: flex;
  flex-direction: column;
  overflow: auto;
  background: var(--app-panel);
  min-width: 0;
  min-height: 0;
}

/* 分隔区：浅灰底 + 两侧细线；中央不再放布局切换（已移到标题栏）。
   宽度取 8px（design-spec 的 w28 偏宽，视觉上把编辑器与响应区推得太开；两侧 1px 描边后实际 10px），
   光标与悬停底色沿用，拖拽热区仍覆盖整条。 */
.splitter {
  flex: 0 0 8px;
  position: relative;
  z-index: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--app-bg);
  cursor: col-resize;
}

.work.right .splitter {
  border-left: 1px solid var(--app-border);
  border-right: 1px solid var(--app-border);
}

.work.bottom .splitter {
  cursor: row-resize;
  border-top: 1px solid var(--app-border);
  border-bottom: 1px solid var(--app-border);
}

.splitter:hover {
  background: var(--app-row-hover);
}

.modal-ft {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

/* 关闭未保存的新建请求时的说明行 */
.draft-hint {
  margin: 0 0 12px;
  font-size: 12px;
  color: var(--app-muted);
  line-height: 1.6;
}

/* 冲突条：磁盘文件被外部改动且本页签有未保存编辑（写盘被拒后的两个出口） */
.conflict-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 0 0 auto;
  margin-bottom: 6px;
  padding: 6px 10px;
  border: 1px solid var(--app-warn);
  border-radius: 6px;
  background: var(--app-warn-tint);
  color: var(--app-text-2);
  font-size: 12px;
}

.conflict-bar .ct {
  flex: 1 1 auto;
  min-width: 0;
}

.conflict-bar .cb {
  border: 1px solid var(--app-border-strong);
  border-radius: 6px;
  background: var(--app-panel);
  color: var(--app-text);
  font-family: inherit;
  font-size: 12px;
  padding: 3px 10px;
  cursor: pointer;
  flex: 0 0 auto;
}

.conflict-bar .cb.strong {
  background: var(--app-warn);
  border-color: var(--app-warn);
  color: var(--app-on-accent);
  font-weight: 600;
}
</style>
