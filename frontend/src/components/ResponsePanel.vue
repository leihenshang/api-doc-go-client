<script setup lang="ts">
// 响应面板（design-spec §2）：响应头行「响应 · 200 OK · 12 ms · 1.2 KB」+
// 页签（响应体 / 响应头 N / 响应字段 N）。响应体可切换「美化(JSON 树) / 原始」。
// 字段映射不再是响应体下方的一段，而是独立页签：点「更新响应字段」才解析，且重复更新只追加新字段。
// 保存响应（Bruno 的 Save Response）：把本次响应与请求快照写入集合 examples/，可从下拉回看与删除。
import { NButton, NIcon, NInput, NModal, NPopconfirm, NSelect, NTag } from 'naive-ui'
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

/** 面板展示的响应：回看示例时用示例快照，否则用本次响应。 */
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
</script>

<template>
  <div class="resp">
    <n-alert v-if="tab.error" type="error" :bordered="false" class="err">{{ tab.error }}</n-alert>

    <template v-else-if="display">
      <div class="head">
        <span class="ttl">{{ t('resp.title') }}</span>
        <n-tag :type="statusType" size="small" :bordered="false" class="badge" :title="display.proto">
          {{ statusLabel }}
        </n-tag>
        <span class="meta mono">{{ meta }}</span>
        <span class="sp" />
        <button
          v-if="isJson && (seg === 'body' || seg === 'fields')"
          class="toggle"
          type="button"
          :title="t('resp.updateFieldsHint')"
          @click="updateFields"
        >
          <n-icon :component="RefreshOutline" :size="13" />
          {{ t('resp.updateFields') }}
        </button>
        <template v-if="seg === 'body'">
          <button v-if="isJson" class="toggle" :class="{ on: view === 'pretty' }" type="button" @click="view = 'pretty'">
            {{ t('resp.pretty') }}
          </button>
          <button v-if="isJson" class="toggle" :class="{ on: view === 'raw' }" type="button" @click="view = 'raw'">
            {{ t('resp.raw') }}
          </button>
          <button
            v-if="isJson && view === 'pretty'"
            class="toggle"
            type="button"
            :title="t('json.expandAll')"
            @click="jv?.expandAll()"
          >
            <n-icon :component="ExpandOutline" :size="14" />
          </button>
          <button
            v-if="isJson && view === 'pretty'"
            class="toggle"
            type="button"
            :title="t('json.collapseAll')"
            @click="jv?.collapseAll()"
          >
            <n-icon :component="ContractOutline" :size="14" />
          </button>
        </template>
        <button v-if="!example" class="toggle" type="button" :title="t('resp.saveHint')" @click="openSave">
          <n-icon :component="SaveOutline" :size="13" />
          {{ t('resp.save') }}
        </button>
        <n-select
          v-if="examples.length"
          :value="viewingUid"
          :options="exampleOptions"
          size="tiny"
          class="expick"
          :title="t('resp.examples')"
          @update:value="viewingUid = $event"
        />
        <n-popconfirm v-if="example" @positive-click="removeExample">
          <template #trigger>
            <button class="toggle danger" type="button" :title="t('resp.deleteExample')">
              <n-icon :component="TrashOutline" :size="13" />
            </button>
          </template>
          {{ t('resp.deleteConfirm', { name: example.name }) }}
        </n-popconfirm>
        <button class="toggle" type="button" @click="copyBody">
          <n-icon :component="CopyOutline" :size="13" />
          {{ copied ? t('common.copied') : t('resp.copyBody') }}
        </button>
      </div>

      <div v-if="example" class="exnote">
        <n-icon :component="BookmarkOutline" :size="13" />
        <span>{{ t('resp.viewingExample', { name: example.name }) }}</span>
        <button class="link" type="button" @click="viewingUid = ''">{{ t('resp.backToLive') }}</button>
      </div>

      <div class="seg">
        <button class="seg-tab" :class="{ on: seg === 'body' }" type="button" @click="seg = 'body'">
          {{ t('resp.body') }}
        </button>
        <button class="seg-tab" :class="{ on: seg === 'headers' }" type="button" @click="seg = 'headers'">
          {{ t('resp.headers') }}<span v-if="headerCount" class="num">{{ headerCount }}</span>
        </button>
        <button class="seg-tab" :class="{ on: seg === 'fields' }" type="button" @click="seg = 'fields'">
          {{ t('resp.fieldsTab') }}<span v-if="fields.length" class="num">{{ fields.length }}</span>
        </button>
        <span class="url mono" :title="display.url">{{ display.url }}</span>
      </div>

      <div class="pane">
        <template v-if="seg === 'body'">
          <div v-if="display.binary" class="binhint">{{ t('resp.binary') }}</div>
          <json-viewer v-if="isJson && view === 'pretty'" ref="jv" :text="display.body" />
          <pre v-else class="raw mono">{{ rawText }}</pre>
        </template>
        <div v-else-if="seg === 'headers'" class="hlist">
          <div v-for="h in display.headers" :key="h.name" class="hrow mono">
            <span class="hn">{{ h.name }}</span>
            <span class="hv">{{ h.value }}</span>
          </div>
        </div>
        <response-fields v-else :rows="fields" @meaning="setMeaning" />
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
  padding: 10px 12px 10px;
}

.err {
  margin-bottom: 8px;
}

.head {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  row-gap: 6px;
  overflow: hidden;
  flex: 0 0 auto;
}

.ttl {
  font-size: 12.5px;
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
  font-size: 11.5px;
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
  gap: 4px;
  border: 1px solid var(--app-border);
  background: var(--app-panel);
  border-radius: 5px;
  font-size: 11px;
  font-family: inherit;
  padding: 2px 9px;
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
  gap: 4px;
  margin-top: 8px;
  border-bottom: 1px solid var(--app-border);
  flex: 0 0 auto;
}

.seg-tab {
  border: none;
  background: none;
  padding: 7px 10px;
  font-size: 12.5px;
  font-family: inherit;
  color: var(--app-muted);
  cursor: pointer;
  border-bottom: 2px solid transparent;
  margin-bottom: -1px;
  white-space: nowrap;
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
  font-size: 11.5px;
  color: var(--app-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-align: right;
}

.pane {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
  margin-top: 8px;
}

.binhint {
  font-size: 12px;
  color: var(--app-warn);
  margin-bottom: 6px;
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