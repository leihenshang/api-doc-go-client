<script setup lang="ts">
// 响应面板：状态栏 / 页签（响应体 / 响应头 / 尾元数据）/ 工具条 / 内容区。
// 响应字段表已移到请求编辑器的「响应字段（Resp）」分段（侧栏 Resp 节点入口）：
// 本面板只保留「更新响应字段」动作 —— 已有字段时先弹覆盖确认（可勾选「值一致时继承说明」），
// 更新成功后字段表写入请求模型并把编辑器切到 Resp 分段。
import FindBar from '@/components/FindBar.vue'
import JsonTree from '@/components/JsonTree.vue'
import ResponseToolbar from '@/components/ResponseToolbar.vue'
import { useDomFind } from '@/composables/useDomFind'
import { formatBytes, httpStatusText } from '@/lib/format'
import { grpcCodeLabel, grpcStatusType } from '@/lib/grpc'
import { api } from '@/lib/ipc'
import { message } from '@/lib/notice'
import { buildRespDoc, loadFields, persistRespDoc, rebuildFields, rootJsonType, rowsFromDoc, type FieldRow, type PersistTimer } from '@/lib/responseFields'
import { detectFileResponse } from '@/lib/responseFile'
import type { Tab } from '@/stores/tabs'
import { useTabsStore } from '@/stores/tabs'
import type { ResponseExample, SendResult } from '@/types'
import { BookmarkOutline, DownloadOutline, TrashOutline } from '@vicons/ionicons5'
import { NAlert, NButton, NCheckbox, NIcon, NInput, NModal, NPopconfirm, NSelect, NTag } from 'naive-ui'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ tab: Tab }>()
const { t } = useI18n()
const tabs = useTabsStore()

// 默认不换行：长值横向滚动看全，需要时用工具条换行图标开关
const view = ref<'pretty' | 'raw'>('pretty')
const wrap = ref(false)
const seg = ref<'body' | 'headers' | 'trailers'>('body')
const jv = ref<InstanceType<typeof JsonTree> | null>(null)

/** 覆盖确认（仅当字段表已有内容时弹出） */
const showOverwrite = ref(false)
const INHERIT_KEY = 'client.resp.inheritMeaning'
const inheritMeaning = ref(localStorage.getItem(INHERIT_KEY) !== '0')

// 正在回看的已保存示例（不依赖本次响应：刷新后没有实时响应也能回看快照）
const example = computed(() => examples.value.find((e) => e.uid === viewingUid.value) ?? null)

const display = computed<SendResult | null>(() => {
  // 回看示例时整个面板展示快照；否则展示本次实时响应
  const r = (example.value?.response as SendResult | undefined) ?? props.tab.response
  if (!r) return null
  // 全字段归一：go 侧有 omitempty，缺字段时归一成空值——否则渲染期读 .length 会抛错
  return {
    url: r.url ?? '',
    status: r.status ?? 0,
    proto: r.proto ?? '',
    timeMs: r.timeMs ?? 0,
    size: r.size ?? 0,
    sentSize: r.sentSize ?? 0,
    contentType: r.contentType ?? '',
    binary: !!r.binary,
    headers: r.headers ?? [],
    body: r.body ?? '',
    trailers: r.trailers ?? [],
    warnings: r.warnings ?? [],
    script: r.script ?? null,
    truncated: !!r.truncated,
    fullSize: r.fullSize ?? 0,
  }
})

const body = computed(() => display.value?.body ?? '')

const isJson = computed(() => /^[{[]/.test(body.value.trim()))

const rawText = computed(() => {
  if (view.value === 'raw' || !isJson.value) return body.value
  try {
    return JSON.stringify(JSON.parse(body.value), null, 2)
  } catch {
    return body.value
  }
})

const headerCount = computed(() => display.value?.headers?.length ?? 0)
const trailerCount = computed(() => display.value?.trailers?.length ?? 0)
const isGrpcRes = computed(() => (display.value?.proto ?? '') === 'gRPC')

// 文件类响应（下载意图，如 GET xxx/file.md、带 Content-Disposition、或二进制）：
// 不当接口结果解析/美化，改为提示下载查看。判定见 lib/responseFile。
const fileInfo = computed(() =>
  detectFileResponse({
    url: display.value?.url ?? props.tab.request?.url,
    contentType: display.value?.contentType,
    headers: display.value?.headers,
    binary: display.value?.binary,
  }),
)

const examples = ref<ResponseExample[]>([])
const viewingUid = ref('')

const exampleOptions = computed(() => [
  { value: '', label: t('resp.liveResponse') },
  ...examples.value.map((e) => ({ value: e.uid, label: e.name })),
])

// 切换请求：无响应时回到 body 页签。immediate：会话恢复直接打开的 tab 不会再触发 uid 变化，
// 挂载时也要拉一次示例列表（否则「已保存的响应示例」下拉在无响应态是空的）
watch(
  () => props.tab.uid,
  () => {
    seg.value = 'body'
    viewingUid.value = ''
    void reloadExamples()
  },
  { immediate: true },
)

// 重新发送后回到本次响应并复位页签，避免新响应还停在旧页签上
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
  } catch (e) {
    // 拉取失败静默置空会让用户看到空下拉却不知原因，至少留一条控制台告警便于排查
    console.warn('[resp] 加载响应示例失败:', props.tab.uid, e)
    examples.value = []
  }
}

const meta = computed(() => {
  if (!display.value) return ''
  // 亚毫秒级显示 0 会让人怀疑「没发出去」：统一显示 <1 ms（与 Postman 一致）
  const ms = display.value.timeMs < 1 ? '<1' : String(display.value.timeMs)
  const parts = [`${ms} ms`, formatBytes(display.value.size)]
  if (display.value.sentSize) parts.push(`↑ ${formatBytes(display.value.sentSize)}`)
  return parts.join(' · ')
})

// gRPC 的码名与徽章语义统一走 lib/grpc.ts：此前这里把所有非零码一律显示成
// NOT_OK(n)，掩盖了真实码（如 NOT_FOUND(5)），且与别处的码表是两份真相。
const statusType = computed<'success' | 'warning' | 'error'>(() => {
  const s = display.value?.status ?? 0
  if (isGrpcRes.value) return grpcStatusType(s)
  if (s >= 200 && s < 300) return 'success'
  if (s >= 300 && s < 500) return 'warning'
  return 'error'
})

const statusLabel = computed(() => {
  const s = display.value?.status ?? 0
  if (isGrpcRes.value) return grpcCodeLabel(s)
  return `${s} ${httpStatusText(s)}`.trim()
})

const copied = ref(false)

// ---- 更新响应字段（覆盖确认 + 值一致继承说明）----

const isGrpcReq = computed(() => !!props.tab.request?.grpc)

/** 当前字段表：已落盘请求取请求模型 RespDoc；草稿 / gRPC 回退一次性本地存档。 */
function currentRows(): FieldRow[] {
  const doc = props.tab.request?.respDoc
  const rows = rowsFromDoc(doc)
  return rows.length ? rows : loadFields(props.tab.uid)
}

/**
 * 「更新响应字段」：解析当前展示的响应体并**覆盖**字段表。
 * 已有字段时先弹覆盖确认（可勾选「值一致时继承已有说明」）；首次更新或空表直接执行。
 */
async function updateFields(): Promise<void> {
  if (!display.value) return
  if (!body.value.trim()) {
    message.info(t('resp.fieldsNeedBody'))
    return
  }
  const old = currentRows()
  if (old.length > 0) {
    showOverwrite.value = true
    return
  }
  await doUpdateFields(old, inheritMeaning.value)
}

async function confirmOverwrite(): Promise<void> {
  showOverwrite.value = false
  localStorage.setItem(INHERIT_KEY, inheritMeaning.value ? '1' : '0')
  await doUpdateFields(currentRows(), inheritMeaning.value)
}

async function doUpdateFields(old: FieldRow[], inherit: boolean): Promise<void> {
  try {
    const { rows } = rebuildFields(old, body.value, inherit)
    const ok = await persistRows(rows, true)
    if (!ok) return
    // 字段表已挂到请求模型：直接把编辑器切到「响应字段」分段（集合树不再挂 Resp 节点）
    if (!isGrpcReq.value) {
      tabs.focusRespFields(props.tab.key)
    }
    message.success(t('resp.fieldsUpdated', { n: rows.length }))
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

/** 含义输入去抖写盘。草稿 / gRPC 请求暂存 localStorage；HTTP 请求写回请求模型 RespDoc（immediate 用于「更新」按钮）。 */
const persistTimer: PersistTimer = { current: null }
async function persistRows(rows: FieldRow[], immediate = false): Promise<boolean> {
  // 快照优先：有本次响应就用它的 type/sample/status/headers，缺省回退既有 respDoc（组装规则见 buildRespDoc）。
  const doc = buildRespDoc(rows, props.tab.request?.respDoc, {
    type: rootJsonType(body.value),
    sample: body.value,
    status: display.value?.status,
    headers: display.value?.headers?.length ? display.value.headers : undefined,
  })
  return persistRespDoc({
    uid: props.tab.uid,
    draft: props.tab.draft,
    grpc: isGrpcReq.value,
    rows,
    doc,
    immediate,
    timer: persistTimer,
    attach: (d) => { if (props.tab.request) props.tab.request.respDoc = d },
    onError: (e) => message.error(e instanceof Error ? e.message : String(e)),
  })
}

onBeforeUnmount(() => {
  if (persistTimer.current) clearTimeout(persistTimer.current)
})

async function copyBody(): Promise<void> {
  if (!(await writeClipboard(body.value))) {
    message.error(t('resp.copyFailed'))
    return
  }
  copied.value = true
  setTimeout(() => (copied.value = false), 1200)
}

async function writeClipboard(text: string): Promise<boolean> {
  try {
    await navigator.clipboard?.writeText(text)
    return true
  } catch {
    return false
  }
}

function openSave(): void {
  saveName.value = t('resp.exampleDefault')
  showSave.value = true
}
const showSave = ref(false)
const saveName = ref('')

async function submitSave(): Promise<void> {
  const name = saveName.value.trim()
  if (!name) return
  try {
    const saved = await api.saveResponseExample(props.tab.request, name, props.tab.response as SendResult)
    await reloadExamples()
    showSave.value = false
    // 保存后自动切到该示例快照（与「回看只读」交互一致，可经「回到本次响应」返回）
    viewingUid.value = saved.uid
    message.success(t('resp.exampleSaved', { name: saved.name }))
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

async function saveBody(): Promise<void> {
  try {
    const path = await api.saveResponseBody('response.txt', false, body.value)
    if (path) message.success(t('resp.savedFile', { path }))
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

// 文件类响应的下载：用检测到的文件名，二进制走 base64 解码写盘（saveResponseBody 内部处理）。
async function downloadFile(): Promise<void> {
  try {
    const path = await api.saveResponseBody(fileInfo.value.filename, !!display.value?.binary, body.value)
    if (path) message.success(t('resp.savedFile', { path }))
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

async function removeExample(): Promise<void> {
  try {
    await api.deleteResponseExample(props.tab.uid, viewingUid.value || (example.value?.uid ?? ''))
    await reloadExamples()
    viewingUid.value = ''
    message.success(t('common.deleted'))
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

// ---- 查找（内容区高亮 + 上下切换；逻辑见 composables/useDomFind）----
const { findOn, findKw, findTotal, findIndex, paneRef, findBarRef, openFind, findNext, findPrev } = useDomFind({
  getSeg: () => seg.value,
  getView: () => view.value,
  getBody: () => body.value,
  getHeaders: () => display.value?.headers,
  getTrailers: () => display.value?.trailers,
  hasResponse: () => !!display.value,
})
</script>

<template>
  <div class="resp">
    <n-alert v-if="tab.error" type="error" class="err">{{ tab.error }}</n-alert>

    <template v-if="!tab.error">
      <!-- 状态栏常驻：尚未发送 / 回看示例时都在同一位置；无内容时只隐藏状态码与耗时/体积。
           示例下拉也留在这里 —— 没有本次响应时仍可直接切到已保存的示例快照。 -->
      <div class="status-bar" data-testid="resp.statusbar">
        <span class="ttl">{{ t('resp.title') }}</span>
        <n-tag v-if="display?.status" :type="statusType" :bordered="false" class="badge" data-testid="resp.status">
          {{ statusLabel }}
        </n-tag>
        <span v-if="display" class="meta mono" data-testid="resp.meta">{{ meta }}</span>
        <span class="sp" />
        <n-popconfirm v-if="example" @positive-click="removeExample">
          <template #trigger>
            <button class="toggle danger" type="button" data-testid="resp.exampleDelete"
              :title="t('resp.deleteExample')">
              <n-icon :component="TrashOutline" :size="13" />
            </button>
          </template>
          <span>{{ t('resp.deleteConfirm', { name: example.name }) }}</span>
        </n-popconfirm>
        <n-select v-model:value="viewingUid" :options="exampleOptions" size="small" class="expick"
          data-testid="resp.examples" />
      </div>

      <template v-if="display">
        <div v-if="display.warnings?.length" class="warns" data-testid="resp.warnings">
          <span v-for="(msg, i) in display.warnings" :key="i" class="warn">{{ msg }}</span>
        </div>

        <div class="seg">
          <button class="seg-tab" :class="{ on: seg === 'body' }" type="button" data-testid="resp.tab" data-seg="body"
            @click="seg = 'body'">
            {{ t('resp.body') }}
          </button>
          <button class="seg-tab" :class="{ on: seg === 'headers' }" type="button" data-testid="resp.tab"
            data-seg="headers" @click="seg = 'headers'">
            {{ isGrpcRes ? t('resp.initialMetadata') : t('resp.headers') }}<span v-if="headerCount" class="num">{{
              headerCount }}</span>
          </button>
          <button v-if="isGrpcRes" class="seg-tab" :class="{ on: seg === 'trailers' }" type="button"
            data-testid="resp.tab" data-seg="trailers" @click="seg = 'trailers'">
            {{ t('resp.trailers') }}<span v-if="trailerCount" class="num">{{ trailerCount }}</span>
          </button>
        </div>

        <!-- 文件类响应不显示解析/美化/查找/保存示例等工具（改为下方下载卡片） -->
        <response-toolbar v-if="!fileInfo.isFile" v-model:view="view" v-model:wrap="wrap" v-model:find-on="findOn"
          :is-json="isJson" :copied="copied" :can-save-example="!example" :findable="!!body"
          @update-fields="updateFields" @copy-body="copyBody" @save-file="saveBody" @save-example="openSave"
          @find="openFind" />

        <find-bar v-if="findOn" ref="findBarRef" v-model:keyword="findKw" :total="findTotal" :index="findIndex"
          @next="findNext" @prev="findPrev" @close="findOn = false" />

        <div v-if="example" class="exnote" data-testid="resp.exnote">
          <n-icon :component="BookmarkOutline" :size="13" />
          <span>{{ t('resp.viewingExample', { name: example.name }) }}</span>
          <button class="link" type="button" data-testid="resp.backToLive" @click="viewingUid = ''">
            {{ t('resp.backToLive') }}
          </button>
        </div>

        <div ref="paneRef" class="pane">
          <template v-if="seg === 'body'">
            <div v-if="display.script?.scriptError" class="binhint warn" data-testid="resp.scriptError">
              {{ display.script.scriptError }}
            </div>
            <div v-if="display.script?.asserts?.length" class="asserts" data-testid="resp.asserts">
              <div v-for="(a, i) in display.script.asserts" :key="i" class="arow" :class="a.passed ? 'ok' : 'fail'">
                <span class="astate">✓</span>
                <span class="aname">{{ a.name || a.expr }}</span>
                <span class="aexpr mono">{{ a.expr }}</span>
                <span v-if="a.error" class="aerr">{{ a.error }}</span>
              </div>
            </div>
            <!-- 文件类响应（下载意图，如 xxx/file.md、Content-Disposition 附件、二进制）：
                 只提示下载查看，不把内容当接口结果解析/美化/展示。 -->
            <div v-if="fileInfo.isFile" class="file-card" data-testid="resp.fileCard">
              <n-icon :component="DownloadOutline" :size="24" class="fc-ico" />
              <p class="fc-text">{{ t('resp.filePrompt', { name: fileInfo.filename }) }}</p>
              <button class="fc-btn" type="button" data-testid="resp.downloadFile" @click="downloadFile">
                <n-icon :component="DownloadOutline" :size="14" />
                {{ t('resp.downloadFile') }}
              </button>
            </div>
            <template v-else>
              <!-- 二进制响应：body 是 base64 文本，给明确提示再展示原文 -->
              <div v-if="display.binary" class="binhint" data-testid="resp.binaryHint">{{ t('resp.binary') }}</div>
              <json-tree v-if="isJson && view === 'pretty'" ref="jv" :text="body" :wrap="wrap" />
              <!-- 原始视图固定折行（item 17）：minify 后的一行 JSON 不折会溢出视框；
                   「换行」开关只作用于美化树 -->
              <pre v-else class="raw mono" data-testid="resp.rawBody">{{ rawText }}</pre>
            </template>
          </template>

          <div v-else-if="seg === 'headers'" class="hlist">
            <div v-for="h in display.headers" :key="h.name" class="hrow" data-testid="resp.headerRow">
              <span class="hn mono">{{ h.name }}</span>
              <span class="hv">{{ h.value }}</span>
            </div>
          </div>

          <div v-else class="hlist">
            <div v-for="h in display.trailers" :key="h.name" class="hrow" data-testid="resp.trailerRow">
              <span class="hn mono">{{ h.name }}</span>
              <span class="hv">{{ h.value }}</span>
            </div>
          </div>
        </div>
      </template>
      <!-- 无本次响应也未回看示例：空态（状态栏仍在上方常驻） -->
      <div v-else class="empty">
        <svg class="plane" data-testid="resp.emptyIcon" viewBox="0 0 24 24" fill="none" stroke="currentColor"
          stroke-width="1.4">
          <path d="M22 2 11 13M22 2l-7 20-4-9-9-4 20-7Z" />
        </svg>
        <p class="etitle">{{ t('resp.empty') }}</p>
        <ul class="shortcuts">
          <li><kbd>Ctrl</kbd> + <kbd>Enter</kbd><span>{{ t('resp.sendHint') }}</span></li>
          <li><kbd>Ctrl</kbd> + <kbd>N</kbd><span>{{ t('resp.newHint') }}</span></li>
          <li><kbd>Ctrl</kbd> + <kbd>E</kbd><span>{{ t('resp.envHint') }}</span></li>
          <li><kbd>Ctrl</kbd> + <kbd>W</kbd><span>{{ t('resp.closeHint') }}</span></li>
          <li><kbd>Ctrl</kbd> + <kbd>S</kbd><span>{{ t('resp.saveNowHint') }}</span></li>
        </ul>
      </div>
    </template>

    <n-modal v-model:show="showSave" preset="card" :title="t('resp.saveResponse')" style="width: 440px">
      <div class="save-form">
        <span class="flabel">{{ t('resp.exampleName') }}</span>
        <n-input v-model:value="saveName" size="small" data-testid="resp.exampleNameInput"
          :placeholder="t('resp.exampleNamePlaceholder')" />
        <p class="fhint">{{ t('resp.exampleHint') }}</p>
      </div>
      <template #footer>
        <div class="modal-ft">
          <n-button size="small" data-testid="resp.exampleCancel" @click="showSave = false">{{ t('common.cancel') }}</n-button>
          <n-button size="small" type="primary" data-testid="resp.exampleSave" @click="submitSave">
            {{ t('common.save') }}
          </n-button>
        </div>
      </template>
    </n-modal>

    <!-- 覆盖字段表确认：已有字段时更新 = 覆盖重建；勾选后同路径同值的字段沿用旧说明 -->
    <n-modal v-model:show="showOverwrite" preset="card" :title="t('resp.fieldsOverwriteTitle')" style="width: 440px">
      <p class="ow-text">{{ t('resp.fieldsOverwriteContent') }}</p>
      <n-checkbox v-model:checked="inheritMeaning" data-testid="resp.fieldsInherit">
        {{ t('resp.inheritMeaning') }}
      </n-checkbox>
      <template #footer>
        <div class="modal-ft">
          <n-button size="small" data-testid="resp.fieldsCancel" @click="showOverwrite = false">{{ t('common.cancel') }}</n-button>
          <n-button size="small" type="primary" data-testid="resp.fieldsConfirm" @click="confirmOverwrite">
            {{ t('resp.fieldsOverwriteOk') }}
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
  gap: 8px;
  flex: 0 0 auto;
  height: 44px;
  border-bottom: 1px solid var(--app-border);
}

.ttl {
  font-size: 13px;
  font-weight: 600;
  color: var(--app-text);
}

.badge {
  font-family: var(--app-mono);
  font-weight: 700;
  flex: 0 0 auto;
}

.meta {
  font-size: 12px;
  color: var(--app-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.sp {
  flex: 1;
}

.expick {
  width: 132px;
  flex: 0 0 auto;
}

.toggle {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  border: 1px solid var(--app-border);
  background: var(--app-panel);
  border-radius: 6px;
  font-size: 11.5px;
  font-family: inherit;
  padding: 4px 9px;
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
  gap: 20px;
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
  padding: 7px 2px;
  font-size: 12px;
  font-family: inherit;
  color: var(--app-muted);
  cursor: pointer;
  border-bottom: 2px solid transparent;
  margin-bottom: -1px;
  border-radius: 4px 4px 0 0;
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
  display: inline-block;
  margin-left: 5px;
  min-width: 16px;
  padding: 0 5px;
  border-radius: 999px;
  background: var(--app-chip);
  font-size: 10.5px;
  font-weight: 500;
  line-height: 15px;
  color: var(--app-muted);
  text-align: center;
  vertical-align: 1px;
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

/* 文件类响应下载卡片：居中提示 + 下载按钮，替代内容解析视图 */
.file-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 28px 16px;
  text-align: center;
  color: var(--app-text-2);
}

.file-card .fc-ico {
  color: var(--app-accent);
}

.file-card .fc-text {
  margin: 0;
  font-size: 12.5px;
  line-height: 1.6;
  max-width: 420px;
  color: var(--app-muted);
  word-break: break-all;
}

.fc-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border: 1px solid var(--app-accent);
  background: var(--app-accent-tint);
  color: var(--app-accent-dark);
  border-radius: 6px;
  padding: 6px 14px;
  font-size: 12.5px;
  font-family: inherit;
  font-weight: 600;
  cursor: pointer;
}

.fc-btn:hover {
  filter: brightness(1.05);
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
  min-height: 0;
  overflow: auto;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: safe center;
  gap: 6px;
  color: var(--app-muted);
}

.plane {
  /* SVG 只有 viewBox 时浏览器按默认 300×150 渲染，必须显式定尺寸（40 的 1.5 倍） */
  width: 60px;
  height: 60px;
  flex: none;
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

.ow-text {
  margin: 0 0 10px;
  font-size: 12.5px;
  color: var(--app-text-2);
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
