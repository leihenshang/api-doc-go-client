<script setup lang="ts">
// 响应面板（response-panel design-spec §3）：状态栏 → 标签栏 → 操作工具条 → 内容区。
// 保存▾ 菜单（§4）：保存响应体为文件 / 保存全部字段为变量 / 保存选中值为变量（+ 保存响应示例，见有意差异），
// 底部「作用域」chips（集合/环境/全局）决定变量落点（映射见 lib/saveVars.ts）。
import { NAlert, NButton, NIcon, NInput, NModal, NPopconfirm, NSelect, NTag } from 'naive-ui'
import { BookmarkOutline, TrashOutline } from '@vicons/ionicons5'
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import JsonTree from '@/components/JsonTree.vue'
import ResponseFields from '@/components/ResponseFields.vue'
import ResponseToolbar from '@/components/ResponseToolbar.vue'
import { formatBytes, httpStatusText } from '@/lib/format'
import { api } from '@/lib/ipc'
import { message } from '@/lib/notice'
import { extractFields, loadFields, mergeFields, saveFields, type FieldRow } from '@/lib/responseFields'
import { deriveVarName, removeVar, saveVars, scopeVarNames, VAR_NAME_RE, type VarEntry, type VarScope } from '@/lib/saveVars'
import type { Tab } from '@/stores/tabs'
import type { ResponseExample, SendResult } from '@/types'

const props = defineProps<{ tab: Tab }>()
const { t } = useI18n()

const view = ref<'pretty' | 'raw'>('pretty')
const wrap = ref(true)
const seg = ref<'body' | 'headers' | 'fields'>('body')
const scope = ref<VarScope>('env')
const jv = ref<InstanceType<typeof JsonTree> | null>(null)
const inlineFieldsEl = ref<HTMLElement | null>(null)

const fields = ref<FieldRow[]>([])
const examples = ref<ResponseExample[]>([])
const viewingUid = ref('') // 非空 = 正在回看已保存的示例
const showSave = ref(false)
const saveName = ref('')
const copied = ref(false)
const showVar = ref(false) // 保存选中值为变量弹窗
const varName = ref('')
const varValue = ref('')

// 仅在选中了具体示例时才算"在看示例"：uid 为空/缺失一律视为本次响应，防止下拉与提示条状态错位
const example = computed(() =>
  viewingUid.value ? (examples.value.find((e) => e.uid === viewingUid.value) ?? null) : null,
)

/** 面板展示的响应：回看示例时用示例快照，否则用本次响应。
 *  headers/body 允许缺失（YAML omitempty、二进制等），必须归一成空值——
 *  否则渲染期读 `.length` 会抛错，整个面板（页签内容 + 下拉）都会卡死。 */
const display = computed<SendResult | null>(() => {
  const ex = example.value
  if (!ex) {
    const r = props.tab.response
    return r ? { ...r, headers: r.headers ?? [], body: r.body ?? '' } : null
  }
  return {
    url: ex.request.url ?? '',
    status: ex.response.status ?? 0,
    proto: ex.response.proto,
    timeMs: ex.response.timeMs ?? 0,
    size: ex.response.size ?? 0,
    contentType: ex.response.contentType,
    binary: ex.response.binary,
    headers: ex.response.headers ?? [],
    body: ex.response.body ?? '',
    script: null, // example 快照未存脚本产物
  }
})

const body = computed(() => display.value?.body ?? '')

const isJson = computed(() => {
  const text = body.value.trim()
  if (!text || display.value?.binary) return false
  return text.startsWith('{') || text.startsWith('[')
})

const rawText = computed(() => {
  if (view.value === 'raw' || !isJson.value) return body.value
  try {
    return JSON.stringify(JSON.parse(body.value), null, 2)
  } catch {
    return body.value
  }
})

const headerCount = computed(() => display.value?.headers?.length ?? 0)

const statusType = computed<'success' | 'warning' | 'error'>(() => {
  const s = display.value?.status ?? 0
  if (s >= 200 && s < 300) return 'success'
  if (s >= 400 && s < 500) return 'warning'
  return 'error'
})

const statusLabel = computed(() => {
  const s = display.value?.status ?? 0
  return `${s} ${httpStatusText(s)}`.trim()
})

const meta = computed(() => {
  const r = display.value
  if (!r) return ''
  return `${r.timeMs} ms${t('common.sep')}${formatBytes(r.size)}`
})

const exampleOptions = computed(() => [
  { label: t('resp.liveResponse'), value: '' },
  ...examples.value.filter((e) => !!e.uid).map((e) => ({ label: e.name, value: e.uid })),
])

const scopeLabel = computed(
  () =>
    ({ collection: t('resp.scopeCollection'), env: t('resp.scopeEnv'), global: t('resp.scopeGlobal') })[scope.value],
)

/** 当前作用域下已保存为变量的变量名（书签判定依据）。 */
const scopeNames = computed(() => scopeVarNames(scope.value, props.tab))
/** 已书签的字段路径集合（供字段表点亮）。 */
const bookmarks = computed(
  () => new Set(fields.value.filter((f) => scopeNames.value.has(deriveVarName(f.path))).map((f) => f.path)),
)

// 切换请求：换用该请求的字段存档与示例列表，页签回到「响应体」
watch(
  () => props.tab.uid,
  (uid) => {
    fields.value = loadFields(uid)
    viewingUid.value = ''
    seg.value = 'body'
    void reloadExamples()
  },
  { immediate: true },
)

// 重新发送后回到本次响应并复位页签，避免新响应体"消失"
watch(
  () => props.tab.response,
  () => {
    viewingUid.value = ''
    seg.value = 'body'
  },
)

async function reloadExamples(): Promise<void> {
  try {
    examples.value = await api.listResponseExamples(props.tab.uid)
  } catch {
    examples.value = [] // 读取失败（集合未就绪等）不应影响响应展示
  }
}

/** 「更新响应字段」：解析当前响应体并增量合并（只追加新字段与含义），停留在当前页签。
 *  任何失败只提示，不允许抛错中断渲染（否则面板内容与下拉会整体卡死）。 */
function updateFields(): void {
  try {
    const { rows, added } = mergeFields(fields.value, body.value)
    fields.value = rows
    saveFields(props.tab.uid, rows)
    if (added > 0) {
      message.success(t('resp.fieldsAdded', { n: added }))
    } else {
      message.info(t('resp.fieldsNoChange'))
    }
    if (seg.value === 'body') {
      void nextTick(() => inlineFieldsEl.value?.scrollIntoView({ block: 'nearest' }))
    }
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

function setMeaning(path: string, value: string): void {
  fields.value = fields.value.map((r) => (r.path === path ? { ...r, meaning: value } : r))
  saveFields(props.tab.uid, fields.value)
}

function removeField(path: string): void {
  fields.value = fields.value.filter((r) => r.path !== path)
  saveFields(props.tab.uid, fields.value)
}

function removeMany(paths: string[]): void {
  const set = new Set(paths)
  fields.value = fields.value.filter((r) => !set.has(r.path))
  saveFields(props.tab.uid, fields.value)
}

function clearFields(): void {
  fields.value = []
  saveFields(props.tab.uid, [])
}

function openSave(): void {
  saveName.value = t('resp.exampleDefault')
  showSave.value = true
}

/** 保存响应示例：请求快照取当前草稿，响应取本次结果（回看示例时不重复保存）。 */
async function submitSave(): Promise<void> {
  const name = saveName.value.trim()
  const res = props.tab.response
  if (!name || !res) return
  try {
    const saved = await api.saveResponseExample(props.tab.request, name, res)
    showSave.value = false
    await reloadExamples()
    viewingUid.value = saved.uid
    message.success(t('resp.exampleSaved', { name: saved.name }))
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

async function removeExample(): Promise<void> {
  const ex = example.value
  if (!ex) return
  try {
    await api.deleteResponseExample(props.tab.uid, ex.uid)
    viewingUid.value = ''
    await reloadExamples()
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

async function copyBody(): Promise<void> {
  const text = display.value?.body
  if (!text) return
  await navigator.clipboard.writeText(text)
  copied.value = true
  setTimeout(() => (copied.value = false), 1200)
}

/** 保存响应体为文件（JSON → .json，二进制 base64 解码 → .bin）。 */
async function saveBody(): Promise<void> {
  const d = display.value
  if (!d) return
  const ext = d.binary ? 'bin' : isJson.value ? 'json' : 'txt'
  const name = `response-${d.status || 'body'}.${ext}`
  try {
    const path = await api.saveResponseBody(name, d.binary, d.body)
    if (path) message.success(t('resp.savedFile', { path }))
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

/** 统一变量写入 + 提示；无环境等前置条件不满足时给引导。 */
async function applyVars(entries: VarEntry[]): Promise<void> {
  if (!entries.length) {
    message.info(t('resp.varNoFields'))
    return
  }
  try {
    await saveVars(scope.value, props.tab, entries)
    message.success(t('resp.varsSaved', { n: entries.length, scope: scopeLabel.value }))
  } catch (e) {
    if (e instanceof Error && e.message === 'no-env') message.warning(t('resp.varNoEnv'))
    else message.error(e instanceof Error ? e.message : String(e))
  }
}

/** 保存全部字段为变量：对当前响应体的叶子字段按路径派生变量名。 */
async function saveAllVars(): Promise<void> {
  const rows = extractFields(body.value).filter((r) => r.type !== 'array' && r.type !== 'object')
  await applyVars(rows.map((r) => ({ name: deriveVarName(r.path), value: r.value })))
}

/** 保存选中值为变量：取面板内当前文本选区，弹窗确认变量名。 */
function openVarModal(): void {
  const sel = window.getSelection()?.toString() ?? ''
  if (!sel.trim()) {
    message.info(t('resp.selectFirst'))
    return
  }
  varValue.value = sel
  const token = sel.trim()
  varName.value = VAR_NAME_RE.test(token) && token.length <= 60 ? token : ''
  showVar.value = true
}

async function submitVar(): Promise<void> {
  const name = varName.value.trim()
  if (!VAR_NAME_RE.test(name)) {
    message.warning(t('resp.varNameInvalid'))
    return
  }
  showVar.value = false
  await applyVars([{ name, value: varValue.value }])
}

/** 字段行「变量书签」：已书签 → 删变量；未书签 → 按当前作用域保存字段值。 */
async function toggleBookmark(path: string): Promise<void> {
  const name = deriveVarName(path)
  try {
    if (scopeNames.value.has(name)) {
      await removeVar(scope.value, props.tab, name)
      message.success(t('resp.varRemoved', { name }))
      return
    }
    const row = fields.value.find((r) => r.path === path)
    if (!row) return
    if (row.type === 'array' || row.type === 'object') {
      message.info(t('resp.varBookmarkLeaf'))
      return
    }
    await saveVars(scope.value, props.tab, [{ name, value: row.value }])
    message.success(t('resp.varSaved', { name, scope: scopeLabel.value }))
  } catch (e) {
    if (e instanceof Error && e.message === 'no-env') message.warning(t('resp.varNoEnv'))
    else message.error(e instanceof Error ? e.message : String(e))
  }
}
</script>

<template>
  <div class="resp">
    <n-alert v-if="tab.error" type="error" :bordered="false" class="err">{{ tab.error }}</n-alert>

    <template v-else-if="display">
      <!-- 状态栏：响应 · 200 OK · 12 ms · 1.2 KB -->
      <div class="status-bar">
        <span class="ttl">{{ t('resp.title') }}</span>
        <n-tag :type="statusType" size="small" :bordered="false" class="badge" data-testid="resp.status" :title="display.proto">
          {{ statusLabel }}
        </n-tag>
        <span class="meta mono">{{ meta }}</span>
      </div>

      <!-- 标签栏：响应体 / 响应头 N / 响应字段 N + 请求 URL -->
      <div class="seg">
        <button class="seg-tab" :class="{ on: seg === 'body' }" type="button" data-testid="resp.tab" data-seg="body" @click="seg = 'body'">
          {{ t('resp.body') }}
        </button>
        <button class="seg-tab" :class="{ on: seg === 'headers' }" type="button" data-testid="resp.tab" data-seg="headers" @click="seg = 'headers'">
          {{ t('resp.headers') }}<span v-if="headerCount" class="num">{{ headerCount }}</span>
        </button>
        <button class="seg-tab" :class="{ on: seg === 'fields' }" type="button" data-testid="resp.tab" data-seg="fields" @click="seg = 'fields'">
          {{ t('resp.fieldsTab') }}<span v-if="fields.length" class="num">{{ fields.length }}</span>
        </button>
        <span class="url mono" data-testid="resp.url" :title="display.url">{{ display.url }}</span>
      </div>

      <response-toolbar
        v-model:view="view"
        v-model:wrap="wrap"
        :is-json="isJson"
        :copied="copied"
        :scope="scope"
        :can-save-example="!example && !!tab.response"
        @expand-all="jv?.expandAll()"
        @collapse-all="jv?.collapseAll()"
        @update-fields="updateFields"
        @copy-body="copyBody"
        @update:scope="scope = $event"
        @save-file="saveBody"
        @save-all-vars="saveAllVars"
        @save-selected-var="openVarModal"
        @save-example="openSave"
      >
        <!-- 响应下拉 + 删除：排在「更新响应字段 / 复制响应体」这一组的最前面（从状态栏移入） -->
        <template #leading>
          <n-select
            :value="viewingUid"
            :options="exampleOptions"
            size="tiny"
            class="expick"
            data-testid="resp.examples"
            :title="t('resp.examples')"
            @update:value="viewingUid = String($event ?? '')"
          />
          <n-popconfirm v-if="example" @positive-click="removeExample">
            <template #trigger>
              <button class="toggle danger" type="button" data-testid="resp.exampleDelete" :title="t('resp.deleteExample')">
                <n-icon :component="TrashOutline" :size="13" />
                <span>{{ t('common.delete') }}</span>
              </button>
            </template>
            {{ t('resp.deleteConfirm', { name: example.name }) }}
          </n-popconfirm>
        </template>
      </response-toolbar>

      <div v-if="example" class="exnote" data-testid="resp.exnote">
        <n-icon :component="BookmarkOutline" :size="13" />
        <span>{{ t('resp.viewingExample', { name: example.name }) }}</span>
        <button class="link" type="button" data-testid="resp.backToLive" @click="viewingUid = ''">
          {{ t('resp.backToLive') }}
        </button>
      </div>

      <div class="pane">
        <template v-if="seg === 'body'">
          <div v-if="display.script?.scriptError" class="binhint warn" data-testid="resp.scriptError">
            {{ display.script.scriptError }}
          </div>
          <div v-if="display.script?.asserts?.length" class="asserts" data-testid="resp.asserts">
            <div v-for="(a, i) in display.script.asserts" :key="i" class="arow" :class="a.passed ? 'ok' : 'fail'" data-testid="resp.assert">
              <span class="astate">{{ a.passed ? '✓' : '✗' }}</span>
              <span class="aname">{{ a.name || a.expr }}</span>
              <span class="aexpr mono">{{ a.expr }}</span>
              <span v-if="a.error" class="aerr">{{ a.error }}</span>
            </div>
          </div>
          <div v-if="display.binary" class="binhint">{{ t('resp.binary') }}</div>
          <json-tree v-if="isJson && view === 'pretty'" ref="jv" :text="body" :wrap="wrap" />
          <pre v-else class="raw mono" :class="{ nowrap: !wrap }" data-testid="resp.rawBody">{{ rawText }}</pre>
          <!-- 字段表就地展示在响应体下方：点「更新响应字段」后响应体不消失、字段紧跟其后 -->
          <div v-if="fields.length" ref="inlineFieldsEl" class="inline-fields">
            <response-fields
              :rows="fields"
              :bookmarks="bookmarks"
              @meaning="setMeaning"
              @bookmark="toggleBookmark"
              @remove="removeField"
              @remove-many="removeMany"
              @clear="clearFields"
            />
          </div>
        </template>
        <div v-else-if="seg === 'headers'" class="hlist">
          <div v-for="h in display.headers" :key="h.name" class="hrow mono" data-testid="resp.headerRow">
            <span class="hn">{{ h.name }}</span>
            <span class="hv">{{ h.value }}</span>
          </div>
        </div>
        <response-fields
          v-else
          :rows="fields"
          :bookmarks="bookmarks"
          @meaning="setMeaning"
          @bookmark="toggleBookmark"
          @remove="removeField"
          @remove-many="removeMany"
          @clear="clearFields"
        />
      </div>
    </template>

    <div v-else class="empty">
      <svg class="plane" viewBox="0 0 24 24" width="56" height="56" fill="none" stroke="currentColor" stroke-width="1.4">
        <path d="M22 2 11 13" />
        <path d="M22 2 15 22l-4-9-9-4 20-7Z" />
      </svg>
      <p class="etitle">{{ t('resp.empty') }}</p>
      <ul class="shortcuts">
        <li><kbd>Ctrl</kbd> + <kbd>Enter</kbd><span>{{ t('resp.sendHint') }}</span></li>
        <li><kbd>Ctrl</kbd> + <kbd>N</kbd><span>{{ t('resp.newHint') }}</span></li>
        <li><kbd>Ctrl</kbd> + <kbd>E</kbd><span>{{ t('resp.envHint') }}</span></li>
        <li><kbd>Ctrl</kbd> + <kbd>W</kbd><span>{{ t('resp.closeHint') }}</span></li>
        <li><kbd>Ctrl</kbd> + <kbd>S</kbd><span>{{ t('resp.saveNowHint') }}</span></li>
      </ul>
      <div v-if="examples.length" class="empty-ex">
        <n-select :value="viewingUid" :options="exampleOptions" size="small" style="width: 220px" @update:value="viewingUid = $event" />
      </div>
    </div>

    <n-modal v-model:show="showSave" preset="card" :title="t('resp.saveResponse')" style="width: 440px">
      <div class="save-form">
        <span class="flabel">{{ t('resp.exampleName') }}</span>
        <n-input
          v-model:value="saveName"
          size="small"
          data-testid="resp.exampleNameInput"
          :placeholder="t('resp.exampleNamePlaceholder')"
          @keyup.enter="submitSave"
        />
        <p class="fhint">{{ t('resp.exampleHint') }}</p>
      </div>
      <template #footer>
        <div class="modal-ft">
          <n-button size="small" @click="showSave = false">{{ t('common.cancel') }}</n-button>
          <n-button size="small" type="primary" :disabled="!saveName.trim()" @click="submitSave">{{ t('common.save') }}</n-button>
        </div>
      </template>
    </n-modal>

    <!-- 保存选中值为变量：变量名 + 值预览 + 当前作用域 -->
    <n-modal v-model:show="showVar" preset="card" :title="t('resp.saveSelectedVar')" style="width: 440px">
      <div class="save-form">
        <span class="flabel">{{ t('resp.varName') }}</span>
        <n-input
          v-model:value="varName"
          size="small"
          data-testid="resp.varNameInput"
          :placeholder="t('resp.varNamePlaceholder')"
          @keyup.enter="submitVar"
        />
        <span class="flabel">{{ t('resp.value') }}</span>
        <pre class="var-preview mono" data-testid="resp.varValue">{{ varValue }}</pre>
        <p class="fhint">{{ t('resp.scope') }}：{{ scopeLabel }}</p>
      </div>
      <template #footer>
        <div class="modal-ft">
          <n-button size="small" @click="showVar = false">{{ t('common.cancel') }}</n-button>
          <n-button size="small" type="primary" :disabled="!VAR_NAME_RE.test(varName.trim())" @click="submitVar">
            {{ t('common.save') }}
          </n-button>
        </div>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.resp {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  padding: 8px 10px 8px;
}

.err {
  margin-bottom: 8px;
}

.status-bar {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 0 0 auto;
  min-width: 0;
  height: 44px;
  border-bottom: 1px solid var(--app-border);
}

.ttl {
  font-size: 12px;
  font-weight: 600;
  color: var(--app-text);
  flex: 0 0 auto;
}

.badge {
  font-family: var(--app-mono);
  font-weight: 700;
  flex: 0 0 auto;
}

.meta {
  font-size: 11px;
  color: var(--app-muted);
  white-space: nowrap;
  flex: 0 0 auto;
}

.sp {
  flex: 1 1 auto;
}

.toggle {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  border: 1px solid var(--app-border);
  background: var(--app-panel);
  border-radius: 5px;
  font-size: 10.5px;
  font-family: inherit;
  padding: 2px 7px;
  cursor: pointer;
  color: var(--app-text-2);
  white-space: nowrap;
  flex: 0 0 auto;
}

.toggle:hover {
  border-color: var(--app-accent);
  color: var(--app-accent);
}

.toggle.danger:hover {
  border-color: var(--app-danger);
  color: var(--app-danger);
}

.expick {
  width: 140px;
  flex: 0 0 auto;
}

.exnote {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 8px;
  padding: 4px 8px;
  border-radius: 6px;
  background: var(--app-accent-tint);
  color: var(--app-text-2);
  font-size: 12px;
  flex: 0 0 auto;
}

.exnote .link {
  margin-left: auto;
  border: none;
  background: none;
  padding: 0 2px;
  font-family: inherit;
  font-size: 12px;
  color: var(--app-accent);
  cursor: pointer;
}

.seg {
  display: flex;
  align-items: center;
  gap: 2px;
  height: 40px;
  border-bottom: 1px solid var(--app-border);
  flex: 0 0 auto;
  overflow-x: auto;
  scrollbar-width: none;
}

.seg::-webkit-scrollbar {
  height: 0;
}

.seg-tab {
  position: relative;
  border: none;
  background: none;
  padding: 7px 10px;
  font-size: 12px;
  font-family: inherit;
  color: var(--app-muted);
  cursor: pointer;
  border-bottom: 2px solid transparent;
  margin-bottom: -1px;
  white-space: nowrap;
  flex: 0 0 auto;
}

.seg-tab:hover {
  color: var(--app-text);
  background: var(--app-row-hover);
}

.seg-tab.on {
  color: var(--app-accent);
  font-weight: 600;
  border-bottom-color: var(--app-accent);
}

.num {
  margin-left: 4px;
  font-size: 10.5px;
  color: var(--app-placeholder);
}

.url {
  flex: 1 1 auto;
  margin-left: 8px;
  font-size: 11px;
  color: var(--app-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-align: right;
  min-width: 0;
}

.pane {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
  margin-top: 6px;
}

.binhint {
  font-size: 12px;
  color: var(--app-warn);
  margin-bottom: 6px;
}

.binhint.warn {
  color: var(--app-danger);
}

.asserts {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-bottom: 8px;
}

.asserts .arow {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  padding: 4px 8px;
  border-radius: 6px;
  border: 1px solid var(--app-border);
}

.asserts .arow.ok {
  border-color: var(--app-accent);
}

.asserts .arow.fail {
  border-color: var(--app-danger);
}

.asserts .astate {
  font-weight: 700;
}

.asserts .aname {
  font-weight: 500;
}

.asserts .aexpr {
  color: var(--app-muted);
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.asserts .aerr {
  color: var(--app-danger);
  max-width: 200px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.raw {
  margin: 0;
  padding: 10px;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  background: var(--app-surface-2);
  font-size: 12px;
  line-height: 1.55;
  white-space: pre-wrap;
  word-break: break-all;
}

.raw.nowrap {
  white-space: pre;
  word-break: normal;
}

.inline-fields {
  margin-top: 10px;
}

.hlist {
  display: flex;
  flex-direction: column;
}

.hrow {
  display: flex;
  gap: 10px;
  font-size: 12px;
  padding: 3px 6px;
  border-bottom: 1px dashed var(--app-dash);
}

.hrow:hover {
  background: var(--app-hover-soft);
}

.hn {
  color: var(--app-code-key);
  flex: 0 0 200px;
  overflow: hidden;
  text-overflow: ellipsis;
}

.hv {
  word-break: break-all;
}

.empty {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  color: var(--app-muted);
}

.plane {
  color: var(--app-border-strong);
  margin-bottom: 6px;
}

.etitle {
  margin: 0;
  font-size: 13px;
  color: var(--app-placeholder);
}

.shortcuts {
  list-style: none;
  margin: 10px 0 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.shortcuts li {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--app-placeholder);
}

.shortcuts span {
  margin-left: 6px;
}

.empty-ex {
  margin-top: 12px;
}

.save-form {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.flabel {
  font-size: 12.5px;
  color: var(--app-text-2);
}

.fhint {
  margin: 0;
  font-size: 11.5px;
  color: var(--app-muted);
}

.var-preview {
  margin: 0;
  max-height: 120px;
  overflow: auto;
  padding: 6px 8px;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  background: var(--app-surface-2);
  font-size: 11.5px;
  white-space: pre-wrap;
  word-break: break-all;
}

.modal-ft {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

kbd {
  border: 1px solid var(--app-border-strong);
  border-bottom-width: 2px;
  border-radius: 4px;
  padding: 1px 6px;
  font-family: var(--app-mono);
  font-size: 11px;
  background: var(--app-panel);
  color: var(--app-muted);
}
</style>
