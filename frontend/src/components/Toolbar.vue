<script setup lang="ts">
// 工具栏：左侧集合切换 + 离线/本地徽章，右侧环境切换 + Ctrl+K 本地搜索入口（design-spec §2）。
import { NIcon, NDropdown } from 'naive-ui'
import {
  ChevronDownOutline,
  DocumentTextOutline,
  FolderOpenOutline,
  LayersOutline,
  RefreshOutline,
  SearchOutline,
  SettingsOutline,
  TimeOutline,
} from '@vicons/ionicons5'
import { computed, h } from 'vue'
import { useI18n } from 'vue-i18n'
import EnvPicker from '@/components/EnvPicker.vue'
import type { Env } from '@/types'

const props = defineProps<{ name: string; dir: string; envs: Env[]; currentEnv: string }>()
const emit = defineEmits<{
  'open-other': []
  reload: []
  history: []
  settings: []
  palette: []
  'manage-env': []
  'update:currentEnv': [v: string]
}>()

const { t } = useI18n()

const menu = computed(() => [
  { key: 'open', label: t('toolbar.openOther'), icon: () => h(NIcon, { component: FolderOpenOutline }) },
  { key: 'reload', label: t('toolbar.reload'), icon: () => h(NIcon, { component: RefreshOutline }) },
  { key: 'd1', type: 'divider' },
  { key: 'history', label: t('history.title'), icon: () => h(NIcon, { component: TimeOutline }) },
  { key: 'settings', label: t('settings.title'), icon: () => h(NIcon, { component: SettingsOutline }) },
])

// 目录名（不带路径）作为「本地」徽章文案
const folder = computed(() => props.dir.split(/[\\/]/).filter(Boolean).pop() ?? props.dir)

function onMenu(key: string | number): void {
  if (key === 'open') emit('open-other')
  else if (key === 'reload') emit('reload')
  else if (key === 'history') emit('history')
  else if (key === 'settings') emit('settings')
}
</script>

<template>
  <div class="toolbar">
    <n-dropdown trigger="click" :options="menu" @select="onMenu">
      <button class="coll" type="button" :title="t('toolbar.switchCollection')">
        <n-icon :component="LayersOutline" :size="15" class="ci" />
        <span class="nm">{{ name }}</span>
        <n-icon :component="ChevronDownOutline" :size="12" class="ar" />
      </button>
    </n-dropdown>

    <span class="badge ok"><span class="dot" />{{ t('local.offline') }}</span>
    <span class="badge plain" :title="dir">
      <n-icon :component="DocumentTextOutline" :size="12" />
      <span class="bn">{{ folder }}</span>
    </span>

    <span class="sp" />

    <env-picker
      :envs="envs"
      :model-value="currentEnv"
      @update:model-value="emit('update:currentEnv', $event)"
      @manage="emit('manage-env')"
    />

    <button class="search" type="button" :title="t('toolbar.search')" @click="emit('palette')">
      <n-icon :component="SearchOutline" :size="14" />
      <span class="sl">{{ t('toolbar.search') }}</span>
      <kbd>Ctrl</kbd><kbd>K</kbd>
    </button>
  </div>
</template>

<style scoped>
.toolbar {
  height: var(--app-toolbar-h);
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 12px;
  background: var(--app-panel);
  border-bottom: 1px solid var(--app-border);
}

.coll {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 28px;
  padding: 0 8px;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  background: var(--app-panel);
  color: var(--app-text);
  font-size: 13px;
  font-family: inherit;
  cursor: pointer;
}

.coll:hover {
  background: var(--app-row-hover);
}

.ci {
  color: var(--app-muted);
}

.ar {
  color: var(--app-placeholder);
}

.nm {
  max-width: 200px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: 500;
}

.badge {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 22px;
  padding: 0 9px;
  border-radius: 999px;
  font-size: 11px;
  white-space: nowrap;
}

.badge.ok {
  background: var(--app-accent-tint);
  color: var(--app-accent-dark);
}

.badge .dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--app-accent);
}

.badge.plain {
  background: #efefef;
  color: var(--app-muted);
  max-width: 240px;
}

.bn {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--app-mono);
}

.sp {
  flex: 1 1 auto;
}

.search {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 28px;
  padding: 0 10px;
  border: 1px solid var(--app-accent-tint);
  border-radius: 6px;
  background: var(--app-accent-tint);
  color: var(--app-accent-dark);
  font-size: 12.5px;
  font-family: inherit;
  cursor: pointer;
}

.search:hover {
  border-color: var(--app-accent);
}

.sl {
  white-space: nowrap;
}

kbd {
  border: 1px solid #cfe0d6;
  border-radius: 4px;
  padding: 0 4px;
  font-family: var(--app-mono);
  font-size: 10px;
  line-height: 15px;
  background: #fff;
  color: var(--app-accent-dark);
}
</style>
