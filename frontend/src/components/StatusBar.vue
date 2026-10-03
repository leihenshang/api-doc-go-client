<script setup lang="ts">
// 底部状态栏：左侧同步状态，右侧版本号。
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SyncStatus } from '@/types'

const props = defineProps<{
  requests: number
  envs: number
  sync?: SyncStatus | null
  /** 正在进行的写盘个数（>0 时显示「保存中…」） */
  savingCount?: number
  /** 最后一次成功写盘的时间戳（>0 时显示「已保存 HH:mm」） */
  lastSavedAt?: number
}>()
const emit = defineEmits<{ 'open-sync': [] }>()
const { t } = useI18n()

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
    <!-- 保存状态：写盘进行中 / 最近一次成功保存的时间（手动保存模式下帮助确认「存了没」） -->
    <span v-if="(props.savingCount ?? 0) > 0" class="save run" data-testid="statusbar.saving">{{ t('status.saving') }}</span>
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
  color: var(--app-info, #2080f0);
  border-color: var(--app-info, #2080f0);
}

.sync.warn {
  color: var(--app-warn, #f0a020);
  border-color: var(--app-warn, #f0a020);
}

.sync.err {
  color: var(--app-danger, #d03050);
  border-color: var(--app-danger, #d03050);
}

.sync:hover {
  filter: brightness(1.1);
}
</style>
