<script setup lang="ts">
// 根组件（AppShell，design-spec §2）：标题栏 → 工具栏 → 三栏主体（侧栏 / 标签+请求+响应）→ 状态栏。
// 请求区与响应区的位置由全局设置 responseLayout 决定，占比由分隔条拖动调整（松手落盘）。
import {
  NButton,
  NConfigProvider,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NSelect,
  NSpin,
  darkTheme,
  dateEnUS,
  dateZhCN,
  enUS,
  zhCN,
} from 'naive-ui'
import type { GlobalThemeOverrides } from 'naive-ui'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch, watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import CommandPalette from '@/components/CommandPalette.vue'
import CodeGenDialog from '@/components/CodeGenDialog.vue'
import CookieDialog from '@/components/CookieDialog.vue'
import CurlImportDialog from '@/components/CurlImportDialog.vue'
import EnvManager from '@/components/EnvManager.vue'
import ImportDialog from '@/components/ImportDialog.vue'
import MockDialog from '@/components/MockDialog.vue'
import SyncDialog from '@/components/SyncDialog.vue'
import HistoryDialog from '@/components/HistoryDialog.vue'
import Overview from '@/components/Overview.vue'
import RequestBar from '@/components/RequestBar.vue'
import RequestEditor from '@/components/RequestEditor.vue'
import ResponsePanel from '@/components/ResponsePanel.vue'
import SettingsDialog from '@/components/SettingsDialog.vue'
import Sidebar from '@/components/Sidebar.vue'
import StatusBar from '@/components/StatusBar.vue'
import TabBar from '@/components/TabBar.vue'
import TitleBar from '@/components/TitleBar.vue'
import Toolbar from '@/components/Toolbar.vue'
import Welcome from '@/components/Welcome.vue'
import { api, onAppEvent } from '@/lib/ipc'
import { message } from '@/lib/notice'
import { nextTheme, isDark } from '@/lib/theme'
import { useCollectionStore } from '@/stores/collection'
import { useSettingsStore } from '@/stores/settings'
import { blankGrpcRequest, useTabsStore } from '@/stores/tabs'
import type { Tab } from '@/stores/tabs'
import type { RequestDoc, SyncStatus, TreeNode } from '@/types'

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
const lastDir = localStorage.getItem('client.lastDir') ?? ''

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

// ---- 打开 / 切换集合 ----
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
  tabs.reset()
  try {
    await coll.open(dir)
    // 恢复上次的 tab 现场（uid 仍在集合内才打开）
    await tabs.restoreSession()
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
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
  void settings.load()
  window.addEventListener('keydown', onHotkey)
  // 关窗前 flush 未保存改动（防抖未触发的最后编辑）；新建草稿没有磁盘副本，同步补一份到 localStorage
  window.addEventListener('beforeunload', () => {
    void tabs.flushAll()
    tabs.saveDrafts()
  })
  // 外部改动（D1/D2 + G10）：干净 tab 自动重载；脏 tab 不覆盖，提示用户
  onAppEvent('collection:changed', () => onExternalChange())
  // 同步状态轮询（状态栏）
  syncTimer = setInterval(() => {
    void refreshSyncStatus()
  }, 5000)
  if (lastDir) void openCollection(lastDir)
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

function onExternalChange(): void {
  const dirty = tabs.tabs.filter((t) => t.dirty)
  if (dirty.length === 0) {
    void coll.reload().then(() => tabs.refreshResolve())
    return
  }
  // G10：有未保存编辑时绝不静默覆盖
  message.warning(t('tree.externalChange', { n: dirty.length }), { duration: 6000 })
}

onBeforeUnmount(() => window.removeEventListener('keydown', onHotkey))

// ---- 新建请求：直接开一个未落盘的空 tab；名称与分组在**关闭时**才问 ----
const folderOptions = computed(() => {
  const out: { label: string; value: string }[] = [{ label: t('prompt.folder'), value: '' }]
  const walk = (nodes: TreeNode[]): void => {
    for (const n of nodes) {
      if (n.type === 'folder') {
        out.push({ label: n.name, value: n.path })
        if (n.children) walk(n.children)
      }
    }
  }
  walk(coll.tree)
  return out
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
  draftForm.value = { name: draftName(tab), folder: tab.draftFolder }
}

/** 关掉草稿保存框并复位模式。 */
function closeDraftDialog(): void {
  draftTab.value = null
  draftSaveOnly.value = false
}

/** 统一关闭入口：草稿弹保存框，其余（已落盘）直接关。 */
function requestClose(key: string): void {
  const tab = tabs.tabs.find((x) => x.key === key)
  if (!tab) return
  if (!tab.draft) {
    void tabs.close(key)
    return
  }
  openDraftDialog(tab, false)
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
  savingDraft.value = true
  try {
    await tabs.saveDraft(tab.key, draftForm.value.folder, name)
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
  <n-config-provider
    :locale="naiveLocale"
    :date-locale="naiveDateLocale"
    :theme="naiveTheme"
    :theme-overrides="themeOverrides"
  >
    <div class="app">
      <!-- 标题栏常驻：无边框窗口下即使没打开集合也要有拖动区（主题切换也放这里，未打开集合时也可用） -->
      <!-- 布局切换跟着「有没有打开的请求」走：没有请求时该按钮组不渲染 -->
      <title-bar
        :dark="settings.isDark"
        :layout="tabs.active ? settings.responseLayout : null"
        @toggle-theme="toggleTheme"
        @set-layout="setLayout"
      />

      <n-spin :show="coll.loading">
        <template v-if="coll.ready && coll.info">
          <toolbar
            :name="coll.name"
            :dir="coll.dir"
            :envs="coll.info.envs"
            :current-env="coll.currentEnv"
            @open-other="openCollection('')"
            @reload="coll.reload()"
            @history="showHistory = true"
            @cookies="showCookies = true"
            @settings="showSettings = true"
            @palette="showPalette = true"
            @manage-env="showEnvManager = true"
            @import="showImport = true"
            @export="exportDoc($event)"
            @mock="showMock = true"
            @sync="showSync = true"
            @update:currentEnv="pickEnv"
          />

          <div class="body">
            <aside class="side">
              <sidebar
                :tree="coll.tree"
                :name="coll.name"
                :active-uid="tabs.active?.uid ?? ''"
                @open="tabs.openRequest($event)"
                @new-request="newDraft"
                @new-grpc-request="newGrpcDraft"
                @import-curl="openCurl"
              />
            </aside>

            <main class="main">
              <tab-bar
                :tabs="tabs.tabs"
                :active-key="tabs.activeKey"
                @select="tabs.setActive($event)"
                @select-overview="tabs.setActive('')"
                @close="requestClose($event)"
                @new="newDraft()"
                @new-grpc="newGrpcDraft()"
                @reorder="(from: number, to: number) => tabs.reorder(from, to)"
              />

              <div v-if="tabs.active" class="detail">
                <request-bar :tab="tabs.active" @codegen="showCodegen = true" />
                <div ref="workEl" class="work" :class="settings.responseLayout">
                  <section ref="editorEl" class="editor-col" @scroll.passive="saveScrolls">
                    <request-editor :tab="tabs.active" />
                  </section>

                  <!-- 纯拖动条：布局切换已移到标题栏（TitleBar 的 .layouts），中缝只留拖拽与分隔线 -->
                  <div
                    class="splitter"
                    role="separator"
                    :title="t('editor.resizeHint')"
                    :aria-orientation="settings.responseLayout === 'right' ? 'vertical' : 'horizontal'"
                    @pointerdown="startResize"
                  />

                  <section ref="respEl" class="resp-col" :style="respStyle" @scroll.passive="saveScrolls">
                    <response-panel :tab="tabs.active" />
                  </section>
                </div>
              </div>

              <overview v-else :info="coll.info" @new-request="newDraft()" />
            </main>
          </div>

          <status-bar
            :requests="requestCount"
            :envs="coll.info.envs.length"
            :sync="syncStatus"
            @open-sync="showSync = true"
          />
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
      <command-palette
        v-model:show="showPalette"
        :tree="coll.tree"
        :envs="coll.info?.envs ?? []"
        @open-request="tabs.openRequest($event)"
        @switch-env="pickEnv"
        @command="onCommand"
      />

      <!-- 导入 cURL：粘贴命令 → 解析预览 → 开成未落盘草稿 tab -->
      <curl-import-dialog v-model:show="showCurl" :folder="curlFolder" @imported="onCurlImported" />

      <!-- 未保存的新建请求：关闭时（可丢弃）或 Ctrl+S 时（只保存）在这里问名称与分组 -->
      <n-modal
        :show="draftTab !== null"
        preset="card"
        :title="t('prompt.saveDraftTitle')"
        style="width: 460px"
        @update:show="(v: boolean) => (v ? undefined : closeDraftDialog())"
      >
        <p class="draft-hint">{{ t(draftSaveOnly ? 'prompt.saveOnlyHint' : 'prompt.saveDraftHint') }}</p>
        <n-form label-placement="left" label-width="86">
          <n-form-item :label="t('prompt.reqName')">
            <n-input v-model:value="draftForm.name" data-testid="draft.name" @keyup.enter="confirmSaveDraft" />
          </n-form-item>
          <n-form-item :label="t('prompt.folder')">
            <n-select v-model:value="draftForm.folder" :options="folderOptions" tag filterable data-testid="draft.folder" />
          </n-form-item>
        </n-form>
        <template #footer>
          <div class="modal-ft">
            <n-button size="small" data-testid="draft.cancel" @click="closeDraftDialog">{{ t('common.cancel') }}</n-button>
            <n-button v-if="!draftSaveOnly" size="small" data-testid="draft.discard" @click="discardDraft">{{ t('prompt.discard') }}</n-button>
            <n-button
              size="small"
              type="primary"
              :disabled="!draftForm.name.trim()"
              :loading="savingDraft"
              data-testid="draft.save"
              @click="confirmSaveDraft"
            >
              {{ t('common.save') }}
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
  width: 260px;
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
</style>
