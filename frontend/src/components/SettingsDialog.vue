<script setup lang="ts">
// 全局设置：界面（语言/缩放/响应区位置）、网络策略（Doc C5）、本地数据（Doc C10）。
import { NButton, NCheckbox, NInputNumber, NModal, NPopconfirm, NSelect } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/ipc'
import { useSettingsStore } from '@/stores/settings'
import type { Settings } from '@/types'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ 'update:show': [v: boolean]; saved: [] }>()
const { t, locale } = useI18n()
const settings = useSettingsStore()

const form = ref<Settings>({ ...settings.form })
const error = ref('')
const notice = ref('')
const saving = ref(false)

const langOptions = [
  { label: '简体中文', value: 'zh-CN' },
  { label: 'English', value: 'en-US' },
]
const scaleOptions = [
  { label: '100%', value: 1 },
  { label: '125%', value: 1.25 },
  { label: '150%', value: 1.5 },
  { label: '200%', value: 2 },
]
const layoutOptions = computed(() => [
  { label: t('settings.layoutRight'), value: 'right' },
  { label: t('settings.layoutBottom'), value: 'bottom' },
])

watch(
  () => props.show,
  (v) => {
    if (!v) return
    error.value = ''
    notice.value = ''
    form.value = { ...settings.form }
  },
)

// 语言即时生效（切换后无需保存即可看到界面变化）
function pickLang(v: string): void {
  locale.value = v
  localStorage.setItem('client.lang', v)
}

async function save(): Promise<void> {
  saving.value = true
  error.value = ''
  try {
    await settings.save({ ...form.value })
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
  <n-modal :show="show" preset="card" :title="t('settings.title')" style="width: 580px" @update:show="close">
    <div class="body">
      <p v-if="error" class="err">{{ error }}</p>
      <p v-if="notice" class="ok">{{ notice }}</p>

      <div class="sec">{{ t('settings.appearance') }}</div>
      <div class="row">
        <span class="lbl">{{ t('settings.language') }}</span>
        <n-select :value="locale" :options="langOptions" size="small" class="num" @update:value="pickLang" />
      </div>
      <div class="row">
        <span class="lbl">{{ t('settings.uiScale') }}</span>
        <n-select v-model:value="form.uiScale" :options="scaleOptions" size="small" class="num" />
      </div>
      <div class="row">
        <span class="lbl">{{ t('settings.responseLayout') }}</span>
        <n-select v-model:value="form.responseLayout" :options="layoutOptions" size="small" class="num" />
      </div>

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
  width: 130px;
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
