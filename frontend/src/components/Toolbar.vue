<script setup lang="ts">
// 集合 / 环境 / 本地搜索这一组控件（design-spec §2）。
// 2026-10-03 起并入标题栏（TitleBar 的默认插槽），不再独占一行 —— testid 沿用 toolbar.* 保持兼容。
import { NIcon, NDropdown } from 'naive-ui'
import {
  ChevronDownOutline,
  CloudDownloadOutline,
  CloudUploadOutline,
  FolderOpenOutline,
  LayersOutline,
  LockClosedOutline,
  PlayOutline,
  RefreshOutline,
  SearchOutline,
  SettingsOutline,
  TimeOutline,
} from '@vicons/ionicons5'
import { computed, h } from 'vue'
import { useI18n } from 'vue-i18n'
import EnvPicker from '@/components/EnvPicker.vue'
import type { Env } from '@/types'

const { name, envs, currentEnv } = defineProps<{ name: string; dir: string; envs: Env[]; currentEnv: string }>()
const emit = defineEmits<{
  'open-other': []
  reload: []
  history: []
  cookies: []
  settings: []
  palette: []
  'manage-env': []
  import: []
  export: [format: 'markdown' | 'html']
  mock: []
  sync: []
  'update:currentEnv': [v: string]
}>()

const { t } = useI18n()

const menu = computed(() => [
  { key: 'open', label: t('toolbar.openOther'), icon: () => h(NIcon, { component: FolderOpenOutline }) },
  { key: 'reload', label: t('toolbar.reload'), icon: () => h(NIcon, { component: RefreshOutline }) },
  { key: 'd1', type: 'divider' },
  { key: 'sync', label: t('sync.title'), icon: () => h(NIcon, { component: CloudUploadOutline }) },
  { key: 'import', label: t('import.title'), icon: () => h(NIcon, { component: CloudDownloadOutline }) },
  { key: 'export-md', label: t('export.markdown'), icon: () => h(NIcon, { component: CloudUploadOutline }) },
  { key: 'export-html', label: t('export.html'), icon: () => h(NIcon, { component: CloudUploadOutline }) },
  { key: 'mock', label: t('mock.title'), icon: () => h(NIcon, { component: PlayOutline }) },
  { key: 'd2', type: 'divider' },
  { key: 'history', label: t('history.title'), icon: () => h(NIcon, { component: TimeOutline }) },
  { key: 'cookies', label: t('cookies.title'), icon: () => h(NIcon, { component: LockClosedOutline }) },
  { key: 'settings', label: t('settings.title'), icon: () => h(NIcon, { component: SettingsOutline }) },
])

function onMenu(key: string | number): void {
  if (key === 'open') emit('open-other')
  else if (key === 'reload') emit('reload')
  else if (key === 'history') emit('history')
  else if (key === 'cookies') emit('cookies')
  else if (key === 'settings') emit('settings')
  else if (key === 'import') emit('import')
  else if (key === 'export-md') emit('export', 'markdown')
  else if (key === 'export-html') emit('export', 'html')
  else if (key === 'mock') emit('mock')
  else if (key === 'sync') emit('sync')
}
</script>

<template>
  <div class="toolbar">
    <n-dropdown trigger="click" :options="menu" @select="onMenu">
      <button class="coll" type="button" data-testid="toolbar.collection" :title="t('toolbar.switchCollection')">
        <n-icon :component="LayersOutline" :size="15" class="ci" />
        <span class="nm">{{ name }}</span>
        <n-icon :component="ChevronDownOutline" :size="12" class="ar" />
      </button>
    </n-dropdown>

    <env-picker
      :envs="envs"
      :model-value="currentEnv"
      @update:model-value="emit('update:currentEnv', $event)"
      @manage="emit('manage-env')"
    />

    <button class="search" type="button" data-testid="toolbar.palette" :title="t('toolbar.search')" @click="emit('palette')">
      <n-icon :component="SearchOutline" :size="14" />
      <span class="sl">{{ t('toolbar.search') }}</span>
      <kbd>Ctrl</kbd><kbd>K</kbd>
    </button>
  </div>
</template>

<style scoped>
/* 嵌入标题栏：不再有自己的高度/边框/底色，由标题栏统一排版。
   整组控件显式 no-drag —— 标题栏是可拖动区，不加会让点击集合/环境变成拖窗口。 */
.toolbar {
  --wails-draggable: no-drag;
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
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
  border: 1px solid var(--app-kbd-border);
  border-radius: 4px;
  padding: 0 4px;
  font-family: var(--app-mono);
  font-size: 10px;
  line-height: 15px;
  background: var(--app-kbd-bg);
  color: var(--app-accent-dark);
}
</style>
