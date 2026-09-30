<script setup lang="ts">
// 响应面板（design-spec §2）：响应头行「响应 · 200 OK · 12 ms · 1.2 KB」+
// 页签（响应体 / 响应头 N / 响应字段 N）。响应体可切换「美化(JSON 树) / 原始」。
// 字段映射不再是响应体下方的一段，而是独立页签：点「更新响应字段」才解析，且重复更新只追加新字段。
// 保存响应（Bruno 的 Save Response）：把本次响应与请求快照写入集合 examples/，可从下拉回看与删除。
import { NAlert, NButton, NIcon, NInput, NModal, NPopconfirm, NSelect, NTag } from 'naive-ui'
import {
  BookmarkOutline,
  ContractOutline,
  CopyOutline,
  ExpandOutline,
  RefreshOutline,
  SaveOutline,
  TrashOutline,
} from '@vicons/ionicons5'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import JsonViewer from '@/components/JsonViewer.vue'
import ResponseFields from '@/components/ResponseFields.vue'
import { formatBytes, httpStatusText } from '@/lib/format'
import { api } from '@/lib/ipc'
import { message } from '@/lib/notice'
import { loadFields, mergeFields, saveFields, type FieldRow } from '@/lib/responseFields'
import type { Tab } from '@/stores/tabs'
import type { ResponseExample, SendResult } from '@/types'

const props = defineProps<{ tab: Tab }>()
const { t } = useI18n()

const view = ref<'pretty' | 'raw'>('pretty')
const seg = ref<'body' | 'headers' | 'fields'>('body')
const jv = ref<InstanceType<typeof JsonViewer> | null>(null)

const fields = ref<FieldRow[]>([])
const examples = ref<ResponseExample[]>([])
const viewingUid = ref('') // 非空 = 正在回看已保存的示例
const showSave = ref(false)
const saveName = ref('')
const copied = ref(false)

const example = computed(() => examples.value.find((e) => e.uid === viewingUid.value) ?? null)

/** 面板展示的响应：回看示例时用示例快照，否则用本次响应；两侧字段与布局保持一致。 */
const display = computed<SendResult | null>(() => {
  const ex = example.value
  if (!ex) return props.tab.response
  return {
    url: ex.request.url,
    status: ex.response.status,
    proto: ex.response.proto,
    timeMs: ex.response.timeMs,
    size: ex.response.size,
    contentType: ex.response.contentType,
    binary: ex.response.binary,
    headers: ex.response.headers,
    body: ex.response.body,
    // 无脚本产物：example 快照未存 script
    script: null,
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

const headerCount = computed(() => display.value?.headers.length ?? 0)

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
  ...examples.value.map((e) => ({ label: e.name, value: e.uid })),
])

// 切换请求：换用该请求的字段存档与示例列表
watch(
  () => props.tab.uid,
  (uid) => {
    fields.value = loadFields(uid)
    viewingUid.value = ''
    void reloadExamples()
  },
  { immediate: true },
)

// 重新发送后回到本次响应，不停留在旧示例上
watch(
  () => props.tab.response,
  () => {
    viewingUid.value = ''
  },
)

async function reloadExamples(): Promise<void> {
  try {
    examples.value = await api.listResponseExamples(props.tab.uid)
  } catch {
    examples.value = [] // 读取失败（集合未就绪等）不应影响响应展示
  }
}

/** 「更新响应字段」：解析当前响应体并增量合并（只追加新字段，不删除已有字段与含义）。 */
function updateFields(): void {
  const { rows, added } = mergeFields(fields.value, body.value)
  fields.value = rows
  saveFields(props.tab.uid, rows)
  seg.value = 'fields'
  if (added > 0) message.success(t('resp.fieldsAdded', { n: added }))
  else message.info(t('resp.fieldsNoChange'))
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

/** 保存响应：请求快照取当前草稿，响应取本次结果（回看示例时不重复保存）。 */
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

/** E21：把响应体保存到本地（二进制自动 base64 解码）。 */
async function saveBody(): Promise<void> {
  const d = display.value
  if (!d) return
  const ext = d.binary ? 'bin' : 'txt'
  const name = `response-${d.status || 'body'}.${ext}`
  try {
    const path = await api.saveResponseBody(name, d.binary, d.body)
    if (path) message.success(t('resp.savedFile', { path }))
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}
</script>

<template>
  <div class="resp">
    <n-alert v-if="tab.error" type="error" :bordered="false" class="err">{{ tab.error }}</n-alert>

    <template v-else-if="display">
      <!-- 第 1 排：状态信息 + 示例选择 -->
      <div class="head">
        <div class="info">
          <span class="ttl">{{ t('resp.title') }}</span>
          <n-tag
            :type="statusType"
            size="small"
            :bordered="false"
            class="badge"
            data-testid="resp.status"
            :title="display.proto"
          >
            {{ statusLabel }}
          </n-tag>
          <span class="meta mono">{{ meta }}</span>
        </div>
        <span class="sp" />
        <!-- 响应切换下拉：始终显示，方便随时回看历史示例 -->
        <n-select
          :value="viewingUid"
          :options="exampleOptions"
          size="tiny"
          class="expick"
          data-testid="resp.examples"
          :title="t('resp.examples')"
          @update:value="viewingUid = $event"
        />
        <n-popconfirm v-if="example" @positive-click="removeExample">
          <template #trigger>
            <button class="toggle danger" type="button" data-testid="resp.exampleDelete" :title="t('resp.deleteExample')">
              <n-icon :component="TrashOutline" :size="13" />
            </button>
          </template>
          {{ t('resp.deleteConfirm', { name: example.name }) }}
        </n-popconfirm>
      </div>

      <!-- 第 2 排：视图 + 字段 + 保存/复制（放不下自动换行到第 3 排） -->
      <div class="head ops-row">
        <div class="ops">
          <template v-if="seg === 'body' && isJson">
            <button class="toggle" :class="{ on: view === 'pretty' }" type="button" data-testid="resp.pretty" @click="view = 'pretty'">
              {{ t('resp.pretty') }}
            </button>
            <button class="toggle" :class="{ on: view === 'raw' }" type="button" data-testid="resp.raw" @click="view = 'raw'">
              {{ t('resp.raw') }}
            </button>
            <span class="div" />
            <button v-if="view === 'pretty'" class="toggle" type="button" data-testid="resp.expandAll" :title="t('json.expandAll')" @click="jv?.expandAll()">
              <n-icon :component="ExpandOutline" :size="14" />
            </button>
            <button v-if="view === 'pretty'" class="toggle" type="button" data-testid="resp.collapseAll" :title="t('json.collapseAll')" @click="jv?.collapseAll()">
              <n-icon :component="ContractOutline" :size="14" />
            </button>
          </template>
          <span v-if="seg === 'body' && isJson" class="div" />
          <button
            v-if="isJson && (seg === 'body' || seg === 'fields')"
            class="toggle"
            type="button"
            data-testid="resp.updateFields"
            :title="t('resp.updateFieldsHint')"
            @click="updateFields"
          >
            <n-icon :component="RefreshOutline" :size="13" />
            {{ t('resp.updateFields') }}
          </button>
          <span class="div" />
          <button v-if="!example" class="toggle" type="button" data-testid="resp.save" :title="t('resp.saveHint')" @click="openSave">
            <n-icon :component="SaveOutline" :size="13" />
            {{ t('resp.save') }}
          </button>
          <button class="toggle" type="button" data-testid="resp.copyBody" @click="copyBody">
            <n-icon :component="CopyOutline" :size="13" />
            {{ copied ? t('common.copied') : t('resp.copyBody') }}
          </button>
          <button v-if="!example" class="toggle" type="button" data-testid="resp.saveBody" :title="t('resp.saveBody')" @click="saveBody">
            <n-icon :component="SaveOutline" :size="13" />
            {{ t('resp.saveBody') }}
          </button>
        </div>
      </div>

      <div v-if="example" class="exnote" data-testid="resp.exnote">
        <n-icon :component="BookmarkOutline" :size="13" />
        <span>{{ t('resp.viewingExample', { name: example.name }) }}</span>
        <button class="link" type="button" data-testid="resp.backToLive" @click="viewingUid = ''">
          {{ t('resp.backToLive') }}
        </button>
      </div>

      <div class="seg">
        <button
          class="seg-tab"
          :class="{ on: seg === 'body' }"
          type="button"
          data-testid="resp.tab"
          data-seg="body"
          @click="seg = 'body'"
        >
          {{ t('resp.body') }}
        </button>
        <button
          class="seg-tab"
          :class="{ on: seg === 'headers' }"
          type="button"
          data-testid="resp.tab"
          data-seg="headers"
          @click="seg = 'headers'"
        >
          {{ t('resp.headers') }}<span v-if="headerCount" class="num">{{ headerCount }}</span>
        </button>
        <button
          class="seg-tab"
          :class="{ on: seg === 'fields' }"
          type="button"
          data-testid="resp.tab"
          data-seg="fields"
          @click="seg = 'fields'"
        >
          {{ t('resp.fieldsTab') }}<span v-if="fields.length" class="num">{{ fields.length }}</span>
        </button>
        <span class="url mono" data-testid="resp.url" :title="display.url">{{ display.url }}</span>
      </div>

      <div class="pane">
        <template v-if="seg === 'body'">
          <div v-if="display.script?.scriptError" class="binhint warn" data-testid="resp.scriptError">
            {{ display.script.scriptError }}
          </div>
          <div v-if="display.script?.asserts?.length" class="asserts" data-testid="resp.asserts">
            <div
              v-for="(a, i) in display.script.asserts"
              :key="i"
              class="arow"
              :class="a.passed ? 'ok' : 'fail'"
              data-testid="resp.assert"
            >
              <span class="astate">{{ a.passed ? '✓' : '✗' }}</span>
              <span class="aname">{{ a.name || a.expr }}</span>
              <span class="aexpr mono">{{ a.expr }}</span>
              <span v-if="a.error" class="aerr">{{ a.error }}</span>
            </div>
          </div>
          <div v-if="display.binary" class="binhint">{{ t('resp.binary') }}</div>
          <json-viewer v-if="isJson && view === 'pretty'" ref="jv" :text="display.body" />
          <pre v-else class="raw mono" data-testid="resp.rawBody">{{ rawText }}</pre>
        </template>
        <div v-else-if="seg === 'headers'" class="hlist">
          <div v-for="h in display.headers" :key="h.name" class="hrow mono">
            <span class="hn">{{ h.name }}</span>
            <span class="hv">{{ h.value }}</span>
          </div>
        </div>
        <response-fields
          v-else
          :rows="fields"
          @meaning="setMeaning"
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
        <n-select
          :value="viewingUid"
          :options="exampleOptions"
          size="small"
          style="width: 220px"
          @update:value="viewingUid = $event"
        />
      </div>
    </div>

    <n-modal v-model:show="showSave" preset="card" :title="t('resp.saveResponse')" style="width: 440px">
      <div class="save-form">
        <span class="flabel">{{ t('resp.exampleName') }}</span>
        <n-input
          v-model:value="saveName"
          size="small"
          :placeholder="t('resp.exampleNamePlaceholder')"
          @keyup.enter="submitSave"
        />
        <p class="fhint">{{ t('resp.exampleHint') }}</p>
      </div>
      <template #footer>
        <div class="modal-ft">
          <n-button size="small" @click="showSave = false">{{ t('common.cancel') }}</n-button>
          <n-button size="small" type="primary" :disabled="!saveName.trim()" @click="submitSave">
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
  gap: 0;
}

.err {
  margin-bottom: 8px;
}

.head {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
  row-gap: 4px;
  flex: 0 0 auto;
  min-width: 0;
  padding-bottom: 2px;
}

/* 第 2 排：操作按钮，允许换到第 3 排 */
.ops-row {
  overflow: visible;
  padding-bottom: 6px;
  border-bottom: 1px solid var(--app-border);
}

.info {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  flex: 0 0 auto;
  min-width: 0;
}

.ops {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 3px;
  flex: 1 1 auto;
  min-width: 0;
}

.ops .div {
  width: 1px;
  height: 13px;
  background: var(--app-border);
  margin: 0 2px;
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

.toggle.on {
  border-color: var(--app-accent);
  color: var(--app-accent);
  background: var(--app-accent-tint);
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
  margin-top: 0;
  padding-top: 6px;
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
  border-color: var(--app-danger, #d03050);
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
  color: var(--app-danger, #d03050);
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