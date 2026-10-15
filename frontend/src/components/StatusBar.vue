<script setup lang="ts">
// 底部状态栏：左侧同步状态，中间集合规模/保存状态，右侧全局设置、请求历史、回收站、Cookie 管理入口。
import type { MCPStatus, SyncStatus } from '@/types'
import { CodeSlashOutline, LockClosedOutline, SettingsOutline, TimeOutline, TrashOutline } from '@vicons/ionicons5'
import { NIcon } from 'naive-ui'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  requests: number
  envs: number
  sync?: SyncStatus | null
  /** 正在进行的写盘个数（>0 时显示「保存中…」） */
  savingCount?: number
  /** 最后一次成功写盘的时间戳（>0 时显示「已保存 HH:mm」） */
  lastSavedAt?: number
  /** 内嵌 MCP 服务运行状态（运行中 / 出错 / 未启用）；null 视为未启用 */
  mcp?: MCPStatus | null
}>()
const emit = defineEmits<{ 'open-sync': []; history: []; trash: []; cookies: []; settings: []; mcp: [] }>()
const { t } = useI18n()

/** MCP 入口状态：运行中=ok、出错=err、其它（未启用/未加载）=off。 */
const mcpClass = computed(() => {
  if (props.mcp?.running) return 'ok'
  if (props.mcp?.error) return 'err'
  return 'off'
})

/** MCP 入口的提示文案（title）：运行中带地址、出错带错误、否则提示未启用。 */
const mcpTitle = computed(() => {
  if (props.mcp?.running) return `${t('status.mcpRunning')}：${props.mcp.url}`
  if (props.mcp?.error) return `${t('status.mcpError')}：${props.mcp.error}`
  return t('status.mcpStopped')
})

/** 同步状态摘要（状态栏左侧）。 */
const syncText = computed(() => {
  const s = props.sync
  if (!s || !s.linked) return ''
  if (s.running) return t('status.syncRunning')
  const parts: string[] = [t('status.syncLinked')]
  if (s.dirtyCount) parts.push(t('status.syncDirty', { n: s.dirtyCount }))
  if (s.conflicts) parts.push(t('status.syncConflicts', { n: s.conflicts }))
  if (s.lastError) parts.push(t('status.syncError'))
  else if (s.lastSyncAt) parts.push(t('status.syncAt', { t: clock(s.lastSyncAt) }))
  return parts.join(t('common.sep'))
})

const syncClass = computed(() => {
  const s = props.sync
  if (!s || !s.linked) return ''
  if (s.running) return 'run'
  if (s.lastError) return 'err'
  if (s.conflicts) return 'warn'
  return 'ok'
})

function clock(ms: number): string {
  const d = new Date(ms)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}`
}
</script>

<template>
  <footer class="statusbar">
    <span v-if="syncText" class="sync" :class="syncClass" data-testid="statusbar.sync" @click="emit('open-sync')">
      {{ syncText }}
    </span>
    <span class="sp" />

    <!-- 全局入口：图标 + 文字，位于统计信息左侧（原在集合切换下拉里，现收到底栏） -->
    <span class="acts">
      <button class="act" type="button" data-testid="statusbar.history" @click="emit('history')">
        <n-icon :component="TimeOutline" :size="14" />
        <span class="lb">{{ t('history.title') }}</span>
      </button>
      <!-- 回收站：当前工作区 .trash 的入口（item 20） -->
      <button class="act" type="button" data-testid="statusbar.trash" @click="emit('trash')">
        <n-icon :component="TrashOutline" :size="14" />
        <span class="lb">{{ t('trash.title') }}</span>
      </button>
      <button class="act" type="button" data-testid="statusbar.cookies" @click="emit('cookies')">
        <n-icon :component="LockClosedOutline" :size="14" />
        <span class="lb">{{ t('cookies.title') }}</span>
      </button>
      <button class="act" type="button" :class="mcpClass" :title="mcpTitle"
        data-testid="statusbar.mcp" @click="emit('mcp')">
        <n-icon :component="CodeSlashOutline" :size="14" />
        <span class="lb">{{ t('status.mcp') }}</span>
        <span class="dot" />
      </button>
      <button class="act" type="button" data-testid="statusbar.settings" @click="emit('settings')">
        <n-icon :component="SettingsOutline" :size="14" />
        <span class="lb">{{ t('settings.title') }}</span>
      </button>
    </span>

    <!-- 集合规模：与集合树一致（种子/扫描的权威计数来自 App 传入的 props） -->
    <span class="counts" data-testid="statusbar.counts">
      {{ t('status.requests', { n: props.requests }) }}{{ t('common.sep') }}{{ t('status.envs', { n: props.envs }) }}
    </span>
    <!-- 保存状态：写盘进行中 / 最近一次成功保存的时间（手动保存模式下帮助确认「存了没」） -->
    <span v-if="(props.savingCount ?? 0) > 0" class="save run" data-testid="statusbar.saving">{{ t('status.saving')
    }}</span>
    <span v-else-if="(props.lastSavedAt ?? 0) > 0" class="save" data-testid="statusbar.saved">
      {{ t('status.savedAt', { t: clock(props.lastSavedAt ?? 0) }) }}
    </span>
  </footer>
</template>

<style scoped>
.statusbar {
  height: var(--app-statusbar-h);
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 12px;
  background: var(--app-bg);
  border-top: 1px solid var(--app-border);
  font-family: var(--app-mono);
  font-size: 11px;
  color: var(--app-muted);
  user-select: none;
}

.sp {
  flex: 1 1 auto;
}

.counts {
  color: var(--app-muted);
  white-space: nowrap;
}

.save {
  font-size: 11.5px;
  color: var(--app-muted);
  white-space: nowrap;
}

.save.run {
  color: var(--app-accent);
}

.sync {
  cursor: pointer;
  padding: 1px 8px;
  border-radius: 4px;
  border: 1px solid var(--app-border);
  white-space: nowrap;
}

.sync.ok {
  color: var(--app-accent);
  border-color: var(--app-accent);
}

.sync.run {
  color: var(--app-info);
  border-color: var(--app-info);
}

.sync.warn {
  color: var(--app-warn);
  border-color: var(--app-warn);
}

.sync.err {
  color: var(--app-danger);
  border-color: var(--app-danger);
}

.sync:hover {
  filter: brightness(1.1);
}

/* 全局入口：图标 + 文字，位于统计信息左侧 */
.acts {
  display: flex;
  align-items: center;
  gap: 2px;
}

.act {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 20px;
  padding: 0 6px;
  border: none;
  border-radius: 4px;
  background: none;
  color: var(--app-muted);
  font-family: inherit;
  font-size: 11px;
  cursor: pointer;
}

.act:hover {
  background: var(--app-row-hover);
  color: var(--app-accent);
}

/* MCP 入口状态：运行中绿、出错红、未启用灰；小圆点直观反映运行状态 */
.act.ok {
  color: var(--app-accent);
}

.act.err {
  color: var(--app-danger);
}

.dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--app-muted);
  flex: 0 0 auto;
}

.act.ok .dot {
  background: var(--app-accent);
}

.act.err .dot {
  background: var(--app-danger);
}

.lb {
  white-space: nowrap;
}
</style>
