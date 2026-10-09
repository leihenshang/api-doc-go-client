<script setup lang="ts">
// 请求设置区（design-spec §2 请求面板）：Params / Body / Headers / Auth / Docs。
// 文档编辑器与服务端 Web 保持一致（md-editor-v3 的 MdEditor）。
import GrpcMessagePane from '@/components/GrpcMessagePane.vue'
import GrpcMetadataPane from '@/components/GrpcMetadataPane.vue'
import GrpcOptionsPane from '@/components/GrpcOptionsPane.vue'
import GrpcSchemaPane from '@/components/GrpcSchemaPane.vue'
import JsonBodyEditor from '@/components/JsonBodyEditor.vue'
import KeyValueTable from '@/components/KeyValueTable.vue'
import { buildApiDoc } from '@/lib/genDocs'
import { isGrpc } from '@/lib/grpc'
import { api } from '@/lib/ipc'
import { hasVarPlaceholder, isParsableJson } from '@/lib/jsonHighlight'
import { message } from '@/lib/notice'
import { loadFields } from '@/lib/responseFields'
import { isDark } from '@/lib/theme'
import { useCollectionStore } from '@/stores/collection'
import type { Tab } from '@/stores/tabs'
import { useTabsStore } from '@/stores/tabs'
import type { Auth } from '@/types'
import { MdEditor } from 'md-editor-v3'
import 'md-editor-v3/lib/preview.css'
import 'md-editor-v3/lib/style.css'
import { NCheckbox, NInput, NSelect } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ tab: Tab }>()
const tabs = useTabsStore()
const coll = useCollectionStore()
const { t, locale } = useI18n()

// mirror 模式只读（mode 限制，T38）
const readonly = computed(() => coll.isReadOnly)

// ---- Params ↔ URL 双向联动（syncingFrom 防循环） ----
const syncingFrom = ref<'url' | 'params' | null>(null)

/** 从 URL 的 query 段回填 params（URL 编辑时调用）。 */
function syncParamsFromUrl(): void {
  syncingFrom.value = 'url'
  try {
    const raw = props.tab.request.url
    const idx = raw.indexOf('?')
    const queryStr = idx >= 0 ? raw.slice(idx + 1) : ''
    const existing = new Map(props.tab.request.params.map((p) => [p.name, p]))
    const next: typeof props.tab.request.params = []
    if (queryStr) {
      for (const pair of queryStr.split('&')) {
        if (!pair) continue
        const eq = pair.indexOf('=')
        const name = decodeURIComponent(eq < 0 ? pair : pair.slice(0, eq))
        const value = eq < 0 ? '' : decodeURIComponent(pair.slice(eq + 1))
        if (!name) continue
        const old = existing.get(name)
        next.push({ name, value, enabled: old?.enabled ?? true, description: old?.description })
      }
    }
    // 保留表格里手动加的、URL 里没有的未启用行
    for (const p of props.tab.request.params) {
      if (p.name && !queryStr.includes(encodeURIComponent(p.name))) {
        next.push(p)
      }
    }
    props.tab.request.params.splice(0, props.tab.request.params.length, ...next)
  } finally {
    setTimeout(() => (syncingFrom.value = null), 0)
  }
}

/** 从 params 重拼 URL 的 query 段（参数表编辑时调用）。 */
function syncUrlFromParams(): void {
  syncingFrom.value = 'params'
  try {
    const raw = props.tab.request.url
    const idx = raw.indexOf('?')
    const base = idx >= 0 ? raw.slice(0, idx) : raw
    const parts: string[] = []
    for (const p of props.tab.request.params) {
      if (!p.enabled || !p.name.trim()) continue
      const k = encodeURIComponent(p.name.trim())
      const v = encodeURIComponent(p.value ?? '')
      parts.push(`${k}=${v}`)
    }
    props.tab.request.url = parts.length ? `${base}?${parts.join('&')}` : base
  } finally {
    setTimeout(() => (syncingFrom.value = null), 0)
  }
}

/** 各页签上一次见到的 URL：用来区分「用户改了 URL」与「切页签导致取值自然变化」 */
const lastUrlByTab = new Map<string, string>()

// URL 改了 → 回填 params（gRPC 没有 query 段，跳过）。
// 切页签同样会走到这里 —— 这个 watch 的取值是「当前 tab 的 url」，props.tab 一换取值就变了 ——
// 但那是换了个请求、不是编辑：只重建参数表，不标脏。否则什么都没改也会冒出未保存圆点
// （实测打开第二个请求、右键切换页签都会触发），关闭页签时还会为此多问一次。
watch(
  () => [props.tab.key, props.tab.request.url] as const,
  ([key, url]) => {
    const known = lastUrlByTab.get(key)
    const edited = known !== undefined && known !== url
    // 记录必须放在守卫之前：被守卫挡掉的那次也要更新基线，否则下次会被误判成编辑
    lastUrlByTab.set(key, url)
    if (isGrpcReq.value || readonly.value || syncingFrom.value === 'params') return
    syncParamsFromUrl()
    if (edited) touch()
  },
)

function onParamsChange(): void {
  if (readonly.value || syncingFrom.value === 'url') return
  syncUrlFromParams()
  touch()
}

type Seg =
  | 'schema'
  | 'message'
  | 'metadata'
  | 'options'
  | 'params'
  | 'body'
  | 'headers'
  | 'auth'
  | 'vars'
  | 'script'
  | 'tests'
  | 'docs'

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

/** 生成并覆写 Docs：请求示例取 curl 代码；响应字段取该请求已存档的字段表；
 *  响应示例从已保存示例里挑最新一个 JSON 体（否则省略响应节）。成功不提示，失败才提示。 */
async function genDocs(): Promise<void> {
  try {
    const curl = await api.generateCode('curl', coll.currentEnv, props.tab.request)
    const fields = loadFields(props.tab.uid)
    const examples = await api.listResponseExamples(props.tab.uid)
    const ex = examples.find((e) => !e.response.binary && /^[{[]/.test((e.response.body ?? '').trim()))
    docs.value = buildApiDoc({
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
        reqExample: t('docs.reqExample'),
        resFields: t('docs.resFields'),
        resExample: t('docs.resExample'),
        note: t('docs.note'),
        colName: t('docs.colName'),
        colType: t('docs.colType'),
        colDesc: t('docs.colDesc'),
        colRequired: t('docs.colRequired'),
      },
    })
  } catch (err) {
    message.error(err instanceof Error ? err.message : String(err))
  }
}

/** 下载 Docs：把当前 Docs 内容经原生保存对话框落盘为 .md（复用 SaveResponseBody 的写盘能力）。 */
async function downloadDoc(): Promise<void> {
  try {
    const safeName = (props.tab.request.name || 'request').replace(/[\\/:*?"<>|]/g, '_')
    const path = await api.saveResponseBody(`${safeName}.md`, false, docs.value)
    if (path) message.success(t('docs.downloaded', { path }))
  } catch (err) {
    message.error(err instanceof Error ? err.message : String(err))
  }
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

// 切换 tab 时刷新解析预览，并把分段复位到该协议的首页签（HTTP 是 Params，gRPC 是 Schema）
watch(
  () => props.tab.key,
  () => {
    seg.value = isGrpcReq.value ? 'schema' : 'params'
    tabs.refreshResolve()
  },
)
</script>

<template>
  <div class="editor">
    <div class="seg">
      <button v-for="s in segments" :key="s.key" class="seg-tab" :class="{ on: seg === s.key }" type="button"
        data-testid="req.tab" :data-seg="s.key" @click="seg = s.key">
        {{ s.label }}<span v-if="s.dot" class="badge" />
      </button>
    </div>

    <div class="seg-body">
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
          <n-select v-model:value="authType" :options="authOptions" size="small" class="atype" />
        </div>
        <template v-if="authType === 'basic'">
          <div class="arow">
            <span class="lbl">{{ t('auth.username') }}</span>
            <n-input v-model:value="auth.username" size="small" @input="touch" />
          </div>
          <div class="arow">
            <span class="lbl">{{ t('auth.password') }}</span>
            <n-input v-model:value="auth.password" size="small" type="password" show-password-on="click"
              @input="touch" />
          </div>
        </template>
        <template v-else-if="authType === 'bearer'">
          <div class="arow">
            <span class="lbl">{{ t('auth.token') }}</span>
            <n-input v-model:value="auth.token" size="small" @input="touch" />
          </div>
        </template>
        <template v-else-if="authType === 'apikey'">
          <div class="arow">
            <span class="lbl">{{ t('auth.key') }}</span>
            <n-input v-model:value="auth.key" size="small" @input="touch" />
          </div>
          <div class="arow">
            <span class="lbl">{{ t('auth.value') }}</span>
            <n-input v-model:value="auth.value" size="small" @input="touch" />
          </div>
          <div class="arow">
            <span class="lbl">{{ t('auth.in') }}</span>
            <n-select v-model:value="auth.in" :options="authInOptions" size="small" class="atype"
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
            ]" size="small" class="btype" @update:value="bodyType = $event" />
            <button v-if="bodyType === 'json'" class="fmt" type="button" @click="formatJson">
              {{ t('editor.formatJson') }}
            </button>
            <span v-if="bodyType === 'json' && jsonBroken" class="fmtbad" data-testid="body.jsonBroken">
              {{ t('editor.jsonBroken') }}
            </span>
          </div>
          <!-- JSON 走带语法着色的编辑器（透明 textarea 叠高亮层，编辑行为不变）；
               纯文本仍用原来的输入框，不引入没必要的着色。 -->
          <json-body-editor v-if="bodyType === 'json'" v-model="tab.request.body.raw" :rows="12"
            :placeholder="t('editor.rawPlaceholder')" @input="touch" />
          <n-input v-else-if="bodyType === 'text'" v-model:value="tab.request.body.raw" type="textarea" :rows="12"
            class="mono raw" :placeholder="t('editor.rawPlaceholder')" @input="touch" />
          <key-value-table v-else-if="bodyType === 'form' || bodyType === 'multipart'" :rows="tab.request.body.form"
            :label="bodyType === 'form' ? t('editor.bodyForm') : t('editor.bodyMultipart')"
            :show-type="bodyType === 'multipart'" @change="touch" />
        </div>
      </template>
      <div v-else-if="seg === 'vars'" class="script-pane">
        <p class="hint">{{ t('editor.varsHint') }}</p>
        <div v-for="(v, i) in varsPre" :key="i" class="srow" data-testid="vars.row">
          <n-checkbox v-model:checked="v.enabled" size="small" @update:checked="touch" />
          <n-input v-model:value="v.name" size="small" :placeholder="t('editor.colName')" data-testid="vars.name"
            @input="touch" />
          <n-input v-model:value="v.value" size="small" :placeholder="t('editor.colValue')" data-testid="vars.value"
            @input="touch" />
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
            data-testid="assert.name" @input="touch" />
          <n-input v-model:value="a.expr" size="small" class="mono" :placeholder="t('editor.assertExpr')"
            data-testid="assert.expr" @input="touch" />
          <button class="rm" type="button" :title="t('common.delete')" @click="removeAt('asserts', i)">×</button>
        </div>
        <button class="link" type="button" data-testid="assert.add" @click="addAssert">{{ t('editor.addRow') }}</button>
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
