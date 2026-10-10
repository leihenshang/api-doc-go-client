<script setup lang="ts">
// 集合 / 环境 / 本地搜索这一组控件（design-spec §2）。
// 2026-10-03 起并入标题栏（TitleBar 的默认插槽），不再独占一行 —— testid 沿用 toolbar.* 保持兼容。
import EnvPicker from '@/components/EnvPicker.vue'
import type { CollectionSummary, Env } from '@/types'
import {
  CheckmarkOutline,
  ChevronDownOutline,
  CloudDownloadOutline,
  CloudUploadOutline,
  FolderOpenOutline,
  LayersOutline,
  PlayOutline,
  RefreshOutline,
  SearchOutline
} from '@vicons/ionicons5'
import { NDropdown, NIcon } from 'naive-ui'
import { computed, h } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  name: string
  dir: string
  envs: Env[]
  currentEnv: string
  /** 已打开的工作目录（多根：菜单里可以直接切换活动根） */
  roots: CollectionSummary[]
  activeRoot: string
  /** 最近打开过、当前没打开的目录（启动恢复有上限，超出的从这里一键补开） */
  recent: { dir: string; name: string }[]
}>()
// 注意：不要写 `const { name, envs, currentEnv } = props` —— 解构 props 会丢掉响应性，
// 环境列表变了（新建/改名/删除环境）下拉仍是首次渲染时的那份，看起来就像「切不过去」。
// 模板里一律用 props.xxx 取值。
const emit = defineEmits<{
  'open-other': []
  reload: []
  palette: []
  'manage-env': []
  import: []
  export: [format: 'markdown' | 'html']
  mock: []
  sync: []
  /** 切换活动根（多根并存） */
  activate: [root: string]
  'update:currentEnv': [v: string]
  /** 打开一个「最近打开过、当前没打开」的工作目录 */
  'open-dir': [dir: string]
}>()

const { t } = useI18n()

/** 「工作目录」分组：列出已打开的根，当前根打勾；点其它根即切过去。 */
const workspaceOptions = computed(() =>
  props.roots.map((r) => ({
    key: `root:${r.root}`,
    label: r.readOnly ? `${r.info.name}（${t('tree.readOnly')}）` : r.info.name,
    icon: r.root === props.activeRoot ? () => h(NIcon, { component: CheckmarkOutline }) : undefined,
  })),
)

/**
 * 「最近打开」分组：上次打开过、这次没恢复的目录（启动恢复有数量上限，超出的不静默丢）。
 * 只列目录名，点一下就打开 —— 否则用户得重新去系统对话框里翻。
 */
const recentOptions = computed(() =>
  props.recent.map((r) => ({
    key: `dir:${r.dir}`,
    label: r.name,
    // 同名目录不少见：把完整路径放进 title，悬停可分辨
    title: r.dir,
  })),
)

const menu = computed(() => [
  ...(workspaceOptions.value.length
    ? [{ type: 'group' as const, key: 'ws', label: t('toolbar.workspaces'), children: workspaceOptions.value }]
    : []),
  ...(recentOptions.value.length
    ? [{ type: 'group' as const, key: 'recent', label: t('toolbar.recentDirs'), children: recentOptions.value }]
    : []),
  { key: 'open', label: t('toolbar.openOther'), icon: () => h(NIcon, { component: FolderOpenOutline }) },
  { key: 'reload', label: t('toolbar.reload'), icon: () => h(NIcon, { component: RefreshOutline }) },
  { key: 'd1', type: 'divider' as const },
  { key: 'sync', label: t('sync.title'), icon: () => h(NIcon, { component: CloudUploadOutline }) },
  { key: 'import', label: t('import.title'), icon: () => h(NIcon, { component: CloudDownloadOutline }) },
  { key: 'export-md', label: t('export.markdown'), icon: () => h(NIcon, { component: CloudUploadOutline }) },
  { key: 'export-html', label: t('export.html'), icon: () => h(NIcon, { component: CloudUploadOutline }) },
  { key: 'mock', label: t('mock.title'), icon: () => h(NIcon, { component: PlayOutline }) },
])

function onMenu(key: string | number): void {
  if (typeof key === 'string' && key.startsWith('root:')) {
    emit('activate', key.slice('root:'.length))
    return
  }
  if (typeof key === 'string' && key.startsWith('dir:')) {
    emit('open-dir', key.slice('dir:'.length))
    return
  }
  if (key === 'open') emit('open-other')
  else if (key === 'reload') emit('reload')
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
        <span class="nm">{{ props.name }}</span>
        <n-icon :component="ChevronDownOutline" :size="12" class="ar" />
      </button>
    </n-dropdown>

    <env-picker :envs="props.envs" :model-value="props.currentEnv"
      @update:model-value="emit('update:currentEnv', $event)" @manage="emit('manage-env')" />

    <button class="search" type="button" data-testid="toolbar.palette" :title="t('toolbar.search')"
      @click="emit('palette')">
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
