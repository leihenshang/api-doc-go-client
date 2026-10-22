<script setup lang="ts">
// 集合级公共请求参数（清单 defaults 段）：集合内所有 HTTP 请求共享的请求头与认证。
// 同名请求头由请求覆盖；认证仅在请求选择「继承集合默认」或未设置时生效。
import KeyValueTable from '@/components/KeyValueTable.vue'
import { api } from '@/lib/ipc'
import { message } from '@/lib/notice'
import type { Auth, CollectionDefaults, KV } from '@/types'
import { NButton, NInput, NModal, NSelect } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ 'update:show': [v: boolean]; saved: [] }>()
const { t } = useI18n()

const headers = ref<KV[]>([])
const auth = ref<Auth>({ type: 'none' })
const busy = ref(false)
const error = ref('')

const authType = computed({
  get: () => auth.value.type || 'none',
  set: (v: string) => {
    auth.value = { ...auth.value, type: v }
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

watch(
  () => props.show,
  async (v) => {
    if (v) await load()
  },
)

async function load(): Promise<void> {
  error.value = ''
  try {
    const d = await api.getCollectionDefaults()
    headers.value = (d.headers ?? []).map((h) => ({ ...h }))
    auth.value = { type: 'none', ...(d.auth ?? {}) }
    if (!auth.value.type) auth.value.type = 'none'
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

async function save(): Promise<void> {
  busy.value = true
  error.value = ''
  try {
    const d: CollectionDefaults = {
      headers: headers.value,
      auth: authType.value === 'none' ? null : auth.value,
    }
    await api.setCollectionDefaults(d)
    message.success(t('collDefaults.saved'))
    emit('saved')
    emit('update:show', false)
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <n-modal :show="show" preset="card" :title="t('collDefaults.title')" style="width: 680px"
    data-testid="collDefaults.dialog" @update:show="emit('update:show', false)">
    <div class="wrap">
      <p class="hint">{{ t('collDefaults.hint') }}</p>

      <section class="sec">
        <h4 class="sec-title">{{ t('collDefaults.headers') }}</h4>
        <key-value-table :rows="headers" :label="t('collDefaults.headers')" />
      </section>

      <section class="sec">
        <h4 class="sec-title">{{ t('collDefaults.auth') }}</h4>
        <div class="arow">
          <span class="lbl">{{ t('auth.type') }}</span>
          <n-select v-model:value="authType" :options="authOptions" size="small" class="atype"
            data-testid="collDefaults.auth.type" />
        </div>
        <template v-if="authType === 'basic'">
          <div class="arow">
            <span class="lbl">{{ t('auth.username') }}</span>
            <n-input v-model:value="auth.username" size="small" data-testid="collDefaults.auth.username" />
          </div>
          <div class="arow">
            <span class="lbl">{{ t('auth.password') }}</span>
            <n-input v-model:value="auth.password" size="small" type="password" show-password-on="click"
              data-testid="collDefaults.auth.password" />
          </div>
        </template>
        <template v-else-if="authType === 'bearer'">
          <div class="arow">
            <span class="lbl">{{ t('auth.token') }}</span>
            <n-input v-model:value="auth.token" size="small" :placeholder="'{{token}}'"
              data-testid="collDefaults.auth.token" />
          </div>
        </template>
        <template v-else-if="authType === 'apikey'">
          <div class="arow">
            <span class="lbl">{{ t('auth.key') }}</span>
            <n-input v-model:value="auth.key" size="small" data-testid="collDefaults.auth.key" />
          </div>
          <div class="arow">
            <span class="lbl">{{ t('auth.value') }}</span>
            <n-input v-model:value="auth.value" size="small" :placeholder="'{{apiKey}}'"
              data-testid="collDefaults.auth.value" />
          </div>
          <div class="arow">
            <span class="lbl">{{ t('auth.in') }}</span>
            <n-select v-model:value="auth.in" :options="authInOptions" size="small" class="atype"
              data-testid="collDefaults.auth.in" />
          </div>
        </template>
      </section>

      <p v-if="error" class="err" data-testid="collDefaults.error">{{ error }}</p>
    </div>

    <template #footer>
      <div class="foot">
        <n-button size="small" data-testid="collDefaults.cancel" @click="emit('update:show', false)">
          {{ t('common.cancel') }}
        </n-button>
        <n-button size="small" type="primary" :loading="busy" data-testid="collDefaults.save" @click="save">
          {{ t('common.save') }}
        </n-button>
      </div>
    </template>
  </n-modal>
</template>

<style scoped>
.wrap {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.hint {
  margin: 0;
  font-size: 12px;
  color: var(--app-text-dim);
  line-height: 1.6;
}

.sec {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.sec-title {
  margin: 0;
  font-size: 12.5px;
  font-weight: 600;
  color: var(--app-text);
}

.arow {
  display: grid;
  grid-template-columns: 120px minmax(0, 1fr);
  align-items: center;
  gap: 10px;
}

.lbl {
  font-size: 12px;
  color: var(--app-text-dim);
}

.atype {
  width: 200px;
}

.err {
  margin: 0;
  font-size: 12px;
  color: var(--app-danger);
}

.foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
