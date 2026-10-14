<script setup lang="ts">
// 请求历史（Doc C10）：点击记录可回到对应请求；「详情」展开当时的请求参数与响应（item 19）。
// 重放功能已下线：历史快照只作回看，不再原样重发。
import MethodTag from '@/components/MethodTag.vue'
import { api } from '@/lib/ipc'
import type { HistoryEntry } from '@/types'
import { NButton, NModal, NPopconfirm } from 'naive-ui'
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ 'update:show': [v: boolean]; open: [uid: string] }>()
const { t } = useI18n()

const items = ref<HistoryEntry[]>([])
const error = ref('')
/** 展开详情的行下标（-1 = 全部收起；单开互斥，避免弹窗被撑爆） */
const expanded = ref(-1)

watch(
  () => props.show,
  async (v) => {
    if (v) await reload()
  },
)

async function reload(): Promise<void> {
  error.value = ''
  expanded.value = -1
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

/** 展开/收起详情：不触发行点击的「跳到请求」语义。 */
function toggleDetail(i: number): void {
  expanded.value = expanded.value === i ? -1 : i
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

      <div v-for="(e, i) in items" :key="i" class="item">
        <div class="row" data-testid="history.row" :class="{ clickable: !!e.uid }" @click="pick(e)">
          <method-tag :method="e.method || 'GET'" />
          <span class="url mono" :title="e.url">{{ e.url }}</span>
          <span class="st" :class="statusClass(e)">{{ e.error ? t('history.failed') : e.status }}</span>
          <span class="meta">{{ e.timeMs }} ms</span>
          <span class="meta">{{ e.size }} B</span>
          <span class="time muted">{{ clock(e.time) }}</span>
          <button class="dt" type="button" data-testid="history.detail" :title="t('history.detail')"
            @click.stop="toggleDetail(i)">
            {{ expanded === i ? t('history.collapse') : t('history.detail') }}
          </button>
        </div>

        <!-- 详情：当时的请求参数与响应（item 19；快照来自发送时落盘，旧记录可能缺） -->
        <div v-if="expanded === i" class="detail" data-testid="history.detailBody">
          <div class="dcol">
            <div class="dt-h">{{ t('history.reqParams') }}</div>
            <template v-if="e.request">
              <div v-for="(p, j) in (e.request.params ?? []).filter((x) => x.name)" :key="`p${j}`" class="kv mono">
                <span class="k">{{ p.name }}</span><span class="v">{{ p.value }}</span>
                <span v-if="p.description" class="d muted">{{ p.description }}</span>
              </div>
              <div v-if="!(e.request.params ?? []).some((x) => x.name)" class="none muted">{{ t('history.noSnapshot') }}</div>
            </template>
            <div v-else class="none muted">{{ t('history.noSnapshot') }}</div>
            <div class="dt-h">{{ t('history.reqHeaders') }}</div>
            <template v-if="e.request">
              <div v-for="(h, j) in (e.request.headers ?? []).filter((x) => x.name)" :key="`h${j}`" class="kv mono">
                <span class="k">{{ h.name }}</span><span class="v">{{ h.value }}</span>
              </div>
            </template>
            <div class="dt-h">{{ t('history.reqBody') }}</div>
            <pre v-if="e.request?.body?.raw?.trim()" class="body mono">{{ e.request.body.raw }}</pre>
            <div v-else class="none muted">{{ t('history.noSnapshot') }}</div>
          </div>
          <div class="dcol">
            <div class="dt-h">{{ t('history.resp') }}
              <span v-if="e.response" class="st" :class="e.response.status >= 200 && e.response.status < 400 ? 'ok' : 'warn'">{{ e.response.status }}</span>
            </div>
            <template v-if="e.response">
              <div v-for="(h, j) in (e.response.headers ?? [])" :key="`rh${j}`" class="kv mono">
                <span class="k">{{ h.name }}</span><span class="v">{{ h.value }}</span>
              </div>
              <pre v-if="e.response.body?.trim()" class="body mono">{{ e.response.body }}</pre>
            </template>
            <div v-else class="none muted">{{ t('history.noSnapshot') }}</div>
          </div>
        </div>
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

.item {
  border-radius: 5px;
}

.row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 5px 6px;
  border-radius: 5px;
  font-size: 12.5px;
}

/* 详情按钮：与行内元信息同权重，弱化处理 */
.dt {
  border: 1px solid var(--app-border);
  background: var(--app-panel);
  color: var(--app-text-2);
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 4px;
  cursor: pointer;
  margin-left: auto;
  flex: 0 0 auto;
}

.dt:hover {
  border-color: var(--app-accent);
  color: var(--app-accent);
}

.row.clickable {
  cursor: pointer;
}

.row:hover {
  background: var(--app-row-hover);
}

/* 详情双栏：左请求（参数/头/体），右响应（状态/头/体） */
.detail {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
  padding: 4px 8px 10px;
  border-top: 1px dashed var(--app-border);
}

.dcol {
  min-width: 0;
}

.dt-h {
  font-size: 11.5px;
  font-weight: 600;
  color: var(--app-text-2);
  margin: 8px 0 4px;
  display: flex;
  align-items: center;
  gap: 6px;
}

.kv {
  display: flex;
  gap: 8px;
  font-size: 11.5px;
  padding: 1px 0;
  min-width: 0;
}

.kv .k {
  color: var(--app-code-key);
  flex: 0 0 auto;
  max-width: 40%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kv .v {
  color: var(--app-text);
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kv .d {
  flex: 0 0 auto;
  max-width: 35%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.body {
  margin: 4px 0 0;
  padding: 6px 8px;
  border: 1px solid var(--app-border);
  border-radius: 5px;
  background: var(--app-surface-2);
  font-size: 11px;
  line-height: 1.5;
  max-height: 160px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
}

.none {
  font-size: 11.5px;
  margin: 2px 0;
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
