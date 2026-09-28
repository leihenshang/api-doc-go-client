<script setup lang="ts">
// 全局设置：发送策略（证书/超时/重定向，Doc C5）与本地数据（Cookie/历史，Doc C10）。
import { NButton, NCheckbox, NInputNumber, NModal, NPopconfirm } from 'naive-ui'
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/ipc'
import type { Settings } from '@/types'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ 'update:show': [v: boolean]; saved: [] }>()
const { t } = useI18n()

const defaults: Settings = {
  insecureSsl: false,
  timeoutSec: 30,
  followRedirects: true,
  maxRedirects: 5,
  persistCookies: true,
  historyLimit: 200,
}

const form = ref<Settings>({ ...defaults })
const error = ref('')
const notice = ref('')
const saving = ref(false)

watch(
  () => props.show,
  async (v) => {
    if (!v) return
    error.value = ''
    notice.value = ''
    try {
      form.value = { ...defaults, ...(await api.getSettings()) }
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    }
  },
)

async function save(): Promise<void> {
  saving.value = true
  error.value = ''
  try {
    await api.saveSettings({ ...form.value })
    emit('saved')
    emit('update:show', false)
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    saving.value = false
  }
}

async function clearCookies(): Promise<void> {
  error.value = ''
  try {
    await api.clearCookies()
    notice.value = t('settings.cookiesCleared')
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

function close(): void {
  emit('update:show', false)
}
</script>

<template>
  <n-modal :show="show" preset="card" :title="t('settings.title')" style="width: 560px" @update:show="close">
    <div class="body">
      <p v-if="error" class="err">{{ error }}</p>
      <p v-if="notice" class="ok">{{ notice }}</p>

      <div class="sec">{{ t('settings.network') }}</div>
      <div class="row">
        <n-checkbox v-model:checked="form.insecureSsl">{{ t('settings.insecureSsl') }}</n-checkbox>
      </div>
      <p class="hint muted">{{ t('settings.insecureSslHint') }}</p>

      <div class="row">
        <span class="lbl">{{ t('settings.timeout') }}</span>
        <n-input-number v-model:value="form.timeoutSec" size="small" :min="1" :max="600" class="num" />
      </div>

      <div class="row">
        <n-checkbox v-model:checked="form.followRedirects">{{ t('settings.followRedirects') }}</n-checkbox>
      </div>
      <div class="row">
        <span class="lbl">{{ t('settings.maxRedirects') }}</span>
        <n-input-number
          v-model:value="form.maxRedirects"
          size="small"
          :min="1"
          :max="50"
          :disabled="!form.followRedirects"
          class="num"
        />
      </div>

      <div class="sec">{{ t('settings.local') }}</div>
      <div class="row">
        <n-checkbox v-model:checked="form.persistCookies">{{ t('settings.persistCookies') }}</n-checkbox>
        <span class="sp" />
        <n-popconfirm @positive-click="clearCookies">
          <template #trigger>
            <n-button size="tiny" tertiary>{{ t('settings.clearCookies') }}</n-button>
          </template>
          {{ t('common.confirm') }}？
        </n-popconfirm>
      </div>
      <div class="row">
        <span class="lbl">{{ t('settings.historyLimit') }}</span>
        <n-input-number v-model:value="form.historyLimit" size="small" :min="10" :max="5000" class="num" />
      </div>
    </div>

    <template #footer>
      <div class="ft">
        <n-button size="small" @click="close">{{ t('common.cancel') }}</n-button>
        <n-button size="small" type="primary" :loading="saving" @click="save">{{ t('common.save') }}</n-button>
      </div>
    </template>
  </n-modal>
</template>

<style scoped>
.body {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.sec {
  margin-top: 6px;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 1px;
  text-transform: uppercase;
  color: var(--app-muted);
}

.row {
  display: flex;
  align-items: center;
  gap: 10px;
}

.lbl {
  font-size: 13px;
  width: 130px;
}

.num {
  width: 120px;
}

.hint {
  font-size: 11.5px;
  margin: 0 0 2px;
}

.err {
  color: #d03050;
  font-size: 12.5px;
  margin: 0;
}

.ok {
  color: var(--app-accent);
  font-size: 12.5px;
  margin: 0;
}

.sp {
  flex: 1 1 auto;
}

.ft {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
