<script setup lang="ts">
// 响应字段（Resp 分段，从 RequestEditor 拆出）：字段表格 / 响应数据 / 响应头快照三个子页签，
// 「生成数据表格」/「从本次响应生成」的派生与新增/覆盖确认都在这里。
// 真相源是请求模型 tab.request.respDoc；所有编辑只改内存并标脏，落盘由 Ctrl+S 完成。
import JsonBodyEditor from '@/components/JsonBodyEditor.vue'
import ResponseFields from '@/components/ResponseFields.vue'
import { useResponseFields } from '@/composables/useResponseFields'
import { isGrpc } from '@/lib/grpc'
import { message } from '@/lib/notice'
import {
  deriveFields,
  emptyFieldRow as emptyRespFieldRow,
  mergeAppend,
  mergeOverwrite,
  stableRespHeaders,
  type FieldRow,
  type RespDocLive,
} from '@/lib/responseFields'
import { respSubViewByTab } from '@/lib/tabViewState'
import { detectTextKind, prettyText } from '@/lib/textFormat'
import { useCollectionStore } from '@/stores/collection'
import type { Tab } from '@/stores/tabs'
import { useTabsStore } from '@/stores/tabs'
import type { ResponseDoc } from '@/types'
import { NButton, NModal, NSelect } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ tab: Tab }>()
/** 当前子页签由父级持有（页内查找按它判断是否有可查找 textarea）。 */
type RespView = 'fields' | 'body' | 'headers'
const view = defineModel<RespView>('view', { default: 'body' })

const tabs = useTabsStore()
const coll = useCollectionStore()
const { t } = useI18n()
const readonly = computed(() => coll.isReadOnly)
const isGrpcReq = computed(() => isGrpc(props.tab.request))

function touch(): void {
  if (readonly.value) return
  tabs.touch(props.tab.key)
}

/** 只标脏、不触发自动保存（格式化后由用户显式保存）。 */
function touchManual(): void {
  if (readonly.value) return
  tabs.markDirty(props.tab.key)
}

const { fields, sample, initFields, onSampleInput, applyRows, patchRow, addRow, removeRow } = useResponseFields({
  getTab: () => props.tab,
  isGrpc: () => isGrpcReq.value,
  onDirty: () => tabs.touch(props.tab.key),
})

// 切请求时重新取 respDoc，并恢复该 tab 上次停留的子页签
watch(
  () => props.tab.key,
  () => {
    const saved = respSubViewByTab.get(props.tab.key) as RespView | undefined
    view.value = saved && ['body', 'headers', 'fields'].includes(saved) ? saved : 'body'
    initFields()
  },
)
watch(view, (v) => respSubViewByTab.set(props.tab.key, v))

/** 「本次响应」（最近一次真实发送的响应体）：一般只读；「从本次响应生成」选「覆盖」时
 *  会把这份响应体一并写入 respDoc.sample（与字段表同一次提交）。 */
const liveRespBody = computed(() => (props.tab.response?.body ?? ''))
/** 「从本次响应生成」的按钮提示：没有本次响应时说明为什么不可用（禁用态靠外层 span 的 title 显示）。 */
const genLiveTitle = computed(() => (liveRespBody.value.trim() ? t('resp.genLiveHint') : t('resp.genNoLive')))

const respGenDialog = ref(false)
const respGenDerived = ref<FieldRow[]>([])
/** 本次生成的来源：sample=手动填的响应数据；live=本次真实响应（决定是否顺带落 status/headers 快照）。 */
const respGenSource = ref<'sample' | 'live'>('sample')

/** 生成来源是「本次响应」时带上的快照元信息（status/headers/contentType），手动来源返回 undefined。
 *  响应头先过 stableRespHeaders：Date / Keep-Alive / Content-Length / Set-Cookie 等易变或敏感头
 *  不写进集合文件（否则每次 live 生成都会让 YAML 抖动）；自定义头（X-*）保留。 */
function liveRespMeta(): RespDocLive | undefined {
  const r = props.tab.response
  if (!r) return undefined
  const headers = stableRespHeaders(r.headers ?? [])
  return {
    status: r.status,
    headers: headers.length ? headers : undefined,
    contentType: r.contentType || undefined,
  }
}

/** 解析生成来源：返回 null 表示「空 / 非法 JSON」，返回空数组表示「合法 JSON 但没有字段」。 */
function parseGenSource(text: string): FieldRow[] | null {
  if (text.trim() === '') return null
  try {
    JSON.parse(text)
  } catch {
    return null
  }
  return deriveFields(text)
}

/**
 * 「生成数据表格」/「从本次响应生成」：派生 5 列 → 空表直接生成，非空表弹「新增 / 覆盖」。
 * 生成默认只动表格（respDoc.params）；唯一例外是「从本次响应生成」+「覆盖」——
 * 此时把本次响应体一并写入「响应数据」（respDoc.sample），避免表格更新后样例还是旧数据。
 * 「从本次响应生成」额外把本次 status/headers/contentType 记进 respDoc（响应头子页签与状态徽标的数据源）。
 */
function startRespGenerate(source: 'sample' | 'live'): void {
  if (readonly.value || isGrpcReq.value) return
  const text = source === 'sample' ? sample.value : liveRespBody.value
  const derived = parseGenSource(text)
  if (derived === null) {
    message.warning(source === 'sample' ? t('resp.genInvalidSample') : t('resp.genInvalidLive'))
    return
  }
  if (!derived.length) {
    message.warning(t('resp.genEmpty'))
    return
  }
  respGenSource.value = source
  if (!fields.value.length) {
    applyRespRows(derived, derived.length, source)
    return
  }
  respGenDerived.value = derived
  respGenDialog.value = true
}

/** 生成结果落到表格：写回内存 respDoc.params（含 required）并标脏，切到「字段表格」子页签。
 *  nextSample 非 undefined 时同时覆盖「响应数据」（live + 覆盖：响应体一并更新）。 */
function applyRespRows(rows: FieldRow[], n: number, source: 'sample' | 'live', nextSample?: string): void {
  applyRows(rows, source === 'live' ? liveRespMeta() : undefined, nextSample)
  view.value = 'fields'
  message.success(t('resp.genDone', { n }))
}

/** 弹窗确认：新增=只追加缺失路径；覆盖=按本次派生重建、同路径只继承含义。
 *  「从本次响应生成」选覆盖时，响应体也替换为本次响应（新增不动响应数据）。 */
function onConfirmRespGenerate(mode: 'append' | 'overwrite'): void {
  const cur = fields.value
  const derived = respGenDerived.value
  const source = respGenSource.value
  const merged = mode === 'append' ? mergeAppend(cur, derived) : mergeOverwrite(cur, derived)
  const nextSample = mode === 'overwrite' && source === 'live' ? liveRespBody.value : undefined
  respGenDialog.value = false
  respGenDerived.value = []
  applyRespRows(merged, mode === 'append' ? merged.length - cur.length : merged.length, source, nextSample)
}

function onCancelRespGenerate(): void {
  respGenDialog.value = false
  respGenDerived.value = []
}

function onRespFieldUpdate(index: number, patch: Partial<FieldRow>): void {
  patchRow(index, patch)
}
function onRespFieldAdd(): void {
  addRow(emptyRespFieldRow())
}
function onRespFieldRemove(index: number): void {
  removeRow(index)
}

/** 响应数据格式化失败提示（常显按钮，点击时才判断内容）。 */
const respFormatError = ref(false)

const respSample = computed({
  get: () => sample.value,
  set: (v: string) => {
    respFormatError.value = false
    onSampleInput(v)
  },
})
// 外部同步 / 切换请求改写 sample 时，清掉上一轮的格式化错误提示
watch(sample, () => {
  respFormatError.value = false
})

/** 一键复制响应数据；按钮文案在「复制 / 已复制」间短暂切换。 */
const copied = ref(false)
let copiedTimer: ReturnType<typeof setTimeout> | null = null
async function copyData(): Promise<void> {
  try {
    if (!navigator.clipboard?.writeText) throw new Error('clipboard unavailable')
    await navigator.clipboard.writeText(sample.value)
    copied.value = true
    if (copiedTimer) clearTimeout(copiedTimer)
    copiedTimer = setTimeout(() => {
      copied.value = false
    }, 1200)
  } catch {
    message.error(t('resp.copyFailed'))
  }
}

/** 响应数据内容类型（用于选择着色语言；格式化按钮常显，点击时才校验）。 */
const respDataKind = computed(() => detectTextKind(sample.value))
function formatRespData(): void {
  const out = prettyText(sample.value)
  respFormatError.value = !out
  if (!out) return
  onSampleInput(out.text)
  // 格式化是「需要用户确认」的场景：只标脏，不排自动保存
  touchManual()
  message.info(t('editor.formatNeedsSave'))
}

// ---- 响应内容类型（对齐 Go respDoc.contentType 与同步键 retContentType）----
const respContentType = computed(() => props.tab.request.respDoc?.contentType || 'json')
const contentTypeOptions = computed(() => [
  { label: t('editor.ctJson'), value: 'json' },
  { label: t('editor.ctText'), value: 'text' },
  { label: t('editor.ctXml'), value: 'xml' },
  { label: t('editor.ctHtml'), value: 'html' },
  { label: t('editor.ctBinary'), value: 'binary' },
])
const contentTypeLabel = computed(
  () => contentTypeOptions.value.find((o) => o.value === respContentType.value)?.label ?? respContentType.value,
)

/** 与字段表共用 respDoc 真相源：只有类型真的变了才标脏（落盘与同步随 contentType 往返）。 */
function setContentType(v: string): void {
  const req = props.tab.request
  const doc: ResponseDoc = req.respDoc ?? { type: '', params: [], sample: '' }
  if (doc.contentType === v) return
  req.respDoc = { ...doc, contentType: v }
  touch()
}

const respSnap = computed(() => props.tab.request.respDoc ?? null)
</script>

<template>
  <div class="resp-pane">
    <!-- 一行工具条：左侧子页签，右侧响应类型（避免三行控件堆叠） -->
    <div class="resp-toolbar">
      <div class="resp-sub" role="tablist">
        <button class="sub-tab" :class="{ on: view === 'body' }" type="button" role="tab"
          :aria-selected="view === 'body'" data-testid="resp.segTab" data-view="body"
          @click="view = 'body'">
          {{ t('editor.respTabBody') }}<span v-if="respSnap?.status" class="sub-badge">{{ respSnap.status }}</span>
        </button>
        <button class="sub-tab" :class="{ on: view === 'headers' }" type="button" role="tab"
          :aria-selected="view === 'headers'" data-testid="resp.segTab" data-view="headers"
          @click="view = 'headers'">
          {{ t('editor.respTabHeaders') }}<span v-if="respSnap?.headers?.length" class="sub-badge">{{
            respSnap.headers.length }}</span>
        </button>
        <!-- 字段表格放到最后（先手工生成表格，再看字段表） -->
        <button class="sub-tab" :class="{ on: view === 'fields' }" type="button" role="tab"
          :aria-selected="view === 'fields'" data-testid="resp.segTab" data-view="fields"
          @click="view = 'fields'">
          {{ t('editor.respTabFields') }}<span v-if="fields.length" class="sub-badge">{{ fields.length }}</span>
        </button>
      </div>
      <!-- 响应类型：落 respDoc.contentType（同步键 retContentType），非 JSON 时样例不格式化 -->
      <div class="resp-ct">
        <span class="lbl">{{ t('editor.respContentType') }}</span>
        <n-select :value="respContentType" :options="contentTypeOptions" size="small" class="ctype"
          data-testid="resp.contentType" :title="t('editor.respContentTypeHint')"
          @update:value="(v: string) => setContentType(v)" />
        <span class="ct-badge" data-testid="resp.contentTypeBadge">{{ contentTypeLabel }}</span>
      </div>
      <!-- 字段表格的两个生成来源（gRPC 不支持 respDoc，故整段不渲染）：
           ① 手动填的「响应数据」文本；② 本次真实响应（有响应才可点）。
           生成默认只动表格；「从本次响应生成」选「覆盖」时一并更新「响应数据」。
           已有表格时弹「新增 / 覆盖」。 -->
      <div v-if="!isGrpcReq" class="resp-gen">
        <button class="fmt" type="button" data-testid="resp.generate" :disabled="readonly"
          :title="t('resp.genBtnHint')" @click="startRespGenerate('sample')">
          {{ t('resp.genBtn') }}
        </button>
        <span class="fmt-tip" :title="genLiveTitle">
          <button class="fmt" type="button" data-testid="resp.generateLive"
            :disabled="readonly || !liveRespBody.trim()" :title="genLiveTitle"
            @click="startRespGenerate('live')">
            {{ t('resp.genLiveBtn') }}
          </button>
        </span>
      </div>
    </div>
    <!-- 字段表格：5 列全可编辑（字段名 / 类型 / 必填 / 示例值 / 说明）+ 增行 / 删行 -->
    <div v-if="view === 'fields' && !isGrpcReq" class="resp-fields">
      <response-fields :rows="fields" :readonly="readonly" @update="onRespFieldUpdate" @add="onRespFieldAdd"
        @remove="onRespFieldRemove" />
    </div>
    <!-- 响应数据：可编辑的响应数据文本（字段表格只在点「生成数据表格」时派生，不看这里的变化） -->
    <div v-else-if="view === 'body'" class="resp-data">
      <div class="resp-data-hd">
        <p class="resp-data-hint">{{ t('editor.respDataHint') }}</p>
        <span v-if="respFormatError" class="fmtbad" data-testid="resp.formatBroken">
          {{ t('editor.formatBroken') }}
        </span>
        <div class="resp-data-actions">
          <button v-if="sample.trim()" class="fmt" type="button" data-testid="resp.copyData" @click="copyData">
            {{ copied ? t('common.copied') : t('common.copy') }}
          </button>
          <button class="fmt" type="button" data-testid="resp.formatData" :disabled="readonly" @click="formatRespData">
            {{ t('editor.formatJson') }}
          </button>
        </div>
      </div>
      <div class="resp-data-editor" data-testid="resp.dataInput">
        <json-body-editor v-model="respSample" :rows="16" :readonly="readonly" :lang="respDataKind ?? 'text'"
          :placeholder="t('editor.respDataPh')" testid="resp.dataRaw" />
      </div>
    </div>
    <div v-else class="resp-heads" data-testid="resp.segHeaders">
      <div v-if="!respSnap?.headers?.length" class="resp-heads-empty">{{ t('editor.respSnapEmpty') }}</div>
      <div v-for="(h, i) in respSnap?.headers ?? []" :key="`${h.name}-${i}`" class="hrow"
        data-testid="resp.segHeaderRow">
        <span class="hn mono">{{ h.name }}</span>
        <span class="hv">{{ h.value }}</span>
      </div>
    </div>

    <!-- 「生成数据表格」（响应字段）确认弹窗：已有表格时二选一（新增 / 覆盖），空表不弹 -->
    <n-modal v-model:show="respGenDialog" preset="card" class="gen-modal"
      style="width: 460px; max-width: calc(100vw - 32px)" :title="t('resp.genTitle')">
      <div data-testid="resp.genDialog">
        <p class="gen-hint">{{ t('resp.genHint') }}</p>
        <p v-if="respGenSource === 'live'" class="gen-hint" data-testid="resp.genHintLive">
          {{ t('resp.genHintLive') }}
        </p>
        <div class="gen-actions">
          <n-button size="small" type="primary" data-testid="resp.genAppend" @click="onConfirmRespGenerate('append')">
            {{ t('body.genAppend') }}
          </n-button>
          <n-button size="small" data-testid="resp.genOverwrite" @click="onConfirmRespGenerate('overwrite')">
            {{ t('body.genOverwrite') }}
          </n-button>
          <n-button size="small" quaternary data-testid="resp.genCancel" @click="onCancelRespGenerate">
            {{ t('common.cancel') }}
          </n-button>
        </div>
      </div>
    </n-modal>
  </div>
</template>

<style scoped>
/* Resp 分段：一行工具条 + 内容区，避免响应类型/子页签/提示三行控件堆叠 */
.resp-pane {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 10px 12px 12px;
  min-height: 0;
}

.resp-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 10px;
}

/* 字段表格的生成按钮组（响应数据 / 本次响应两个来源） */
.resp-gen {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 0 0 auto;
}

.resp-gen .fmt:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* 外层 span 负责显示提示：原生 button 禁用后不触发鼠标事件，自身 title 不会弹出 */
.resp-gen .fmt-tip {
  display: inline-flex;
}

/* 字段表格：5 列可编辑，仅表格本身 */
.resp-fields {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-height: 0;
}

/* 「响应数据」可编辑区：提示 + 文本域 */
.resp-data {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-height: 0;
}

.resp-data-hint {
  margin: 0;
  font-size: 11.5px;
  color: var(--app-muted);
}

.resp-data-hd {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  min-height: 28px;
}

.resp-data-hd .resp-data-hint {
  flex: 1 1 auto;
}

.resp-data-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-left: auto;
}

.resp-data-editor {
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.resp-data-editor :deep(.jb) {
  width: 100%;
}

/* 响应类型行：下拉 + 类型徽标（与子页签同排风格） */
.resp-ct {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-left: auto;
  padding: 0;
}

.resp-ct .lbl {
  font-size: 12px;
  color: var(--app-muted);
}

.resp-ct .ctype {
  width: 120px;
}

.ct-badge {
  font-size: 10px;
  line-height: 14px;
  padding: 0 6px;
  border-radius: 9px;
  background: var(--app-accent-tint);
  color: var(--app-accent-dark);
}

.resp-sub {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  padding: 2px;
  border-radius: 8px;
  background: var(--app-bg);
}

.sub-tab {
  border: none;
  background: none;
  border-radius: 6px;
  padding: 5px 10px;
  font-family: inherit;
  font-size: 12px;
  color: var(--app-muted);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 5px;
}

.sub-tab:hover {
  color: var(--app-text);
}

.sub-tab.on {
  background: var(--app-panel);
  color: var(--app-accent-dark);
  font-weight: 600;
  box-shadow: 0 1px 2px rgb(0 0 0 / 6%);
}

.sub-badge {
  font-family: var(--app-mono);
  font-size: 10px;
  line-height: 14px;
  padding: 0 4px;
  border-radius: 4px;
  background: var(--app-chip);
  color: var(--app-text-2);
}

.resp-heads {
  margin: 8px 12px 12px;
  border: 1px solid var(--app-border);
  border-radius: 8px;
  overflow: auto;
  max-height: calc(100% - 52px);
}

.resp-heads .hrow {
  display: flex;
  gap: 10px;
  padding: 5px 10px;
  font-size: 12px;
  border-bottom: 1px solid var(--app-border);
}

.resp-heads .hrow:last-child {
  border-bottom: none;
}

.resp-heads .hn {
  flex: 0 0 auto;
  min-width: 140px;
  color: var(--app-text-2);
}

.resp-heads .hv {
  flex: 1 1 auto;
  color: var(--app-text);
  word-break: break-all;
}

.resp-heads-empty {
  padding: 14px 10px;
  font-size: 12px;
  color: var(--app-placeholder);
}

.fmt {
  border: 1px solid var(--app-border);
  background: var(--app-panel);
  border-radius: 6px;
  font-size: 11.5px;
  font-family: inherit;
  padding: 3px 9px;
  color: var(--app-text-2);
  cursor: pointer;
}

.fmt:hover {
  border-color: var(--app-accent);
  color: var(--app-accent);
}

.fmt:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.fmtbad {
  font-size: 11.5px;
  color: var(--app-danger);
}

/* 生成数据表格弹窗 */
.gen-hint {
  margin: 0 0 14px;
  font-size: 12.5px;
  line-height: 1.7;
  color: var(--app-muted);
}

.gen-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
