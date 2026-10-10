<script setup lang="ts">
// 集合树：根行（集合名 + 本地徽章）→ 分组 / 请求；底部为「本地存储」说明卡（design-spec §2）。
// 操作方式对齐服务端 Web：顶部工具条（展开/收起 + 新建分组 + 新建请求）、独立搜索行；
// 分组行 hover「＋/✎/🗑」、请求行 hover「✎/🗑」；行内输入回车提交、Esc 取消；删除二次确认。
import MethodTag from '@/components/MethodTag.vue'
import { message } from '@/lib/notice'
import { useCollectionStore } from '@/stores/collection'
import { useTabsStore } from '@/stores/tabs'
import type { CollectionSummary, TreeNode } from '@/types'
import {
  AddOutline,
  ChevronDownOutline,
  ChevronForwardOutline,
  CloseOutline,
  ContractOutline,
  CreateOutline,
  ExpandOutline,
  FolderOpenOutline,
  GitNetworkOutline,
  MoveOutline,
  OpenOutline,
  RefreshOutline,
  Star,
  StarOutline,
  SyncOutline,
  TerminalOutline,
  TrashOutline
} from '@vicons/ionicons5'
import type { InputInst } from 'naive-ui'
import { NButton, NDropdown, NIcon, NInput, NModal, NPopconfirm, NSelect } from 'naive-ui'
import { computed, h, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  /** 已打开的工作目录（单树多根：每个根行下面挂自己的树） */
  roots: CollectionSummary[]
  /** 活动根的标识（集合级动作都作用于它） */
  activeRoot: string
  /** 活动页签对应的请求 uid（行选中态 + 自动展开/滚动） */
  activeUid?: string
}>()
const emit = defineEmits<{
  open: [uid: string]
  'new-request': [folder: string]
  'new-grpc-request': [folder: string]
  'import-curl': [folder: string]
  /** 点击某个根：把它切成活动根 */
  activate: [root: string]
  /** 重载某个工作目录 */
  'reload-root': [root: string]
  /** 在系统文件管理器中打开某个工作目录 */
  'reveal-root': [root: string]
  /** 关闭某个工作目录 */
  'close-root': [root: string]
  /** 打开同步（作用于被点击的根） */
  sync: []
}>()
const { t } = useI18n()
const coll = useCollectionStore()
const tabs = useTabsStore()

const keyword = ref('')
/** 分组折叠态（键 = 根 + 路径，见 folderKey） */
const collapsed = ref(new Set<string>())
/** 工作目录折叠态（每个根行上的箭头） */
const collapsedRoots = ref(new Set<string>())
const treeEl = ref<HTMLElement | null>(null)
const editing = ref<{ root: string; uid: string; value: string } | null>(null)
const adding = ref<{ root: string; parent: string } | null>(null)
const addValue = ref('')
const addInput = ref<InputInst | null>(null)

interface Row {
  /** 所属工作目录（根行的 root 就是它自己） */
  root: string
  /** 根行（工作目录标题）：没有 node，不参与层级连接线 */
  isRoot: boolean
  node: TreeNode | null
  depth: number
  /** 是否是父级（可见）子行里的最后一个：决定本行连接线画成 ├ 还是 └ */
  last: boolean
  /**
   * 逐级祖先的竖向连接线是否继续，下标 i 对应深度 i+1 的祖先（长度 = depth - 1）。
   * 祖先还有后续兄弟才需要把竖线延伸下来，否则那一列是空的。
   */
  lines: boolean[]
  /** 置顶收藏区（平铺展示，不是集合根的子行）：不参与连接线 */
  pinned?: boolean
}

const INDENT = 16

/**
 * 连接线列相对「本行内容起点」的偏移，取展开按钮宽度的一半（`.caret` 宽 16px，图标在按钮里居中）。
 * 这样父分组的连接线正好从它的展开图标圆心垂下来，子行的横线则终止在内容起点 —— 于是
 * 「竖线 — 展开图标 — 内容」在同一列上对齐；如果偏移取 0，竖线会贴在按钮左边缘，看着与图标错开。
 */
const GUIDE_OFFSET = 8

const folderMenu = computed(() => [
  { key: 'request', label: t('tree.newRequest'), icon: () => h(NIcon, { component: AddOutline }) },
  { key: 'grpc', label: t('grpc.newRequest'), icon: () => h(NIcon, { component: GitNetworkOutline }) },
  { key: 'curl', label: t('curl.title'), icon: () => h(NIcon, { component: TerminalOutline }) },
  { key: 'folder', label: t('tree.newSubFolder'), icon: () => h(NIcon, { component: FolderOpenOutline }) },
])

/** 集合标题 “···” 下拉：集合级动作统一入口（展开/收起、新建分组/请求、导入 curl、重载、关闭）。 */
const rootMenu = computed(() => [
  {
    key: 'all',
    label: allCollapsed.value ? t('tree.expandAll') : t('tree.collapseAll'),
    icon: () => h(NIcon, { component: allCollapsed.value ? ExpandOutline : ContractOutline }),
  },
  { key: 'folder', label: t('tree.newFolder'), icon: () => h(NIcon, { component: FolderOpenOutline }) },
  { key: 'req', label: t('tree.newRequest'), icon: () => h(NIcon, { component: AddOutline }) },
  { key: 'grpc', label: t('grpc.newRequest'), icon: () => h(NIcon, { component: GitNetworkOutline }) },
  { key: 'curl', label: t('curl.title'), icon: () => h(NIcon, { component: TerminalOutline }) },
  { key: 'sync', label: t('sync.title'), icon: () => h(NIcon, { component: SyncOutline }) },
  { type: 'divider', key: 'd1' },
  { key: 'reveal', label: t('tree.revealDir'), icon: () => h(NIcon, { component: OpenOutline }) },
  { key: 'reload', label: t('tree.reloadDir'), icon: () => h(NIcon, { component: RefreshOutline }) },
  { key: 'close', label: t('tree.closeDir'), icon: () => h(NIcon, { component: CloseOutline }) },
])

/** 集合级动作先切到被点击的工作目录（写操作作用于它），再分发各分支。 */
async function onRootMenu(root: string, key: string | number): Promise<void> {
  // 只读动作（打开所在目录）作用于被点击的根本身：不切活动根，避免连带切换页签组
  if (key === 'reveal') {
    emit('reveal-root', root)
    return
  }
  await ensureActive(root)
  if (key === 'all') toggleAll()
  else if (key === 'folder') void startAdd(root, '')
  else if (key === 'req') emit('new-request', '')
  else if (key === 'grpc') emit('new-grpc-request', '')
  else if (key === 'curl') emit('import-curl', '')
  else if (key === 'sync') emit('sync')
  else if (key === 'reload') emit('reload-root', root)
  else if (key === 'close' && confirm(t('tree.confirmCloseDir', { name: rootName(root) }))) emit('close-root', root)
}

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

/** 每个根的树（搜索时按关键字过滤：命中的请求保留其祖先分组）。 */
function treeOf(root: CollectionSummary): TreeNode[] {
  return searching.value ? filterTree(root.info.tree, keyword.value.trim().toLowerCase()) : root.info.tree
}

/** 分组折叠态按「根 + 路径」记：两个根里可能有同路径的分组（拷贝出来的集合）。 */
function folderKey(root: string, path: string): string {
  return `${root}\n${path}`
}

/**
 * 根行下方有没有可见内容（子行，或正在输入的「新建分组」行）。
 * 决定根行要不要从展开图标向下接一条连接线：收起 / 空集合时不画，
 * 否则会留下一截悬空的竖线。
 */
function rootHasVisibleKids(root: string): boolean {
  if (collapsedRoots.value.has(root)) return false
  if (adding.value?.root === root && adding.value.parent === '') return true
  const item = props.roots.find((r) => r.root === root)
  return !!item && treeOf(item).length > 0
}

const rows = computed<Row[]>(() => {
  const out: Row[] = []
  // H5：收藏的请求置顶展示（不改变磁盘顺序）；收藏按根隔离，置顶区只显示活动根的
  const favUids = coll.favSet
  const favRows: Row[] = []
  // lines 逐级下传：进入子层时把「本行还有后续兄弟」补进列尾，正好是子行的一列祖先竖线。
  // 注意深度 0：挂在**根行**下的分组，其祖先列是根行这一列 —— 根行不是树节点、不画线，
  // 所以第一层下传空数组（否则 depth=1 行会在自己的列上多画一条竖线，├ 被画成满行）。
  const walk = (root: string, nodes: TreeNode[], depth: number, lines: boolean[]): void => {
    nodes.forEach((n, i) => {
      const last = i === nodes.length - 1
      out.push({ root, isRoot: false, node: n, depth, last, lines })
      if (n.type === 'folder' && n.children?.length && !isCollapsed(root, n.path)) {
        walk(root, n.children, depth + 1, depth === 0 ? [] : [...lines, !last])
      }
    })
  }
  for (const r of props.roots) {
    const tree = treeOf(r)
    // 搜索时该根没有命中：整根隐藏（否则多根平铺会被一堆空根行淹没）
    if (searching.value && tree.length === 0) continue
    out.push({ root: r.root, isRoot: true, node: null, depth: -1, last: true, lines: [] })
    if (collapsedRoots.value.has(r.root)) continue
    walk(r.root, tree, 0, [])
  }
  if (!searching.value && favUids.size) {
    for (const row of out) {
      if (row.isRoot || row.node?.type !== 'request' || !favUids.has(row.node.uid)) continue
      if (row.root !== props.activeRoot) continue // 置顶区只对活动根（收藏本身按根隔离）
      // 置顶区是平铺展示，不参与层级连接线
      favRows.push({ ...row, depth: 0, last: true, lines: [], pinned: true })
    }
    if (favRows.length) {
      // 收置顶区：在最前插入
      return [...favRows, ...out]
    }
  }
  return out
})

/** 行内「新建子分组」输入行所属的父行（连接线要与目标层级对齐）。 */
const addParentRow = computed(
  () =>
    rows.value.find(
      (r) =>
        !r.isRoot &&
        r.node?.type === 'folder' &&
        r.root === adding.value?.root &&
        r.node.path === adding.value?.parent,
    ) ?? null,
)

/** 根行的显示名 / 完整路径 / 只读态（模板里用，避免写一大串 find）。 */
function rootName(root: string): string {
  return props.roots.find((r) => r.root === root)?.info.name ?? ''
}

function rootDir(root: string): string {
  return props.roots.find((r) => r.root === root)?.info.dir ?? ''
}

function rootReadOnly(root: string): boolean {
  return props.roots.find((r) => r.root === root)?.readOnly ?? false
}

const allCollapsed = computed(() => {
  if (!props.roots.length) return false
  if (props.roots.some((r) => !collapsedRoots.value.has(r.root))) return false
  const dirs: string[] = []
  for (const r of props.roots) {
    const walk = (nodes: TreeNode[]): void => {
      for (const x of nodes) {
        if (x.type === 'folder') {
          dirs.push(folderKey(r.root, x.path))
          if (x.children) walk(x.children)
        }
      }
    }
    walk(r.info.tree)
  }
  return dirs.every((k) => collapsed.value.has(k))
})

function isCollapsed(root: string, path: string): boolean {
  return !searching.value && collapsed.value.has(folderKey(root, path))
}

function toggle(root: string, path: string): void {
  const key = folderKey(root, path)
  const next = new Set(collapsed.value)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  collapsed.value = next
}

/** 折叠/展开单个工作目录（根行上的箭头）。 */
function toggleRoot(root: string): void {
  const next = new Set(collapsedRoots.value)
  if (next.has(root)) next.delete(root)
  else next.add(root)
  collapsedRoots.value = next
}

/**
 * 点根行（集合名这一整片区域）：切活动根，且集合收起时展开它 ——
 * 「点一下就该看到集合下的第一层」，不必去点那个小箭头。只展开不收起：
 * 收起是明确的收缩意图，仍走行内箭头（caret）切换，避免误点把树折掉。
 *
 * 行内控件（caret / ··· 点状菜单 / 徽章 / 下拉）自己处理点击，这里要放行，
 * 不能因为用户只是想打开菜单就顺带切活动根、展开集合。
 */
function onRootRowClick(root: string, ev: MouseEvent): void {
  const el = ev.target as HTMLElement | null
  if (el?.closest('.act, .caret, .n-dropdown, .n-input, input, textarea, select, a')) return
  if (collapsedRoots.value.has(root)) {
    const next = new Set(collapsedRoots.value)
    next.delete(root)
    collapsedRoots.value = next
  }
  emit('activate', root)
}

function toggleAll(): void {
  if (allCollapsed.value) {
    collapsed.value = new Set()
    collapsedRoots.value = new Set()
    return
  }
  const next = new Set<string>()
  const roots = new Set<string>()
  for (const r of props.roots) {
    roots.add(r.root)
    const walk = (nodes: TreeNode[]): void => {
      for (const x of nodes) {
        if (x.type === 'folder') {
          next.add(folderKey(r.root, x.path))
          if (x.children) walk(x.children)
        }
      }
    }
    walk(r.info.tree)
  }
  collapsed.value = next
  collapsedRoots.value = roots
}

function fail(e: unknown): void {
  message.error(e instanceof Error ? e.message : String(e))
}

/**
 * 集合级动作（新建/重命名/删除/移动/打开）都由后端作用于**活动根**，
 * 所以操作非活动根里的行之前，必须先把那一根切成活动根 ——
 * 统一走 tabs.switchRoot：切根必须同时把标签组换过来（标签按根分组）。
 */
async function ensureActive(root: string): Promise<void> {
  if (root && root !== coll.activeRoot) await tabs.switchRoot(root)
}

// ---- 行内重命名 ----
async function startRename(root: string, node: TreeNode): Promise<void> {
  adding.value = null
  await ensureActive(root) // 重命名是集合级写操作：先把这一根切成活动根
  editing.value = { root, uid: node.uid, value: node.name }
}

async function submitRename(): Promise<void> {
  const cur = editing.value
  if (!cur) return
  const name = cur.value.trim()
  editing.value = null
  // 空名 / 未改动 = 取消，不触发保存
  if (!name) return
  const node = findNode(cur.root, cur.uid)
  if (!node || node.name === name) return
  try {
    await ensureActive(cur.root)
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

async function startAdd(root: string, parent: string): Promise<void> {
  editing.value = null
  addValue.value = ''
  await ensureActive(root)
  adding.value = { root, parent }
  if (parent) {
    const key = folderKey(root, parent)
    const next = new Set(collapsed.value)
    next.delete(key)
    collapsed.value = next
  }
  await nextTick()
  addInput.value?.focus()
}

async function submitAdd(): Promise<void> {
  const cur = adding.value
  if (!cur) return
  const name = addValue.value.trim()
  // 空名 = 取消，隐藏输入框
  if (!name) {
    adding.value = null
    addValue.value = ''
    return
  }
  adding.value = null
  addValue.value = ''
  try {
    await ensureActive(cur.root)
    await coll.createFolder(cur.parent, name)
  } catch (e) {
    fail(e)
  }
}

// ---- 删除 ----
async function removeFolder(root: string, node: TreeNode): Promise<void> {
  try {
    await ensureActive(root)
    await coll.deleteFolder(node.uid)
  } catch (e) {
    fail(e)
  }
}

async function removeRequest(root: string, node: TreeNode): Promise<void> {
  try {
    await ensureActive(root)
    await tabs.deleteRequest(node.uid)
    await coll.reload()
  } catch (e) {
    fail(e)
  }
}

async function onFolderMenu(root: string, node: TreeNode, key: string | number): Promise<void> {
  await ensureActive(root)
  if (key === 'request') emit('new-request', node.path)
  else if (key === 'grpc') emit('new-grpc-request', node.path)
  else if (key === 'curl') emit('import-curl', node.path)
  else void startAdd(root, node.path)
}

// ---- 拖动调整上级目录（自己实现，不用 HTML5 拖拽）----
// 拖动分组或请求行到目标分组上即改其上级目录；拖到顶部「集合根」行则移回根。
//
// 为什么不用 draggable / HTML5 DnD：桌面端跑在 WebView2 里，原生拖拽在嵌入式 WebView 下
// 触发不可靠（用户反馈：鼠标根本拖不动），而且拖拽期间渲染进程的输入派发会被原生拖拽循环
// 阻塞 —— 现象就是「按下去动了、但什么都不会发生」。改成 pointerdown/move/up 自己算命中，
// 行为在 WebView2 / Chromium / Firefox 下一致，也不再依赖 dataTransfer。
const DRAG_THRESHOLD = 4 // px：位移小于它算点击，不算拖动

const dragNode = ref<{ root: string; node: TreeNode } | null>(null)
/** 按下但还没越过阈值：等他动了才算拖动，避免影响正常点击打开请求 */
const pending = ref<{ root: string; node: TreeNode; x: number; y: number } | null>(null)
/** 当前指针下的放置目标；ok=false 表示非法（自己/后代/跨工作目录），给「禁止」反馈而不是静默 */
const dropHint = ref<{
  root: string
  uid: string
  path: string
  ok: boolean
  tip?: string
} | null>(null)
const dragging = computed(() => dragNode.value !== null)

/** 搜索过滤时树是残缺的，这时不允许拖动（拖到看不见的分组会让人困惑） */
const dragEnabled = computed(() => !searching.value && editing.value === null)

/** 拖动结束后紧跟的那次 click 要吞掉，否则松手会顺手打开请求 */
let suppressClick = false

/**
 * 能否把 node 放进 destRoot 里的 destPath（空串 = 该工作目录的根）。
 * 规则：目标必须是分组或根；同根内分组不能移入自己或自己的后代（会成环）；
 * 跨工作目录两种都能搬：请求单搬、分组连同其下子分组与请求整棵搬（目标侧重建 + 源进 .trash）。
 */
function canDrop(node: TreeNode, dragRoot: string, destRoot: string, destPath: string): boolean {
  if (dragRoot !== destRoot) return true
  if (node.type === 'folder' && destPath === node.path) return false
  if (node.type === 'folder' && destPath.startsWith(node.path + '/')) return false
  return true
}

function onRowPointerDown(root: string, node: TreeNode, ev: PointerEvent): void {
  if (!dragEnabled.value || ev.button !== 0) return
  // 只排除「行内操作控件」：展开箭头、悬停动作按钮、重命名输入框、下拉与链接。
  // 注意不能一刀切排除所有 button —— 请求/分组的名字本身就是个 <button class="rname">，
  // 一刀切会让整行都拖不动（实测就是这个原因导致鼠标拖起来毫无反应）。
  const el = ev.target as HTMLElement | null
  if (el?.closest('.act, .caret, .n-dropdown, .n-input, input, textarea, select, a')) return
  pending.value = { root, node, x: ev.clientX, y: ev.clientY }
  window.addEventListener('pointermove', onPointerMove)
  window.addEventListener('pointerup', onPointerUp)
  window.addEventListener('pointercancel', cancelDrag)
  window.addEventListener('keydown', onDragKeydown)
}

function detachDragListeners(): void {
  window.removeEventListener('pointermove', onPointerMove)
  window.removeEventListener('pointerup', onPointerUp)
  window.removeEventListener('pointercancel', cancelDrag)
  window.removeEventListener('keydown', onDragKeydown)
}

/** 指针所在位置对应的放置目标（用 elementFromPoint 命中行，不依赖事件冒泡顺序）。 */
function hintAt(x: number, y: number): typeof dropHint.value {
  const drag = dragNode.value
  if (!drag) return null
  const el = document.elementFromPoint(x, y) as HTMLElement | null
  const row = el?.closest('[data-testid="tree.row"], [data-testid="tree.root"]') as HTMLElement | null
  if (!row) return null
  const root = row.dataset.root ?? ''
  const isRootRow = row.dataset.testid === 'tree.root'
  if (!isRootRow && row.dataset.kind !== 'folder') return null // 请求行不能当容器
  const path = isRootRow ? '' : (row.dataset.path ?? '')
  // 跨根搬分组时目标下已有同名分组：后端会拒（CreateFolder 会把同名目录复用掉），
  // 这里提前置灰并把原因写在落点提示上，别让用户拖完才吃一个报错
  const tip =
    drag.node.type === 'folder' && hasSameNamedFolder(root, path, drag.node.name)
      ? t('tree.dupFolderHint', { name: drag.node.name })
      : undefined
  return {
    root,
    uid: isRootRow ? '__root__' : (row.dataset.uid ?? ''),
    path,
    ok: canDrop(drag.node, drag.root, root, path) && !tip,
    tip,
  }
}

/** 目标分组（空 = 该根的根）下是否已有同名分组。 */
function hasSameNamedFolder(root: string, destPath: string, name: string): boolean {
  const item = props.roots.find((r) => r.root === root)
  if (!item) return false
  const nodes = destPath ? (findFolderByPath(item.info.tree, destPath)?.children ?? []) : item.info.tree
  return nodes.some((n) => n.type === 'folder' && n.name === name)
}

/** 在树里按相对路径找分组节点（跨根提示用；只看当前根之外的根时也走它）。 */
function findFolderByPath(nodes: TreeNode[], path: string): TreeNode | null {
  for (const n of nodes) {
    if (n.type !== 'folder') continue
    if (n.path === path) return n
    if (n.children) {
      const hit = findFolderByPath(n.children, path)
      if (hit) return hit
    }
  }
  return null
}

function onPointerMove(ev: PointerEvent): void {
  const p = pending.value
  if (!p) return
  if (!dragNode.value) {
    if (Math.abs(ev.clientX - p.x) < DRAG_THRESHOLD && Math.abs(ev.clientY - p.y) < DRAG_THRESHOLD) return
    dragNode.value = { root: p.root, node: p.node }
  }
  ev.preventDefault() // 拖拽期间不选中文字
  dropHint.value = hintAt(ev.clientX, ev.clientY)
}

function onDragKeydown(ev: KeyboardEvent): void {
  if (ev.key === 'Escape') cancelDrag()
}

function cancelDrag(): void {
  detachDragListeners()
  pending.value = null
  dragNode.value = null
  dropHint.value = null
}

function onPointerUp(ev: PointerEvent): void {
  const drag = dragNode.value
  const hint = drag ? hintAt(ev.clientX, ev.clientY) : null
  const dragged = drag !== null
  detachDragListeners()
  pending.value = null
  dragNode.value = null
  dropHint.value = null
  if (!dragged) return // 只是点击：交给 click 处理
  suppressClick = true
  setTimeout(() => (suppressClick = false), 0)
  if (drag && hint?.ok) void performMove(drag.root, drag.node, hint.root, hint.path)
}

/**
 * 点击树里的行：请求行打开对应页签（拖动刚结束的那次点击忽略）；分组行折叠/展开。
 *
 * 分组行点**名字**也能折展（不必非去点那个小箭头）—— 文件管理器里就是这样，只认箭头的命中区太小。
 * 行内控件（箭头 / 下拉 / 操作按钮 / 重命名输入框）自己处理点击，这里要放行，否则点「+」建子项时会顺带折展一次。
 * 点的是非活动根里的请求时，先把那一根切成活动根 —— 页签是按根分组的。
 */
function onRowClick(root: string, node: TreeNode, ev: MouseEvent): void {
  if (editing.value || suppressClick) return
  const el = ev.target as HTMLElement | null
  if (el?.closest('.act, .caret, .n-dropdown, .n-input, input, textarea, select, a')) return
  if (node.type === 'folder') {
    toggle(root, node.path)
    return
  }
  if (node.type !== 'request') return
  if (root && root !== coll.activeRoot) {
    void ensureActive(root).then(() => emit('open', node.uid))
    return
  }
  emit('open', node.uid)
}

/** 执行移动：同目录改上级；跨目录则把请求 / 分组整棵搬到另一个工作目录。 */
async function performMove(
  root: string,
  node: TreeNode,
  destRoot: string,
  destPath: string,
): Promise<void> {
  if (!canDrop(node, root, destRoot, destPath)) return
  // 已经在目标位置就不动（避免无意义的写盘与提示）
  if (root === destRoot && node.path === destPath) return
  try {
    if (root !== destRoot) {
      // 跨工作目录：目标侧重建（新 uid/路径）+ 源进 .trash；分组连同子分组与请求整棵搬
      if (node.type === 'folder') {
        const moved = await coll.moveFolderTo(root, node.uid, destRoot, destPath)
        // 源根里这些请求已经不在这个目录了，对应标签一并摘掉（否则打开就是空壳）
        for (const uid of moved) tabs.forgetRequest(uid)
      } else {
        await coll.moveRequestTo(root, node.uid, destRoot, destPath)
        tabs.forgetRequest(node.uid)
      }
      message.success(t('tree.movedToDir', { name: rootName(destRoot) }))
      return
    }
    await ensureActive(root)
    if (node.type === 'folder') await coll.moveFolder(node.uid, destPath)
    else await coll.moveRequest(node.uid, destPath)
    // 目标分组若是折叠的会自动展开，否则用户看不到「移动成功了」
    const key = folderKey(root, destPath)
    if (destPath && collapsed.value.has(key)) {
      const next = new Set(collapsed.value)
      next.delete(key)
      collapsed.value = next
    }
    // 已打开的页签要同步路径（面包屑与后续保存都靠它）
    if (node.type === 'request') tabs.syncPath(node.uid, destPath)
    // 成功不再弹提示：落点横线 + 树即时刷新已经把结果说清楚了，多一条 toast 只是噪音
  } catch (e) {
    fail(e)
  }
}

// ---- 移动到目标分组 ----
const moving = ref<{ root: string; node: TreeNode; dest: string } | null>(null)

async function startMove(root: string, node: TreeNode): Promise<void> {
  await ensureActive(root)
  // 默认目标：该根的根（空串）；分组不能移入自己或后代（后端也会拒）
  moving.value = { root, node, dest: '' }
}

const destOptions = computed(() => {
  const out: { label: string; value: string }[] = [{ label: t('tree.moveRoot'), value: '' }]
  const root = moving.value?.root ?? coll.activeRoot
  const item = props.roots.find((r) => r.root === root)
  if (!item) return out
  const walk = (nodes: TreeNode[]): void => {
    for (const n of nodes) {
      if (n.type !== 'folder') continue
      // 分组移动时排除自己与后代
      if (
        moving.value?.node.type === 'folder' &&
        (n.uid === moving.value.node.uid || n.path.startsWith(moving.value.node.path + '/'))
      ) {
        continue
      }
      out.push({ label: n.name, value: n.path })
      if (n.children) walk(n.children)
    }
  }
  walk(item.info.tree)
  return out
})

async function submitMove(): Promise<void> {
  const cur = moving.value
  if (!cur) return
  moving.value = null
  try {
    await ensureActive(cur.root)
    if (cur.node.type === 'folder') await coll.moveFolder(cur.node.uid, cur.dest)
    else await coll.moveRequest(cur.node.uid, cur.dest)
  } catch (e) {
    fail(e)
  }
}

/** 在指定根的树里按 uid 找节点。 */
function findNode(root: string, uid: string): TreeNode | null {
  const item = props.roots.find((r) => r.root === root)
  return item ? searchNode(item.info.tree, uid) : null
}

function searchNode(nodes: TreeNode[], uid: string): TreeNode | null {
  for (const n of nodes) {
    if (n.uid === uid && (n.type === 'folder' || n.type === 'request')) return n
    if (n.children) {
      const hit = searchNode(n.children, uid)
      if (hit) return hit
    }
  }
  return null
}

// 某节点的祖先分组路径（用于把当前选中的请求自动展开到可见）；只在该根的树里找
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

/**
 * 滚动到指定根里的某一行。
 * 不能用属性选择器拼 root：Windows 路径里有反斜杠 / 冒号，CSS 转义很容易踩坑，
 * 直接按数据集比对最稳。
 */
function scrollToRow(root: string, uid: string): void {
  const list = treeEl.value?.querySelectorAll('[data-uid]') ?? []
  for (const el of Array.from(list)) {
    if (el.getAttribute('data-root') === root && el.getAttribute('data-uid') === uid) {
      el.scrollIntoView({ block: 'nearest' })
      return
    }
  }
}

// tab 切换 → 集合树同步选中：展开（该根 / 祖先分组）并滚动到可见位置
watch(
  () => props.activeUid,
  async (uid) => {
    if (!uid) return
    const root = coll.activeRoot
    const item = props.roots.find((r) => r.root === root)
    if (!item) return
    const trail = ancestorPaths(item.info.tree, uid)
    const unfoldRoot = collapsedRoots.value.has(root)
    const folded = (trail ?? []).filter((p) => collapsed.value.has(folderKey(root, p)))
    if (unfoldRoot || folded.length) {
      const roots = new Set(collapsedRoots.value)
      roots.delete(root)
      const next = new Set(collapsed.value)
      for (const p of folded) next.delete(folderKey(root, p))
      collapsedRoots.value = roots
      collapsed.value = next
      await nextTick()
    }
    // 搜索过滤后该行可能不在可视行里，静默跳过
    scrollToRow(root, uid)
  },
  { immediate: true },
)
</script>

<template>
  <div class="sidebar">
    <div class="search">
      <n-input v-model:value="keyword" size="small" clearable :placeholder="t('sidebar.search')" />
    </div>

    <div ref="treeEl" class="tree" :class="{ 'dnd-active': dragging }">
      <div v-if="!rows.length && !adding" class="empty muted">{{ t('sidebar.empty') }}</div>

      <template v-for="row in rows" :key="row.isRoot ? `root:${row.root}` : `${row.root}|${row.node?.path}`">
        <!-- 工作目录根行：既是该根的标题，也是「移回该根顶层」的放置目标（拖动时高亮） -->
        <template v-if="row.isRoot">
          <div class="row root" :class="{
            on: row.root === props.activeRoot,
            'root-active': row.root === props.activeRoot,
            'drop-ok': dragging && dropHint?.root === row.root && dropHint?.uid === '__root__' && dropHint.ok,
            'drop-bad': dragging && dropHint?.root === row.root && dropHint?.uid === '__root__' && !dropHint.ok,
          }" :style="{ paddingLeft: '8px' }" :data-root="row.root" :data-path="''" data-testid="tree.root"
            @click="onRootRowClick(row.root, $event)">
            <!-- 根行 → 子行的连接线：从展开图标圆心向下接出（收起 / 没有子行时不画） -->
            <span v-if="rootHasVisibleKids(row.root)" class="guides" aria-hidden="true" data-testid="tree.root.guides">
              <span class="gl root-down" :style="{ left: GUIDE_OFFSET + 'px' }" />
            </span>
            <button class="caret" data-testid="tree.root.caret" :title="t('tree.expandAll')"
              @click.stop="toggleRoot(row.root)">
              <n-icon :component="collapsedRoots.has(row.root) ? ChevronForwardOutline : ChevronDownOutline"
                :size="13" />
            </button>
            <span class="rname coll-name" :title="rootDir(row.root)">{{ rootName(row.root) }}</span>
            <span v-if="rootReadOnly(row.root)" class="badge badge-ro">{{ t('tree.readOnly') }}</span>
            <span v-else class="badge">{{ t('local.badge') }}</span>
            <span v-if="dragging" class="drop-tip">{{ dropHint?.root === row.root ? dropHint?.tip : t('tree.dropRoot')
            }}</span>
            <span class="actions">
              <n-dropdown trigger="click" placement="bottom-start" :options="rootMenu"
                @select="onRootMenu(row.root, $event)">
                <!-- 不能加 @click.stop：会拦在 NDropdown 的包装层之前，导致下拉打不开（同分组行「＋」） -->
                <button class="act" type="button" data-testid="tree.root.menu" :title="t('tree.more')" aria-label="···">
                  ···
                </button>
              </n-dropdown>
            </span>
          </div>

          <!-- 该根顶层的「新建分组」输入行：它是根的子行，所以与子行同缩进、同连接线。
             注意挂在根行分支里（不是 v-else-if）：挂外面时「任意一行」都能匹配，会渲染出多个输入框。 -->
          <div v-if="adding && adding.root === row.root && adding.parent === ''" class="row"
            :style="{ paddingLeft: `${8 + INDENT}px` }">
            <span class="guides" aria-hidden="true">
              <!-- 输入行排在子行最前：后面还有子行就是 ├，集合本来为空才是 └ -->
              <span class="gl own" :class="{ last: !rootHasVisibleKids(row.root) }"
                :style="{ left: GUIDE_OFFSET + 'px' }" />
              <span class="gl stub" :style="{ left: GUIDE_OFFSET + 'px' }" />
            </span>
            <n-input :ref="setAddRef" v-model:value="addValue" size="tiny" :placeholder="t('tree.folderName')"
              @keyup.enter="submitAdd" @blur="submitAdd" @keyup.esc="adding = null" />
          </div>
        </template>

        <template v-else>
          <div class="row" :class="{
            folder: row.node?.type === 'folder',
            on: row.node?.uid === props.activeUid && row.root === props.activeRoot,
            clickable: row.node?.type === 'request',
            dim: row.root !== props.activeRoot,
            dragging: dragNode?.root === row.root && dragNode?.node.uid === row.node?.uid,
            'drop-ok': dropHint?.root === row.root && dropHint?.uid === row.node?.uid && dropHint.ok,
            'drop-bad': dropHint?.root === row.root && dropHint?.uid === row.node?.uid && !dropHint.ok,
          }" :data-root="row.root" :data-uid="row.node?.uid" :data-kind="row.node?.type" :data-path="row.node?.path"
            data-testid="tree.row"
            :aria-current="row.node?.uid === props.activeUid && row.root === props.activeRoot ? 'true' : undefined"
            :style="{ paddingLeft: 8 + (row.depth + 1) * INDENT + 'px' }"
            @click="onRowClick(row.root, row.node!, $event)"
            @pointerdown="onRowPointerDown(row.root, row.node!, $event)">
            <!-- 层级连接线：祖先列竖线 + 本行 ├/└ + 指向内容的短横线（纯装饰，不参与布局）。
                 深度 0（直接挂在集合根下的分组/请求）也要画：它的那一列就是根行展开图标的圆心，
                 由根行自己向下接出（见 .gl.root-down），于是「集合 → 顶层条目」连成一条。 -->
            <span v-if="row.depth >= 0 && !row.pinned" class="guides" aria-hidden="true" data-testid="tree.row.guides">
              <span v-for="(cont, i) in row.lines" :key="`l${i}`" class="gl" :class="{ on: cont }"
                :style="{ left: (i + 1) * INDENT + GUIDE_OFFSET + 'px' }" />
              <span class="gl own" :class="{ last: row.last }"
                :style="{ left: row.depth * INDENT + GUIDE_OFFSET + 'px' }" />
              <span class="gl stub" :style="{ left: row.depth * INDENT + GUIDE_OFFSET + 'px' }" />
            </span>
            <template v-if="row.node!.type === 'folder'">
              <!-- 必须 .stop：不用它的话，箭头自己的折展会先把图标节点换掉，
                   同一个 click 冒泡到行时 ev.target 已是脱离 DOM 的旧节点 → 行里「排除 .caret」判不出来
                   → 又折展一次，等于没反应（实测）。 -->
              <!-- 没有下级（子分组 / 请求都为空）的目录不画展开图标：它无可展开，与叶子一致 -->
              <button v-if="!!row.node!.children?.length" class="caret" data-testid="tree.row.caret" :title="t('tree.expandAll')"
                @click.stop="toggle(row.root, row.node!.path)">
                <n-icon :component="isCollapsed(row.root, row.node!.path) ? ChevronForwardOutline : ChevronDownOutline"
                  :size="13" />
              </button>
              <n-input v-if="editing?.root === row.root && editing?.uid === row.node!.uid" v-model:value="editing.value"
                size="tiny" class="rename" @keyup.enter="submitRename" @blur="submitRename"
                @keyup.esc="editing = null" />
              <span v-else class="fname" data-testid="tree.row.name">{{ row.node!.name }}</span>
              <span class="actions">
                <n-dropdown trigger="click" placement="bottom-start" :options="folderMenu"
                  @select="onFolderMenu(row.root, row.node!, $event)">
                  <!-- 不能加 @click.stop：会拦在 NDropdown 的包装层之前，导致下拉打不开 -->
                  <button class="act" type="button" data-testid="tree.row.plus" :title="t('tree.new')">
                    <n-icon :component="AddOutline" :size="13" />
                  </button>
                </n-dropdown>
                <button class="act" type="button" data-testid="tree.row.rename" :title="t('tree.rename')"
                  @click.stop="startRename(row.root, row.node!)">
                  <n-icon :component="CreateOutline" :size="13" />
                </button>
                <button class="act" type="button" data-testid="tree.row.move" :title="t('tree.move')"
                  @click.stop="startMove(row.root, row.node!)">
                  <n-icon :component="MoveOutline" :size="13" />
                </button>
                <n-popconfirm @positive-click="removeFolder(row.root, row.node!)">
                  <template #trigger>
                    <button class="act danger" type="button" data-testid="tree.row.delete" :title="t('tree.delDir')"
                      @click.stop>
                      <n-icon :component="TrashOutline" :size="13" />
                    </button>
                  </template>
                  {{ t('tree.confirmDelFolder', { name: row.node!.name }) }}
                </n-popconfirm>
              </span>
            </template>

            <template v-else>
              <method-tag :method="row.node!.method ?? 'GET'" />
              <n-input v-if="editing?.root === row.root && editing?.uid === row.node!.uid" v-model:value="editing.value"
                size="tiny" class="rename" @keyup.enter="submitRename" @blur="submitRename"
                @keyup.esc="editing = null" />
              <button v-else class="rname" data-testid="tree.row.name" :title="row.node!.name">
                {{ row.node!.name }}
              </button>
              <span class="actions">
                <button class="act" type="button" data-testid="tree.row.fav"
                  :title="coll.isFav(row.node!.uid) ? t('tree.unfav') : t('tree.fav')"
                  @click.stop="coll.toggleFav(row.node!.uid)">
                  <n-icon :component="coll.isFav(row.node!.uid) ? Star : StarOutline" :size="13" />
                </button>
                <button class="act" type="button" data-testid="tree.row.rename" :title="t('tree.rename')"
                  @click.stop="startRename(row.root, row.node!)">
                  <n-icon :component="CreateOutline" :size="13" />
                </button>
                <button class="act" type="button" data-testid="tree.row.move" :title="t('tree.move')"
                  @click.stop="startMove(row.root, row.node!)">
                  <n-icon :component="MoveOutline" :size="13" />
                </button>
                <n-popconfirm @positive-click="removeRequest(row.root, row.node!)">
                  <template #trigger>
                    <button class="act danger" type="button" data-testid="tree.row.delete" :title="t('tree.delApi')"
                      @click.stop>
                      <n-icon :component="TrashOutline" :size="13" />
                    </button>
                  </template>
                  {{ t('tree.confirmDelRequest', { name: row.node!.name }) }}
                </n-popconfirm>
              </span>
            </template>
          </div>

          <!-- 该分组下的「新建子分组」输入行 -->
          <div
            v-if="adding && adding.root === row.root && adding.parent === row.node!.path && row.node!.type === 'folder'"
            class="row" :style="{ paddingLeft: 8 + (row.depth + 2) * INDENT + 'px' }">
            <!-- 输入行是该分组的子行：接上它那一列（还有后续兄弟则竖线继续），自己是 └ -->
            <span v-if="addParentRow" class="guides" aria-hidden="true">
              <span v-for="(cont, i) in addParentRow.lines" :key="`l${i}`" class="gl" :class="{ on: cont }"
                :style="{ left: (i + 1) * INDENT + GUIDE_OFFSET + 'px' }" />
              <!-- 父分组自身那一列：它挂在集合根下（depth 0）时没有祖先列，不能画 -->
              <span v-if="addParentRow.depth > 0" class="gl" :class="{ on: !addParentRow.last }"
                :style="{ left: addParentRow.depth * INDENT + GUIDE_OFFSET + 'px' }" />
              <span class="gl own last" :style="{ left: (addParentRow.depth + 1) * INDENT + GUIDE_OFFSET + 'px' }" />
              <span class="gl stub" :style="{ left: (addParentRow.depth + 1) * INDENT + GUIDE_OFFSET + 'px' }" />
            </span>
            <n-input :ref="setAddRef" v-model:value="addValue" size="tiny" :placeholder="t('tree.subFolderName')"
              @keyup.enter="submitAdd" @blur="submitAdd" @keyup.esc="adding = null" />
          </div>
        </template>
      </template>
    </div>

    <n-modal :show="!!moving" preset="card" :title="t('tree.moveTitle')" style="width: 420px"
      @update:show="(v: boolean) => { if (!v) moving = null }">
      <div v-if="moving" class="move-body">
        <p class="move-hint">{{ t('tree.moveHint', { name: moving.node.name }) }}</p>
        <n-select v-model:value="moving.dest" :options="destOptions" size="small" data-testid="tree.move.dest" />
      </div>
      <template #footer>
        <div class="move-ft">
          <n-button size="small" @click="moving = null">{{ t('common.cancel') }}</n-button>
          <n-button size="small" type="primary" data-testid="tree.move.ok" @click="submitMove">
            {{ t('tree.moveOk') }}
          </n-button>
        </div>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.move-body {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.move-hint {
  margin: 0;
  font-size: 12px;
  color: var(--app-muted);
}

.move-ft {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>

<style scoped>
.sidebar {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}

.search {
  padding: 0 8px 6px 10px;
}

.tree {
  position: relative;
  flex: 1 1 auto;
  overflow: auto;
  padding-bottom: 10px;
}

.empty {
  padding: 16px 12px;
  font-size: 12px;
}

/* ---- 拖动调整上级目录 ---- */

.row.dragging {
  opacity: 0.45;
}

/* 拖动期间禁止选中文字（拖拽由指针事件驱动，不靠原生拖拽） */
.tree.dnd-active {
  user-select: none;
  cursor: grabbing;
}

/* 合法放置目标：左强调条 + 淡底，并在**底边拉一条横线**（最直观地指出会落到哪一行）。
   用伪元素画线而不是 border：border 会改变行高，把指针下的行推走。 */
.row.drop-ok {
  position: relative;
  box-shadow: inset 2px 0 0 var(--app-accent);
  background: var(--app-accent-tint);
  border-radius: 4px;
}

.row.drop-ok::after {
  content: '';
  position: absolute;
  left: 0;
  right: 0;
  bottom: -1px;
  height: 2px;
  background: var(--app-accent);
  border-radius: 2px;
}

/* 非法目标也给一条红色底横线：位置照样看得见，但用颜色说明「这里不行」 */
.row.drop-bad::after {
  content: '';
  position: absolute;
  left: 0;
  right: 0;
  bottom: -1px;
  height: 2px;
  background: var(--app-danger);
  border-radius: 2px;
}

/* 非法目标（自己/自己的后代）：淡红底，不给 accent 反馈 */
.row.drop-bad {
  position: relative;
  background: var(--app-danger-tint);
  cursor: not-allowed;
}

.drop-tip {
  margin-left: auto;
  padding-left: 8px;
  font-size: 11px;
  color: var(--app-accent-dark);
  white-space: nowrap;
}

.row {
  position: relative;
  display: flex;
  align-items: center;
  gap: 3px;
  min-height: 26px;
  padding-right: 6px;
  cursor: pointer;
  user-select: none;
}

/* ---- 集合树连接线（层级可见性）----
   缩进由行的 padding-left 提供，连接线用绝对定位叠加在缩进区里（不占布局，行高/命中不受影响）：
   第 m 列在 x = 8 + m*INDENT + GUIDE_OFFSET，也就是「深度 m-1 那个分组的展开图标圆心」那一列。 */
.guides {
  position: absolute;
  left: 8px;
  top: 0;
  bottom: 0;
  pointer-events: none;
}

/* 竖向连接线：默认留空（该级祖先已是最后一个子行），.on 才落线 */
.gl {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 1px;
  background: transparent;
}

.gl.on {
  background: var(--app-guide);
}

/* 本行的 ├：整行高，接到下一个兄弟 */
.gl.own {
  background: var(--app-guide);
}

/* 本行的 └：只画到中线 */
.gl.own.last {
  bottom: auto;
  height: 50%;
}

/* 指向内容的短横线（与 .gl.own 同列，落在行的垂直中线上）：宽度 = INDENT - GUIDE_OFFSET */
.gl.stub {
  top: calc(50% - 0.5px);
  bottom: auto;
  width: 8px;
  height: 1px;
  background: var(--app-guide);
  border-radius: 1px;
}

/* 根行向下的接出线：从行中线（也就是展开图标的圆心高度）落到行底，
   正好与第一层子行那一列的竖线接上 —— 「集合 → 顶层条目」连成一条。 */
.gl.root-down {
  top: 50%;
  bottom: 0;
  background: var(--app-guide);
}

.row:hover {
  background: var(--app-row-hover);
}

/* 请求行整行可点（打开详情） */
.row.clickable {
  cursor: pointer;
}

/* 请求行方法与文字：同字号、同基线。
   方法徽标默认 10px（给页签 / 请求栏那种紧凑场景），树上比 12.5px 的请求名小一截；
   字号不同时，相同 line-height 下两者的基线会差出 1px 以上，看着就是两个字错位。
   这里统一成与请求名同样的字号（line-height 本来就都是 26px），基线随之对齐到 0.5px 内。 */
.row>.mt,
.row>.rname {
  display: inline-flex;
  align-items: center;
  line-height: 26px;
  font-size: 12.5px;
}

/* 方法与名称之间再收窄 */
.row>.mt+.rname {
  margin-left: -2px;
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

/* 集合根行：行高略高、字号比树里的条目大一档，层级一眼可辨。
   选择器必须写成 `.row.root > .coll-name`：`.row > .rname`（同为两个类）也命中根行的名字，
   两者同在时按后出现的算 —— 只写 `.coll-name` 的话 15px 会被 12.5px 盖掉（旧代码的 14px 就是这么失效的）。 */
.row.root {
  min-height: 30px;
}

.row.root>.coll-name {
  font-size: 15px;
  color: var(--app-text);
}

.badge {
  font-size: 10px;
  font-weight: 700;
  line-height: 15px;
  padding: 0 6px;
  border-radius: 999px;
  background: var(--app-accent-tint);
  color: var(--app-accent-dark);
  flex: 0 0 auto;
  margin-left: 2px;
}

/* 只读工作目录（同步镜像）：警示色徽章，提醒写操作会被拒 */
.badge-ro {
  background: var(--app-warn-tint);
  color: var(--app-warn);
}

/* 非活动根：整棵树降低对比度，让当前工作目录更突出（仍可读、可操作） */
.row.dim>.fname,
.row.dim>.rname {
  color: var(--app-muted);
}

/* 活动根：底色 + 左侧强调条 + 名称再大一档，一眼看出「集合级操作会落到谁身上」 */
.row.root.root-active {
  background: var(--app-info-tint);
  box-shadow: inset 2px 0 0 var(--app-accent);
}

.row.root.root-active>.coll-name {
  font-size: 16px;
  color: var(--app-accent-dark);
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
  /* 与请求名同字号、同字重（不加重）：目录与请求在树里是平级条目，靠文件夹图标区分即可 */
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

/* 集合根行的「···」常驻显示（不用悬停才出现） */
.row.root .actions {
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
</style>
