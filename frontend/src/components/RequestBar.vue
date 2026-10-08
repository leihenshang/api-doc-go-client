<script setup lang="ts">
// 请求栏（design-spec §2 地址栏）：HTTP 是「方法选择器（语义色）+ URL」，gRPC 是
// 「协议徽标 + 服务/方法选择器 + 服务地址」（G1.3）；右侧统一是 格式化 / 生成代码 / Send。
import MethodTag from '@/components/MethodTag.vue'
import VarInput from '@/components/VarInput.vue'
import {
    grpcMethodFullName,
    grpcOf,
    grpcSendBlocker,
    grpcServiceShort,
    grpcStreamKey,
    isGrpc,
    splitGrpcMethod,
} from '@/lib/grpc'
import { api } from '@/lib/ipc'
import { methodColor, methodTint } from '@/lib/method'
import { message } from '@/lib/notice'
import { useCollectionStore } from '@/stores/collection'
import type { Tab } from '@/stores/tabs'
import { useTabsStore } from '@/stores/tabs'
import type { GrpcMethodInfo, RequestDoc } from '@/types'
import { CodeOutline, OptionsOutline, SendOutline, SyncOutline } from '@vicons/ionicons5'
import type { SelectOption } from 'naive-ui'
import { NIcon, NSelect } from 'naive-ui'
import { computed, h, type VNode } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ tab: Tab }>()
const emit = defineEmits<{ codegen: [] }>()
const tabs = useTabsStore()
const coll = useCollectionStore()
const { t } = useI18n()

const methods = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS'].map((m) => ({ label: m, value: m }))

const controlStyle = computed(
  () => `--vi-h:34px;--m-color:${methodColor(props.tab.request.method)};--m-tint:${methodTint(props.tab.request.method)}`,
)

// ---- gRPC 分支 ----
const isGrpcReq = computed(() => isGrpc(props.tab.request))
/** 当前请求的 grpc 段（直接改它的字段即改请求，touch() 由各 handler 负责）。 */
const grpc = computed(() => grpcOf(props.tab.request))
/** 定义里的方法：按服务分组，标签带服务短名（便于按服务名过滤）+ 流式标注。 */
const grpcMethodOptions = computed<SelectOption[]>(() =>
  (props.tab.grpcSchema?.services ?? []).map((s) => ({
    type: 'group',
    key: s.name,
    label: s.name,
    children: s.methods.map((m) => ({
      value: m.fullName,
      label:
        m.stream === 'unary'
          ? `${grpcServiceShort(s.name)}/${m.name}`
          : `${grpcServiceShort(s.name)}/${m.name} · ${t(grpcStreamKey(m.stream))}`,
    })),
  })),
)
const grpcMethodsByFullName = computed(() => {
  const map = new Map<string, GrpcMethodInfo>()
  for (const s of props.tab.grpcSchema?.services ?? []) for (const m of s.methods) map.set(m.fullName, m)
  return map
})
/** 已选方法（选中项存在时是选项值，定义缺失时退化成请求里存的 `服务.方法`）。 */
const grpcMethodValue = computed(() => grpcMethodFullName(grpc.value.service, grpc.value.method))
/** 选方法 = 写回服务 / 方法 / 流式形态（stream 供后续流式支持与保存时标注）。 */
function setGrpcMethod(value: string | null): void {
  const g = props.tab.request.grpc
  if (!g) return
  const fullName = value ?? ''
  const known = grpcMethodsByFullName.value.get(fullName)
  if (!fullName) {
    g.service = ''
    g.method = ''
    g.stream = ''
  } else if (known) {
    g.service = splitGrpcMethod(fullName).service
    g.method = known.name
    g.stream = known.stream
  } else {
    const parts = splitGrpcMethod(fullName)
    g.service = parts.service
    g.method = parts.method
    g.stream = ''
  }
  touch()
}
/** 不能发送的原因（G3.6）；HTTP 分支恒为空串。 */
const sendBlocker = computed(() =>
  isGrpcReq.value ? grpcSendBlocker(props.tab.request, !!props.tab.grpcSchema) : '',
)

// 地址栏不再单独占一行展示「替换后」文本：值只在悬停变量时给出（见 VarInput）。
// 敏感变量在提示里只显示掩码，取当前环境的 secret 标记。
const secretNames = computed(() => {
  const env = coll.info?.envs.find((e) => e.name === coll.currentEnv)
  return env?.vars.filter((v) => v.secret).map((v) => v.name) ?? []
})

// 变量自动提示候选：当前环境全部变量名 + 内置动态变量（二者在 VarInput 内部已去重排序）。
const varSuggestions = computed(() => coll.currentEnvVarNames)

// 下拉与已选值均按方法着色
function renderMethod(option: SelectOption): VNode {
  const v = String(option.value ?? '')
  return h(
    'span',
    { style: `color:${methodColor(v)};font-weight:600;font-family:var(--app-mono)` },
    String(option.label ?? v),
  )
}

function touch(): void {
  tabs.touch(props.tab.key)
}

function send(): void {
  void tabs.send(props.tab.key)
}

function cancel(): void {
  tabs.cancelSend(props.tab.key)
}

// 规范化：去首尾空白，并把 {{ var }} 收成 {{var}}（HTTP 作用在 URL，gRPC 作用在服务地址）
function formatUrl(): void {
  const r = props.tab.request
  const raw = r.grpc ? r.grpc.target : r.url
  const fixed = raw.trim().replace(/\{\{\s*([^{}]+?)\s*\}\}/g, '{{$1}}')
  if (r.grpc) r.grpc.target = fixed
  else r.url = fixed
  touch()
}

/** 把 curl 解析结果写回当前请求；草稿且尚无名字时用解析出的名字（保存对话框会预填）。 */
function applyCurl(doc: RequestDoc): void {
  const r = props.tab.request
  r.method = doc.method
  r.url = doc.url
  r.params = doc.params
  r.headers = doc.headers
  r.body = doc.body
  r.auth = doc.auth ?? { type: 'none' }
  if (props.tab.draft && !r.name.trim() && doc.name) r.name = doc.name
  touch()
}

/** 地址栏直接粘贴 curl 命令：解析后覆盖整个请求（新建草稿时最顺手的一条路径）。 */
async function onUrlPaste(e: ClipboardEvent): Promise<void> {
  const text = e.clipboardData?.getData('text') ?? ''
  if (!/^\s*curl(\.exe)?\s/i.test(text)) return
  e.preventDefault()
  try {
    const doc = await api.parseCurl(text)
    applyCurl(doc)
    message.success(t('editor.curlPasted', { method: doc.method, url: doc.url }))
  } catch (err) {
    message.error(err instanceof Error ? err.message : String(err))
  }
}
</script>

<template>
  <div class="bar-wrap">
    <div class="req-line" :style="controlStyle">
      <!-- gRPC（G1.3）：协议徽标 + 服务 / 方法选择器 + 服务地址（host:port，支持 {{变量}}） -->
      <template v-if="isGrpcReq">
        <method-tag method="GRPC" class="proto" data-testid="req.grpcBadge" />
        <!-- 未选方法时给 null：naive 用空串会当成「已选中」而吃掉 placeholder -->
        <n-select
          :value="grpcMethodValue || null"
          :options="grpcMethodOptions"
          class="gmethod"
          filterable
          clearable
          :disabled="!grpcMethodOptions.length"
          :placeholder="grpcMethodOptions.length ? t('grpc.pickMethod') : t('grpc.needProto')"
          :title="grpcMethodOptions.length ? '' : t('grpc.block.proto')"
          data-testid="req.grpcMethod"
          @update:value="setGrpcMethod"
        />
        <var-input
          v-model="grpc.target"
          data-testid="req.url"
          :placeholder="t('grpc.targetPlaceholder')"
          :vars="tab.resolve?.values ?? {}"
          :missing="tab.resolve?.missing ?? []"
          :secrets="secretNames"
          :suggestions="varSuggestions"
          @update:model-value="touch"
        />
      </template>
      <template v-else>
        <n-select
          v-model:value="tab.request.method"
          :options="methods"
          :render-label="renderMethod"
          class="method"
          data-testid="req.method"
          @update:value="touch"
        />
        <var-input
          v-model="tab.request.url"
          data-testid="req.url"
          :placeholder="t('editor.urlPlaceholder')"
          :vars="tab.resolve?.values ?? {}"
          :missing="tab.resolve?.missing ?? []"
          :secrets="secretNames"
          :suggestions="varSuggestions"
          @update:model-value="touch"
          @paste="onUrlPaste"
        />
      </template>
      <button class="icon-btn" type="button" data-testid="req.format" :title="t('editor.formatUrl')" @click="formatUrl">
        <n-icon :component="OptionsOutline" :size="16" />
      </button>
      <!-- 生成代码：HTTP 是 curl/fetch/axios/go/python，gRPC 是 grpcurl（Go 侧按协议分派，G11.5） -->
      <button
        class="icon-btn"
        type="button"
        data-testid="req.codegen"
        :title="t('codegen.title')"
        @click="emit('codegen')"
      >
        <n-icon :component="CodeOutline" :size="16" />
      </button>
      <button
        class="send"
        type="button"
        data-testid="req.send"
        :disabled="tab.sending || coll.isReadOnly || !!sendBlocker"
        :title="coll.isReadOnly ? t('sync.mirrorReadonly') : sendBlocker ? t(sendBlocker) : ''"
        @click="send"
      >
        <n-icon :component="tab.sending ? SyncOutline : SendOutline" :size="15" :class="{ spin: tab.sending }" />
        <span>{{ tab.sending ? t('editor.sending') : t('editor.send') }}</span>
      </button>
      <button
        v-if="tab.sending"
        class="cancel"
        type="button"
        data-testid="req.cancel"
        :title="t('editor.cancelSend')"
        @click="cancel"
      >
        {{ t('editor.cancelSend') }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.bar-wrap {
  flex: 0 0 auto;
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 8px 12px;
  border-bottom: 1px solid var(--app-border);
  background: var(--app-panel);
}

.req-line {
  display: flex;
  align-items: center;
  gap: 6px;
}

.method {
  width: 110px;
  flex: 0 0 auto;
}

/* 方法选择器整块统一底色，避免 arrow 区块与 label 区块色差分半 */
.method :deep(.n-base-selection) {
  background: var(--m-tint) !important;
  border-radius: 6px;
  --n-color: var(--m-tint) !important;
}

.method :deep(.n-base-selection .n-base-selection-label) {
  color: var(--m-color);
  font-family: var(--app-mono);
  font-weight: 600;
}

.method :deep(.n-base-selection-suffix),
.method :deep(.n-base-selection-arrow) {
  background: transparent;
  color: var(--m-color);
}

/* 方法选择器：选中值必须在这 34px 里垂直居中。
   naive 的 .n-base-selection-label 高度写的是 var(--n-height)，本主题下该变量没落到 label 上，
   于是退化成"一行高"（21px）贴顶 —— 实测文字中线 149.5 vs 同行 URL 输入框/发送按钮 156，偏高 6.5px。
   这里显式给出行高（--vi-h 由 .req-line 的 controlStyle 提供），naive 自带的 align-items:center 即可居中。 */
.method :deep(.n-base-selection-label) {
  height: var(--vi-h, 34px);
}

/* 协议徽标（gRPC）：占位与行高对齐方法选择器；文字色由 MethodTag 走 --app-method-grpc */
.proto {
  display: inline-flex;
  align-items: center;
  height: 34px;
  flex: 0 0 auto;
}

/* 服务 / 方法选择器：与 HTTP 方法选择器同一套底色、文字色与居中处理（注释见上） */
.gmethod {
  width: 200px;
  flex: 0 0 auto;
}

.gmethod :deep(.n-base-selection) {
  background: var(--m-tint) !important;
  border-radius: 6px;
  --n-color: var(--m-tint) !important;
}

.gmethod :deep(.n-base-selection .n-base-selection-label) {
  color: var(--m-color);
  font-family: var(--app-mono);
  font-size: 12px;
  font-weight: 600;
}

.gmethod :deep(.n-base-selection-suffix),
.gmethod :deep(.n-base-selection-arrow) {
  background: transparent;
  color: var(--m-color);
}

.gmethod :deep(.n-base-selection-label) {
  height: var(--vi-h, 34px);
}

.icon-btn {
  flex: 0 0 auto;
  width: 34px;
  height: 34px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  background: var(--app-panel);
  color: var(--app-muted);
  cursor: pointer;
}

.icon-btn:hover {
  border-color: var(--app-accent);
  color: var(--app-accent);
}

.send {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 34px;
  border: none;
  background: var(--app-send);
  color: var(--app-on-accent);
  font-family: inherit;
  font-weight: 600;
  font-size: 13px;
  padding: 0 18px;
  border-radius: 6px;
  cursor: pointer;
}

.send:hover:not(:disabled) {
  background: var(--app-send-hover);
}

.send:disabled {
  opacity: 0.75;
  cursor: default;
}

.cancel {
  flex: 0 0 auto;
  height: 34px;
  border: 1px solid var(--app-border);
  background: var(--app-panel);
  color: var(--app-danger, #d03050);
  font-family: inherit;
  font-weight: 600;
  font-size: 13px;
  padding:0 14px;
  border-radius: 6px;
  cursor: pointer;
}

.cancel:hover {
  border-color: var(--app-danger, #d03050);
}

.spin {
  animation: rot 0.9s linear infinite;
}

@keyframes rot {
  to {
    transform: rotate(360deg);
  }
}

</style>
