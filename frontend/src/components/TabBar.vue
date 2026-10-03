<script setup lang="ts">
// 请求标签栏（design-spec §2）：首个固定为 Collection 概览页，其后是各请求 tab。
// dot 表示有未落盘编辑，method 前置按语义色着色，响应状态码跟随 tab 展示。
// G7：请求 tab 可拖拽排序（HTML5 DnD）。
// 右键 tab 弹上下文菜单：关闭当前 / 关闭左侧所有 / 关闭右侧所有 / 复制新建 / 保存所有 / 关闭全部。
import { NDropdown, NIcon } from 'naive-ui'
import type { DropdownOption } from 'naive-ui'
import {
  AddOutline,
  ChevronBackOutline,
  ChevronForwardOutline,
  CloseCircleOutline,
  CloseOutline,
  CopyOutline,
  LayersOutline,
  SaveOutline,
} from '@vicons/ionicons5'
import { computed, h, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import MethodTag from '@/components/MethodTag.vue'
import type { Tab } from '@/stores/tabs'

const props = defineProps<{ tabs: Tab[]; activeKey: string }>()
const emit = defineEmits<{
  select: [key: string]
  'select-overview': []
  close: [key: string]
  new: []
  'new-grpc': []
  reorder: [from: number, to: number]
  command: [key: string, tabKey: string]
}>()
const { t } = useI18n()

const dragFrom = ref(-1)

/** 「＋」下拉：新建 HTTP / gRPC 请求（都是未落盘草稿，关闭时才问保存）。 */
const newMenu = computed(() => [
  { key: 'http', label: t('tree.newRequest') },
  { key: 'grpc', label: t('grpc.newRequest') },
])

function onNewMenu(key: string | number): void {
  if (key === 'grpc') emit('new-grpc')
  else emit('new')
}

// ---- 右键上下文菜单 ----
const menuKey = ref('')
const menuX = ref(0)
const menuY = ref(0)

/** 菜单项随位置变化：最左的 tab 没有「左侧」，最右的没有「右侧」。 */
const menuOptions = computed<DropdownOption[]>(() => {
  const i = props.tabs.findIndex((x) => x.key === menuKey.value)
  const item = (key: string, label: string, icon: unknown, disabled = false) => ({
    key,
    label,
    disabled,
    icon: () => h(NIcon, { component: icon as never }),
  })
  return [
    item('close', t('tab.closeCurrent'), CloseOutline),
    item('close-left', t('tab.closeLeft'), ChevronBackOutline, i <= 0),
    item('close-right', t('tab.closeRight'), ChevronForwardOutline, i < 0 || i >= props.tabs.length - 1),
    { type: 'divider', key: 'd1' },
    item('duplicate', t('tab.duplicate'), CopyOutline),
    item('save-all', t('tab.saveAll'), SaveOutline),
    { type: 'divider', key: 'd2' },
    item('close-all', t('tab.closeAll'), CloseCircleOutline, props.tabs.length === 0),
  ]
})

function openMenu(i: number, e: MouseEvent): void {
  const tab = props.tabs[i]
  if (!tab) return
  emit('select', tab.key) // 右键先激活，和主流编辑器一致
  menuKey.value = tab.key
  menuX.value = e.clientX
  menuY.value = e.clientY
}

function closeMenu(): void {
  menuKey.value = ''
}

function onMenuSelect(key: string | number): void {
  const target = menuKey.value
  menuKey.value = ''
  if (target) emit('command', String(key), target)
}

function onDragStart(i: number, e: DragEvent): void {
  dragFrom.value = i
  if (e.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', String(i))
  }
}

function onDragOver(e: DragEvent): void {
  if (dragFrom.value >= 0 && e.dataTransfer) e.dataTransfer.dropEffect = 'move'
}

function onDrop(i: number, e: DragEvent): void {
  e.preventDefault()
  const from = dragFrom.value
  dragFrom.value = -1
  if (from < 0 || from === i) return
  emit('reorder', from, i)
}
</script>

<template>
  <div class="tabbar">
    <button class="tab coll" :class="{ active: !activeKey }" type="button" data-testid="tab.overview" @click="emit('select-overview')">
      <n-icon :component="LayersOutline" :size="13" />
      <span class="tt">{{ t('tab.collection') }}</span>
    </button>

    <div class="tabs">
      <div
        v-for="(tab, i) in tabs"
        :key="tab.key"
        class="tab"
        :class="{ active: tab.key === activeKey, dragging: i === dragFrom }"
        role="button"
        :tabindex="0"
        data-testid="tab.item"
        :data-tab-key="tab.key"
        :data-tab-title="tab.title"
        :draggable="true"
        :title="tab.draft ? t('prompt.saveDraftHint') : tab.request.path"
        @click="emit('select', tab.key)"
        @keydown.enter="emit('select', tab.key)"
        @contextmenu.prevent="openMenu(i, $event)"
        @dragstart="onDragStart(i, $event)"
        @dragover.prevent="onDragOver($event)"
        @drop="onDrop(i, $event)"
        @dragend="dragFrom = -1"
      >
        <method-tag :method="tab.request.method" />
        <span v-if="tab.dirty || tab.draft" class="dot" :title="t('tab.unsaved')" />
        <span class="tt">{{ tab.title }}</span>
        <span v-if="tab.response" class="st" :class="tab.response.status < 400 ? 'ok' : 'err'">
          {{ tab.response.status }}
        </span>
        <button class="x" type="button" data-testid="tab.close" :title="t('tab.closeHint')" @click.stop="emit('close', tab.key)">
          <n-icon :component="CloseOutline" :size="13" />
        </button>
      </div>
    </div>

    <n-dropdown trigger="click" placement="bottom-end" :options="newMenu" @select="onNewMenu">
      <button class="add" type="button" data-testid="tab.new" :title="t('tab.newHint')">
        <n-icon :component="AddOutline" :size="16" />
      </button>
    </n-dropdown>

    <!-- 右键 tab 的上下文菜单（手动定位到鼠标处；点外面自动收起） -->
    <n-dropdown
      trigger="manual"
      placement="bottom-start"
      :x="menuX"
      :y="menuY"
      :show="menuKey !== ''"
      :options="menuOptions"
      data-testid="tab.menu"
      @select="onMenuSelect"
      @clickoutside="closeMenu"
    />
  </div>
</template>

<style scoped>
.tabbar {
  height: 38px;
  flex: 0 0 auto;
  display: flex;
  align-items: stretch;
  gap: 2px;
  padding: 0 8px;
  border-bottom: 1px solid var(--app-border);
  background: var(--app-panel);
}

.tabs {
  display: flex;
  align-items: stretch;
  gap: 2px;
  overflow-x: auto;
  scrollbar-width: none;
}

.tabs::-webkit-scrollbar {
  height: 0;
}

.tab {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  max-width: 230px;
  padding: 8px 10px;
  border: none;
  border-bottom: 2px solid transparent;
  background: transparent;
  font-family: inherit;
  font-size: 12.5px;
  color: var(--app-muted);
  cursor: pointer;
  white-space: nowrap;
}

.tab:hover {
  background: var(--app-row-hover);
}

.tab.dragging {
  opacity: 0.5;
}

.tab.active {
  color: var(--app-text);
  border-bottom-color: var(--app-accent);
}

.tab.coll {
  flex: 0 0 auto;
  border-right: 1px solid var(--app-border);
  padding-right: 14px;
}

.tab.coll .tt {
  font-weight: 500;
}

.tt {
  overflow: hidden;
  max-width: 140px;
  text-overflow: ellipsis;
}

.dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--app-accent);
  flex: 0 0 auto;
}

.st {
  font-family: var(--app-mono);
  font-size: 10px;
  padding: 0 4px;
  border-radius: 4px;
  line-height: 14px;
}

.st.ok {
  background: var(--app-accent-tint);
  color: var(--app-accent-dark);
}

.st.err {
  background: var(--app-danger-tint);
  color: var(--app-danger);
}

.x {
  border: none;
  background: transparent;
  color: var(--app-placeholder);
  border-radius: 3px;
  padding: 2px;
  line-height: 1;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: 0 0 auto;
}

.x:hover {
  background: var(--app-chip-hover);
  color: var(--app-danger);
}

.add {
  border: none;
  background: transparent;
  font-size: 16px;
  color: var(--app-muted);
  cursor: pointer;
  padding: 0 10px;
  flex: 0 0 auto;
}

.add:hover {
  color: var(--app-accent);
}
</style>
