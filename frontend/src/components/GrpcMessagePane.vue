<script setup lang="ts">
// gRPC Message 分段（设计文档 G5）：请求消息（protojson 文本）的编辑、按 proto 生成样例、
// 按定义实时校验（G5.3，错误带字段路径）与字段提示（G5.4，点击插入）。
// 与 Go 侧分工：样例生成、字段枚举、校验都在 Go（proto 描述符在手），前端只管编辑与展示。
import { NButton, NInput, NPopconfirm, NSpin } from 'naive-ui'
import type { InputInst } from 'naive-ui'
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { grpcOf } from '@/lib/grpc'
import { api } from '@/lib/ipc'
import { message } from '@/lib/notice'
import { useCollectionStore } from '@/stores/collection'
import { useTabsStore } from '@/stores/tabs'
import type { Tab } from '@/stores/tabs'
import type { GrpcFieldInfo } from '@/types'

const props = defineProps<{ tab: Tab }>()
const tabs = useTabsStore()
const coll = useCollectionStore()
const { t } = useI18n()

const CHECK_DEBOUNCE = 350

const inputRef = ref<InputInst | null>(null)
const fields = ref<GrpcFieldInfo[]>([])
const checkError = ref('')
const checking = ref(false)
let checkTimer: ReturnType<typeof setTimeout> | null = null

const grpc = computed(() => grpcOf(props.tab.request))
const readonly = computed(() => coll.isReadOnly)
const protoRel = computed(() => (grpc.value.proto ?? '').trim())
const hasMethod = computed(() => !!protoRel.value && !!grpc.value.service.trim() && !!grpc.value.method.trim())
const imports = computed(() => grpc.value.imports ?? [])

/** 请求消息：写回草稿即标脏（自动保存），并顺带触发一次按定义校验。 */
const body = computed({
  get: () => grpc.value.message ?? '',
  set: (v: string) => {
    grpc.value.message = v
    touch()
    scheduleCheck()
  },
})

function touch(): void {
  tabs.touch(props.tab.key)
}

/** 按定义校验（防抖）：结果内联展示，错误带字段路径（G5.3）。 */
function scheduleCheck(): void {
  if (checkTimer) clearTimeout(checkTimer)
  checkTimer = setTimeout(() => void checkNow(), CHECK_DEBOUNCE)
}

async function checkNow(): Promise<void> {
  if (!hasMethod.value) {
    checkError.value = ''
    return
  }
  checking.value = true
  try {
    checkError.value = await api.grpcValidateMessage(
      protoRel.value,
      imports.value,
      grpc.value.service,
      grpc.value.method,
      grpc.value.message ?? '',
    )
  } catch (e) {
    // 定义本身有问题（文件被移除 / 解析失败）：当提示展示，不阻断编辑
    checkError.value = e instanceof Error ? e.message : String(e)
  } finally {
    checking.value = false
  }
}

async function loadFields(): Promise<void> {
  if (!hasMethod.value) {
    fields.value = []
    return
  }
  try {
    fields.value = await api.grpcMessageFields(
      protoRel.value,
      imports.value,
      grpc.value.service,
      grpc.value.method,
    )
  } catch {
    fields.value = []
  }
}

function refresh(): void {
  void loadFields()
  void checkNow()
}

onMounted(refresh)
// 换 tab / 换方法 / 换定义都要重新取字段与校验
watch(() => [props.tab.key, protoRel.value, grpc.value.service, grpc.value.method].join('|'), refresh)

/** 按 proto 生成样例（G5.2）：已有内容时先确认，避免误覆盖。 */
async function sample(): Promise<void> {
  if (readonly.value || !hasMethod.value) return
  try {
    const text = await api.grpcSampleMessage(protoRel.value, imports.value, grpc.value.service, grpc.value.method)
    body.value = text
    await nextTick()
    void checkNow()
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

/** 一键美化：非法 JSON 原样保留并提示（与 HTTP 请求体的处理一致）。 */
function formatJson(): void {
  if (readonly.value) return
  try {
    body.value = JSON.stringify(JSON.parse(body.value), null, 2)
  } catch {
    message.warning(t('grpc.msg.jsonInvalid'))
  }
}

/** 字段占位值：按类型给一个**合法**的默认值，用户只改内容不改结构。 */
function placeholderOf(f: GrpcFieldInfo): string {
  if (f.repeated) return '[]'
  switch (f.kind) {
    case 'map':
    case 'message':
      return '{}'
    case 'enum':
      return '0' // protojson 接受枚举数字；首个枚举值恒为 0
    default:
      return f.type === 'bool' ? 'false' : f.type.startsWith('int') || f.type.startsWith('uint') || f.type.startsWith('float') || f.type.startsWith('double') ? '0' : '""'
  }
}

/** 点击字段 → 在光标处插入 `"jsonName": 默认值`（空消息时给出完整骨架）。 */
async function insertField(f: GrpcFieldInfo): Promise<void> {
  if (readonly.value) return
  const snippet = `"${f.jsonName}": ${placeholderOf(f)}`
  const textarea = inputRef.value?.textareaElRef
  if (!body.value.trim()) {
    body.value = `{\n  ${snippet}\n}`
  } else if (textarea) {
    const start = textarea.selectionStart ?? body.value.length
    const end = textarea.selectionEnd ?? start
    body.value = body.value.slice(0, start) + snippet + body.value.slice(end)
    await nextTick()
    const caret = start + snippet.length
    textarea.focus()
    textarea.setSelectionRange(caret, caret)
  } else {
    body.value = `${body.value}\n${snippet}`
  }
  scheduleCheck()
}
</script>

<template>
  <div class="msg-pane">
    <div class="tool">
      <n-popconfirm v-if="body.trim()" :disabled="readonly || !hasMethod" @positive-click="sample">
        <template #trigger>
          <n-button size="small" type="primary" :disabled="readonly || !hasMethod" data-testid="grpc.message.sample">
            {{ t('grpc.msg.sample') }}
          </n-button>
        </template>
        <span>{{ t('grpc.msg.sampleConfirm') }}</span>
      </n-popconfirm>
      <n-button
        v-else
        size="small"
        type="primary"
        :disabled="readonly || !hasMethod"
        data-testid="grpc.message.sample"
        @click="sample"
      >
        {{ t('grpc.msg.sample') }}
      </n-button>
      <n-button
        size="small"
        quaternary
        :disabled="readonly || !body.trim()"
        data-testid="grpc.message.format"
        @click="formatJson"
      >
        {{ t('grpc.msg.format') }}
      </n-button>
      <span class="sp" />
      <span v-if="!hasMethod" class="note">{{ t('grpc.msg.needMethod') }}</span>
      <span v-else-if="checking" class="note" data-testid="grpc.message.checking">
        <n-spin :size="12" />{{ t('grpc.msg.checking') }}
      </span>
      <span v-else-if="checkError" class="note bad" data-testid="grpc.message.error">{{ checkError }}</span>
      <span v-else class="note ok" data-testid="grpc.message.ok">{{ t('grpc.msg.valid') }}</span>
    </div>

    <div class="split">
      <n-input
        ref="inputRef"
        v-model:value="body"
        type="textarea"
        class="mono editor"
        :rows="14"
        :disabled="readonly"
        :placeholder="t('grpc.msg.placeholder')"
        data-testid="grpc.message.input"
      />

      <div class="fields" data-testid="grpc.message.fields">
        <div class="fh">
          <span>{{ t('grpc.msg.fields') }}</span>
          <span v-if="fields.length" class="num">{{ fields.length }}</span>
        </div>
        <div v-if="fields.length" class="flist">
          <button
            v-for="f in fields"
            :key="f.name"
            class="frow"
            type="button"
            :title="f.comment ? `${f.jsonName}: ${f.type}\n${f.comment}` : `${f.jsonName}: ${f.type}`"
            :data-testid="`grpc.field.${f.name}`"
            @click="insertField(f)"
          >
            <span class="f-n mono">{{ f.jsonName }}</span>
            <span class="f-t mono">{{ f.type }}</span>
          </button>
        </div>
        <p v-else-if="hasMethod" class="f-empty">{{ t('grpc.msg.fieldsEmpty') }}</p>
        <p v-else class="f-empty">{{ t('grpc.msg.needMethod') }}</p>
        <p class="f-hint">{{ t('grpc.msg.insertHint') }}</p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.msg-pane {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px 12px;
  min-height: 0;
  flex: 1 1 auto;
}

.tool {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  flex: 0 0 auto;
}

.sp {
  flex: 1 1 auto;
}

.note {
  font-size: 11.5px;
  color: var(--app-muted);
}

.note.ok {
  color: var(--app-accent);
}

.note.bad {
  color: var(--app-danger);
  max-width: 60%;
  text-align: right;
  word-break: break-all;
}

.split {
  display: flex;
  gap: 10px;
  align-items: stretch;
  min-height: 0;
  flex: 1 1 auto;
}

.editor {
  flex: 1 1 auto;
  min-width: 0;
}

.editor :deep(textarea) {
  font-size: 12.5px;
  line-height: 1.65;
}

.fields {
  width: 260px;
  flex: 0 0 auto;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.fh {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 8px;
  border-bottom: 1px solid var(--app-border);
  font-size: 12px;
  font-weight: 600;
  color: var(--app-text);
  flex: 0 0 auto;
}

.num {
  display: inline-block;
  min-width: 16px;
  padding: 0 5px;
  border-radius: 999px;
  background: var(--app-chip);
  font-size: 10.5px;
  font-weight: 500;
  line-height: 15px;
  color: var(--app-muted);
  text-align: center;
}

.flist {
  overflow: auto;
  padding: 4px;
  min-height: 0;
}

.frow {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  border: none;
  background: none;
  border-radius: 4px;
  padding: 4px 6px;
  font-family: inherit;
  text-align: left;
  cursor: pointer;
}

.frow:hover {
  background: var(--app-row-hover);
}

.f-n {
  font-size: 12px;
  color: var(--app-accent-dark);
  flex: 0 0 auto;
}

.f-t {
  flex: 1 1 auto;
  min-width: 0;
  font-size: 11px;
  color: var(--app-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.f-empty,
.f-hint {
  margin: 0;
  padding: 6px 8px;
  font-size: 11.5px;
  color: var(--app-muted);
  line-height: 1.6;
}

.f-hint {
  margin-top: auto;
  border-top: 1px solid var(--app-border);
}
</style>
