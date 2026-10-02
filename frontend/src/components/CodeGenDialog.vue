<script setup lang="ts">
// 代码生成（H6 / G11.5）：按当前草稿 + 环境渲染变量。
// HTTP 生成 curl / fetch / axios / go / python；gRPC 只生成 grpcurl（别的模板对 gRPC 没有意义）。
import { NButton, NModal, NSelect } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/ipc'
import { message } from '@/lib/notice'
import { useCollectionStore } from '@/stores/collection'
import type { RequestDoc } from '@/types'

const props = defineProps<{ show: boolean; request: RequestDoc | null }>()
const emit = defineEmits<{ 'update:show': [v: boolean] }>()
const { t } = useI18n()
const coll = useCollectionStore()

const lang = ref('curl')
const code = ref('')
const error = ref('')
const loading = ref(false)

/** gRPC 请求（grpc 段存在）：语言列表只剩 grpcurl。 */
const isGrpc = computed(() => !!props.request?.grpc)

const langOptions = computed(() =>
  isGrpc.value
    ? [{ label: 'grpcurl', value: 'grpcurl' }]
    : [
        { label: 'cURL', value: 'curl' },
        { label: 'fetch', value: 'fetch' },
        { label: 'axios', value: 'axios' },
        { label: 'Go', value: 'go' },
        { label: 'Python', value: 'python' },
      ],
)

async function gen(): Promise<void> {
  if (!props.request) return
  loading.value = true
  error.value = ''
  try {
    code.value = await api.generateCode(lang.value, coll.currentEnv, props.request)
  } catch (e) {
    code.value = ''
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

watch(
  () => [props.show, lang.value] as const,
  ([show]) => {
    if (!show) return
    // 协议与语言不匹配时先切到该协议的默认语言（会再次触发本 watch 完成生成）
    if (isGrpc.value && lang.value !== 'grpcurl') {
      lang.value = 'grpcurl'
      return
    }
    if (!isGrpc.value && lang.value === 'grpcurl') {
      lang.value = 'curl'
      return
    }
    void gen()
  },
)

async function copy(): Promise<void> {
  if (!code.value) return
  await navigator.clipboard.writeText(code.value)
  message.success(t('common.copied'))
}
</script>

<template>
  <n-modal
    :show="show"
    preset="card"
    :title="t('codegen.title')"
    style="width: 720px"
    @update:show="emit('update:show', false)"
  >
    <div class="wrap">
      <div class="hd">
        <n-select
          v-model:value="lang"
          :options="langOptions"
          size="small"
          class="lang"
          data-testid="codegen.lang"
        />
        <span class="sp" />
        <n-button size="small" data-testid="codegen.copy" @click="copy">{{ t('codegen.copy') }}</n-button>
      </div>
      <p v-if="error" class="err">{{ error }}</p>
      <pre class="code mono" data-testid="codegen.body">{{ code || t('codegen.empty') }}</pre>
    </div>
  </n-modal>
</template>

<style scoped>
.wrap {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.hd {
  display: flex;
  align-items: center;
  gap: 8px;
}

.lang {
  width: 140px;
}

.sp {
  flex: 1;
}

.err {
  margin: 0;
  color: var(--app-danger, #d03050);
  font-size: 12px;
}

.code {
  margin: 0;
  padding: 12px;
  background: var(--app-surface-2);
  border: 1px solid var(--app-border);
  border-radius: 6px;
  font-size: 12px;
  line-height: 1.6;
  max-height: 50vh;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
