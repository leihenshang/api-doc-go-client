<script setup lang="ts">
// 「导出 Markdown / HTML」的范围选择弹窗：全部导出，或只挑部分目录 / 请求 / 文档。
// 集合大时一份文档几百节，用户经常只想给某个模块出文档，所以导出前先选范围。
// 组件只负责「选出了什么」，落盘仍由 App.vue 调 Go 侧（与直接导出走同一条链路）。
import { NButton, NInput, NModal, NTree } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ExportSelection, TreeNode } from '@/types'

const props = defineProps<{
  show: boolean
  /** 导出格式，只用于标题与默认文件名展示。 */
  format: 'markdown' | 'html'
  /** 集合树（与侧栏同源：folder / request / doc 混排）。 */
  tree: TreeNode[]
}>()
const emit = defineEmits<{
  'update:show': [v: boolean]
  confirm: [sel: ExportSelection]
}>()

const { t } = useI18n()

/** all = 整个集合；pick = 只导出勾选项。 */
const mode = ref<'all' | 'pick'>('all')
const keyword = ref('')
const checkedKeys = ref<string[]>([])

/** n-tree 只认 key / label / children：方法名与文档图标直接拼进 label，省掉自定义渲染槽。 */
interface PickNode {
  key: string
  label: string
  children?: PickNode[]
  /** naive-ui 的 TreeOption 要求可索引，缺了这条会判为不兼容。 */
  [k: string]: unknown
}

/** uid → 条目类型：勾选结果要按类型分别回传（三条链路各查各的表）。 */
const kindByKey = computed(() => {
  const m = new Map<string, 'folder' | 'request' | 'doc'>()
  const walk = (nodes: TreeNode[]): void => {
    for (const n of nodes) {
      m.set(n.uid, n.type)
      if (n.children?.length) walk(n.children)
    }
  }
  walk(props.tree)
  return m
})

/** 全部可勾选的 key（含目录）：用于「全选」与初始态。 */
const allKeys = computed(() => [...kindByKey.value.keys()])

function toPickNode(n: TreeNode): PickNode {
  const label = n.type === 'request' ? `${n.method ?? ''} ${n.name}`.trim() : n.type === 'doc' ? `📄 ${n.name}` : n.name
  return { key: n.uid, label, children: n.children?.length ? n.children.map(toPickNode) : undefined }
}

/**
 * 搜索时保留命中节点及其祖先：只显示命中的叶子会丢掉层级，勾选后看不出归属。
 * 目录自身命中时整棵子树都保留（用户搜的是模块名，想看里面有哪些接口）。
 */
function filterNodes(nodes: PickNode[], kw: string): PickNode[] {
  if (!kw) return nodes
  const out: PickNode[] = []
  for (const n of nodes) {
    const selfHit = n.label.toLowerCase().includes(kw)
    if (selfHit) {
      out.push(n)
      continue
    }
    const kids = n.children ? filterNodes(n.children, kw) : []
    if (kids.length) out.push({ ...n, children: kids })
  }
  return out
}

const pickTree = computed<PickNode[]>(() => filterNodes(props.tree.map(toPickNode), keyword.value.trim().toLowerCase()))

/** 勾选统计：选中目录只算它子树里的叶子（否则目录与叶子会被重复计数）。 */
const summary = computed(() => {
  const set = new Set(checkedKeys.value)
  let reqs = 0
  let docs = 0
  let folders = 0
  const walk = (nodes: TreeNode[], inherited: boolean): void => {
    for (const n of nodes) {
      const picked = inherited || set.has(n.uid)
      if (n.type === 'folder') {
        if (picked && !inherited) folders++
        walk(n.children ?? [], picked)
        continue
      }
      if (!picked) continue
      if (n.type === 'doc') docs++
      else reqs++
    }
  }
  walk(props.tree, false)
  return { reqs, docs, folders }
})

const nothingPicked = computed(() => mode.value === 'pick' && summary.value.reqs + summary.value.docs + summary.value.folders === 0)

// 每次打开回到「全部」并重置勾选（默认勾满：从全选开始取消，比从空开始勾更省事）
watch(
  () => props.show,
  (v) => {
    if (!v) return
    mode.value = 'all'
    keyword.value = ''
    checkedKeys.value = [...allKeys.value]
  },
)

function toggleMode(v: 'all' | 'pick'): void {
  mode.value = v
  if (v === 'pick' && checkedKeys.value.length === 0) checkedKeys.value = [...allKeys.value]
}

/** n-tree 的 key 可能是 string | number（这里全是 uid 字符串），统一成 string。 */
function onCheck(keys: Array<string | number>): void {
  checkedKeys.value = keys.map(String)
}

function selectAll(): void {
  checkedKeys.value = [...allKeys.value]
}

function clearAll(): void {
  checkedKeys.value = []
}

function confirm(): void {
  if (nothingPicked.value) return
  const sel: ExportSelection = { all: mode.value === 'all', groups: [], requests: [], docs: [] }
  if (mode.value === 'pick') {
    for (const key of checkedKeys.value) {
      const kind = kindByKey.value.get(key)
      if (kind === 'folder') sel.groups.push(key)
      else if (kind === 'doc') sel.docs.push(key)
      else if (kind === 'request') sel.requests.push(key)
    }
  }
  emit('confirm', sel)
  emit('update:show', false)
}
</script>

<template>
  <n-modal :show="show" preset="card" :title="t('exportDialog.title', { format: format === 'html' ? 'HTML' : 'Markdown' })"
    style="width: 560px; max-width: 94vw" data-testid="export.dialog" @update:show="emit('update:show', $event)">
    <div class="ex-body">
      <div class="ex-modes">
        <button class="ex-mode" :class="{ on: mode === 'all' }" type="button" data-testid="export.modeAll"
          @click="toggleMode('all')">{{ t('exportDialog.all') }}</button>
        <button class="ex-mode" :class="{ on: mode === 'pick' }" type="button" data-testid="export.modePick"
          @click="toggleMode('pick')">{{ t('exportDialog.pick') }}</button>
      </div>

      <template v-if="mode === 'pick'">
        <div class="ex-tools">
          <n-input v-model:value="keyword" size="small" clearable :placeholder="t('exportDialog.search')"
            data-testid="export.search" />
          <n-button size="tiny" tertiary data-testid="export.selectAll" @click="selectAll">{{ t('exportDialog.selectAll') }}</n-button>
          <n-button size="tiny" tertiary data-testid="export.clear" @click="clearAll">{{ t('exportDialog.clear') }}</n-button>
        </div>
        <div class="ex-tree" data-testid="export.tree">
          <n-tree v-if="pickTree.length" block-line cascade checkable :data="pickTree" :checked-keys="checkedKeys"
            :selectable="false" :default-expand-all="true" key-field="key" label-field="label"
            children-field="children" @update:checked-keys="onCheck" />
          <p v-else class="ex-empty">{{ t('exportDialog.noMatch') }}</p>
        </div>
        <p class="ex-sum" data-testid="export.summary">
          {{ t('exportDialog.summary', { reqs: summary.reqs, docs: summary.docs, folders: summary.folders }) }}
        </p>
      </template>
      <p v-else class="ex-sum">{{ t('exportDialog.allHint', { n: summary.reqs + summary.docs }) }}</p>
    </div>

    <template #footer>
      <div class="ex-ft">
        <span class="ex-tip">{{ nothingPicked ? t('exportDialog.empty') : '' }}</span>
        <n-button size="small" data-testid="export.cancel" @click="emit('update:show', false)">{{ t('common.cancel') }}</n-button>
        <n-button size="small" type="primary" :disabled="nothingPicked" data-testid="export.confirm" @click="confirm">
          {{ t('exportDialog.confirm') }}
        </n-button>
      </div>
    </template>
  </n-modal>
</template>

<style scoped>
.ex-body {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-height: 0;
}

.ex-modes {
  display: flex;
  gap: 4px;
}

.ex-mode {
  border: none;
  background: none;
  border-radius: 5px;
  padding: 5px 12px;
  font-family: inherit;
  font-size: 12px;
  color: var(--app-muted);
  cursor: pointer;
}

.ex-mode:hover {
  background: var(--app-row-hover);
  color: var(--app-text);
}

.ex-mode.on {
  background: var(--app-accent-tint);
  color: var(--app-accent-dark);
  font-weight: 600;
}

.ex-tools {
  display: flex;
  align-items: center;
  gap: 6px;
}

.ex-tools :deep(.n-input) {
  flex: 1 1 auto;
}

.ex-tree {
  max-height: 46vh;
  overflow: auto;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  padding: 6px 8px;
}

.ex-sum {
  margin: 0;
  font-size: 11.5px;
  color: var(--app-muted);
}

.ex-empty {
  margin: 12px 0;
  text-align: center;
  font-size: 12px;
  color: var(--app-muted);
}

.ex-ft {
  display: flex;
  align-items: center;
  gap: 8px;
}

.ex-tip {
  flex: 1 1 auto;
  font-size: 11.5px;
  color: var(--app-muted);
}
</style>
