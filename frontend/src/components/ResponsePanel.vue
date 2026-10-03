<script setup lang="ts">
// 响应面板（response-panel design-spec §3）：状态栏 → 标签栏 → 操作工具条 → 内容区。
// 保存▾ 菜单（§4）只保留两个落盘动作：保存响应示例 / 保存响应体为文件（.json），
// 「保存全部字段为变量 / 保存选中值为变量」与「作用域」chips 已下线（有意差异）；
// 变量入口只剩字段表的「变量书签」，落点固定为当前环境（映射见 lib/saveVars.ts）。
import { NAlert, NButton, NIcon, NInput, NModal, NPopconfirm, NSelect, NTag } from 'naive-ui'
import { BookmarkOutline, TrashOutline } from '@vicons/ionicons5'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import JsonTree from '@/components/JsonTree.vue'
import ResponseFields from '@/components/ResponseFields.vue'
import ResponseToolbar from '@/components/ResponseToolbar.vue'
import { formatBytes, httpStatusText } from '@/lib/format'
import { grpcCodeLabel, grpcStatusType } from '@/lib/grpc'
import { api } from '@/lib/ipc'
import { message } from '@/lib/notice'
import { loadFields, mergeFields, saveFields, type FieldRow } from '@/lib/responseFields'
import { deriveVarName, removeVar, saveVars, scopeVarNames, type VarScope } from '@/lib/saveVars'
import type { Tab } from '@/stores/tabs'
import type { ResponseExample, SendResult } from '@/types'

const props = defineProps<{ tab: Tab }>()
const { t } = useI18n()

/** 变量书签的落点：作用域 chips 下线后固定为「环境」。 */
const VAR_SCOPE: VarScope = 'env'

const view = ref<'pretty' | 'raw'>('pretty')
const wrap = ref(true)
const seg = ref<'body' | 'headers' | 'trailers' | 'fields'>('body')
const jv = ref<InstanceType<typeof JsonTree> | null>(null)

const fields = ref<FieldRow[]>([])
const examples = ref<ResponseExample[]>([])
const viewingUid = ref('') // 非空 = 正在回看已保存的示例
const showSave = ref(false)
const saveName = ref('')
const copied = ref(false)

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
    if (!r) return null
    // 全字段归一：Go 侧有 omitempty，缺字段时渲染期读属性/取值会得到 undefined（空值展示或 NaN）
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
      script: r.script ?? null,
    }
  }
  return {
    url: ex.request.url ?? '',
    status: ex.response.status ?? 0,
    proto: ex.response.proto ?? '',
    timeMs: ex.response.timeMs ?? 0,
    size: ex.response.size ?? 0,
    contentType: ex.response.contentType ?? '',
    binary: !!ex.response.binary,
    headers: ex.response.headers ?? [],
    body: ex.response.body ?? '',
    trailers: [], // 示例快照不存尾元数据
    script: null, // example 快照未存脚本产物
  }
})

/** gRPC 响应：proto 由执行器固定写成 "gRPC"，此时的 status 是 gRPC code（不是 HTTP 状态码）。 */
const isGrpcRes = computed(() => (display.value?.proto ?? '') === 'gRPC')
const trailerCount = computed(() => display.value?.trailers?.length ?? 0)

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
  // gRPC：0=OK 绿，取消 / 超时黄，其余红（G8.2）
  if (isGrpcRes.value) return grpcStatusType(s)
  if (s >= 200 && s < 300) return 'success'
  if (s >= 300 && s < 500) return 'warning'
  return 'error'
})

const statusLabel = computed(() => {
  const s = display.value?.status ?? 0
  // gRPC：状态码名 + 码值（`OK(0)` / `NOT_FOUND(5)`）
  if (isGrpcRes.value) return grpcCodeLabel(s)
  return `${s} ${httpStatusText(s)}`.trim()
})

const meta = computed(() => {
  const r = display.value
  if (!r) return ''
  const out = `${r.timeMs} ms${t('common.sep')}${formatBytes(r.size)}`
  // gRPC：把发送的请求消息大小也带上（G8.3）
  return r.sentSize ? `${out}${t('common.sep')}↑ ${formatBytes(r.sentSize)}` : out
})

const exampleOptions = computed(() => [
  { label: t('resp.liveResponse'), value: '' },
  ...examples.value.filter((e) => !!e.uid).map((e) => ({ label: e.name, value: e.uid })),
])

/** 书签判定依据：当前环境里已存在的变量名。 */
const scopeNames = computed(() => scopeVarNames(VAR_SCOPE, props.tab))
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
 *  字段表只存在于「响应字段」页签（响应体下方不再挂表），所以提示里带上页签名，点了不会「没反应」。
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

/** 写剪贴板：优先异步 Clipboard API（桌面壳/安全上下文），不可用或拒绝时降级 execCommand。
 *  失败必须给出提示 —— 否则点「复制响应体」看起来毫无反应。 */
async function writeClipboard(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // 权限被拒 / 非安全上下文：走下面的兜底
  }
  try {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.setAttribute('readonly', '')
    ta.style.position = 'fixed'
    ta.style.top = '-1000px'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand('copy')
    ta.remove()
    return ok
  } catch {
    return false
  }
}

async function copyBody(): Promise<void> {
  const text = display.value?.body
  if (!text) {
    message.info(t('resp.copyEmpty'))
    return
  }
  if (!(await writeClipboard(text))) {
    message.error(t('resp.copyFailed'))
    return
  }
  copied.value = true // 按钮文案切到「已复制」作为反馈
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

/** 字段行「变量书签」：已书签 → 删变量；未书签 → 存成当前环境的变量。
 *  「保存 ▾」里的变量入口与作用域 chips 已下线，变量落点固定为当前环境（映射见 lib/saveVars.ts）。 */
async function toggleBookmark(path: string): Promise<void> {
  const name = deriveVarName(path)
  try {
    if (scopeNames.value.has(name)) {
      await removeVar(VAR_SCOPE, props.tab, name)
      message.success(t('resp.varRemoved', { name }))
      return
    }
    const row = fields.value.find((r) => r.path === path)
    if (!row) return
    if (row.type === 'array' || row.type === 'object') {
      message.info(t('resp.varBookmarkLeaf'))
      return
    }
    await saveVars(VAR_SCOPE, props.tab, [{ name, value: row.value }])
    message.success(t('resp.varSaved', { name, scope: t('resp.scopeEnv') }))
  } catch (e) {
    if (e instanceof Error && e.message === 'no-env') message.warning(t('resp.varNoEnv'))
    else message.error(e instanceof Error ? e.message : String(e))
  }
}
</script>

<template>
  <div class="resp">
    <n-alert v-if="tab.error" type="error" :bordered="false" class="err">{{ tab.error }}</n-alert>

    <!-- 状态栏（design-spec §3 第 1 排，h44）在「尚未发送」时也占位：
         标题与右上角「已保存的响应示例 ▾」的样式、位置与发送后完全一致，
         没有响应时只是不渲染状态码徽章与耗时/体积（display 为空时这些信息不存在） -->
    <div v-if="!tab.error" class="status-bar" data-testid="resp.statusbar">
      <span class="ttl">{{ t('resp.title') }}</span>
      <n-tag
        v-if="display"
        :type="statusType"
        size="small"
        :bordered="false"
        class="badge"
        data-testid="resp.status"
        :title="display.proto"
      >
        {{ statusLabel }}
      </n-tag>
      <span v-if="display" class="meta mono" data-testid="resp.meta">{{ meta }}</span>
      <span class="sp" />
      <!-- 删除示例 + 「本次响应 ▾」：发送前后共用这一处，切换时下拉不会跳位置 -->
      <n-popconfirm v-if="example" @positive-click="removeExample">
        <template #trigger>
          <button class="toggle danger" type="button" data-testid="resp.exampleDelete" :title="t('resp.deleteExample')">
            <n-icon :component="TrashOutline" :size="13" />
            <span>{{ t('common.delete') }}</span>
          </button>
        </template>
        <span>{{ t('resp.deleteConfirm', { name: example.name }) }}</span>
      </n-popconfirm>
      <!-- 无响应又没有已保存示例时不摆下拉（只剩「本次响应」一项没有意义） -->
      <n-select
        v-if="display || examples.length"
        :value="viewingUid"
        :options="exampleOptions"
        size="small"
        class="expick"
        data-testid="resp.examples"
        :title="t('resp.examples')"
        @update:value="viewingUid = String($event ?? '')"
      />
    </div>

    <!-- 内容分支：出错时只留错误条；否则「有响应 → 响应区 / 无响应 → 空态提示」。
         注意这里必须自己带条件，不能写成 v-else-if/v-else —— 上一版的 v-else-if 挂到了上面
         状态栏的 v-if 上，链条变成「无错误 ? 状态栏 : …」，导致响应区与空态永远不渲染。 -->
    <template v-if="!tab.error && display">
      <!-- 标签栏：响应体 / 响应头 N / 响应字段 N + 请求 URL -->
      <div class="seg">
        <button class="seg-tab" :class="{ on: seg === 'body' }" type="button" data-testid="resp.tab" data-seg="body" @click="seg = 'body'">
          {{ t('resp.body') }}
        </button>
        <button class="seg-tab" :class="{ on: seg === 'headers' }" type="button" data-testid="resp.tab" data-seg="headers" @click="seg = 'headers'">
          {{ t(isGrpcRes ? 'resp.initialMetadata' : 'resp.headers') }}<span v-if="headerCount" class="num">{{ headerCount }}</span>
        </button>
        <!-- 尾元数据只存在于 gRPC（HTTP 响应没有这个概念），故按协议出现（G8.5） -->
        <button
          v-if="isGrpcRes"
          class="seg-tab"
          :class="{ on: seg === 'trailers' }"
          type="button"
          data-testid="resp.tab"
          data-seg="trailers"
          @click="seg = 'trailers'"
        >
          {{ t('resp.trailers') }}<span v-if="trailerCount" class="num">{{ trailerCount }}</span>
        </button>
        <button class="seg-tab" :class="{ on: seg === 'fields' }" type="button" data-testid="resp.tab" data-seg="fields" @click="seg = 'fields'">
          {{ t('resp.fieldsTab') }}<span v-if="fields.length" class="num">{{ fields.length }}</span>
        </button>
        <span class="url mono" data-testid="resp.url" :title="display.url">{{ display.url }}</span>
      </div>

      <!-- 操作工具条（design-spec §3 第 3 排）：左侧视图/显示，右侧解析/输出；
           「本次响应 ▾」按设计稿回到状态栏，工具条不再挂行首插槽 -->
      <response-toolbar
        v-model:view="view"
        v-model:wrap="wrap"
        :is-json="isJson"
        :copied="copied"
        :can-save-example="!example && !!tab.response"
        @expand-all="jv?.expandAll()"
        @collapse-all="jv?.collapseAll()"
        @update-fields="updateFields"
        @copy-body="copyBody"
        @save-file="saveBody"
        @save-example="openSave"
      />

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
        </template>
        <div v-else-if="seg === 'headers'" class="hlist">
          <div v-for="h in display.headers" :key="h.name" class="hrow mono" data-testid="resp.headerRow">
            <span class="hn">{{ h.name }}</span>
            <span class="hv">{{ h.value }}</span>
          </div>
        </div>
        <div v-else-if="seg === 'trailers'" class="hlist">
          <div v-for="tr in display.trailers" :key="tr.name" class="hrow mono" data-testid="resp.trailerRow">
            <span class="hn">{{ tr.name }}</span>
            <span class="hv">{{ tr.value }}</span>
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

    <div v-else-if="!tab.error" class="empty">
      <svg class="plane" data-testid="resp.emptyIcon" viewBox="0 0 24 24" width="56" height="56" fill="none" stroke="currentColor" stroke-width="1.4">
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

/* 状态栏（design-spec §3 第 1 排，h44）：响应 · 200 OK 徽章 · 12 ms · 145 KB，右侧「本次响应 ▾」 */
.status-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 0 0 auto;
  min-width: 0;
  height: 44px;
  border-bottom: 1px solid var(--app-border);
}

.ttl {
  font-size: 13px;
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
  font-size: 12px;
  color: var(--app-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  flex: 0 1 auto;
  min-width: 0;
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

/* 状态栏右侧「本次响应 ▾」：描边胶囊，切换历史响应 */
.expick {
  width: 132px;
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

/* 标签栏（design-spec §3 第 2 排，h40）：页签间距 20，右侧请求 URL */
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

/* 激活页签：绿字 + 2px 绿色下划线 */
.seg-tab.on {
  color: var(--app-accent);
  font-weight: 600;
  border-bottom-color: var(--app-accent);
}

/* 计数徽标（灰底胶囊，design-spec §3 第 2 排） */
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

/* 尚未发送的提示区：飞机图标 + 快捷键提示常驻。
   状态栏现在也占一行，面板被拖矮时（竖向布局 20%）内容可能放不下：
   overflow 兜住滚动，safe center 让内容超出时改为顶部对齐，避免居中把图标/提示裁掉。 */
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
