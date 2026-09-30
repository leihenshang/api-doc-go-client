<script setup lang="ts">
// 本地 Mock（H7）：启动/停止本地服务，按请求方法+路径匹配，回放示例或合成 JSON。
import { NButton, NInputNumber, NModal } from 'naive-ui'
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/ipc'
import { message } from '@/lib/notice'
import type { MockStatus } from '@/types'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ 'update:show': [v: boolean] }>()
const { t } = useI18n()

const port = ref(0)
const status = ref<MockStatus | null>(null)
const error = ref('')
const busy = ref(false)

watch(
  () => props.show,
  async (v) => {
    if (v) await refresh()
  },
)

async function refresh(): Promise<void> {
  error.value = ''
  try {
    status.value = await api.mockStatus()
    if (status.value?.port) port.value = status.value.port
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

async function start(): Promise<void> {
  busy.value = true
  error.value = ''
  try {
    status.value = await api.startMock(port.value || 0)
    message.success(t('mock.started', { url: status.value?.url ?? '' }))
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function stop(): Promise<void> {
  busy.value = true
  error.value = ''
  try {
    await api.stopMock()
    status.value = await api.mockStatus()
    message.success(t('mock.stopped'))
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function copyUrl(): Promise<void> {
  if (!status.value?.url) return
  await navigator.clipboard.writeText(status.value.url)
  message.success(t('common.copied'))
}
</script>

<template>
  <n-modal
    :show="show"
    preset="card"
    :title="t('mock.title')"
    style="width: 520px"
    @update:show="emit('update:show', false)"
  >
    <div class="wrap">
      <p class="hint">{{ t('mock.hint') }}</p>
      <div class="row">
        <span class="lbl">{{ t('mock.port') }}</span>
        <n-input-number v-model:value="port" size="small" :min="0" :max="65535" class="num" data-testid="mock.port" />
      </div>
      <div class="row">
        <template v-if="status?.running">
          <span class="ok" data-testid="mock.url">{{ status.url }}</span>
          <n-button size="small" @click="copyUrl">{{ t('codegen.copy') }}</n-button>
          <span class="sp" />
          <n-button size="small" type="error" :loading="busy" data-testid="mock.stop" @click="stop">
            {{ t('mock.stop') }}
          </n-button>
        </template>
        <template v-else>
          <span class="muted">{{ t('mock.notRunning') }}</span>
          <span class="sp" />
          <n-button size="small" type="primary" :loading="busy" data-testid="mock.start" @click="start">
            {{ t('mock.start') }}
          </n-button>
        </template>
      </div>
      <p v-if="status?.running" class="muted">{{ t('mock.hits', { n: status.hits }) }}</p>
      <p v-if="error" class="err">{{ error }}</p>
    </div>
  </n-modal>
</template>

<style scoped>
.wrap {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.hint {
  margin: 0;
  font-size: 12px;
  color: var(--app-muted);
}

.row {
  display: flex;
  align-items: center;
  gap: 10px;
}

.lbl {
  width: 48px;
  font-size: 12px;
  color: var(--app-muted);
}

.num {
  width: 140px;
}

.sp {
  flex: 1;
}

.ok {
  font-family: var(--app-mono);
  color: var(--app-accent);
}

.muted {
  color: var(--app-muted);
  font-size: 12px;
}

.err {
  margin: 0;
  color: var(--app-danger, #d03050);
  font-size: 12px;
}
</style>
