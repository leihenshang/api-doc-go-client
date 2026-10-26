<script setup lang="ts">
// 请求设置区（design-spec §2 请求面板）：Params / Body / Headers / Auth / Docs。
// 文档编辑器与服务端 Web 保持一致（md-editor-v3 的 MdEditor）。
import BodyFieldsTable from '@/components/BodyFieldsTable.vue'
import FindBar from '@/components/FindBar.vue'
import GrpcMessagePane from '@/components/GrpcMessagePane.vue'
import GrpcMetadataPane from '@/components/GrpcMetadataPane.vue'
import GrpcOptionsPane from '@/components/GrpcOptionsPane.vue'
import GrpcSchemaPane from '@/components/GrpcSchemaPane.vue'
import JsonBodyEditor from '@/components/JsonBodyEditor.vue'
import KeyValueTable from '@/components/KeyValueTable.vue'
import RequestDocsPane from '@/components/RequestDocsPane.vue'
import ResponsePane from '@/components/ResponsePane.vue'
import { useTextareaFind } from '@/composables/useTextareaFind'
import { useUrlParamsSync } from '@/composables/useUrlParamsSync'
import { deriveBodyFields, emptyFieldRow, mergeBodyFields, patchFieldRow, removeFieldRow } from '@/lib/bodyFields'
import { isGrpc } from '@/lib/grpc'
import { hasVarPlaceholder, isParsableJson } from '@/lib/jsonHighlight'
import { message } from '@/lib/notice'
import { requestSegByTab, respSubViewByTab } from '@/lib/tabViewState'
import { detectTextKind, prettyText } from '@/lib/textFormat'
import { useCollectionStore } from '@/stores/collection'
import type { Tab } from '@/stores/tabs'
import { useTabsStore } from '@/stores/tabs'
import type { Auth, BodyField } from '@/types'
import { SearchOutline } from '@vicons/ionicons5'
import { NButton, NCheckbox, NIcon, NInput, NModal, NSelect } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ tab: Tab }>()
const tabs = useTabsStore()
const coll = useCollectionStore()
const { t } = useI18n()

// mirror 模式只读（mode 限制，T38）
const readonly = computed(() => coll.isReadOnly)

// ---- Params ↔ URL 双向联动（syncingFrom 防循环；逻辑见 composables/useUrlParamsSync）----
const { onParamsChange } = useUrlParamsSync({
  getRequest: () => props.tab.request,
  getKey: () => props.tab.key,
  isGrpc: () => isGrpcReq.value,
  isReadonly: () => readonly.value,
  openKeys: () => tabs.tabs.map((t) => t.key),
  touch,
})

type Seg =
  | 'schema' | 'message' | 'metadata' | 'options'
  | 'params' | 'body' | 'headers' | 'auth' | 'resp' | 'vars' | 'script' | 'tests' | 'docs'

const isGrpcReq = computed(() => isGrpc(props.tab.request))
/** gRPC 请求没有 query / 请求头 / 请求体，分段换成「定义 + 协议无关的变量 / 脚本 / 断言 / 文档」。 */
const savedSeg = requestSegByTab.get(props.tab.key) as Seg | undefined
const seg = ref<Seg>(savedSeg ?? (isGrpcReq.value ? 'schema' : 'params'))
// 二级页签在本次运行内按 tab 记住：切换请求后再切回，仍停留在上次的分段。
watch(seg, (v) => requestSegByTab.set(props.tab.key, v))

// 脚本/断言/前置变量（E15–E17）
const varsPre = computed({
  get: () => props.tab.request.varsPreRequest ?? [],
  set: (v) => {
    props.tab.request.varsPreRequest = v
    touch()
  },
})
const scriptPre = computed({
  get: () => props.tab.request.script?.preRequest ?? '',
  set: (v: string) => {
    if (!props.tab.request.script) props.tab.request.script = {}
    props.tab.request.script.preRequest = v
    touch()
  },
})
const scriptPost = computed({
  get: () => props.tab.request.script?.postResponse ?? '',
  set: (v: string) => {
    if (!props.tab.request.script) props.tab.request.script = {}
    props.tab.request.script.postResponse = v
    touch()
  },
})
const asserts = computed({
  get: () => props.tab.request.asserts ?? [],
  set: (v) => {
    props.tab.request.asserts = v
    touch()
  },
})

function addVar(): void {
  varsPre.value = [...varsPre.value, { name: '', value: '', enabled: true }]
}
function addAssert(): void {
  asserts.value = [...asserts.value, { name: '', expr: '' }]
}
function removeAt(list: 'vars' | 'asserts', i: number): void {
  if (list === 'vars') varsPre.value = varsPre.value.filter((_, j) => j !== i)
  else asserts.value = asserts.value.filter((_, j) => j !== i)
}

/** 前置变量：末行一有输入就就地补一行空行（省去手动点「添加一行」）。
 *  只在末尾已有行时才补 —— 空表不主动建行，避免给没用到 vars 的请求凭空写出空段（落盘会带上）。 */
function onVarsEdit(): void {
  touch()
  const list = props.tab.request.varsPreRequest
  if (!list || list.length === 0) return
  const last = list[list.length - 1]
  if (last.name.trim() === '' && last.value.trim() === '') return
  list.push({ name: '', value: '', enabled: true })
}

/** 断言：末行一有输入就就地补一行空行（同上，空表不主动建行）。 */
function onAssertsEdit(): void {
  touch()
  const list = props.tab.request.asserts
  if (!list || list.length === 0) return
  const last = list[list.length - 1]
  if ((last.name ?? '').trim() === '' && last.expr.trim() === '') return
  list.push({ name: '', expr: '' })
}

const auth = computed<Auth>(() => props.tab.request.auth ?? { type: 'none' })
const authType = computed({
  get: () => auth.value.type || 'none',
  set: (v: string) => {
    props.tab.request.auth = { ...auth.value, type: v }
    touch()
  },
})
const authOptions = computed(() => [
  { label: t('auth.none'), value: 'none' },
  { label: t('auth.inherit'), value: 'inherit' },
  { label: t('auth.basic'), value: 'basic' },
  { label: t('auth.bearer'), value: 'bearer' },
  { label: t('auth.apikey'), value: 'apikey' },
])
const authInOptions = computed(() => [
  { label: t('auth.header'), value: 'header' },
  { label: t('auth.query'), value: 'query' },
])
const hasAuth = computed(() => auth.value.type !== '' && auth.value.type !== 'none')

const bodyType = computed({
  get: () => props.tab.request.body.type,
  set: (v: string) => {
    props.tab.request.body.type = v as typeof props.tab.request.body.type
    touch()
  },
})

/** Docs 默认只读预览；编辑态由父级持有（页内查找按它判断是否有可查找 textarea）。 */
const docsEditing = ref(false)

// 页签角标：该段有内容时显示小圆点
const hasParams = computed(() => props.tab.request.params.some((p) => p.name.trim()))
const hasHeaders = computed(() => props.tab.request.headers.some((h) => h.name.trim()))
const hasBody = computed(
  () =>
    props.tab.request.body.type !== 'none' &&
    (props.tab.request.body.raw.trim() !== '' || props.tab.request.body.form.some((f) => f.name.trim())),
)
const hasDocs = computed(() => props.tab.request.docs.trim() !== '')
const hasVars = computed(() => (props.tab.request.varsPreRequest ?? []).some((v) => v.name.trim()))
const hasScript = computed(
  () => !!(props.tab.request.script?.preRequest?.trim() || props.tab.request.script?.postResponse?.trim()),
)
const hasAsserts = computed(() => (props.tab.request.asserts ?? []).some((a) => a.expr.trim()))

const httpSegments = computed<{ key: Seg; label: string; dot: boolean }[]>(() => [
  { key: 'params', label: t('editor.params'), dot: hasParams.value },
  { key: 'body', label: t('editor.body'), dot: hasBody.value },
  { key: 'headers', label: t('editor.headers'), dot: hasHeaders.value },
  { key: 'auth', label: t('editor.auth'), dot: hasAuth.value },
  { key: 'resp', label: t('editor.respFields'), dot: hasResp.value },
  { key: 'vars', label: t('editor.vars'), dot: hasVars.value },
  { key: 'script', label: t('editor.script'), dot: hasScript.value },
  { key: 'tests', label: t('editor.tests'), dot: hasAsserts.value },
  { key: 'docs', label: t('editor.docs'), dot: hasDocs.value },
])

/** 定义了 .proto 即亮角标（对齐 HTTP 侧各段的 dot 语义） */
const hasProto = computed(() => !!(props.tab.request.grpc?.proto ?? '').trim())
/** 请求消息（Message 分段） */
const hasMessage = computed(() => !!(props.tab.request.grpc?.message ?? '').trim())
/** Metadata 有启用行即亮角标 */
const hasMetadata = computed(() => (props.tab.request.grpc?.metadata ?? []).some((m) => m.name.trim()))

/** 连接设置非默认（TLS / 压缩 / 超时覆盖）时亮角标 */
const hasGrpcOptions = computed(() => {
  const g = props.tab.request.grpc
  if (!g) return false
  const mode = (g.tls?.mode ?? '').trim().toLowerCase()
  return (mode !== '' && mode !== 'none' && mode !== 'plaintext') || g.compress === 'gzip' || !!props.tab.request.settings?.timeoutSec
})

const grpcSegments = computed<{ key: Seg; label: string; dot: boolean }[]>(() => [
  { key: 'schema', label: t('grpc.schema'), dot: hasProto.value },
  { key: 'message', label: t('grpc.message'), dot: hasMessage.value },
  { key: 'metadata', label: t('grpc.metadata'), dot: hasMetadata.value },
  { key: 'options', label: t('grpc.options.title'), dot: hasGrpcOptions.value },
  { key: 'vars', label: t('editor.vars'), dot: hasVars.value },
  { key: 'script', label: t('editor.script'), dot: hasScript.value },
  { key: 'tests', label: t('editor.tests'), dot: hasAsserts.value },
  { key: 'docs', label: t('editor.docs'), dot: hasDocs.value },
])

const segments = computed(() => (isGrpcReq.value ? grpcSegments.value : httpSegments.value))

function touch(): void {
  if (readonly.value) return
  tabs.touch(props.tab.key)
}

/** 只标脏、不触发自动保存（格式化后由用户显式保存）。 */
function touchManual(): void {
  if (readonly.value) return
  tabs.markDirty(props.tab.key)
}

// 一键格式化请求体：JSON 缩进 / XML 标签缩进；非 JSON/XML 或解析失败时保持 原文
/** 文本体里是否可能是 JSON/XML（只看前缀，用于按钮显隐；真正格式化时再解析）。 */
const bodyFormatKind = computed(() => detectTextKind(props.tab.request.body.raw))
function formatBody(): void {
  const out = prettyText(props.tab.request.body.raw)
  if (!out) return
  props.tab.request.body.raw = out.text
  touchManual()
  message.info(t('editor.formatNeedsSave'))
}

/** JSON 模式下语法有误（只在内容非空、且不含 {{变量}} 时判断）。 */
const jsonBroken = computed(
  () => props.tab.request.body.raw.trim() !== '' && !hasVarPlaceholder(props.tab.request.body.raw) && !isParsableJson(props.tab.request.body.raw),
)

/** 一键复制请求体原文；按钮文案在「复制 / 已复制」间短暂切换。 */
const copiedBody = ref(false)
let copiedTimer: ReturnType<typeof setTimeout> | null = null
async function copyBody(): Promise<void> {
  try {
    if (!navigator.clipboard?.writeText) throw new Error('clipboard unavailable')
    await navigator.clipboard.writeText(props.tab.request.body.raw)
    copiedBody.value = true
    if (copiedTimer) clearTimeout(copiedTimer)
    copiedTimer = setTimeout(() => {
      copiedBody.value = false
    }, 1200)
  } catch {
    message.error(t('resp.copyFailed'))
  }
}

// ---- 请求体「原文 / 表格」视图（仅 JSON 体）----
// 原文即 JSON 本体；表格是 body.fields（唯一真源），只能手工「生成数据表格」派生，
// 派生后完全由用户编辑（5 列 / 增行 / 删行）。不再从 JSON 自动派生，也不反向写回 JSON。
type BodyView = 'raw' | 'table'
const bodyView = ref<BodyView>('raw')
const fieldRows = computed<BodyField[]>(() => props.tab.request.body.fields ?? [])

/** 生成确认弹窗（已有表格时二选一：新增 / 覆盖）。 */
const genDialog = ref(false)
/** 本次派生的结果（弹窗里选择模式后与之合并）。 */
const genDerived = ref<BodyField[]>([])

/** 写入表格并标脏（切换视图不标脏，只有表格内容变化才 dirty）。 */
function setFieldRows(rows: BodyField[]): void {
  props.tab.request.body.fields = rows
  touch()
}

/**
 * 「生成数据表格」：按 body.raw 派生 5 列（描述列取旧 body.descs 作种子）。
 * 空表 → 直接生成（等价新增到空表）；非空 → 弹窗让用户选「新增 / 覆盖」。
 */
function onGenerateFields(): void {
  if (readonly.value) return
  const derived = deriveBodyFields(props.tab.request.body.raw, props.tab.request.body.descs)
  if (!derived.length) {
    message.warning(t('body.genInvalid'))
    return
  }
  if (!fieldRows.value.length) {
    setFieldRows(derived)
    bodyView.value = 'table'
    return
  }
  genDerived.value = derived
  genDialog.value = true
}

/** 弹窗确认：按模式合并（新增=只追加缺失路径；覆盖=清空重建、同路径只继承描述）。 */
function onConfirmGenerate(mode: 'append' | 'overwrite'): void {
  setFieldRows(mergeBodyFields(mode, fieldRows.value, genDerived.value))
  genDialog.value = false
  genDerived.value = []
  bodyView.value = 'table'
}

/** 取消生成：什么都不改（不标脏）。 */
function onCancelGenerate(): void {
  genDialog.value = false
  genDerived.value = []
}

/** 表格某行某列改动。 */
function onFieldUpdate(index: number, patch: Partial<BodyField>): void {
  setFieldRows(patchFieldRow(fieldRows.value, index, patch))
}

/** 追加一行空行。 */
function onFieldAdd(): void {
  setFieldRows([...fieldRows.value, emptyFieldRow()])
}

/** 删除某行。 */
function onFieldRemove(index: number): void {
  setFieldRows(removeFieldRow(fieldRows.value, index))
}
/**
 * form / multipart 的请求数据示例：根据表格行（启用且有名字）拼出。
 * form = urlencoded 串；multipart = 带 boundary 的分段示意（文件值行以 <file> 占位）。
 * 只读展示，不新增字段、不影响实际发送。
 */
const formExample = computed(() => {
  const b = props.tab.request.body
  const rows = (b.form ?? []).filter((f) => f.enabled && f.name.trim())
  if (!rows.length) return ''
  if (b.type === 'form') {
    return rows.map((f) => `${encodeURIComponent(f.name)}=${encodeURIComponent(f.value)}`).join('&')
  }
  if (b.type === 'multipart') {
    const boundary = '<boundary>'
    const parts = rows.map((f) => {
      const isFile = f.type === 'file'
      const disp = isFile
        ? `Content-Disposition: form-data; name="${f.name}"; filename="${f.value}"`
        : `Content-Disposition: form-data; name="${f.name}"`
      return `--${boundary}\n${disp}\n\n${isFile ? '<file>' : f.value}`
    })
    return `${parts.join('\n')}\n--${boundary}--`
  }
  return ''
})

// 切换 tab 时刷新解析预览，并恢复该 tab 上次停留的分段与响应子页签；
// 新 tab 默认 HTTP → 请求参数、gRPC → 定义、响应子页签 → 响应数据。
watch(
  () => props.tab.key,
  (key, prev) => {
    if (prev) {
      requestSegByTab.set(prev, seg.value)
      respSubViewByTab.set(prev, respView.value)
    }
    const saved = requestSegByTab.get(key) as Seg | undefined
    seg.value = saved && segments.value.some((s) => s.key === saved)
      ? saved
      : (isGrpcReq.value ? 'schema' : 'params')
    const savedResp = respSubViewByTab.get(key) as RespView | undefined
    respView.value = savedResp && ['body', 'headers', 'fields'].includes(savedResp) ? savedResp : 'body'
    tabs.refreshResolve()
  },
)

// ---- 响应字段（Resp 分段）----
// 表格 / 响应数据 / 响应头都由 ResponsePane 维护；父级只保留子页签状态供页内查找判断。
type RespView = 'fields' | 'body' | 'headers'
const savedRespView = respSubViewByTab.get(props.tab.key) as RespView | undefined
const respView = ref<RespView>(savedRespView ?? 'body')
watch(respView, (v) => respSubViewByTab.set(props.tab.key, v))
/** 页签角标：有已生成的响应字段时显示小圆点。 */
const hasResp = computed(() => (props.tab.request.respDoc?.params ?? []).some((p) => p.fieldName))

// ---- 编辑区页内查找（textarea 版；逻辑见 composables/useTextareaFind）----
const { segBodyEl, findOn, findKw, findTotal, findIndex, findBarRef, findable, openFind, applyFind, findNext, findPrev } =
  useTextareaFind({
    getSeg: () => seg.value,
    getDocsEditing: () => docsEditing.value,
    getBodyType: () => props.tab.request.body.type,
    getRespView: () => respView.value,
    isGrpc: () => isGrpcReq.value,
  })
</script>

<template>
  <div class="editor">
    <div class="seg">
      <button v-for="s in segments" :key="s.key" class="seg-tab" :class="{ on: seg === s.key }" type="button"
        data-testid="req.tab" :data-seg="s.key" @click="seg = s.key">
        {{ s.label }}<span v-if="s.dot" class="badge" />
      </button>
      <!-- 当前分段内容支持页内查找时显示（JSON/文本请求体、脚本、Docs 源码编辑） -->
      <button v-if="findable()" class="find-btn" :class="{ on: findOn }" type="button" data-testid="req.find"
        :title="t('find.placeholder')" @click="findOn ? (findOn = false) : openFind()">
        <n-icon :component="SearchOutline" :size="14" />
      </button>
    </div>

    <find-bar v-if="findOn" ref="findBarRef" v-model:keyword="findKw" :total="findTotal" :index="findIndex"
      @next="findNext" @prev="findPrev" @close="findOn = false" />

    <div ref="segBodyEl" class="seg-body" @input="findOn ? applyFind() : undefined">
      <!-- gRPC：定义分段（导入 / 更新 / 移除 .proto、服务方法列表） -->
      <grpc-schema-pane v-if="isGrpcReq && seg === 'schema'" :tab="tab" />
      <!-- gRPC：请求消息（生成样例 / 按定义校验 / 字段提示） -->
      <grpc-message-pane v-else-if="isGrpcReq && seg === 'message'" :tab="tab" />
      <!-- gRPC：Metadata（键值表复用） -->
      <grpc-metadata-pane v-else-if="isGrpcReq && seg === 'metadata'" :tab="tab" />
      <!-- gRPC：连接与选项（明文/TLS、超时、压缩） -->
      <grpc-options-pane v-else-if="isGrpcReq && seg === 'options'" :tab="tab" />
      <key-value-table v-else-if="seg === 'params'" :rows="tab.request.params" :label="t('editor.query')"
        @change="onParamsChange" />
      <key-value-table v-else-if="seg === 'headers'" :rows="tab.request.headers" :label="t('editor.headers')"
        @change="touch" />
      <div v-else-if="seg === 'auth'" class="auth-pane">
        <div class="arow">
          <span class="lbl">{{ t('auth.type') }}</span>
          <n-select v-model:value="authType" :options="authOptions" size="small" class="atype"
            data-testid="auth.type" />
        </div>
        <template v-if="authType === 'basic'">
          <div class="arow">
            <span class="lbl">{{ t('auth.username') }}</span>
            <n-input v-model:value="auth.username" size="small" data-testid="auth.username" @input="touch" />
          </div>
          <div class="arow">
            <span class="lbl">{{ t('auth.password') }}</span>
            <n-input v-model:value="auth.password" size="small" type="password" show-password-on="click"
              data-testid="auth.password" @input="touch" />
          </div>
        </template>
        <template v-else-if="authType === 'bearer'">
          <div class="arow">
            <span class="lbl">{{ t('auth.token') }}</span>
            <n-input v-model:value="auth.token" size="small" data-testid="auth.token" @input="touch" />
          </div>
        </template>
        <template v-else-if="authType === 'apikey'">
          <div class="arow">
            <span class="lbl">{{ t('auth.key') }}</span>
            <n-input v-model:value="auth.key" size="small" data-testid="auth.key" @input="touch" />
          </div>
          <div class="arow">
            <span class="lbl">{{ t('auth.value') }}</span>
            <n-input v-model:value="auth.value" size="small" data-testid="auth.value" @input="touch" />
          </div>
          <div class="arow">
            <span class="lbl">{{ t('auth.in') }}</span>
            <n-select v-model:value="auth.in" :options="authInOptions" size="small" class="atype" data-testid="auth.in"
              @update:value="touch" />
          </div>
        </template>
      </div>
      <template v-else-if="seg === 'body'">
        <div class="body-pane">
          <div class="brow">
            <n-select :value="bodyType" :options="[
              { label: t('editor.bodyNone'), value: 'none' },
              { label: t('editor.bodyJson'), value: 'json' },
              { label: t('editor.bodyText'), value: 'text' },
              { label: t('editor.bodyForm'), value: 'form' },
              { label: t('editor.bodyMultipart'), value: 'multipart' },
            ]" size="small" class="btype" data-testid="body.type" @update:value="bodyType = $event" />
            <button v-if="bodyType === 'json' || (bodyType === 'text' && bodyFormatKind)" class="fmt" type="button"
              data-testid="body.format" @click="formatBody">
              {{ t('editor.formatJson') }}
            </button>
            <button v-if="bodyType === 'json' || bodyType === 'text'" class="fmt" type="button" data-testid="body.copy"
              :disabled="!tab.request.body.raw.trim()" @click="copyBody">
              {{ copiedBody ? t('common.copied') : t('common.copy') }}
            </button>
            <span v-if="bodyType === 'json' && jsonBroken" class="fmtbad" data-testid="body.jsonBroken">
              {{ t('editor.jsonBroken') }}
            </span>
          </div>
          <!-- JSON 体：原文 / 表格 两视图（表格按字段路径编辑备注，写回 body.descs 随同步往返）；
               原文就是请求数据本身，不再单设示例视图。纯文本、表单沿用原编辑器。切换视图不标脏，只有备注改动才 dirty。 -->
          <template v-if="bodyType === 'json'">
            <div class="body-sub-bar">
              <div class="resp-sub" role="group">
                <button class="sub-tab" :class="{ on: bodyView === 'raw' }" type="button" data-testid="body.viewTab"
                  data-view="raw" @click="bodyView = 'raw'">{{ t('editor.bodyTabRaw') }}</button>
                <button class="sub-tab" :class="{ on: bodyView === 'table' }" type="button" data-testid="body.viewTab"
                  data-view="table" @click="bodyView = 'table'">
                  {{ t('editor.bodyTabTable') }}<span v-if="fieldRows.length" class="sub-badge">{{ fieldRows.length
                  }}</span>
                </button>
              </div>
              <!-- 表格只能手工生成：空表直接派生，非空表弹「新增 / 覆盖」 -->
              <button class="fmt" type="button" data-testid="body.generate" @click="onGenerateFields">
                {{ t('body.genBtn') }}
              </button>
            </div>
            <body-fields-table v-if="bodyView === 'table'" :rows="fieldRows" @update="onFieldUpdate" @add="onFieldAdd"
              @remove="onFieldRemove" />
            <json-body-editor v-else v-model="tab.request.body.raw" :rows="12" :placeholder="t('editor.rawPlaceholder')"
              @input="touch" />
          </template>
          <n-input v-else-if="bodyType === 'text'" v-model:value="tab.request.body.raw" type="textarea" :rows="12"
            class="mono raw" :placeholder="t('editor.rawPlaceholder')" @input="touch" />
          <!-- form / multipart：键值表格直接编辑；下方根据表格字段给一份只读的请求数据示例（不新增字段）。 -->
          <template v-else-if="bodyType === 'form' || bodyType === 'multipart'">
            <key-value-table :rows="tab.request.body.form"
              :label="bodyType === 'form' ? t('editor.bodyForm') : t('editor.bodyMultipart')"
              :show-type="bodyType === 'multipart'" @change="touch" />
            <div v-if="formExample" class="bex">
              <div class="bex-hd">{{ t('editor.bodyExampleFromFields') }}</div>
              <pre class="body-example mono" data-testid="body.formExample">{{ formExample }}</pre>
            </div>
          </template>
        </div>
      </template>
      <div v-else-if="seg === 'vars'" class="script-pane">
        <p class="hint">{{ t('editor.varsHint') }}</p>
        <div v-for="(v, i) in varsPre" :key="i" class="srow" data-testid="vars.row">
          <n-checkbox v-model:checked="v.enabled" size="small" @update:checked="touch" />
          <n-input v-model:value="v.name" size="small" :placeholder="t('editor.colName')" data-testid="vars.name"
            @input="onVarsEdit" />
          <n-input v-model:value="v.value" size="small" :placeholder="t('editor.colValue')" data-testid="vars.value"
            @input="onVarsEdit" />
          <button class="rm" type="button" :title="t('common.delete')" @click="removeAt('vars', i)">×</button>
        </div>
        <button class="link" type="button" data-testid="vars.add" @click="addVar">{{ t('editor.addRow') }}</button>
      </div>
      <div v-else-if="seg === 'script'" class="script-pane">
        <p class="hint">{{ t('editor.scriptHint') }}</p>
        <div class="slb">{{ t('editor.scriptPre') }}</div>
        <n-input :value="scriptPre" type="textarea" :rows="6" class="mono raw" data-testid="script.pre"
          :placeholder="t('editor.scriptPrePlaceholder')" @update:value="(v: string) => (scriptPre = v)" />
        <div class="slb">{{ t('editor.scriptPost') }}</div>
        <n-input :value="scriptPost" type="textarea" :rows="6" class="mono raw" data-testid="script.post"
          :placeholder="t('editor.scriptPostPlaceholder')" @update:value="(v: string) => (scriptPost = v)" />
      </div>
      <div v-else-if="seg === 'tests'" class="script-pane">
        <p class="hint">{{ t('editor.testsHint') }}</p>
        <div v-for="(a, i) in asserts" :key="i" class="srow" data-testid="assert.row">
          <n-input v-model:value="a.name" size="small" :placeholder="t('editor.colDesc')" class="aname"
            data-testid="assert.name" @input="onAssertsEdit" />
          <n-input v-model:value="a.expr" size="small" class="mono" :placeholder="t('editor.assertExpr')"
            data-testid="assert.expr" @input="onAssertsEdit" />
          <button class="rm" type="button" :title="t('common.delete')" @click="removeAt('asserts', i)">×</button>
        </div>
        <button class="link" type="button" data-testid="assert.add" @click="addAssert">{{ t('editor.addRow') }}</button>
      </div>
      <!-- 响应字段（Resp）：字段表格 / 响应数据 / 响应头快照由 ResponsePane 维护 -->
      <response-pane v-else-if="seg === 'resp'" v-model:view="respView" :tab="tab" />
      <request-docs-pane v-else v-model:editing="docsEditing" :tab="tab" />
    </div>

    <!-- 「生成数据表格」确认弹窗：已有表格时二选一（新增 / 覆盖），空表不弹 -->
    <n-modal v-model:show="genDialog" preset="card" class="gen-modal"
      style="width: 460px; max-width: calc(100vw - 32px)" :title="t('body.genTitle')">
      <div data-testid="body.genDialog">
        <p class="gen-hint">{{ t('body.genHint') }}</p>
        <div class="gen-actions">
          <n-button size="small" type="primary" data-testid="body.genAppend" @click="onConfirmGenerate('append')">
            {{ t('body.genAppend') }}
          </n-button>
          <n-button size="small" data-testid="body.genOverwrite" @click="onConfirmGenerate('overwrite')">
            {{ t('body.genOverwrite') }}
          </n-button>
          <n-button size="small" quaternary data-testid="body.genCancel" @click="onCancelGenerate">
            {{ t('common.cancel') }}
          </n-button>
        </div>
      </div>
    </n-modal>
  </div>
</template>

<style scoped>
.editor {
  padding: 0 12px;
  display: flex;
  flex-direction: column;
  min-height: 0;
  flex: 1 1 auto;
}

/* 请求体「原文 / 表格」子页签行：左侧页签 + 右侧「生成数据表格」 */
.body-sub-bar {
  display: flex;
  align-items: center;
  gap: 8px;
}

.body-sub-bar .resp-sub {
  flex: 1 1 auto;
}

.body-sub-bar .fmt {
  flex: 0 0 auto;
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

/* 段页签：与响应区统一风格，底部细线 */
.seg {
  display: flex;
  gap: 2px;
  border-bottom: 1px solid var(--app-border);
  flex: 0 0 auto;
  overflow-x: auto;
  scrollbar-width: none;
}

/* 页内查找按钮：推到分段行最右（与各分段标签同行，不抢位置） */
.find-btn {
  flex: 0 0 auto;
  margin-left: auto;
  align-self: center;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border: none;
  border-radius: 5px;
  background: none;
  color: var(--app-muted);
  cursor: pointer;
  padding: 0;
}

.find-btn:hover,
.find-btn.on {
  background: var(--app-row-hover);
  color: var(--app-accent);
}

.seg::-webkit-scrollbar {
  height: 0;
}

.seg-tab {
  position: relative;
  border: none;
  background: none;
  padding: 8px 11px;
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

.badge {
  position: absolute;
  top: 6px;
  right: 4px;
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: var(--app-accent);
}

.seg-body {
  padding: 10px 0;
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
  display: flex;
  flex-direction: column;
}

.body-pane {
  display: flex;
  flex-direction: column;
  gap: 8px;
  /* 让字段表格拿到有界高度：超长时由表格行区内部滚动，而不是把整个分段撑高 */
  flex: 1 1 auto;
  min-height: 0;
}

.auth-pane {
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-width: 560px;
}

.arow {
  display: flex;
  align-items: center;
  gap: 10px;
}

.arow .lbl {
  width: 90px;
  font-size: 12.5px;
  color: var(--app-text-2);
}

.atype {
  width: 160px;
}

.fmtbad {
  font-size: 11.5px;
  color: var(--app-danger);
}

.brow {
  display: flex;
  align-items: center;
  gap: 8px;
}

.btype {
  width: 130px;
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

.raw {
  font-size: 12.5px;
}

.fmt:disabled {
  opacity: 0.5;
  cursor: not-allowed;
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

/* 请求体 JSON 示例（只读）：与响应快照同款的代码块观感 */
.body-example {
  margin: 4px 0 0;
  padding: 10px 12px;
  border: 1px solid var(--app-border);
  border-radius: 8px;
  background: var(--app-code-bg, var(--app-surface-2, transparent));
  font-size: 12.5px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
  overflow: auto;
  flex: 1 1 auto;
  min-height: 0;
}

/* form/multipart 根据字段生成的只读示例：小标题 + 代码块 */
.bex {
  display: flex;
  flex-direction: column;
  margin-top: 4px;
}

.bex-hd {
  font-size: 11.5px;
  color: var(--app-muted);
  margin-bottom: 2px;
}

.bex .body-example {
  flex: 0 0 auto;
  max-height: 220px;
}

.script-pane {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 4px 0;
}

.script-pane .hint {
  margin: 0;
  font-size: 12px;
  color: var(--app-muted);
}

.script-pane .slb {
  font-size: 12px;
  font-weight: 500;
  color: var(--app-text-2);
  margin-top: 4px;
}

.script-pane .srow {
  display: flex;
  align-items: center;
  gap: 8px;
}

.script-pane .aname {
  flex: 0 0 160px;
}

.script-pane .rm {
  border: none;
  background: none;
  color: var(--app-muted);
  cursor: pointer;
  font-size: 14px;
  padding: 0 4px;
}

.script-pane .rm:hover {
  color: var(--app-danger, #d03050);
}

.script-pane .link {
  align-self: flex-start;
  border: none;
  background: none;
  color: var(--app-accent);
  cursor: pointer;
  font-size: 12px;
  padding: 0;
}
</style>
