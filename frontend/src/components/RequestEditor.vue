<script setup lang="ts">
// 请求设置区（design-spec §2 请求面板）：Params / Body / Headers / Auth / Docs。
// 文档编辑器与服务端 Web 保持一致（md-editor-v3 的 MdEditor）。
import BodyFieldsTable from '@/components/BodyFieldsTable.vue'
import FindBar from '@/components/FindBar.vue'
import GenDocDialog from '@/components/GenDocDialog.vue'
import GrpcMessagePane from '@/components/GrpcMessagePane.vue'
import GrpcMetadataPane from '@/components/GrpcMetadataPane.vue'
import GrpcOptionsPane from '@/components/GrpcOptionsPane.vue'
import GrpcSchemaPane from '@/components/GrpcSchemaPane.vue'
import JsonBodyEditor from '@/components/JsonBodyEditor.vue'
import KeyValueTable from '@/components/KeyValueTable.vue'
import ResponseFields from '@/components/ResponseFields.vue'
import { useResponseFields } from '@/composables/useResponseFields'
import { useTextareaFind } from '@/composables/useTextareaFind'
import { useUrlParamsSync } from '@/composables/useUrlParamsSync'
import { toBodyRows, withBodyDesc } from '@/lib/bodyFields'
import { buildApiDoc } from '@/lib/genDocs'
import { isGrpc } from '@/lib/grpc'
import { api } from '@/lib/ipc'
import { hasVarPlaceholder, isParsableJson } from '@/lib/jsonHighlight'
import { message } from '@/lib/notice'
import { loadFields, rowsFromDoc } from '@/lib/responseFields'
import { isDark } from '@/lib/theme'
import { useCollectionStore } from '@/stores/collection'
import type { Tab } from '@/stores/tabs'
import { useTabsStore } from '@/stores/tabs'
import type { Auth, ResponseDoc } from '@/types'
import { SearchOutline } from '@vicons/ionicons5'
import { MdEditor, MdPreview } from 'md-editor-v3'
import 'md-editor-v3/lib/style.css'
import { NCheckbox, NIcon, NInput, NSelect } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ tab: Tab }>()
const tabs = useTabsStore()
const coll = useCollectionStore()
const { t, locale } = useI18n()

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
const seg = ref<Seg>(isGrpcReq.value ? 'schema' : 'params')

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

// 文档：写回草稿即标脏（自动保存）
const docs = computed({
  get: () => props.tab.request.docs,
  set: (v: string) => {
    props.tab.request.docs = v
    touch()
  },
})

// ---- Docs 区「生成接口文档」----
const isHttpReq = computed(() => !isGrpcReq.value)

/** Docs 默认只读预览；仅当用户主动点「编辑」才进入编辑态。 */
const docsEditing = ref(false)
function toggleDocsEditing(): void {
  docsEditing.value = !docsEditing.value
}

/** 预览态是否已有内容：为空时展示空态提示，避免整块空白。 */
const hasDoc = computed(() => (docs.value ?? '').trim().length > 0)

/**
 * 生成接口文档（不直接覆写 Docs）：请求示例取 curl 代码；响应字段取**请求模型 RespDoc**（侧栏 Resp 节点同一份，
 * 草稿 / gRPC 回退本地一次性存档）；响应示例从已保存示例里挑最新一个 JSON 体。
 * 结果先弹「生成文档」对话框（预览/编辑 + 覆盖/追加/下载），由用户显式选择落点。
 */
const showGen = ref(false)
const genText = ref('')

async function genDocs(): Promise<void> {
  try {
    const curl = await api.generateCode('curl', coll.currentEnv, props.tab.request)
    const fromModel = rowsFromDoc(props.tab.request.respDoc)
    const fields = fromModel.length ? fromModel : loadFields(props.tab.uid)
    const examples = await api.listResponseExamples(props.tab.uid)
    const ex = examples.find((e) => !e.response.binary && /^[{[]/.test((e.response.body ?? '').trim()))
    genText.value = buildApiDoc({
      request: props.tab.request,
      curl,
      fields,
      exampleBody: ex?.response.body ?? '',
      labels: {
        desc: t('docs.desc'),
        uri: t('docs.uri'),
        method: t('docs.method'),
        headers: t('docs.headers'),
        params: t('docs.params'),
        body: t('docs.body'),
        bodyParams: t('docs.bodyParams'),
        reqExample: t('docs.reqExample'),
        resFields: t('docs.resFields'),
        resExample: t('docs.resExample'),
        note: t('docs.note'),
        colName: t('docs.colName'),
        colValue: t('docs.colValue'),
        colType: t('docs.colType'),
        colExample: t('docs.colExample'),
        colDesc: t('docs.colDesc'),
        colRequired: t('docs.colRequired'),
        requiredYes: t('docs.requiredYes'),
      },
    })
    showGen.value = true
  } catch (err) {
    message.error(err instanceof Error ? err.message : String(err))
  }
}

/** 对话框回传的落点：覆盖/追加只改 Docs（随自动保存落盘 + 同步）；下载走原生保存对话框。 */
function onGenApply(p: { mode: 'overwrite' | 'append' | 'download'; text: string }): void {
  if (p.mode === 'overwrite') {
    docs.value = p.text
  } else if (p.mode === 'append') {
    const cur = (docs.value ?? '').trim()
    docs.value = cur ? `${cur}\n\n${p.text}` : p.text
  } else {
    void downloadText(p.text)
  }
}

/** 把指定 Markdown 经原生保存对话框落盘为 .md（复用 SaveResponseBody 的写盘能力）。 */
async function downloadText(content: string): Promise<void> {
  try {
    const safeName = (props.tab.request.name || 'request').replace(/[\\/:*?"<>|]/g, '_')
    const path = await api.saveResponseBody(`${safeName}.md`, false, content)
    if (path) message.success(t('docs.downloaded', { path }))
  } catch (err) {
    message.error(err instanceof Error ? err.message : String(err))
  }
}

/** 工具栏「下载」：下载当前 Docs 内容。 */
async function downloadDoc(): Promise<void> {
  await downloadText(docs.value ?? '')
}

const mdLanguage = computed(() => (locale.value === 'en-US' ? 'en-US' : 'zh-CN'))
// md-editor 自带明暗两套配色；暗色下用 github 预览主题（默认主题在深底上对比度不足）
const mdTheme = computed(() => (isDark.value ? 'dark' : 'light'))
const mdPreviewTheme = computed(() => (isDark.value ? 'github' : 'default'))

// 页签角标：该段有内容时显示小圆点（仿 Bruno）
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

// 一键美化请求体 JSON；非法 JSON 原样保留，交给用户自查
function formatJson(): void {
  try {
    props.tab.request.body.raw = JSON.stringify(JSON.parse(props.tab.request.body.raw), null, 2)
    touch()
  } catch {
    return
  }
}

/** JSON 模式下语法有误（只在内容非空、且不含 {{变量}} 时判断）。 */
const jsonBroken = computed(
  () => props.tab.request.body.raw.trim() !== '' && !hasVarPlaceholder(props.tab.request.body.raw) && !isParsableJson(props.tab.request.body.raw),
)

// ---- 请求体「原文 / 表格」视图（仅 JSON 体）----
// 原文即 JSON 本体，表格按字段路径展平（复用响应字段的 extractFields 口径），
// 值列只读、备注列写回 body.descs 随同步往返。有了原文就不再加独立的「示例」视图。
type BodyView = 'raw' | 'table'
const bodyView = ref<BodyView>('raw')
const bodyRows = computed(() => toBodyRows(props.tab.request.body.raw, props.tab.request.body.descs))
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
/** 编辑某字段备注：写回 body.descs（空则删项）并标脏；切换视图不标脏，只有备注改动才 dirty。 */
function onBodyDesc(path: string, note: string): void {
  props.tab.request.body.descs = withBodyDesc(props.tab.request.body.descs, path, note)
  touch()
}

// 切换 tab 时刷新解析预览，并把分段复位到该协议的首页签（HTTP 是 Params，gRPC 是 Schema）；
// 响应字段子页签也复位到「响应数据」（否则会沿用上一个请求停留的字段表格/响应头）。
watch(
  () => props.tab.key,
  () => {
    seg.value = isGrpcReq.value ? 'schema' : 'params'
    respView.value = 'body'
    tabs.refreshResolve()
    initFields()
  },
)

// ---- 响应字段（Resp 分段；侧栏 Resp 节点同源）----
// 真相源是请求模型 tab.request.respDoc；未落盘草稿 / gRPC 回退本地一次性存档。
// 字段表状态与「防抖写回 respDoc」逻辑见 composables/useResponseFields。
const { fields, sample, hasResp, initFields, onSampleInput, setMeaning } = useResponseFields({
  getTab: () => props.tab,
  isGrpc: () => isGrpcReq.value,
  onError: (e) => message.error(e instanceof Error ? e.message : String(e)),
})

// 变量书签已移除（item 24）：字段表不再直接写环境变量，当时的响应数据/头看子页签。

// ---- 响应内容类型（对齐 Go respDoc.contentType 与同步键 retContentType）----
// 默认 json；非 json（text/xml/html/binary）时样例原样展示，不做缩进格式化。
const respContentType = computed(() => props.tab.request.respDoc?.contentType || 'json')

const contentTypeOptions = computed(() => [
  { label: t('editor.ctJson'), value: 'json' },
  { label: t('editor.ctText'), value: 'text' },
  { label: t('editor.ctXml'), value: 'xml' },
  { label: t('editor.ctHtml'), value: 'html' },
  { label: t('editor.ctBinary'), value: 'binary' },
])

/** 当前类型的展示名（下拉未命中时回退原始值，兼容旧数据）。 */
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

// ---- Resp 分段子页签（item 8）：响应数据 / 响应头 / 字段表格（默认先看响应数据） ----
type RespView = 'fields' | 'body' | 'headers'
const respView = ref<RespView>('body')
const respSnap = computed(() => props.tab.request.respDoc ?? null)

// 响应区「更新响应字段」成功后：切到 Resp 分段并直接看「字段表格」（其余情形默认停在响应数据）。
watch(
  () => tabs.respFocus,
  (f) => {
    if (f?.key !== props.tab.key) return
    seg.value = 'resp'
    respView.value = 'fields'
    initFields()
  },
)

// ---- 编辑区页内查找（textarea 版；逻辑见 composables/useTextareaFind）----
const { segBodyEl, findOn, findKw, findTotal, findIndex, findBarRef, findable, openFind, applyFind, findNext, findPrev } =
  useTextareaFind({
    getSeg: () => seg.value,
    getDocsEditing: () => docsEditing.value,
    getBodyType: () => props.tab.request.body.type,
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
            <button v-if="bodyType === 'json'" class="fmt" type="button" @click="formatJson">
              {{ t('editor.formatJson') }}
            </button>
            <span v-if="bodyType === 'json' && jsonBroken" class="fmtbad" data-testid="body.jsonBroken">
              {{ t('editor.jsonBroken') }}
            </span>
          </div>
          <!-- JSON 体：原文 / 表格 两视图（表格按字段路径编辑备注，写回 body.descs 随同步往返）；
               原文就是请求数据本身，不再单设示例视图。纯文本、表单沿用原编辑器。切换视图不标脏，只有备注改动才 dirty。 -->
          <template v-if="bodyType === 'json'">
            <div class="resp-sub" role="group">
              <button class="sub-tab" :class="{ on: bodyView === 'raw' }" type="button" data-testid="body.viewTab"
                data-view="raw" @click="bodyView = 'raw'">{{ t('editor.bodyTabRaw') }}</button>
              <button class="sub-tab" :class="{ on: bodyView === 'table' }" type="button" data-testid="body.viewTab"
                data-view="table" @click="bodyView = 'table'">
                {{ t('editor.bodyTabTable') }}<span v-if="bodyRows.length" class="sub-badge">{{ bodyRows.length
                  }}</span>
              </button>
            </div>
            <body-fields-table v-if="bodyView === 'table'" :rows="bodyRows" @update-desc="onBodyDesc" />
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
      <!-- 响应字段（Resp）：真相源是请求模型 respDoc（与生成接口文档同源）；
           子页签切换字段表格 / 更新时的响应数据与响应头快照（item 8） -->
      <div v-else-if="seg === 'resp'" class="resp-pane">
        <!-- 响应类型：落 respDoc.contentType（同步键 retContentType），非 JSON 时样例不格式化 -->
        <div class="resp-ct">
          <span class="lbl">{{ t('editor.respContentType') }}</span>
          <n-select :value="respContentType" :options="contentTypeOptions" size="small" class="ctype"
            data-testid="resp.contentType" :title="t('editor.respContentTypeHint')"
            @update:value="(v: string) => setContentType(v)" />
          <span class="ct-badge" data-testid="resp.contentTypeBadge">{{ contentTypeLabel }}</span>
        </div>
        <div class="resp-sub" role="group">
          <button class="sub-tab" :class="{ on: respView === 'body' }" type="button" data-testid="resp.segTab"
            data-view="body" @click="respView = 'body'">
            {{ t('editor.respTabBody') }}<span v-if="respSnap?.status" class="sub-badge">{{ respSnap.status }}</span>
          </button>
          <button class="sub-tab" :class="{ on: respView === 'headers' }" type="button" data-testid="resp.segTab"
            data-view="headers" @click="respView = 'headers'">
            {{ t('editor.respTabHeaders') }}<span v-if="respSnap?.headers?.length" class="sub-badge">{{
              respSnap.headers.length }}</span>
          </button>
          <!-- 字段表格放到最后（由「响应数据」派生，先看数据再看字段表） -->
          <button class="sub-tab" :class="{ on: respView === 'fields' }" type="button" data-testid="resp.segTab"
            data-view="fields" @click="respView = 'fields'">{{ t('editor.respTabFields') }}</button>
        </div>
        <!-- 字段表格：由「响应数据」子页签的合法 JSON 实时派生（表格只读，只能逐行写含义） -->
        <div v-if="respView === 'fields'" class="resp-fields">
          <response-fields :rows="fields" @meaning="setMeaning" />
        </div>
        <!-- 响应数据：可编辑；合法 JSON 时自动生成「字段表格」（非法/空则不覆盖既有表） -->
        <div v-else-if="respView === 'body'" class="resp-data">
          <p class="resp-data-hint">{{ t('editor.respDataHint') }}</p>
          <n-input :value="sample" type="textarea" :rows="16" class="mono resp-data-ta" data-testid="resp.dataInput"
            :disabled="readonly" :placeholder="t('editor.respDataPh')" @update:value="onSampleInput" />
        </div>
        <div v-else class="resp-heads" data-testid="resp.segHeaders">
          <div v-if="!respSnap?.headers?.length" class="resp-heads-empty">{{ t('editor.respSnapEmpty') }}</div>
          <div v-for="(h, i) in respSnap?.headers ?? []" :key="`${h.name}-${i}`" class="hrow"
            data-testid="resp.segHeaderRow">
            <span class="hn mono">{{ h.name }}</span>
            <span class="hv">{{ h.value }}</span>
          </div>
        </div>
      </div>
      <div v-else class="docs-pane">
        <div class="docs-bar">
          <button class="gin" type="button" data-testid="docs.edit" @click="toggleDocsEditing">
            {{ docsEditing ? t('docs.view') : t('docs.edit') }}
          </button>
          <template v-if="isHttpReq">
            <button class="gin" type="button" data-testid="docs.gen" :disabled="readonly" @click="genDocs">
              {{ t('docs.gen') }}
            </button>
            <button class="gin" type="button" data-testid="docs.download" @click="downloadDoc">
              {{ t('docs.download') }}
            </button>
          </template>
        </div>
        <md-editor v-if="docsEditing" v-model="docs" :language="mdLanguage" :theme="mdTheme"
          :preview-theme="mdPreviewTheme" class="md-edit" />
        <template v-else>
          <md-preview v-if="hasDoc" :model-value="docs" :language="mdLanguage" :theme="mdTheme"
            :preview-theme="mdPreviewTheme" class="md-preview" />
          <div v-else class="docs-empty">
            <p>{{ t('docs.noDoc') }}</p>
          </div>
        </template>
      </div>
    </div>

    <!-- 生成接口文档结果弹窗（预览/编辑 + 覆盖/追加/下载） -->
    <gen-doc-dialog v-model:show="showGen" :text="genText" @apply="onGenApply" />
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

.md-edit {
  flex: 1 1 auto;
  min-height: 340px;
}

/* Resp 分段子页签（item 8）：字段表格 / 响应数据 / 响应头 快照切换 */
/* 字段表格：由「响应数据」派生，仅表格本身 */
.resp-fields {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

/* 「响应数据」可编辑区：提示 + 文本域 */
.resp-data {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.resp-data-hint {
  margin: 0;
  font-size: 11.5px;
  color: var(--app-muted);
}

.resp-data-ta :deep(textarea) {
  font-size: 12.5px;
}

/* 响应类型行：下拉 + 类型徽标（与子页签同排风格） */
.resp-ct {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 12px 0;
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
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 6px 12px 0;
}

.sub-tab {
  border: none;
  background: none;
  border-radius: 5px;
  padding: 4px 10px;
  font-family: inherit;
  font-size: 12px;
  color: var(--app-muted);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 5px;
}

.sub-tab:hover {
  background: var(--app-row-hover);
  color: var(--app-text);
}

.sub-tab.on {
  background: var(--app-accent-tint);
  color: var(--app-accent-dark);
  font-weight: 600;
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

.resp-snap {
  margin: 8px 12px 12px;
  padding: 10px 12px;
  border: 1px solid var(--app-border);
  border-radius: 8px;
  background: var(--app-code-bg, var(--app-surface-2, transparent));
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
  overflow: auto;
  max-height: calc(100% - 52px);
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

/* Docs 区：顶部工具条（「生成接口文档」）+ 编辑器纵向撑满 */
.docs-pane {
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.docs-bar {
  flex: 0 0 auto;
  display: flex;
  justify-content: flex-end;
  gap: 6px;
  margin-bottom: 6px;
}

.docs-pane .md-edit {
  flex: 1 1 auto;
  min-height: 0;
}

/* 只读预览：撑满并滚动在自身容器内；给最小高度兜底，避免 flex 高度塌陷时预览整体消失 */
.docs-pane .md-preview {
  flex: 1 1 auto;
  min-height: 0;
  height: 100%;
  overflow: auto;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  padding: 12px 16px;
}

/* 预览态没有内容时的空态提示 */
.docs-pane .docs-empty {
  flex: 1 1 auto;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px dashed var(--app-border);
  border-radius: 6px;
  min-height: 160px;
}

.docs-pane .docs-empty p {
  margin: 0;
  font-size: 13px;
  color: var(--app-muted);
}

/* Docs 区（尤其左右布局区间较窄时）顶部工具栏允许换行，避免右侧按钮被截断 */
.docs-pane :deep(.md-editor-toolbar) {
  flex-wrap: wrap;
}

.gin {
  border: 1px solid var(--app-accent-tint);
  background: var(--app-accent-tint);
  color: var(--app-accent-dark);
  border-radius: 6px;
  font-size: 12px;
  font-family: inherit;
  padding: 4px 10px;
  cursor: pointer;
}

.gin:hover:not(:disabled) {
  border-color: var(--app-accent);
}

.gin:disabled {
  opacity: 0.6;
  cursor: default;
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
