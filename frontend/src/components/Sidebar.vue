<script setup lang="ts">
// 集合树：根行（集合名 + 本地徽章）→ 分组 / 请求；底部为「本地存储」说明卡（design-spec §2）。
// 操作方式对齐服务端 Web：顶部工具条（展开/收起 + 新建分组 + 新建请求）、独立搜索行；
// 分组行 hover「＋/✎/🗑」、请求行 hover「✎/🗑」；行内输入回车提交、Esc 取消；删除二次确认。
import { NButton, NDropdown, NIcon, NInput, NPopconfirm } from 'naive-ui'
import type { InputInst } from 'naive-ui'
import {
  AddOutline,
  ChevronDownOutline,
  ChevronForwardOutline,
  ContractOutline,
  CreateOutline,
  DocumentTextOutline,
  ExpandOutline,
  FolderOpenOutline,
  TrashOutline,
} from '@vicons/ionicons5'
import { computed, h, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import MethodTag from '@/components/MethodTag.vue'
import { message } from '@/lib/notice'
import { useCollectionStore } from '@/stores/collection'
import { useTabsStore } from '@/stores/tabs'
import type { TreeNode } from '@/types'

const props = defineProps<{ tree: TreeNode[]; activeUid?: string; name: string }>()
const emit = defineEmits<{ open: [uid: string]; 'new-request': [folder: string] }>()
const { t } = useI18n()
const coll = useCollectionStore()
const tabs = useTabsStore()

const keyword = ref('')
const collapsed = ref(new Set<string>())
const treeEl = ref<HTMLElement | null>(null)
const editing = ref<{ uid: string; value: string } | null>(null)
const adding = ref<{ parent: string } | null>(null)
const addValue = ref('')
const addInput = ref<InputInst | null>(null)

interface Row {
  node: TreeNode
  depth: number
}

const INDENT = 13

const folderMenu = computed(() => [
  { key: 'request', label: t('tree.newRequest'), icon: () => h(NIcon, { component: AddOutline }) },
  { key: 'folder', label: t('tree.newSubFolder'), icon: () => h(NIcon, { component: FolderOpenOutline }) },
])

const searching = computed(() => keyword.value.trim() !== '')

// 搜索：命中请求保留其祖先分组
function filterTree(nodes: TreeNode[], q: string): TreeNode[] {
  const out: TreeNode[] = []
  for (const n of nodes) {
    if (n.type === 'folder') {
      const kids = n.children ? filterTree(n.children, q) : []
      if (kids.length || n.name.toLowerCase().includes(q)) {
        out.push({ ...n, children: kids.length ? kids : n.children })
      }
    } else if (n.name.toLowerCase().includes(q)) {
      out.push(n)
    }
  }
  return out
}

const rows = computed<Row[]>(() => {
  const src = searching.value ? filterTree(props.tree, keyword.value.trim().toLowerCase()) : props.tree
  const out: Row[] = []
  const walk = (nodes: TreeNode[], depth: number): void => {
    for (const n of nodes) {
      out.push({ node: n, depth })
      if (n.type === 'folder' && n.children?.length && !isCollapsed(n.path)) walk(n.children, depth + 1)
    }
  }
  walk(src, 0)
  return out
})

const allCollapsed = computed(() => {
  const dirs: string[] = []
  const walk = (nodes: TreeNode[]): void => {
    for (const x of nodes) {
      if (x.type === 'folder') {
        dirs.push(x.path)
        if (x.children) walk(x.children)
      }
    }
  }
  walk(props.tree)
  return dirs.length > 0 && dirs.every((p) => collapsed.value.has(p))
})

function isCollapsed(path: string): boolean {
  return !searching.value && collapsed.value.has(path)
}

function toggle(path: string): void {
  const next = new Set(collapsed.value)
  if (next.has(path)) next.delete(path)
  else next.add(path)
  collapsed.value = next
}

function toggleAll(): void {
  if (allCollapsed.value) {
    collapsed.value = new Set()
    return
  }
  const next = new Set<string>()
  const walk = (nodes: TreeNode[]): void => {
    for (const x of nodes) {
      if (x.type === 'folder') {
        next.add(x.path)
        if (x.children) walk(x.children)
      }
    }
  }
  walk(props.tree)
  collapsed.value = next
}

function fail(e: unknown): void {
  message.error(e instanceof Error ? e.message : String(e))
}

// ---- 行内重命名 ----
function startRename(node: TreeNode): void {
  adding.value = null
  editing.value = { uid: node.uid, value: node.name }
}

async function submitRename(): Promise<void> {
  const cur = editing.value
  if (!cur) return
  const name = cur.value.trim()
  editing.value = null
  if (!name) return
  const node = findNode(props.tree, cur.uid)
  if (!node || node.name === name) return
  try {
    if (node.type === 'folder') await coll.renameFolder(cur.uid, name)
    else await coll.renameRequest(cur.uid, name)
  } catch (e) {
    fail(e)
  }
}

// ---- 行内新建分组 ----
// v-for 内的模板 ref 会聚成数组，故用函数式 ref 只保留当前那一个输入框
function setAddRef(el: unknown): void {
  addInput.value = (el as InputInst | null) ?? null
}

async function startAdd(parent: string): Promise<void> {
  editing.value = null
  addValue.value = ''
  adding.value = { parent }
  if (parent) {
    const next = new Set(collapsed.value)
    next.delete(parent)
    collapsed.value = next
  }
  await nextTick()
  addInput.value?.focus()
}

async function submitAdd(): Promise<void> {
  const cur = adding.value
  if (!cur) return
  const name = addValue.value.trim()
  if (!name) {
    adding.value = null
    return
  }
  adding.value = null
  addValue.value = ''
  try {
    await coll.createFolder(cur.parent, name)
  } catch (e) {
    fail(e)
  }
}

// ---- 删除 ----
async function removeFolder(node: TreeNode): Promise<void> {
  try {
    await coll.deleteFolder(node.uid)
  } catch (e) {
    fail(e)
  }
}

async function removeRequest(node: TreeNode): Promise<void> {
  try {
    await tabs.deleteRequest(node.uid)
    await coll.reload()
  } catch (e) {
    fail(e)
  }
}

function onFolderMenu(node: TreeNode, key: string | number): void {
  if (key === 'request') emit('new-request', node.path)
  else void startAdd(node.path)
}

function findNode(nodes: TreeNode[], uid: string): TreeNode | null {
  for (const n of nodes) {
    if (n.uid === uid && (n.type === 'folder' || n.type === 'request')) return n
    if (n.children) {
      const hit = findNode(n.children, uid)
      if (hit) return hit
    }
  }
  return null
}

// 某节点的祖先分组路径（用于把当前选中的请求自动展开到可见）
function ancestorPaths(nodes: TreeNode[], uid: string, trail: string[] = []): string[] | null {
  for (const n of nodes) {
    if (n.uid === uid) return trail
    if (n.children) {
      const next = n.type === 'folder' ? [...trail, n.path] : trail
      const hit = ancestorPaths(n.children, uid, next)
      if (hit) return hit
    }
  }
  return null
}

// tab 切换 → 集合树同步选中：展开祖先分组并滚动到可见位置
watch(
  () => props.activeUid,
  async (uid) => {
    if (!uid) return
    const trail = ancestorPaths(props.tree, uid)
    if (trail?.some((p) => collapsed.value.has(p))) {
      const next = new Set(collapsed.value)
      for (const p of trail) next.delete(p)
      collapsed.value = next
      await nextTick()
    }
    // 搜索过滤后该行可能不在可视行里，静默跳过
    treeEl.value?.querySelector(`[data-uid="${uid}"]`)?.scrollIntoView({ block: 'nearest' })
  },
  { immediate: true },
)
</script>

<template>
  <div class="sidebar">
    <div class="head">
      <span class="sp" />
      <n-button quaternary size="tiny" :title="allCollapsed ? t('tree.expandAll') : t('tree.collapseAll')" @click="toggleAll">
        <template #icon>
          <n-icon :component="allCollapsed ? ExpandOutline : ContractOutline" />
        </template>
      </n-button>
      <n-button quaternary size="tiny" :title="t('tree.newFolder')" @click="startAdd('')">
        <template #icon><n-icon :component="FolderOpenOutline" /></template>
      </n-button>
      <n-button quaternary size="tiny" :title="t('tree.newRequest')" @click="emit('new-request', '')">
        <template #icon><n-icon :component="AddOutline" /></template>
      </n-button>
    </div>

    <div class="search">
      <n-input v-model:value="keyword" size="small" clearable :placeholder="t('sidebar.search')" />
    </div>

    <div ref="treeEl" class="tree">
      <div class="row root" :style="{ paddingLeft: '8px' }">
        <n-icon :component="FolderOpenOutline" :size="14" class="ric" />
        <span class="rname coll-name" :title="name">{{ name }}</span>
        <span class="badge">{{ t('local.badge') }}</span>
      </div>

      <div v-if="adding && adding.parent === ''" class="row" :style="{ paddingLeft: `${8 + INDENT}px` }">
        <n-input
          :ref="setAddRef"
          v-model:value="addValue"
          size="tiny"
          :placeholder="t('tree.folderName')"
          @keyup.enter="submitAdd"
          @blur="submitAdd"
          @keyup.esc="adding = null"
        />
      </div>

      <div v-if="!rows.length && !adding" class="empty muted">{{ t('sidebar.empty') }}</div>

      <template v-for="row in rows" :key="row.node.type + row.node.path">
        <div
          class="row"
          :class="{ folder: row.node.type === 'folder', on: row.node.uid === props.activeUid }"
          :data-uid="row.node.uid"
          :aria-current="row.node.uid === props.activeUid ? 'true' : undefined"
          :style="{ paddingLeft: 8 + (row.depth + 1) * INDENT + 'px' }"
        >
          <template v-if="row.node.type === 'folder'">
            <button class="caret" :title="t('tree.expandAll')" @click="toggle(row.node.path)">
              <n-icon
                :component="isCollapsed(row.node.path) ? ChevronForwardOutline : ChevronDownOutline"
                :size="13"
              />
            </button>
            <n-input
              v-if="editing?.uid === row.node.uid"
              v-model:value="editing.value"
              size="tiny"
              class="rename"
              @keyup.enter="submitRename"
              @blur="submitRename"
              @keyup.esc="editing = null"
            />
            <span v-else class="fname">{{ row.node.name }}</span>
            <span class="actions">
              <n-dropdown trigger="click" placement="bottom-start" :options="folderMenu" @select="onFolderMenu(row.node, $event)">
                <!-- 不能加 @click.stop：会拦在 NDropdown 的包装层之前，导致下拉打不开 -->
                <button class="act" type="button" :title="t('tree.new')">
                  <n-icon :component="AddOutline" :size="13" />
                </button>
              </n-dropdown>
              <button class="act" type="button" :title="t('tree.rename')" @click.stop="startRename(row.node)">
                <n-icon :component="CreateOutline" :size="13" />
              </button>
              <n-popconfirm @positive-click="removeFolder(row.node)">
                <template #trigger>
                  <button class="act danger" type="button" :title="t('tree.delDir')" @click.stop>
                    <n-icon :component="TrashOutline" :size="13" />
                  </button>
                </template>
                {{ t('tree.confirmDelFolder', { name: row.node.name }) }}
              </n-popconfirm>
            </span>
          </template>

          <template v-else>
            <method-tag :method="row.node.method ?? 'GET'" />
            <n-input
              v-if="editing?.uid === row.node.uid"
              v-model:value="editing.value"
              size="tiny"
              class="rename"
              @keyup.enter="submitRename"
              @blur="submitRename"
              @keyup.esc="editing = null"
            />
            <button v-else class="rname" :title="row.node.name" @click="emit('open', row.node.uid)">
              {{ row.node.name }}
            </button>
            <span class="actions">
              <button class="act" type="button" :title="t('tree.rename')" @click.stop="startRename(row.node)">
                <n-icon :component="CreateOutline" :size="13" />
              </button>
              <n-popconfirm @positive-click="removeRequest(row.node)">
                <template #trigger>
                  <button class="act danger" type="button" :title="t('tree.delApi')" @click.stop>
                    <n-icon :component="TrashOutline" :size="13" />
                  </button>
                </template>
                {{ t('tree.confirmDelRequest', { name: row.node.name }) }}
              </n-popconfirm>
            </span>
          </template>
        </div>

        <div
          v-if="adding && adding.parent === row.node.path && row.node.type === 'folder'"
          class="row"
          :style="{ paddingLeft: 8 + (row.depth + 2) * INDENT + 'px' }"
        >
          <n-input
            :ref="setAddRef"
            v-model:value="addValue"
            size="tiny"
            :placeholder="t('tree.subFolderName')"
            @keyup.enter="submitAdd"
            @blur="submitAdd"
            @keyup.esc="adding = null"
          />
        </div>
      </template>
    </div>

    <div class="store">
      <div class="sh">
        <n-icon :component="DocumentTextOutline" :size="13" />
        <span>{{ t('local.storage') }}</span>
      </div>
      <p class="sd">{{ t('local.storageHint') }}</p>
    </div>
  </div>
</template>

<style scoped>
.sidebar {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}

.head {
  display: flex;
  align-items: center;
  gap: 2px;
  padding: 4px 6px 0;
}

.sp {
  flex: 1 1 auto;
}

.search {
  padding: 0 8px 6px 10px;
}

.tree {
  flex: 1 1 auto;
  overflow: auto;
  padding-bottom: 10px;
}

.empty {
  padding: 16px 12px;
  font-size: 12px;
}

.row {
  display: flex;
  align-items: center;
  gap: 5px;
  min-height: 26px;
  padding-right: 6px;
  cursor: pointer;
}

.row:hover {
  background: var(--app-row-hover);
}

.row.root {
  cursor: default;
}

/* 选中请求行：design-spec §6.6 的 info-tint 浅蓝底 */
.row.on,
.row.on:hover {
  background: var(--app-info-tint);
}

.row.on .rname {
  color: var(--app-text);
  font-weight: 500;
}

.ric {
  color: var(--app-muted);
  flex: 0 0 auto;
}

.coll-name {
  font-size: 12.5px;
  font-weight: 500;
  color: var(--app-text);
}

.badge {
  font-size: 9.5px;
  line-height: 15px;
  padding: 0 6px;
  border-radius: 999px;
  background: var(--app-accent-tint);
  color: var(--app-accent-dark);
  flex: 0 0 auto;
  margin-left: 2px;
}

.caret {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  flex: 0 0 auto;
  border: none;
  background: none;
  padding: 0;
  color: var(--app-muted);
  cursor: pointer;
}

.fname {
  font-weight: 500;
  font-size: 12.5px;
  color: var(--app-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.rname {
  border: none;
  background: none;
  padding: 0;
  font-size: 12.5px;
  font-family: inherit;
  color: var(--app-text-2);
  cursor: pointer;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-align: left;
  flex: 1 1 auto;
}

.rename {
  flex: 1 1 auto;
  max-width: 190px;
}

.actions {
  display: none;
  align-items: center;
  gap: 3px;
  margin-left: auto;
  flex: 0 0 auto;
}

.row:hover .actions {
  display: inline-flex;
}

.act {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  background: none;
  color: var(--app-muted);
  line-height: 16px;
  padding: 0 3px;
  border-radius: 4px;
  cursor: pointer;
}

.act:hover {
  background: var(--app-chip-strong);
  color: var(--app-text);
}

.act.danger:hover {
  background: var(--app-danger-tint);
  color: var(--app-danger);
}

.store {
  flex: 0 0 auto;
  margin: 8px 10px 10px;
  padding: 9px 11px 10px;
  border: 1px solid var(--app-border);
  border-radius: 8px;
  background: var(--app-panel);
}

.sh {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  font-weight: 600;
  color: var(--app-text-2);
}

.sd {
  margin: 5px 0 0;
  font-size: 11px;
  line-height: 1.5;
  color: var(--app-muted);
}
</style>
