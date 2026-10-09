<script setup lang="ts">
// 全局设置：界面（语言/主题/缩放/响应区位置）、网络策略（Doc C5）、本地数据（Doc C10）。
import { api } from '@/lib/ipc'
import { ThemeDark, ThemeLight, type ThemeMode } from '@/lib/theme'
import { useCollectionStore } from '@/stores/collection'
import { useSettingsStore } from '@/stores/settings'
import type { MCPStatus, Settings } from '@/types'
import { CodeSlashOutline, ColorPaletteOutline, FolderOpenOutline, GlobeOutline } from '@vicons/ionicons5'
import { NButton, NCheckbox, NIcon, NInput, NInputNumber, NModal, NPopconfirm, NSelect } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ 'update:show': [v: boolean]; saved: [] }>()
const { t, locale } = useI18n()
const settings = useSettingsStore()
const coll = useCollectionStore()

const form = ref<Settings>({ ...settings.form })
const error = ref('')
const notice = ref('')
const saving = ref(false)

/** 内嵌 MCP 服务的运行状态：打开设置时拉一次，保存后再拉一次。 */
const mcp = ref<MCPStatus | null>(null)
const mcpBusy = ref(false)

// ---------- 左侧菜单分区 ----------
// 每类设置一个分区，右侧只渲染当前分区（设置项多起来后不至于一路往下滚）。
type SectionKey = 'appearance' | 'network' | 'local' | 'mcp'

const sections = computed(() => [
  { key: 'appearance' as const, label: t('settings.appearance'), icon: ColorPaletteOutline },
  { key: 'network' as const, label: t('settings.network'), icon: GlobeOutline },
  { key: 'local' as const, label: t('settings.local'), icon: FolderOpenOutline },
  { key: 'mcp' as const, label: t('settings.mcp'), icon: CodeSlashOutline },
])

const active = ref<SectionKey>('appearance')

async function loadMCP(): Promise<void> {
  try {
    const st = await api.mcpStatus()
    mcp.value = st
    // 后端在「启用但令牌为空」时会自动生成并落盘，把这个令牌回填到表单，
    // 否则输入框会一直显示空，用户以为没生成（store 里存的仍是提交时的空值）。
    if (st.token && !form.value.mcp.token) form.value.mcp.token = st.token
  } catch {
    mcp.value = null // 后端不支持（如纯浏览器态未注入）时静默降级
  }
}

/** 来源白名单在界面上是逗号分隔的字符串，存的是数组。 */
const originsText = computed({
  get: () => (form.value.mcp?.allowOrigins ?? []).join(', '),
  set: (v: string) => {
    form.value.mcp.allowOrigins = v
      .split(',')
      .map((x) => x.trim())
      .filter(Boolean)
  },
})

/** 端口用文本输入承载，落盘时转成数字；非法输入按 0 处理。 */
const portText = computed({
  get: () => String(form.value.mcp.port),
  set: (v: string) => {
    const n = Number.parseInt(v, 10)
    form.value.mcp.port = Number.isNaN(n) ? 0 : n
  },
})

// ---------- MCP 可访问工作区：直接列出客户端当前加载的所有工作区，勾选授权 ----------

/** 该目录是否已授权（进入白名单）。 */
function isAllow(dir: string): boolean {
  return form.value.mcp.allow.some((a) => a.path === dir)
}

/** 该目录当前的可写标记（未授权时为 false）。 */
function writableOf(dir: string): boolean {
  return form.value.mcp.allow.find((a) => a.path === dir)?.writable ?? false
}

/** 勾选 = 授权该工作区（加入白名单），默认可写；取消勾选 = 移除授权。 */
function toggleAllow(dir: string, v: boolean): void {
  if (v) {
    if (!isAllow(dir)) form.value.mcp.allow.push({ path: dir, writable: true })
  } else {
    const i = form.value.mcp.allow.findIndex((a) => a.path === dir)
    if (i >= 0) form.value.mcp.allow.splice(i, 1)
  }
}

/** 切换某条授权目录的可写开关（保存后生效）。 */
function setAllowWritable(dir: string, v: boolean): void {
  const a = form.value.mcp.allow.find((x) => x.path === dir)
  if (a) a.writable = v
}

async function regenerateToken(): Promise<void> {
  mcpBusy.value = true
  error.value = ''
  try {
    form.value.mcp.token = (await api.regenerateMCPToken()).token
    mcp.value = await api.mcpStatus()
    notice.value = t('settings.mcpTokenRotated')
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    mcpBusy.value = false
  }
}

/** 生成一份可直接粘贴到 MCP 客户端（Claude Code / Cursor 等）的 mcpServers 配置示例。 */
function mcpExample(): string {
  const url = mcp.value?.url ?? ''
  const token = mcp.value?.token ?? ''
  return JSON.stringify(
    {
      mcpServers: {
        apidoc: {
          type: 'http',
          url,
          headers: { Authorization: `Bearer ${token}` },
        },
      },
    },
    null,
    2,
  )
}

/** 一键复制 MCP 配置示例。 */
async function copyExample(): Promise<void> {
  try {
    await navigator.clipboard.writeText(mcpExample())
    notice.value = t('settings.mcpCopied')
  } catch {
    error.value = t('settings.mcpCopyFailed')
  }
}

const langOptions = [
  { label: '简体中文', value: 'zh-CN' },
  { label: 'English', value: 'en-US' },
]
const scaleOptions = [
  { label: '100%', value: 1 },
  { label: '125%', value: 1.25 },
  { label: '150%', value: 1.5 },
  { label: '200%', value: 2 },
]
const layoutOptions = computed(() => [
  { label: t('settings.layoutRight'), value: 'right' },
  { label: t('settings.layoutBottom'), value: 'bottom' },
])
const themeOptions = computed(() => [
  { label: t('settings.themeLight'), value: ThemeLight },
  { label: t('settings.themeDark'), value: ThemeDark },
])

watch(
  () => props.show,
  (v) => {
    if (!v) return
    error.value = ''
    notice.value = ''
    // mcp 是嵌套对象：必须逐层拷贝，否则表单里的编辑会直接写进 store（取消也回不去）
    form.value = { ...settings.form, mcp: { ...settings.form.mcp, allowOrigins: [...(settings.form.mcp?.allowOrigins ?? [])] } }
    active.value = 'appearance' // 每次打开回到第一个分区（否则上次停留的地方会让人以为设置变了）
    void loadMCP()
  },
)

// 语言即时生效（切换后无需保存即可看到界面变化）
function pickLang(v: string): void {
  locale.value = v
  localStorage.setItem('client.lang', v)
}

// 主题同样即时生效，但需要落盘（原生窗口底色也跟随），因此直接走 store 而不是等「保存」
async function pickTheme(v: ThemeMode): Promise<void> {
  form.value.theme = v
  error.value = ''
  try {
    await settings.setTheme(v)
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

/** 保存：写盘 + 重拉 MCP 状态。返回是否成功（供「保存」/「保存并应用」复用）。 */
async function doSave(): Promise<boolean> {
  saving.value = true
  error.value = ''
  let ok = false
  try {
    await settings.save({ ...form.value })
    // MCP 分区改动要重启内嵌服务，后端已自动应用，这里拉最新状态回显
    await loadMCP()
    emit('saved')
    ok = true
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    saving.value = false
  }
  return ok
}

/** 保存并关闭设置窗口。 */
function save(): void {
  void (async () => {
    if (await doSave()) emit('update:show', false)
  })()
}

/** 保存并应用（不关闭窗口）：适合边改边验证的设置项。 */
function saveApply(): void {
  void doSave()
}

async function clearCookies(): Promise<void> {
  error.value = ''
  try {
    await api.clearCookies()
    notice.value = t('settings.cookiesCleared')
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

function close(): void {
  emit('update:show', false)
}
</script>

<template>
  <n-modal :show="show" preset="card" :title="t('settings.title')" style="width: 760px" @update:show="close">
    <div class="cols">
      <!-- 左侧竖向菜单：按设置类型分区，右侧只显示当前分区 -->
      <nav class="nav" :aria-label="t('settings.title')">
        <button
          v-for="sec in sections"
          :key="sec.key"
          class="nav-item"
          :class="{ on: active === sec.key }"
          type="button"
          :data-testid="`settings.nav.${sec.key}`"
          :aria-current="active === sec.key"
          @click="active = sec.key"
        >
          <n-icon :component="sec.icon" :size="15" />
          <span>{{ sec.label }}</span>
        </button>
      </nav>

      <div class="body">
        <p v-if="error" class="err">{{ error }}</p>
        <p v-if="notice" class="ok">{{ notice }}</p>

        <!-- 分区一：界面 -->
        <div v-show="active === 'appearance'" class="pane" data-testid="settings.pane.appearance">
      <div class="row">
        <span class="lbl">{{ t('settings.language') }}</span>
        <n-select
          :value="locale"
          :options="langOptions"
          size="small"
          class="num"
          data-testid="settings.lang"
          @update:value="pickLang"
        />
      </div>
      <div class="row">
        <span class="lbl">{{ t('settings.theme') }}</span>
        <n-select
          :value="form.theme"
          :options="themeOptions"
          size="small"
          class="num"
          data-testid="settings.theme"
          @update:value="pickTheme"
        />
      </div>
      <div class="row">
        <span class="lbl">{{ t('settings.uiScale') }}</span>
        <n-select v-model:value="form.uiScale" :options="scaleOptions" size="small" class="num" data-testid="settings.scale" />
      </div>
      <div class="row">
        <span class="lbl">{{ t('settings.responseLayout') }}</span>
        <n-select
          v-model:value="form.responseLayout"
          :options="layoutOptions"
          size="small"
          class="num"
          data-testid="settings.layout"
        />
      </div>
      <div class="row">
        <span class="lbl">{{ t('settings.autoSave') }}</span>
        <n-checkbox v-model:checked="form.autoSave" data-testid="settings.autoSave">
          <span class="hint">{{ t('settings.autoSaveHint') }}</span>
        </n-checkbox>
      </div>

        </div>

        <!-- 分区二：网络与安全 -->
        <div v-show="active === 'network'" class="pane" data-testid="settings.pane.network">
          <div class="row">
            <n-checkbox v-model:checked="form.insecureSsl" data-testid="settings.insecureSsl">
          {{ t('settings.insecureSsl') }}
        </n-checkbox>
      </div>
      <p class="hint muted">{{ t('settings.insecureSslHint') }}</p>
      <div class="row">
        <span class="lbl">{{ t('settings.timeout') }}</span>
        <n-input-number
          v-model:value="form.timeoutSec"
          size="small"
          :min="1"
          :max="600"
          class="num"
          data-testid="settings.timeout"
        />
      </div>
      <div class="row">
        <n-checkbox v-model:checked="form.followRedirects" data-testid="settings.followRedirects">
          {{ t('settings.followRedirects') }}
        </n-checkbox>
      </div>
      <div class="row">
        <span class="lbl">{{ t('settings.maxRedirects') }}</span>
        <n-input-number
          v-model:value="form.maxRedirects"
          size="small"
          :min="1"
          :max="50"
          :disabled="!form.followRedirects"
          class="num"
          data-testid="settings.maxRedirects"
        />
      </div>
      <div class="row">
        <span class="lbl">{{ t('settings.proxy') }}</span>
        <n-input
          v-model:value="form.proxyUrl"
          size="small"
          class="num"
          data-testid="settings.proxy"
          :placeholder="t('settings.proxyPlaceholder')"
        />
      </div>

        </div>

        <!-- 分区三：本地数据 -->
        <div v-show="active === 'local'" class="pane" data-testid="settings.pane.local">
          <div class="row">
            <n-checkbox v-model:checked="form.persistCookies" data-testid="settings.persistCookies">
          {{ t('settings.persistCookies') }}
        </n-checkbox>
        <span class="sp" />
        <n-popconfirm @positive-click="clearCookies">
          <template #trigger>
            <n-button size="tiny" tertiary>{{ t('settings.clearCookies') }}</n-button>
          </template>
          {{ t('common.confirm') }}？
        </n-popconfirm>
      </div>
      <div class="row">
        <span class="lbl">{{ t('settings.historyLimit') }}</span>
        <n-input-number v-model:value="form.historyLimit" size="small" :min="10" :max="5000" class="num" />
      </div>

      <!-- 多工作目录：启动恢复上限 + 当前占用的监听数（每个根一个文件监听，Windows 上会占住目录） -->
      <div class="row">
        <span class="lbl">{{ t('settings.restoreLimit') }}</span>
        <n-input-number
          v-model:value="form.restoreLimit"
          size="small"
          :min="1"
          :max="32"
          class="num"
          data-testid="settings.restoreLimit"
        />
      </div>
      <p class="hint muted">{{ t('settings.restoreLimitHint') }}</p>
      <div class="row">
        <span class="lbl">{{ t('settings.openRoots') }}</span>
        <span class="muted" data-testid="settings.openRoots">{{ coll.roots.length }}</span>
      </div>
      <p class="hint muted">{{ t('settings.openRootsHint') }}</p>

        </div>

        <!-- 分区四：MCP 服务。生命周期与其它设置不同 —— 它要起停一个真实的监听端口，
             保存后由后端立即应用（地址/端口/令牌/只读/开关）。 -->
        <div v-show="active === 'mcp'" class="pane" data-testid="settings.pane.mcp">
          <!-- 启用服务 -->
          <div class="block">
            <div class="row">
              <n-checkbox v-model:checked="form.mcp.enabled" data-testid="settings.mcpEnabled">
                {{ t('settings.mcpEnabled') }}
              </n-checkbox>
            </div>
          </div>

          <!-- 监听地址：地址 + 端口 两个输入框 -->
          <div class="block">
            <div class="row">
              <span class="lbl">{{ t('settings.mcpAddr') }}</span>
              <n-input
                v-model:value="form.mcp.addr"
                size="small"
                class="grow"
                :disabled="!form.mcp.enabled"
                data-testid="settings.mcpAddr"
                :placeholder="t('settings.mcpAddrPlaceholder')"
              />
              <span class="lbl port-lbl">{{ t('settings.mcpPort') }}</span>
              <n-input
                v-model:value="portText"
                size="small"
                class="port"
                :disabled="!form.mcp.enabled"
                :maxlength="5"
                data-testid="settings.mcpPort"
                :placeholder="t('settings.mcpPortPlaceholder')"
              />
            </div>
          </div>

          <!-- 只读模式：决定所有工作区是否可写（含下方各工作区的「可写」开关） -->
          <div class="block">
            <div class="row">
              <n-checkbox
                v-model:checked="form.mcp.readOnly"
                :disabled="!form.mcp.enabled"
                data-testid="settings.mcpReadOnly"
              >
                {{ t('settings.mcpReadOnly') }}
              </n-checkbox>
            </div>
          </div>

          <!-- 可访问的工作区：直接列出客户端当前加载的所有工作区，勾选授权；默认可写，可写与否受「只读模式」控制。 -->
          <div class="block">
            <div class="row">
              <span class="lbl">{{ t('settings.mcpAllow') }}</span>
              <span class="sp" />
              <span class="muted">{{ t('settings.mcpAllowCount', { n: form.mcp.allow.length }) }}</span>
            </div>
            <div v-if="coll.roots.length" class="dirs" data-testid="settings.mcpAllowList">
              <div v-for="r in coll.roots" :key="r.info.dir" class="dir" data-testid="settings.mcpAllowItem">
                <n-checkbox
                  :checked="isAllow(r.info.dir)"
                  :disabled="!form.mcp.enabled"
                  size="small"
                  data-testid="settings.mcpAllowCheck"
                  @update:checked="(v: boolean) => toggleAllow(r.info.dir, v)"
                >
                  <span class="mono p" :title="r.info.dir">{{ r.info.dir }}</span>
                </n-checkbox>
                <n-checkbox
                  :checked="writableOf(r.info.dir)"
                  :disabled="!form.mcp.enabled || form.mcp.readOnly || !isAllow(r.info.dir)"
                  size="small"
                  data-testid="settings.mcpAllowWritable"
                  @update:checked="(v: boolean) => setAllowWritable(r.info.dir, v)"
                >
                  {{ t('settings.mcpAllowWritable') }}
                </n-checkbox>
              </div>
            </div>
            <p v-else class="hint muted" data-testid="settings.mcpAllowEmpty">{{ t('settings.mcpAllowEmpty') }}</p>
          </div>

          <!-- 来源白名单 -->
          <div class="block">
            <div class="row">
              <span class="lbl">{{ t('settings.mcpOrigins') }}</span>
              <n-input
                v-model:value="originsText"
                size="small"
                class="grow"
                :disabled="!form.mcp.enabled"
                data-testid="settings.mcpOrigins"
                :placeholder="t('settings.mcpOriginsPlaceholder')"
              />
            </div>
          </div>

          <!-- 访问令牌 -->
          <div class="block">
            <div class="row">
              <span class="lbl">{{ t('settings.mcpToken') }}</span>
              <n-input
                :value="form.mcp.token"
                size="small"
                class="grow mono"
                readonly
                data-testid="settings.mcpToken"
                :placeholder="t('settings.mcpTokenPlaceholder')"
              />
              <n-button
                size="tiny"
                tertiary
                :disabled="!form.mcp.enabled"
                :loading="mcpBusy"
                data-testid="settings.mcpTokenRegen"
                @click="regenerateToken"
              >
                {{ t('settings.mcpTokenRegen') }}
              </n-button>
            </div>
          </div>

          <!-- 运行状态 + 配置示例 -->
          <div class="block">
            <p v-if="mcp?.running" class="ok" data-testid="settings.mcpRunning">
              {{ t('settings.mcpRunning', { url: mcp.url, projects: mcp.projects }) }}
            </p>
            <p v-else-if="mcp?.error" class="err" data-testid="settings.mcpError">{{ mcp.error }}</p>
            <div v-if="mcp?.running" class="row">
              <span class="lbl">{{ t('settings.mcpExample') }}</span>
              <span class="sp" />
              <n-button size="tiny" tertiary data-testid="settings.mcpCopyExample" @click="copyExample">
                {{ t('settings.mcpCopyExample') }}
              </n-button>
            </div>
            <pre v-if="mcp?.running" class="ex mono" data-testid="settings.mcpExample">{{ mcpExample() }}</pre>
          </div>
        </div>
      </div>
    </div>

    <template #footer>
      <div class="ft">
        <span class="sp" />
        <n-button size="small" @click="close">{{ t('common.cancel') }}</n-button>
        <n-button size="small" :loading="saving" @click="saveApply">{{ t('settings.saveApply') }}</n-button>
        <n-button size="small" type="primary" :loading="saving" @click="save">{{ t('common.save') }}</n-button>
      </div>
    </template>
  </n-modal>
</template>

<style scoped>
/* 白名单行：等宽路径 + 可写开关 + 删除 */
.dirs {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin: 2px 0 6px;
}

.dir {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 4px 8px;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  background: var(--app-surface-2);
}

.dir .p {
  flex: 1 1 auto;
  font-size: 11.5px;
  color: var(--app-text-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dir .x {
  flex: 0 0 auto;
  border: none;
  background: none;
  color: var(--app-muted);
  cursor: pointer;
  padding: 0 4px;
  border-radius: 4px;
}

.dir .x:hover {
  background: var(--app-danger-tint);
  color: var(--app-danger);
}

/* 两栏：左侧竖向分区菜单 + 右侧当前分区的内容 */
.cols {
  display: flex;
  align-items: stretch;
  gap: 14px;
  min-height: 420px;
}

.nav {
  flex: 0 0 148px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding-right: 10px;
  border-right: 1px solid var(--app-border);
}

.nav-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 8px 10px;
  border: none;
  border-radius: 7px;
  background: none;
  font-family: inherit;
  font-size: 13px;
  color: var(--app-text-2);
  cursor: pointer;
  text-align: left;
}

.nav-item:hover {
  background: var(--app-row-hover);
  color: var(--app-text);
}

.nav-item.on {
  background: var(--app-active);
  color: var(--app-accent);
  font-weight: 600;
}

.body {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-height: 60vh;
  overflow-y: auto;
  padding-right: 2px;
}

.pane {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.row {
  display: flex;
  align-items: center;
  gap: 10px;
}

/* 设置分组块：加边框与留白，让每个设置块界限清晰 */
.block {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px 12px;
  border: 1px solid var(--app-border);
  border-radius: 8px;
  background: var(--app-panel);
}

.port-lbl {
  width: auto;
  flex: 0 0 auto;
}

.lbl {
  font-size: 13px;
  width: 130px;
}

.num {
  width: 130px;
}

/* 端口数字框：地址行里固定窄宽，避免挤占地址宽度 */
.port {
  width: 92px;
}

/* MCP 配置示例：只读等宽块，内容可横向/纵向滚动、可整块选中复制 */
.ex {
  margin: 0;
  padding: 8px 10px;
  font-size: 11.5px;
  line-height: 1.5;
  color: var(--app-text-2);
  background: var(--app-surface-2);
  border: 1px solid var(--app-border);
  border-radius: 6px;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 180px;
  overflow: auto;
}

.grow {
  flex: 1 1 auto;
  min-width: 0;
}

.val {
  font-size: 12px;
  color: var(--app-text-2);
  word-break: break-all;
}

.muted {
  color: var(--app-muted);
}

.mono {
  font-family: var(--app-mono);
}

.hint {
  font-size: 11.5px;
  margin: 0 0 2px;
}

.err {
  color: var(--app-danger);
  font-size: 12.5px;
  margin: 0;
}

.ok {
  color: var(--app-accent);
  font-size: 12.5px;
  margin: 0;
}

.sp {
  flex: 1 1 auto;
}

.ft {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
