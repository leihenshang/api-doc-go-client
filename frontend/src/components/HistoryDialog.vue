<script setup lang="ts">
// 请求历史（Doc C10）：点击记录可回到对应请求；记录只存元信息与状态。
import { NButton, NModal, NPopconfirm } from 'naive-ui'
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import MethodTag from '@/components/MethodTag.vue'
import { api } from '@/lib/ipc'
import type { HistoryEntry } from '@/types'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ 'update:show': [v: boolean]; open: [uid: string] }>()
const { t } = useI18n()

const items = ref<HistoryEntry[]>([])
const error = ref('')

watch(
  () => props.show,
  async (v) => {
    if (v) await reload()
  },
)

async function reload(): Promise<void> {
  error.value = ''
  try {
    items.value = await api.listHistory(300)
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

async function clearAll(): Promise<void> {
  try {
    await api.clearHistory()
    items.value = []
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

function pick(e: HistoryEntry): void {
  if (!e.uid) return
  emit('open', e.uid)
  emit('update:show', false)
}

function clock(ms: number): string {
  return new Date(ms).toLocaleString()
}

function statusClass(e: HistoryEntry): string {
  if (e.error) return 'err'
  if (e.status >= 200 && e.status < 400) return 'ok'
  return 'warn'
}
</script>

<template>
  <n-modal :show="show" preset="card" :title="t('history.title')" style="width: 720px" @update:show="emit('update:show', false)">
    <div class="wrap">
      <div class="hd">
        <span class="muted">{{ t('history.count', { n: items.length }) }}</span>
        <span class="sp" />
        <n-popconfirm @positive-click="clearAll">
          <template #trigger>
            <n-button size="tiny" tertiary>{{ t('history.clear') }}</n-button>
          </template>
          {{ t('history.clearConfirm') }}
        </n-popconfirm>
      </div>

      <p v-if="error" class="err">{{ error }}</p>
      <div v-if="!items.length" class="empty muted">{{ t('history.empty') }}</div>

      <div v-for="(e, i) in items" :key="i" class="row" data-testid="history.row" :class="{ clickable: !!e.uid }" @click="pick(e)">
        <method-tag :method="e.method || 'GET'" />
        <span class="url mono" :title="e.url">{{ e.url }}</span>
        <span class="st" :class="statusClass(e)">{{ e.error ? t('history.failed') : e.status }}</span>
        <span class="meta">{{ e.timeMs }} ms</span>
        <span class="meta">{{ e.size }} B</span>
        <span class="time muted">{{ clock(e.time) }}</span>
      </div>
    </div>
  </n-modal>
</template>

<style scoped>
.wrap {
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 60vh;
  overflow: auto;
}

.hd {
  display: flex;
  align-items: center;
  gap: 8px;
}

.sp {
  flex: 1 1 auto;
}

.row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 5px 6px;
  border-radius: 5px;
  font-size: 12.5px;
}

.row.clickable {
  cursor: pointer;
}

.row:hover {
  background: var(--app-row-hover);
}

.url {
  flex: 1 1 auto;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.st {
  font-size: 11px;
  padding: 0 5px;
  border-radius: 3px;
  flex: 0 0 auto;
}

.st.ok {
  background: var(--app-accent-tint);
  color: var(--app-accent-dark);
}

.st.warn {
  background: var(--app-warn-tint);
  color: var(--app-warn);
}

.st.err {
  background: var(--app-danger-tint);
  color: var(--app-danger);
}

.meta,
.time {
  font-size: 11.5px;
  flex: 0 0 auto;
}

.empty {
  padding: 24px;
  text-align: center;
}

.err {
  color: var(--app-danger);
  font-size: 12.5px;
  margin: 0;
}
</style>
