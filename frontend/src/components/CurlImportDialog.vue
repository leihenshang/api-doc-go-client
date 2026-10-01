<script setup lang="ts">
// 「导入 cURL」：粘贴 curl 命令 → 实时解析预览（方法 / URL / 请求头 / 参数 / 请求体 / 认证）→
// 开成一个未落盘的草稿 tab（保存时机与分组由关闭时的提示决定，见 stores/tabs.openDraft）。
import { NButton, NInput, NModal, NTag } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/ipc'
import type { RequestDoc } from '@/types'

const props = defineProps<{ show: boolean; folder?: string }>()
const emit = defineEmits<{ 'update:show': [v: boolean]; imported: [doc: RequestDoc, folder: string] }>()
const { t } = useI18n()

const text = ref('')
const parsed = ref<RequestDoc | null>(null)
const error = ref('')
const parsing = ref(false)
let timer: ReturnType<typeof setTimeout> | null = null

/** 解析预览：方法 + 路径 + 各类行数（解析失败时只有错误提示）。 */
const summary = computed(() => {
  const p = parsed.value
  if (!p) return null
  return {
    method: p.method,
    url: p.url,
    headers: p.headers.filter((h) => h.name.trim()).length,
    params: p.params.filter((x) => x.name.trim()).length,
    body: p.body.type,
    form: p.body.form.filter((f) => f.name.trim()).length,
    auth: p.auth?.type ?? 'none',
  }
})

async function parseNow(): Promise<void> {
  parsing.value = true
  error.value = ''
  try {
    parsed.value = await api.parseCurl(text.value)
  } catch (e) {
    parsed.value = null
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    parsing.value = false
  }
}

watch(
  () => text.value,
  () => {
    if (timer) clearTimeout(timer)
    if (!text.value.trim()) {
      parsed.value = null
      error.value = ''
      return
    }
    timer = setTimeout(() => void parseNow(), 250)
  },
)

watch(
  () => props.show,
  (v) => {
    if (!v) return
    error.value = ''
  },
)

/** 关闭弹窗并重置（成功导入与取消都要清干净，避免下次打开看到上次内容）。 */
function close(): void {
  emit('update:show', false)
  text.value = ''
  parsed.value = null
  error.value = ''
}

function submit(): void {
  if (!parsed.value) return
  emit('imported', parsed.value, props.folder ?? '')
  close()
}
</script>

<template>
  <n-modal
    :show="props.show"
    preset="card"
    :title="t('curl.title')"
    style="width: 620px"
    @update:show="(v: boolean) => (v ? undefined : close())"
  >
    <p class="hint">{{ t('curl.hint') }}</p>
    <n-input
      v-model:value="text"
      type="textarea"
      :rows="6"
      class="mono ta"
      :placeholder="t('curl.placeholder')"
      data-testid="curl.text"
    />

    <div v-if="error" class="err" data-testid="curl.error">{{ error }}</div>

    <div v-else-if="summary" class="preview" data-testid="curl.preview">
      <div class="prow">
        <span class="k">{{ t('curl.url') }}</span>
        <span class="v mono">
          <n-tag size="small" :bordered="false" class="m">{{ summary.method }}</n-tag>
          {{ summary.url }}
        </span>
      </div>
      <div class="prow">
        <span class="k">{{ t('curl.detail') }}</span>
        <span class="v">
          {{ t('curl.headers', { n: summary.headers }) }} · {{ t('curl.params', { n: summary.params }) }} ·
          {{ t('curl.body', { type: summary.body }) }}<template v-if="summary.form"> ({{ summary.form }})</template> ·
          {{ t('curl.auth', { type: summary.auth }) }}
        </span>
      </div>
    </div>

    <template #footer>
      <div class="ft">
        <n-button size="small" data-testid="curl.cancel" @click="close">{{ t('common.cancel') }}</n-button>
        <n-button
          size="small"
          type="primary"
          :disabled="!parsed"
          :loading="parsing"
          data-testid="curl.ok"
          @click="submit"
        >
          {{ t('curl.import') }}
        </n-button>
      </div>
    </template>
  </n-modal>
</template>

<style scoped>
.hint {
  margin: 0 0 8px;
  font-size: 12px;
  color: var(--app-muted);
}

.ta :deep(textarea) {
  font-family: var(--app-mono);
  font-size: 12px;
  line-height: 1.6;
}

.err {
  margin-top: 10px;
  padding: 8px 10px;
  border-radius: 6px;
  background: var(--app-danger-tint);
  color: var(--app-danger);
  font-size: 12px;
}

.preview {
  margin-top: 10px;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  padding: 8px 10px;
  background: var(--app-panel);
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.prow {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  font-size: 12px;
}

.k {
  flex: 0 0 auto;
  width: 52px;
  color: var(--app-muted);
}

.v {
  flex: 1 1 auto;
  min-width: 0;
  color: var(--app-text);
  word-break: break-all;
}

.m {
  font-family: var(--app-mono);
  font-weight: 600;
  margin-right: 4px;
}

.ft {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
