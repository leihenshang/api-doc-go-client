<script setup lang="ts">
// 根组件（AppShell，design-spec §2）：标题栏 → 工具栏 → 三栏主体（侧栏 / 标签+请求+响应）→ 状态栏。
// 请求区与响应区的位置由全局设置 responseLayout 决定，占比由分隔条拖动调整（松手落盘）。
import CodeGenDialog from '@/components/CodeGenDialog.vue'
import CollectionDefaultsDialog from '@/components/CollectionDefaultsDialog.vue'
import CommandPalette from '@/components/CommandPalette.vue'
import CookieDialog from '@/components/CookieDialog.vue'
import CurlImportDialog from '@/components/CurlImportDialog.vue'
import DocEditor from '@/components/DocEditor.vue'
import EnvManager from '@/components/EnvManager.vue'
import FieldMappingsDialog from '@/components/FieldMappingsDialog.vue'
import HistoryDialog from '@/components/HistoryDialog.vue'
import ImportDialog from '@/components/ImportDialog.vue'
import McpConsentDialog from '@/components/McpConsentDialog.vue'
import MockDialog from '@/components/MockDialog.vue'
import Overview from '@/components/Overview.vue'
import RequestBar from '@/components/RequestBar.vue'
import RequestEditor from '@/components/RequestEditor.vue'
import ResponsePanel from '@/components/ResponsePanel.vue'
import RunCollectionDialog from '@/components/RunCollectionDialog.vue'
import SettingsDialog from '@/components/SettingsDialog.vue'
import Sidebar from '@/components/Sidebar.vue'
import StatusBar from '@/components/StatusBar.vue'
import SyncDialog from '@/components/SyncDialog.vue'
import TabBar from '@/components/TabBar.vue'
import TitleBar from '@/components/TitleBar.vue'
import Toolbar from '@/components/Toolbar.vue'
import TrashDialog from '@/components/TrashDialog.vue'
import Welcome from '@/components/Welcome.vue'
import { useCloseFlow } from '@/composables/useCloseFlow'
import { useSplitLayout } from '@/composables/useSplitLayout'
import { api, hasWailsRuntime, onAppEvent, runtimePlatform } from '@/lib/ipc'
import { buildThemeOverrides } from '@/lib/naiveTheme'
import { dialog, message } from '@/lib/notice'
import { requestQuit } from '@/lib/quit'
import { isDark, nextTheme } from '@/lib/theme'
import { clearRecentDirs as clearStoredRecentDirs, savedActiveDir, savedRecentDirs, savedRootDirs, useCollectionStore } from '@/stores/collection'
import { useSettingsStore } from '@/stores/settings'
import type { Tab } from '@/stores/tabs'
import { blankGrpcRequest, useTabsStore } from '@/stores/tabs'
import type { AuthzRequest, MCPStatus, RequestDoc, SyncStatus, TreeNode } from '@/types'
import { WarningOutline } from '@vicons/ionicons5'
import {
  NButton,
  NConfigProvider,
  NForm,
  NFormItem,
  NIcon,
  NInput,
  NModal,
  NSpin,
  NTreeSelect,
  darkTheme,
  dateEnUS,
  dateZhCN,
  enUS,
  zhCN
} from 'naive-ui'
import { computed, h, nextTick, onBeforeUnmount, onMounted, ref, watch, watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'

const coll = useCollectionStore()
const tabs = useTabsStore()
const settings = useSettingsStore()
const { t, locale } = useI18n()

// 标记运行壳：只有 Wails 桌面壳才给窗口加外框留白 + 阴影（浏览器态留白会变成一圈白边）。
document.documentElement.dataset.shell = hasWailsRuntime() ? 'desktop' : 'web'
// 标记平台：Windows 的窗口不是逐像素透明（见 styles/base.css 的 windows 覆盖），留白会变成厚白框。
document.documentElement.dataset.platform = runtimePlatform()

const naiveLocale = computed(() => (locale.value === 'en-US' ? enUS : zhCN))
const naiveDateLocale = computed(() => (locale.value === 'en-US' ? dateEnUS : dateZhCN))

// naive 主题覆盖（明暗两套色值）集中在 lib/naiveTheme —— 色值与 base.css 的 --app-* 令牌同源，改其一须同步另一。
const naiveTheme = computed(() => (isDark.value ? darkTheme : null))
const themeOverrides = computed(() => buildThemeOverrides(isDark.value))

const showEnvManager = ref(false)
const showSettings = ref(false)
// T2：MCP 风险工具授权弹窗（同一时间只展示最新的一个请求；并发同 key 在 Go 侧已去重）。
const authzReq = ref<AuthzRequest | null>(null)
const showHistory = ref(false)
const showTrash = ref(false)
const showCookies = ref(false)
const showPalette = ref(false)
const showImport = ref(false)
const showRun = ref(false)
const showCodegen = ref(false)
const showMock = ref(false)
const showCollDefaults = ref(false)
const showFieldMappings = ref(false)
const showSync = ref(false)
/** 同步设置/冲突弹窗当前针对的根（空串 = 活动根），支持非活动根的冲突展示 */
const syncRoot = ref('')
/** 正在同步的根列表（侧栏根行按钮转圈） */
const syncingRoots = ref<string[]>([])
const syncStatus = ref<SyncStatus | null>(null)
let syncTimer: ReturnType<typeof setInterval> | null = null
/** 内嵌 MCP 服务运行状态（底栏入口展示用）；App 持有以便设置面板与底栏共享同一份数据。 */
const mcpStatus = ref<MCPStatus | null>(null)
/** 设置弹窗打开时跳到的分区（底栏点 MCP 时传入 'mcp'）。 */
const settingsInitial = ref<'appearance' | 'network' | 'local' | 'mcp'>('appearance')
// MCP 入口把 initialSection 置为 'mcp' 后必须复位：它是「一次性直达指令」，
// 残留会让下一次从命令面板 / 底栏「设置」等常规入口打开时直接落在 MCP 分区
watch(showSettings, (v) => {
  if (!v) settingsInitial.value = 'appearance'
})
/** MCP 状态轮询定时器（5s 一刷，反映运行/停止变化）；卸载时清理。 */
let mcpTimer: ReturnType<typeof setInterval> | null = null
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

// ---- 请求区 / 响应区分栏 + 侧栏拖宽（逻辑见 composables/useSplitLayout）----
const { workEl, editorEl, respEl, respStyle, setLayout, startResize, startSideResize, clampSideWidth } =
  useSplitLayout()

// 主题切换：界面立即生效并落盘（原生窗口底色由 Go 侧 SaveSettings 同步）
async function toggleTheme(): Promise<void> {
  try {
    await settings.setTheme(nextTheme())
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

// ---- 打开 / 切换工作目录（多根）----

/** 恢复上限的兜底值：正常走设置项 `restoreLimit`（默认 8），设置没读到才用它。 */
const MAX_RESTORE_ROOTS = 8

/** 目录基名（跨平台：分隔符可能是 / 或 \）。 */
function dirName(dir: string): string {
  const parts = dir.split(/[\\/]/).filter(Boolean)
  return parts[parts.length - 1] ?? dir
}

/** 「最近打开」清理后手动触发重算：localStorage 本身不是响应式的。 */
const recentRev = ref(0)

/**
 * 最近打开过、当前没打开的目录：启动恢复有数量上限（设置里的「启动恢复目录数」），
 * 超出的不静默丢 —— 它们出现在工具条「最近打开」里，一点就开。
 *
 * 数据源是独立的 MRU 列表（`client.recentDirs`），不是「当前打开的根」：后者在恢复被截断时
 * 只含恢复成功的那几个，用它就等于把没恢复的目录忘了；关掉某个根时也一样。
 */
const recentDirs = computed(() => {
  void recentRev.value
  const open = new Set(coll.roots.map((r) => r.info.dir.trim().toLowerCase()))
  return savedRecentDirs()
    .filter((d) => !open.has(d.trim().toLowerCase()))
    .map((d) => ({ dir: d, name: dirName(d) }))
})
/** 历史里可能只剩当前已打开的根（recentDirs 会过滤掉），仍应允许清理。 */
const hasRecentHistory = computed(() => {
  void recentRev.value
  // roots 也要作为依赖：打开集合会 pushRecentDir，但 localStorage 不是响应式的
  void coll.roots.length
  return savedRecentDirs().length > 0
})

/** 清理最近打开记录；当前打开的根不受影响，仍由 client.dirs 恢复。 */
function clearRecentHistory(): void {
  clearStoredRecentDirs()
  recentRev.value++
  message.success(t('toolbar.recentCleared'))
}

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
    // 打开集合时恢复 tab 现场，但同样不让 activeUid 把根自动展开：集合树默认收起，展开态由记忆决定
    coll.restoring = true
    try {
      await tabs.restoreSession()
    } finally {
      await nextTick()
      coll.restoring = false
    }
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

/**
 * 关闭标签时跳过详情区 rb-fade 过渡 —— 关闭是"移除"，fade 让视觉感觉拖泥带水；
 * 但切换激活、新建、根切换这些"进场/换场"动作仍保留过渡。
 */
const skipDetailTransition = ref(false)
async function withoutDetailTransition<T>(fn: () => T | Promise<T>): Promise<T> {
  skipDetailTransition.value = true
  try {
    return await fn()
  } finally {
    // 等 detailTab 重渲染完成（transition 的 name 已切换为空）再恢复
    await nextTick()
    skipDetailTransition.value = false
  }
}

/** 详情区实际渲染的页签：有活动页签用它；切根瞬间没有活动页签时沿用上一个（避免闪概览）。 */
const detailTab = computed(() => tabs.active ?? (switchingRoot.value ? stuckActive.value : null))

/** B19：当前页签是否为文档页签（主区分派到 DocEditor，隐藏请求栏/响应面板）。 */
const isDocTab = computed(() => (detailTab.value?.kind ?? 'request') === 'doc')

/**
 * 输入型对话框（新建文档等）：Promise 返回Trimmed 输入，取消/空串返回 ''。
 * naive 的 dialog 无原生 prompt，这里用「带输入框的 warning」实现一次即可复用。
 */
function promptName(title: string, placeholder: string): Promise<string> {
  return new Promise((resolve) => {
    let value = ''
    let done = false
    const finish = (v: string): void => {
      if (done) return
      done = true
      resolve(v)
    }
    // 单行输入 + Enter 确认 / Esc 取消；空名不提交（保持弹窗，让用户补名字）
    const submit = (): void => {
      const v = value.trim()
      if (!v) return
      finish(v)
      d.destroy?.()
    }
    const d = dialog.warning({
      title,
      content: () =>
        h(NInput, {
          autofocus: true,
          placeholder,
          onUpdateValue: (v: string) => (value = v),
          onKeydown: (e: KeyboardEvent) => {
            if (e.isComposing) return // 输入法组词中
            if (e.key === 'Enter') {
              e.preventDefault()
              submit()
            } else if (e.key === 'Escape') {
              e.preventDefault()
              finish('')
              d.destroy?.()
            }
          },
        }),
      positiveText: t('common.confirm'),
      negativeText: t('common.cancel'),
      onPositiveClick: submit,
      onClose: () => finish(''),
      onNegativeClick: () => {
        finish('')
        d.destroy?.()
      },
    })
  })
}

/**
 * 通知集合树「把选中行滚到可见」的信号：点页签一律加一 ——
 * 重复点当前页签时 activeUid 没变，只靠 Sidebar 对 activeUid 的 watch 不会触发滚动。
 */
const treeRevealTick = ref(0)
function onTabSelect(key: string): void {
  tabs.setActive(key)
  treeRevealTick.value++
}

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

/** 在已打开的所有集合树里找某请求 uid 所属的工作目录（根）；找不到返回空串。 */
function findRootOfUid(uid: string): string {
  const walk = (nodes: TreeNode[]): boolean => {
    for (const n of nodes) {
      if (n.uid === uid && n.type === 'request') return true
      if (n.children && walk(n.children)) return true
    }
    return false
  }
  return coll.roots.find((r) => walk(r.info.tree))?.root ?? ''
}

/**
 * 打开请求（命令面板跨目录搜索会带上 root）：
 * 目标根不是活动根时先切过去 —— 页签按根分组，别把别根的请求开到当前组的标签栏里。
 */
async function openRequestInRoot(uid: string, root?: string): Promise<void> {
  if (root && root !== coll.activeRoot) await activateRoot(root)
  await tabs.openRequest(uid)
}

/**
 * 从历史记录打开：记录可能属于任一已打开集合，先定位所属集合根并切过去（复用 openRequestInRoot
 * 的切根逻辑，含未保存改动确认），左侧树才会展开并选中对应 API。若直接 tabs.openRequest(uid)
 * 不切根，activeRoot 仍是旧集合，树里找不到该节点 → 选中态跟不过去。
 */
async function openHistoryRequest(uid: string): Promise<void> {
  await openRequestInRoot(uid, findRootOfUid(uid))
}

/** 打开文档（命令面板用）：跨根时先切根，再开文档页签。 */
async function openDocInRoot(uid: string, root?: string): Promise<void> {
  if (root && root !== coll.activeRoot) await activateRoot(root)
  await tabs.openDocTab(uid)
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
  coll.restoring = true
  try {
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
  } finally {
    // 等一次 watcher flush 再解除：恢复会话写入 activeUid 的 watch 可能在 finally 之后才跑
    await nextTick()
    coll.restoring = false
  }
}

function pickEnv(name: string): void {
  coll.setEnv(name)
  tabs.refreshResolve()
}

// ---- 命令面板动作 ----
function onCommand(key: string): void {
  if (key === 'open-dir') void openCollection('')
  else if (key === 'new-request') newDraft()
  else if (key === 'run-collection') showRun.value = true
  else if (key === 'reload') void coll.reload()
  else if (key === 'toggle-layout') setLayout(settings.responseLayout === 'right' ? 'bottom' : 'right')
  else if (key === 'toggle-theme') void toggleTheme()
  else if (key === 'history') showHistory.value = true
  else if (key === 'settings') showSettings.value = true
  else if (key === 'import') showImport.value = true
  else if (key === 'export-md') void exportDoc('markdown')
  else if (key === 'export-html') void exportDoc('html')
}

// ---- 导出（H10：Markdown / HTML；item 26：集合整包 apidocgo）----
async function exportDoc(format: 'markdown' | 'html' | 'apidocgo'): Promise<void> {
  try {
    const ext = format === 'html' ? '.html' : format === 'apidocgo' ? '.apidocgo.json' : '.md'
    const path = await api.exportDoc(format, coll.name + ext)
    if (path) message.success(t('export.saved', { path }))
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

/** 拉取内嵌 MCP 服务运行状态（后端不支持时静默降级为 null）。 */
async function loadMcpStatus(): Promise<void> {
  try {
    mcpStatus.value = await api.mcpStatus()
  } catch {
    mcpStatus.value = null
  }
}

/** 从底栏 MCP 入口打开设置并直接定位到 MCP 分区。 */
function openMcpSettings(): void {
  settingsInitial.value = 'mcp'
  showSettings.value = true
}

async function imported(): Promise<void> {
  await coll.reload()
  tabs.refreshResolve()
}

/** 打开同步设置/冲突弹窗：默认活动根；根行菜单进入时根已先切活动。 */
function openSyncSettings(root = ''): void {
  syncRoot.value = root
  showSync.value = true
}

/**
 * 侧栏根行「刷新」：直接同步指定根，**不切换活动根/页签**。
 * 默认不弹设置窗；同步后有冲突才打开冲突页；缺 PAT 等配置错误时才打开设置。
 */
async function syncRootNow(root: string): Promise<void> {
  if (!root || syncingRoots.value.includes(root)) return
  syncingRoots.value = [...syncingRoots.value, root]
  try {
    const rep = await api.runSyncForRoot(root)
    // 树/页签回填由 collection:changed 事件的 onExternalChange(root) 处理，这里只负责反馈
    const confs = await api.listConflictsFor(root)
    if (confs.length || (rep?.conflicts ?? 0) > 0) {
      message.warning(t('sync.conflictPrompt', { n: confs.length || rep?.conflicts || 0 }))
      syncRoot.value = root
      showSync.value = true
    } else {
      message.success(t('sync.done', { p: rep?.pulled ?? 0, s: rep?.pushed ?? 0 }))
    }
    if (root === coll.activeRoot) await refreshSyncStatus()
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e)
    message.error(msg)
    // 缺少令牌/未绑定：直接打开该根的同步设置（仍不切活动根）
    if (/令牌|PAT|未关联/.test(msg)) {
      syncRoot.value = root
      showSync.value = true
    }
  } finally {
    syncingRoots.value = syncingRoots.value.filter((r) => r !== root)
  }
}

/** 同步弹窗内绑定/冲突处理后的收尾：非活动根只刷它的树，活动根还要回填页签。 */
async function onSynced(root?: string): Promise<void> {
  if (root && root !== coll.activeRoot) {
    await coll.reloadRoot(root)
    return
  }
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
  // 侧栏宽：应用上次拖过的值（夹取到当前 [标题宽, 50%]），窗口缩放时同步夹取
  clampSideWidth()
  window.addEventListener('resize', clampSideWidth)
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
  // T2：外部 AI 调用风险工具 → 弹授权确认；settled 后关闭对应弹窗
  onAppEvent('mcp:authz-request', (payload) => {
    authzReq.value = (payload as AuthzRequest) ?? null
  })
  onAppEvent('mcp:authz-settled', (payload) => {
    const p = payload as { requestId?: string } | null
    if (!p?.requestId || authzReq.value?.requestId === p.requestId) {
      authzReq.value = null
    }
  })
  // 同步状态轮询（状态栏）
  syncTimer = setInterval(() => {
    void refreshSyncStatus()
  }, 5000)
  // MCP 运行状态轮询（底栏入口）：每 5s 刷新一次，反映「启用/停止」变化
  void loadMcpStatus()
  mcpTimer = setInterval(() => {
    void loadMcpStatus()
  }, 5000)
  // 恢复上次打开的工作目录（多根）；单个失败只提示，不阻塞其它根。
  // 必须等设置读完再恢复：恢复上限在设置里，抢跑会拿到默认值，把超出上限的目录当成「不需要恢复」。
  void settingsReady.then(() => restoreRoots())
})

// 唯一的卸载清理入口：keydown / resize 监听与两个轮询定时器都在这里收尾。
// 曾出现过第二个 onBeforeUnmount 重复摘 keydown（《客户端代码分析问题》F-04），
// 摘除本身幂等但会让「监听在哪收尾」变成两处真相，故合并为此一处。
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onHotkey)
  window.removeEventListener('resize', clampSideWidth)
  if (syncTimer) clearInterval(syncTimer)
  if (mcpTimer) clearInterval(mcpTimer)
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

/** 冲突条 / 文档被删提示条上的「仍然保存」：先确认，再用当前编辑覆盖磁盘（删除时=重建文件）。 */
function askOverwrite(key: string): void {
  dialog.warning({
    title: t('conflict.overwriteTitle'),
    content: t('conflict.overwriteHint'),
    positiveText: t('conflict.overwrite'),
    negativeText: t('common.cancel'),
    onPositiveClick: () => tabs.saveOverwriting(key),
  })
}

// ---- 新建请求：直接开一个未落盘的空 tab；名称与分组在**关闭时**才问 ----
// 分组用 n-tree-select 树形选择：key=目录相对路径；根目录用 sentinel 避免空串被当成无效 key
// 经常量锚定组件：Volar「整理导入」曾多次把模板里使用的组件误判为未使用而删除导入，
// 导致弹窗里分组选择器整个不渲染；在 <script> 中显式引用后导入不会再被误删。
const TreeSelectComp = NTreeSelect
const ROOT_KEY = '__root__'
// 页签关闭 / 草稿保存流程（逻辑见 composables/useCloseFlow）
const {
  draftTab,
  draftSaveOnly,
  draftForm,
  savingDraft,
  closeDraftDialog,
  confirmSaveDraft,
  discardDraft,
  onDraftEnter,
  closeAskTab,
  closeAskSeq,
  answerClose,
  requestClose,
  onTabCommand,
  saveActiveNow,
} = useCloseFlow({ withoutDetailTransition, rootKey: ROOT_KEY })

interface TreeOption {
  key: string
  label: string
  children?: TreeOption[]
  [k: string]: unknown
}
const folderTreeOptions = computed<TreeOption[]>(() => {
  const walk = (nodes: TreeNode[]): TreeOption[] =>
    nodes
      .filter((n) => n.type === 'folder')
      .map<TreeOption>((n) => {
        const kids = n.children ? walk(n.children) : undefined
        // 只含接口、没有子目录的文件夹是叶子：省略 children 并显式标记 isLeaf，避免出现可展开箭头
        return kids && kids.length
          ? { key: n.path, label: n.name, children: kids }
          : { key: n.path, label: n.name, isLeaf: true }
      })
  const rest = walk(coll.tree)
  const rootLabel = t('prompt.folderRoot')
  return rest.length
    ? [{ key: ROOT_KEY, label: rootLabel, children: rest }]
    : [{ key: ROOT_KEY, label: rootLabel, isLeaf: true }]
})

/** 新建请求（folder 为保存时的默认分组）；doc 为「导入 cURL」等预填内容。 */
function newDraft(folder = '', doc: RequestDoc | null = null): void {
  tabs.openDraft(doc, folder)
}

/** B19：在指定分组（folder 空 = docs/ 默认位；`.` = 集合根）新建文档并直接开文档页签。 */
async function newDocAt(folder = ''): Promise<void> {
  const name = await promptName(t('docs.new'), t('docs.namePh'))
  if (!name) return
  try {
    const d = await coll.createDoc(name, folder)
    await tabs.openDocTab(d.uid)
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
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
  (next) => {
    // 旧 tab 的滚动位置已在 onScroll 里实时存入 getScroll/saveScroll，无需在此用 prev 回捞
    // （activeKey 已变，prev 找不到对应 DOM）。
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
          :has-recent="hasRecentHistory" @clear-recent="clearRecentHistory" @activate="activateRoot"
          @open-dir="openCollection" @open-other="openCollection('')" @palette="showPalette = true"
          @manage-env="showEnvManager = true" @update:currentEnv="pickEnv" />
      </title-bar>

      <n-spin :show="coll.loading">
        <template v-if="coll.ready && coll.info">
          <div class="body">
            <aside class="side">
              <sidebar :roots="coll.roots" :active-root="coll.activeRoot" :active-uid="tabs.active?.uid ?? ''"
                :reveal-tick="treeRevealTick" :busy-sync-roots="syncingRoots" @activate="activateRoot"
                @reload-root="reloadRoot" @reveal-root="revealRoot" @close-root="closeRoot"
                @open="tabs.openRequest($event)" @new-request="newDraft" @new-grpc-request="newGrpcDraft"
                @new-doc="newDocAt" @import-curl="openCurl" @sync="openSyncSettings()" @sync-root="syncRootNow"
                @open-collection="openCollection('')" @create-collection="openCollection" />
            </aside>

            <!-- 侧栏拖宽手柄：夹在 [标题自然宽, 页面 50%] 之间，松手记入 localStorage -->
            <div class="side-splitter" role="separator" aria-orientation="vertical" data-testid="side.splitter"
              :title="t('sidebar.resizeHint')" @pointerdown="startSideResize" />

            <main class="main">
              <tab-bar :tabs="tabs.tabs" :active-key="tabs.activeKey" @select="onTabSelect"
                @select-overview="tabs.setActive('')" @close="requestClose($event)" @new="newDraft()"
                @reorder="(from: number, to: number) => tabs.reorder(from, to)" @command="onTabCommand" />

              <transition :name="skipDetailTransition ? '' : 'rb-fade'" mode="out-in">
                <div v-if="detailTab" :key="detailTab.key" class="detail">
                  <!-- 冲突条：磁盘文件被外部改动且本页签有未保存编辑，写盘被拒后给出的出口；
                       文档被外部删除（missing）也走同一条：隐藏「重新加载」，保留「仍然保存」重建文件 -->
                  <div v-if="detailTab.conflict || detailTab.missing" class="conflict-bar" data-testid="req.conflict">
                    <n-icon :component="WarningOutline" :size="14" />
                    <span class="ct">{{ detailTab.missing ? t('conflict.docMissingBar') : t('conflict.bar') }}</span>
                    <button v-if="!detailTab.missing" class="cb" type="button" data-testid="conflict.reload"
                      @click="tabs.reloadFromDisk(detailTab.key)">
                      {{ t('conflict.reload') }}
                    </button>
                    <button class="cb strong" type="button" data-testid="conflict.overwrite"
                      @click="askOverwrite(detailTab.key)">
                      {{ t('conflict.overwrite') }}
                    </button>
                    <button v-if="!detailTab.missing" class="cb" type="button" data-testid="conflict.copy"
                      @click="tabs.saveAsCopy(detailTab.key)">
                      {{ t('conflict.saveCopy') }}
                    </button>
                  </div>
                  <!-- B19：文档页签 —— 隐藏请求栏与响应面板，编辑区满幅 -->
                  <template v-if="isDocTab && detailTab">
                    <doc-editor :doc="detailTab.doc!" :readonly="coll.isReadOnly"
                      @update:doc="(d) => tabs.touchDoc(detailTab!.key, d)" @save="saveActiveNow" />
                  </template>
                  <template v-else>
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
                  </template>
                </div>

                <overview v-else :info="coll.info" @new-request="newDraft()" @run="showRun = true"
                  @import="showImport = true" @export="exportDoc($event)" @mock="showMock = true"
                  @collection-defaults="showCollDefaults = true" @field-mappings="showFieldMappings = true" />
              </transition>
            </main>
          </div>

          <status-bar :requests="requestCount" :envs="coll.info.envs.length" :sync="syncStatus" :mcp="mcpStatus"
            :saving-count="tabs.savingCount" :last-saved-at="tabs.lastSavedAt" @open-sync="openSyncSettings()"
            @history="showHistory = true" @trash="showTrash = true" @cookies="showCookies = true"
            @settings="showSettings = true" @mcp="openMcpSettings" />
        </template>

        <welcome v-else :last-dir="lastDir" @open="openCollection" />
      </n-spin>

      <env-manager v-model:show="showEnvManager" @saved="envSaved" />
      <mcp-consent-dialog :req="authzReq" />
      <settings-dialog v-model:show="showSettings" :initial-section="settingsInitial" @saved="loadMcpStatus()" />
      <history-dialog v-model:show="showHistory" @open="openHistoryRequest" />
      <trash-dialog v-model:show="showTrash" />
      <cookie-dialog v-model:show="showCookies" />
      <import-dialog v-model:show="showImport" @imported="imported" />
      <run-collection-dialog v-model:show="showRun" :envs="coll.info?.envs ?? []" :collection-name="coll.info?.name ?? ''" />
      <code-gen-dialog v-model:show="showCodegen" :request="tabs.active?.request ?? null" />
      <mock-dialog v-model:show="showMock" />
      <collection-defaults-dialog v-model:show="showCollDefaults" />
      <field-mappings-dialog v-model:show="showFieldMappings" />
      <sync-dialog v-model:show="showSync" :root="syncRoot" @synced="onSynced" />
      <command-palette v-model:show="showPalette" :tree="coll.tree" :envs="coll.info?.envs ?? []" :roots="coll.roots"
        :active-root="coll.activeRoot" @open-request="openRequestInRoot" @open-doc="openDocInRoot" @switch-env="pickEnv"
        @command="onCommand" />

      <!-- 导入 cURL：粘贴命令 → 解析预览 → 开成未落盘草稿 tab -->
      <curl-import-dialog v-model:show="showCurl" :folder="curlFolder" @imported="onCurlImported" />

      <!-- 未保存的新建请求：关闭时（可丢弃）或 Ctrl+S 时（只保存）在这里问名称与分组 -->
      <n-modal :show="draftTab !== null" preset="card" :title="t('prompt.saveDraftTitle')" style="width: 460px"
        @update:show="(v: boolean) => (v ? undefined : closeDraftDialog())">
        <div @keydown.enter="onDraftEnter">
        <p class="draft-hint">{{ t(draftSaveOnly ? 'prompt.saveOnlyHint' : 'prompt.saveDraftHint') }}</p>
        <n-form label-placement="left" label-width="86">
          <n-form-item :label="t('prompt.reqName')">
            <n-input v-model:value="draftForm.name" autofocus data-testid="draft.name" />
          </n-form-item>
          <n-form-item :label="t('prompt.folder')">
            <component :is="TreeSelectComp" v-model:value="draftForm.folder" :options="folderTreeOptions" show-line
              filterable :placeholder="t('prompt.folderPlaceholder')" data-testid="draft.folder" />
          </n-form-item>
        </n-form>
        </div>
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

<style scoped src="./styles/app-shell.css"></style>
