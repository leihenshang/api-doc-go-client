<script setup lang="ts">
// Ctrl+K 命令面板（design-spec §3）：范围筛选 + 键盘上下选择 + 回车执行。
// 索引数据来自内存中的集合树与环境，无需联网。
import { NIcon, NInput, NModal } from 'naive-ui'
import type { InputInst } from 'naive-ui'
import {
  AddOutline,
  FolderOpenOutline,
  MoonOutline,
  RefreshOutline,
  SearchOutline,
  ServerOutline,
  SettingsOutline,
  SwapVerticalOutline,
  TimeOutline,
} from '@vicons/ionicons5'
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import type { Component } from 'vue'
import { useI18n } from 'vue-i18n'
import MethodTag from '@/components/MethodTag.vue'
import type { Env, TreeNode } from '@/types'

type Scope = 'all' | 'request' | 'env' | 'command'
type Kind = Exclude<Scope, 'all'>

interface Item {
  id: string
  kind: Kind
  label: string
  method?: string
  path: string
  icon?: Component
}

const props = defineProps<{ show: boolean; tree: TreeNode[]; envs: Env[] }>()
const emit = defineEmits<{
  'update:show': [v: boolean]
  'open-request': [uid: string]
  'switch-env': [name: string]
  command: [key: string]
}>()

const { t } = useI18n()

const keyword = ref('')
const scope = ref<Scope>('all')
const cursor = ref(0)
const inputEl = ref<InputInst | null>(null)
const listEl = ref<HTMLElement | null>(null)

const scopes = computed<{ key: Scope; label: string }[]>(() => [
  { key: 'all', label: t('palette.all') },
  { key: 'request', label: t('palette.requests') },
  { key: 'env', label: t('palette.envs') },
  { key: 'command', label: t('palette.commands') },
])

const COMMANDS: { key: string; labelKey: string; icon: Component }[] = [
  { key: 'open-dir', labelKey: 'palette.openDir', icon: FolderOpenOutline },
  { key: 'new-request', labelKey: 'palette.newRequest', icon: AddOutline },
  { key: 'reload', labelKey: 'palette.reload', icon: RefreshOutline },
  { key: 'toggle-layout', labelKey: 'palette.toggleLayout', icon: SwapVerticalOutline },
  { key: 'toggle-theme', labelKey: 'palette.toggleTheme', icon: MoonOutline },
  { key: 'history', labelKey: 'palette.history', icon: TimeOutline },
  { key: 'settings', labelKey: 'palette.settings', icon: SettingsOutline },
]

function collectRequests(nodes: TreeNode[], trail: string[], out: Item[]): void {
  for (const n of nodes) {
    if (n.type === 'folder') {
      collectRequests(n.children ?? [], [...trail, n.name], out)
      continue
    }
    out.push({
      id: n.uid,
      kind: 'request',
      label: n.name,
      method: n.method ?? 'GET',
      path: trail.join(' / '),
    })
  }
}

const items = computed<Item[]>(() => {
  const out: Item[] = []
  collectRequests(props.tree, [], out)
  out.push(
    ...props.envs.map((e) => ({
      id: e.name,
      kind: 'env' as Kind,
      label: `${t('env.title')} · ${e.name}`,
      path: t('palette.envPath'),
    })),
  )
  out.push(
    ...COMMANDS.map((c) => ({
      id: c.key,
      kind: 'command' as Kind,
      label: t(c.labelKey),
      path: t('palette.cmdPath'),
      icon: c.icon,
    })),
  )
  return out
})

const filtered = computed(() => {
  const q = keyword.value.trim().toLowerCase()
  return items.value.filter((it) => {
    if (scope.value !== 'all' && it.kind !== scope.value) return false
    return q === '' || it.label.toLowerCase().includes(q) || it.path.toLowerCase().includes(q)
  })
})

watch(filtered, () => (cursor.value = 0))

watch(
  () => props.show,
  async (v) => {
    if (!v) return
    keyword.value = ''
    scope.value = 'all'
    cursor.value = 0
    await nextTick()
    inputEl.value?.focus()
  },
)

function close(): void {
  emit('update:show', false)
}

function activate(item: Item): void {
  if (item.kind === 'request') emit('open-request', item.id)
  else if (item.kind === 'env') emit('switch-env', item.id)
  else emit('command', item.id)
  close()
}

async function move(step: number): Promise<void> {
  const total = filtered.value.length
  if (!total) return
  cursor.value = (cursor.value + step + total) % total
  await nextTick()
  listEl.value?.querySelector(`[data-idx="${cursor.value}"]`)?.scrollIntoView({ block: 'nearest' })
}

function onKeydown(e: KeyboardEvent): void {
  if (e.key === 'Escape') {
    e.preventDefault()
    close()
    return
  }
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    void move(1)
    return
  }
  if (e.key === 'ArrowUp') {
    e.preventDefault()
    void move(-1)
    return
  }
  if (e.key === 'Enter') {
    e.preventDefault()
    const hit = filtered.value[cursor.value]
    if (hit) activate(hit)
  }
}

// 面板打开时才监听键盘，避免与编辑器快捷键相互干扰；卸载时兜底摘除
watch(
  () => props.show,
  (v) => {
    if (v) window.addEventListener('keydown', onKeydown)
    else window.removeEventListener('keydown', onKeydown)
  },
)

onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))

function setInput(el: unknown): void {
  inputEl.value = (el as InputInst | null) ?? null
}
</script>

<template>
  <n-modal :show="show" :auto-focus="false" style="width: 640px" @update:show="emit('update:show', $event)">
    <div class="palette" data-testid="palette" @keydown.esc="close">
      <div class="search">
        <n-icon :component="SearchOutline" :size="16" class="si" />
        <n-input
          :ref="setInput"
          v-model:value="keyword"
          data-testid="palette.input"
          :bordered="false"
          size="large"
          :placeholder="t('palette.placeholder')"
        />
        <kbd>ESC</kbd>
      </div>

      <div class="chips">
        <button
          v-for="s in scopes"
          :key="s.key"
          class="chip"
          :class="{ on: scope === s.key }"
          type="button"
          data-testid="palette.chip"
          @click="scope = s.key"
        >
          {{ s.label }}
        </button>
      </div>

      <div ref="listEl" class="list">
        <button
          v-for="(it, i) in filtered"
          :key="it.kind + it.id"
          class="item"
          :class="{ on: i === cursor }"
          type="button"
          data-testid="palette.item"
          :data-idx="i"
          @mouseenter="cursor = i"
          @click="activate(it)"
        >
          <method-tag v-if="it.kind === 'request'" :method="it.method ?? 'GET'" filled />
          <n-icon v-else :component="it.kind === 'env' ? ServerOutline : (it.icon ?? SearchOutline)" :size="15" class="ii" />
          <span class="lb">{{ it.label }}</span>
          <span class="pa">{{ it.path }}</span>
        </button>
        <div v-if="!filtered.length" class="none">{{ t('palette.empty') }}</div>
      </div>

      <div class="foot">{{ t('palette.footer') }}</div>
    </div>
  </n-modal>
</template>

<style scoped>
.palette {
  background: var(--app-panel);
  border-radius: 12px;
  box-shadow: var(--app-shadow-pop);
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.search {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 14px 4px 16px;
  border-bottom: 1px solid var(--app-border);
}

.si {
  color: var(--app-accent);
  flex: 0 0 auto;
}

.search :deep(.n-input) {
  flex: 1 1 auto;
  font-size: 14px;
}

kbd {
  border: 1px solid var(--app-border);
  border-radius: 4px;
  padding: 1px 6px;
  font-family: var(--app-mono);
  font-size: 10.5px;
  color: var(--app-muted);
  background: var(--app-surface-2);
  flex: 0 0 auto;
}

.chips {
  display: flex;
  gap: 8px;
  padding: 10px 16px;
}

.chip {
  height: 24px;
  padding: 0 12px;
  border: none;
  border-radius: 999px;
  background: var(--app-chip);
  color: var(--app-text-2);
  font-size: 12px;
  font-family: inherit;
  cursor: pointer;
}

.chip:hover {
  background: var(--app-chip-hover);
}

.chip.on {
  background: var(--app-accent);
  color: var(--app-on-accent);
}

.list {
  height: 292px;
  overflow: auto;
  padding: 0 8px 8px;
}

.item {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 9px 10px;
  border: none;
  background: none;
  border-radius: 8px;
  font-family: inherit;
  font-size: 13px;
  color: var(--app-text-2);
  text-align: left;
  cursor: pointer;
}

.item.on {
  background: var(--app-accent-tint);
}

.ii {
  color: var(--app-accent);
  flex: 0 0 auto;
}

.lb {
  flex: 1 1 auto;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.pa {
  flex: 0 0 auto;
  font-size: 11.5px;
  color: var(--app-placeholder);
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.none {
  padding: 30px 12px;
  text-align: center;
  font-size: 12.5px;
  color: var(--app-placeholder);
}

.foot {
  padding: 10px 16px;
  border-top: 1px solid var(--app-border);
  background: var(--app-surface-2);
  font-family: var(--app-mono);
  font-size: 11px;
  color: var(--app-muted);
}
</style>
