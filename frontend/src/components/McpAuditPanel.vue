<script setup lang="ts">
// T3：MCP 工具调用审计面板（设置 → MCP 服务）。
// 首屏走 ListMCPAudit；之后 mcp:audit 事件增量 unshift（seq 去重、封顶 1000）；
// 过滤变化走服务端重新查询，实时事件在客户端再滤一道。
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { NButton, NInput, NPopconfirm, NSelect, NTag } from 'naive-ui'
import { api, onAppEvent } from '@/lib/ipc'
import type { McpAuditEvent, McpAuditPage } from '@/types'

const { t } = useI18n()

const events = ref<McpAuditEvent[]>([])
const page = ref<McpAuditPage | null>(null)
const loading = ref(false)
const expanded = ref<Set<number>>(new Set())
const filePath = ref('')

const filterTool = ref<string | null>(null)
const filterRisk = ref<string | null>(null)
const filterResult = ref<string | null>(null)
const search = ref('')

const maxLive = 1000

const toolOptions = [
  { label: t('audit.allTools'), value: '' },
  ...[
    'list_workspaces', 'use_workspace', 'list_projects', 'get_project_modules',
    'get_request_detail', 'search_requests', 'list_envs',
    'create_request', 'create_module', 'create_project', 'update_request', 'delete_request',
    'create_env', 'rename_env', 'delete_env', 'set_env_var', 'delete_env_var',
    'send_request',
  ].map((v) => ({ label: v, value: v })),
]
const riskOptions = [
  { label: t('audit.allRisks'), value: '' },
  { label: t('mcpAuthz.riskHigh'), value: 'high' },
  { label: t('mcpAuthz.riskMedium'), value: 'medium' },
  { label: t('audit.riskLow'), value: 'low' },
]
const resultOptions = [
  { label: t('audit.allResults'), value: '' },
  { label: t('audit.resultOk'), value: 'ok' },
  { label: t('audit.resultBizError'), value: 'biz_error' },
  { label: t('audit.resultDenied'), value: 'denied' },
  { label: t('audit.resultPanic'), value: 'panic' },
]

function buildQuery() {
  return {
    tool: filterTool.value || '',
    risk: filterRisk.value || '',
    search: search.value.trim(),
    ok: filterResult.value === 'ok' ? true : filterResult.value ? false : null,
    limit: 200,
  }
}

function kindPass(ev: McpAuditEvent): boolean {
  return !filterResult.value || ev.resultKind === filterResult.value
}
function riskPass(ev: McpAuditEvent): boolean {
  return !filterRisk.value || ev.risk === filterRisk.value
}
function toolPass(ev: McpAuditEvent): boolean {
  return !filterTool.value || ev.tool === filterTool.value
}
function searchPass(ev: McpAuditEvent): boolean {
  const s = search.value.trim()
  if (!s) return true
  return `${ev.tool} ${ev.project ?? ''} ${ev.argsSummary ?? ''} ${ev.resultSummary ?? ''}`.includes(s)
}

async function reload(): Promise<void> {
  loading.value = true
  try {
    const p = await api.listMCPAudit(buildQuery())
    page.value = p
    filePath.value = p.file
    events.value = p.events
  } catch {
    // 无后端/未启用 MCP 时静默：面板显示空态
    events.value = []
  } finally {
    loading.value = false
  }
}

let timer: number | undefined
function onSearch(): void {
  window.clearTimeout(timer)
  timer = window.setTimeout(reload, 250)
}

// 面板随设置弹窗挂载/卸载：用模块级 handler 槽，保证同一时刻只有最新实例收事件
// （弹窗重复打开不会叠加多个处理器）。
let liveHandler: ((p: unknown) => void) | undefined
function onLiveEvent(payload: unknown): void {
  const ev = payload as McpAuditEvent
  if (!ev || typeof ev.seq !== 'number') return
  if (events.value.some((e) => e.seq === ev.seq && e.tsMs === ev.tsMs)) return
  if (!toolPass(ev) || !riskPass(ev) || !kindPass(ev) || !searchPass(ev)) return
  events.value = [ev, ...events.value].slice(0, maxLive)
}

let cancelLive: (() => void) | undefined
onMounted(() => {
  void reload()
  liveHandler = onLiveEvent
  onAppEvent('mcp:audit', (p) => liveHandler?.(p))
  cancelLive = () => {
    if (liveHandler === onLiveEvent) liveHandler = undefined
  }
})
onBeforeUnmount(() => cancelLive?.())

function toggle(seq: number): void {
  const next = new Set(expanded.value)
  if (next.has(seq)) next.delete(seq)
  else next.add(seq)
  expanded.value = next
}
function prettyArgs(s?: string): string {
  if (!s) return '{}'
  try {
    return JSON.stringify(JSON.parse(s), null, 2)
  } catch {
    return s
  }
}
function fmtTime(ev: McpAuditEvent): string {
  const d = new Date(ev.tsMs)
  return d.toLocaleTimeString() + '.' + String(d.getMilliseconds()).padStart(3, '0')
}
function riskType(ev: McpAuditEvent): 'default' | 'warning' | 'error' {
  if (ev.risk === 'high') return 'error'
  if (ev.risk === 'medium') return 'warning'
  return 'default'
}
function riskLabel(risk: string): string {
  if (risk === 'high') return t('mcpAuthz.riskHigh')
  if (risk === 'medium') return t('mcpAuthz.riskMedium')
  return t('audit.riskLow')
}
function approvedLabel(ev: McpAuditEvent): string {
  if (ev.approved === undefined || ev.approved === null) return '—'
  return ev.approved ? '✓' : '✕'
}
function kindLabel(kind: string): string {
  return t(`audit.result_${kind}`)
}

async function doExport(): Promise<void> {
  const path = await api.exportMCPAudit('')
  if (path) filePath.value = path
}
async function doClear(): Promise<void> {
  await api.clearMCPAudit()
  await reload()
}
</script>

<template>
  <div class="audit" data-testid="mcp.audit.list">
    <div class="filters">
      <n-select :options="toolOptions" :value="filterTool" size="tiny" class="f-tool"
        data-testid="mcp.audit.filterTool" @update:value="(v) => { filterTool = v; void reload() }" />
      <n-select :options="riskOptions" :value="filterRisk" size="tiny" class="f-risk"
        data-testid="mcp.audit.filterRisk" @update:value="(v) => { filterRisk = v; void reload() }" />
      <n-select :options="resultOptions" :value="filterResult" size="tiny" class="f-result"
        data-testid="mcp.audit.filterResult" @update:value="(v) => { filterResult = v; void reload() }" />
      <n-input :value="search" size="tiny" class="f-search" :placeholder="t('audit.search')"
        data-testid="mcp.audit.search" @update:value="(v) => { search = v; onSearch() }" />
      <span class="sp" />
      <n-button size="tiny" tertiary :loading="loading" data-testid="mcp.audit.export" @click="doExport">
        {{ t('audit.export') }}
      </n-button>
      <n-popconfirm @positive-click="doClear">
        <template #trigger>
          <n-button size="tiny" quaternary type="error" data-testid="mcp.audit.clear">
            {{ t('audit.clear') }}
          </n-button>
        </template>
        {{ t('audit.clearConfirm') }}
      </n-popconfirm>
    </div>

    <p v-if="filePath" class="file mono" :title="filePath">{{ filePath }}</p>

    <div v-if="!events.length && !loading" class="empty" data-testid="mcp.audit.empty">
      {{ t('audit.empty') }}
    </div>

    <div class="rows">
      <template v-for="ev in events" :key="`${ev.tsMs}-${ev.seq}`">
        <div class="row" :class="{ denied: ev.resultKind === 'denied', panic: ev.resultKind === 'panic' }"
          data-testid="mcp.audit.row" @click="toggle(ev.seq)">
          <span class="c time">{{ fmtTime(ev) }}</span>
          <span class="c risk">
            <n-tag :type="riskType(ev)" size="small">{{ riskLabel(ev.risk) }}</n-tag>
          </span>
          <code class="c tool">{{ ev.tool }}</code>
          <span class="c proj">{{ ev.project || '—' }}</span>
          <span class="c src">{{ ev.transport || ev.client || '—' }}</span>
          <span class="c appr">{{ approvedLabel(ev) }}</span>
          <span class="c kind" :class="ev.resultKind">{{ kindLabel(ev.resultKind) }}</span>
          <span class="c dur">{{ ev.durationMs }}ms</span>
        </div>
        <div v-if="expanded.has(ev.seq)" class="detail" data-testid="mcp.audit.detail">
          <div class="detail-grid">
            <span>session</span><code class="mono">{{ ev.sessionId || '—' }}</code>
            <span>requestId</span><code class="mono">{{ ev.requestId || '—' }}</code>
            <span>grantedBy</span><code class="mono">{{ ev.grantedBy || '—' }}</code>
            <span>seq</span><code class="mono">{{ ev.seq }}</code>
          </div>
          <div class="detail-label">{{ t('mcpAuthz.args') }}</div>
          <pre class="detail-pre">{{ prettyArgs(ev.argsSummary) }}</pre>
          <div class="detail-label">{{ t('audit.resultSummary') }}</div>
          <pre class="detail-pre">{{ ev.resultSummary || '—' }}</pre>
        </div>
      </template>
    </div>

    <p v-if="page?.truncated" class="warn">{{ t('audit.truncated') }}</p>
  </div>
</template>

<style scoped>
.audit {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.filters {
  display: flex;
  gap: 6px;
  align-items: center;
  flex-wrap: wrap;
}
.f-tool { width: 170px; }
.f-risk { width: 110px; }
.f-result { width: 120px; }
.f-search { width: 160px; }
.sp { flex: 1; }
.file {
  font-size: 11px;
  color: var(--app-text-3, #999);
  margin: 0;
  word-break: break-all;
}
.empty {
  padding: 18px;
  text-align: center;
  color: var(--app-text-3, #999);
  border: 1px dashed var(--app-border);
  border-radius: 6px;
}
.rows {
  display: flex;
  flex-direction: column;
  max-height: 280px;
  overflow: auto;
  border: 1px solid var(--app-border);
  border-radius: 6px;
}
.row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 8px;
  font-size: 12px;
  cursor: pointer;
  border-left: 3px solid transparent;
}
.row:nth-child(odd) {
  background: var(--app-surface-2);
}
.row.denied,
.row.panic {
  border-left-color: var(--app-danger);
}
.c {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.time { width: 92px; color: var(--app-text-3, #999); }
.risk { width: 70px; }
.tool { width: 150px; }
.proj { width: 90px; color: var(--app-text-2); }
.src { width: 60px; color: var(--app-text-3, #999); }
.appr { width: 34px; text-align: center; }
.kind { width: 70px; }
.kind.ok { color: var(--app-success, #18a058); }
.kind.biz_error,
.kind.denied,
.kind.panic { color: var(--app-danger); }
.dur { width: 60px; margin-left: auto; text-align: right; color: var(--app-text-3, #999); }
.detail {
  border: 1px solid var(--app-border);
  border-radius: 6px;
  padding: 8px 10px;
  margin-top: -4px;
  background: var(--app-surface-2);
}
.detail-grid {
  display: grid;
  grid-template-columns: 90px 1fr;
  gap: 2px 10px;
  font-size: 11.5px;
}
.detail-grid span {
  color: var(--app-text-3, #999);
}
.detail-label {
  margin: 8px 0 4px;
  color: var(--app-text-3, #999);
  font-size: 11.5px;
}
.detail-pre {
  margin: 0;
  max-height: 160px;
  overflow: auto;
  font-size: 11.5px;
  white-space: pre-wrap;
  word-break: break-all;
  background: var(--app-fill);
  border-radius: 4px;
  padding: 6px;
}
.warn {
  color: var(--app-warn);
  font-size: 11.5px;
  margin: 0;
}
</style>
