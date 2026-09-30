<script setup lang="ts">
// Cookie 管理（H3）：列表 + 单条删除 + 清空。数据来自持久化 Cookie 罐。
import { NButton, NModal, NPopconfirm } from 'naive-ui'
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/ipc'
import type { CookieInfo } from '@/types'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ 'update:show': [v: boolean] }>()
const { t } = useI18n()

const items = ref<CookieInfo[]>([])
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
    items.value = await api.listCookies()
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

async function removeOne(c: CookieInfo): Promise<void> {
  try {
    await api.deleteCookie(c.domain, c.name, c.path)
    items.value = items.value.filter((x) => !(x.domain === c.domain && x.name === c.name && x.path === c.path))
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

async function clearAll(): Promise<void> {
  try {
    await api.clearCookies()
    items.value = []
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

function clock(ms: number): string {
  if (!ms) return t('cookies.session')
  return new Date(ms).toLocaleString()
}
</script>

<template>
  <n-modal
    :show="show"
    preset="card"
    :title="t('cookies.title')"
    style="width: 720px"
    @update:show="emit('update:show', false)"
  >
    <div class="wrap">
      <div class="hd">
        <span class="muted">{{ t('cookies.count', { n: items.length }) }}</span>
        <span class="sp" />
        <n-popconfirm @positive-click="clearAll">
          <template #trigger>
            <n-button size="tiny" tertiary data-testid="cookies.clear">{{ t('cookies.clear') }}</n-button>
          </template>
          {{ t('cookies.clearConfirm') }}
        </n-popconfirm>
      </div>

      <p v-if="error" class="err">{{ error }}</p>
      <div v-if="!items.length" class="empty muted">{{ t('cookies.empty') }}</div>

      <table v-else class="tbl">
        <thead>
          <tr>
            <th>{{ t('cookies.name') }}</th>
            <th>{{ t('cookies.value') }}</th>
            <th>{{ t('cookies.domain') }}</th>
            <th>{{ t('cookies.path') }}</th>
            <th>{{ t('cookies.expires') }}</th>
            <th />
          </tr>
        </thead>
        <tbody>
          <tr v-for="(c, i) in items" :key="i" data-testid="cookies.row">
            <td class="mono">{{ c.name }}</td>
            <td class="mono val" :title="c.value">{{ c.value }}</td>
            <td class="mono">{{ c.domain }}</td>
            <td class="mono">{{ c.path }}</td>
            <td class="meta">{{ clock(c.expires) }}</td>
            <td class="act">
              <n-popconfirm @positive-click="removeOne(c)">
                <template #trigger>
                  <n-button size="tiny" tertiary type="error" data-testid="cookies.row.delete">
                    {{ t('cookies.delete') }}
                  </n-button>
                </template>
                {{ t('cookies.deleteConfirm', { name: c.name }) }}
              </n-popconfirm>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </n-modal>
</template>

<style scoped>
.wrap {
  display: flex;
  flex-direction: column;
  gap: 10px;
  max-height: 60vh;
  overflow: auto;
}

.hd {
  display: flex;
  align-items: center;
  gap: 8px;
}

.sp {
  flex: 1;
}

.muted {
  color: var(--app-muted);
  font-size: 12px;
}

.err {
  color: var(--app-danger, #d03050);
  margin: 0;
  font-size: 12px;
}

.empty {
  padding: 24px 0;
  text-align: center;
}

.tbl {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
}

.tbl th {
  text-align: left;
  font-weight: 500;
  color: var(--app-muted);
  padding: 6px 8px;
  border-bottom: 1px solid var(--app-border);
}

.tbl td {
  padding: 6px 8px;
  border-bottom: 1px solid var(--app-border);
  vertical-align: middle;
}

.mono {
  font-family: var(--app-mono);
}

.val {
  max-width: 180px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.meta {
  color: var(--app-muted);
  white-space: nowrap;
}

.act {
  width: 72px;
  text-align: right;
}
</style>
