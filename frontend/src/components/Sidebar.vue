<script setup lang="ts">
// 集合树：根行（集合名 + 云端徽章，仅已关联服务端时显示）→ 分组 / 请求；底部为「本地存储」说明卡（design-spec §2）。
// 操作方式对齐服务端 Web：顶部工具条（展开/收起 + 新建分组 + 新建请求）、独立搜索行；
// 分组行 hover「＋/✎/🗑」、请求行 hover「✎/🗑」；行内输入回车提交、Esc 取消；删除二次确认。
import MethodTag from '@/components/MethodTag.vue'
import SidebarDialogs from '@/components/SidebarDialogs.vue'
import { useTreeDnd, ROOT_NODE, type DropHint, type TreeDragNode } from '@/composables/useTreeDnd'
import {
  ancestorPaths,
  canDrop,
  filterTree,
  findFolderByPath,
  folderNameTaken,
  searchNode,
  sortTree,
  type SortMode,
} from '@/lib/collTree'
import { api } from '@/lib/ipc'
import { dialog, message } from '@/lib/notice'
import { useCollectionStore } from '@/stores/collection'
import { useTabsStore } from '@/stores/tabs'
import type { CollectionSummary, TreeNode } from '@/types'
import {
  AddOutline,
  ArrowDownOutline,
  ArrowUpOutline,
  CheckmarkOutline,
  ChevronDownOutline,
  ChevronForwardOutline,
  CloseOutline,
  ContractOutline,
  CreateOutline,
  DocumentTextOutline,
  EllipsisHorizontalOutline,
  ExpandOutline,
  FolderOpenOutline,
  GitNetworkOutline,
  OpenOutline,
  RefreshOutline,
  SearchOutline,
  Star,
  StarOutline,
  SwapVerticalOutline,
  SyncOutline,
  TerminalOutline,
  TrashOutline
} from '@vicons/ionicons5'
import type { DropdownOption, InputInst } from 'naive-ui'
import { NDropdown, NIcon, NInput } from 'naive-ui'
import { computed, h, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  /** 已打开的工作目录（单树多根：每个根行下面挂自己的树） */
  roots: CollectionSummary[]
  /** 活动根的标识（集合级动作都作用于它） */
  activeRoot: string
  /** 活动页签对应的请求 uid（行选中态 + 自动展开/滚动） */
  activeUid?: string
  /** 页签点击信号：每点一次页签加一（重复点当前页签时 activeUid 不变，靠它触发滚动） */
  revealTick?: number
  /** 正在同步的根（根行刷新按钮转圈） */
  busySyncRoots?: string[]
}>()
const emit = defineEmits<{
  open: [uid: string]
  'new-request': [folder: string]
  'new-grpc-request': [folder: string]
  'new-doc': [folder: string]
  'import-curl': [folder: string]
  /** 点击某个根：把它切成活动根 */
  activate: [root: string]
  /** 重载某个工作目录 */
  'reload-root': [root: string]
  /** 在系统文件管理器中打开某个工作目录 */
  'reveal-root': [root: string]
  /** 关闭某个工作目录 */
  'close-root': [root: string]
  /** 打开同步设置（作用于活动根） */
  sync: []
  /** 点根行刷新：直接同步指定根，不切换活动根 */
  'sync-root': [root: string]
  /** 工具条「打开集合」：弹出目录选择（App 侧统一走 openCollection 流程） */
  'open-collection': []
  /** 工具条「创建集合」：已选好父目录并填了名称，dir 为完整目标路径（不存在由 OpenCollection 初始化） */
  'create-collection': [dir: string]
}>()
const { t } = useI18n()
const coll = useCollectionStore()
const tabs = useTabsStore()

const keyword = ref('')
/** 分组折叠态（键 = 根 + 路径，见 folderKey） */
const collapsed = ref(new Set<string>())
/** 已见过的目录键：只对首次出现的目录补折叠，树刷新时不覆盖用户手动的展开态。 */
const seenFolders = new Set<string>()
/**
 * 工作目录展开态（仅本次会话，不落盘）。
 * 初值恒为空 => 首次打开 / 进程重启 / 页面 reload 后所有根默认收起。
 * 注意：最小化、关闭到托盘只是隐藏窗口（WindowHide/Show），Sidebar 不卸载，
 * 因此展开态会原样保留；只有组件重建才回到全部收起。避免记忆展开集合带来的复杂性。
 */
const expandedRoots = ref<Set<string>>(new Set())
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

/**
 * 集合标题 "···" 下拉：集合级动作统一入口（展开/收起、新建分组/请求、导入 curl、重载、关闭）。
 *
 * 必须按根生成（不是一份共享的 computed）：菜单挂在**某一个**集合的行上，
 * 「全部收起 / 全部展开」的文案与 testid 取决于**这个根**的折叠态。
 * 早先是一份共享菜单 + 全量 toggleAll，结果点 A 集合的「全部收起」把 B、C 也一起收了 ——
 * 用户按行操作时预期的是"这一行自己的集合"。
 */
function rootMenuFor(root: string): DropdownOption[] {
  const folded = rootAllCollapsed(root)
  return [
    {
      key: 'all',
      label: folded ? t('tree.expandAll') : t('tree.collapseAll'),
      icon: () => h(NIcon, { component: folded ? ExpandOutline : ContractOutline }),
      testid: folded ? 'tree.root.expandAll' : 'tree.root.collapseAll',
    },
    { key: 'folder', label: t('tree.newFolder'), icon: () => h(NIcon, { component: FolderOpenOutline }), testid: 'tree.root.newFolder' },
    { key: 'req', label: t('tree.newRequest'), icon: () => h(NIcon, { component: AddOutline }), testid: 'tree.root.newRequest' },
    { key: 'grpc', label: t('grpc.newRequest'), icon: () => h(NIcon, { component: GitNetworkOutline }), testid: 'tree.root.newGrpc' },
    { key: 'doc', label: t('tree.newDoc'), icon: () => h(NIcon, { component: DocumentTextOutline }), testid: 'tree.row.newDoc' },
    { key: 'curl', label: t('curl.title'), icon: () => h(NIcon, { component: TerminalOutline }), testid: 'tree.root.importCurl' },
    { key: 'sync', label: t('sync.settings'), icon: () => h(NIcon, { component: SyncOutline }), testid: 'tree.root.sync' },
    { type: 'divider', key: 'd1' },
    { key: 'reveal', label: t('tree.revealDir'), icon: () => h(NIcon, { component: OpenOutline }), testid: 'tree.root.reveal' },
    { key: 'reload', label: t('tree.reloadDir'), icon: () => h(NIcon, { component: RefreshOutline }), testid: 'tree.root.reload' },
    { key: 'close', label: t('tree.closeDir'), icon: () => h(NIcon, { component: CloseOutline }), testid: 'tree.root.close' },
  ]
}

/** 集合级动作先切到被点击的工作目录（写操作作用于它），再分发各分支。 */
async function onRootMenu(root: string, key: string | number): Promise<void> {
  // 只读动作（打开所在目录 / 全部收起展开）作用于被点击的根本身：
  // 不切活动根，避免只是想折一下树就把页签组和当前请求都换掉。
  if (key === 'reveal') {
    emit('reveal-root', root)
    return
  }
  if (key === 'all') {
    toggleRootAll(root)
    return
  }
  await ensureActive(root)
  if (key === 'folder') void startAdd(root, '')
  else if (key === 'req') emit('new-request', '')
  else if (key === 'grpc') emit('new-grpc-request', '')
  else if (key === 'doc') emit('new-doc', '.') // 根菜单新建文档落在集合根，而不是默认 docs/
  else if (key === 'curl') emit('import-curl', '')
  else if (key === 'sync') emit('sync')
  else if (key === 'reload') emit('reload-root', root)
  // 关闭集合：居中确认弹窗（item 11）——原生 confirm 在 WebView2 里是顶部提示条，
  // 且无头浏览器会自动吞掉导致自动化测不到（multi-root M1 的根因）
  else if (key === 'close') {
    closeTarget.value = root
    closeShow.value = true
  }
}

// 关闭工作目录的居中确认（目标根 + 显隐分开存，弹窗文案用名称）
const closeShow = ref(false)
const closeTarget = ref('')

// 删除非空分组：必须勾选「删除所有子项」才能确认（整棵移入 .trash，可恢复）
const deleteShow = ref(false)
const deleteTarget = ref<{ root: string; node: TreeNode } | null>(null)
const deleteAgreed = ref(false)
const deleteChildCount = computed(() => {
  const n = deleteTarget.value?.node
  return n ? countDescendants(n) : 0
})
function countDescendants(node: TreeNode): number {
  let out = 0
  for (const c of node.children ?? []) {
    out += 1
    if (c.type === 'folder') out += countDescendants(c)
  }
  return out
}
/** 收集整棵目录下的请求/文档 uid：递归删除后顺手关掉已失效的页签。 */
function collectLeafUids(node: TreeNode, out: string[]): void {
  for (const c of node.children ?? []) {
    if (c.type === 'folder') collectLeafUids(c, out)
    else if (c.type === 'request' || c.type === 'doc') out.push(c.uid)
  }
}
function confirmTreeDelete(): void {
  const tgt = deleteTarget.value
  if (!tgt || !deleteAgreed.value) return
  deleteShow.value = false
  deleteAgreed.value = false
  void removeFolder(tgt.root, tgt.node, true)
}

function confirmCloseRoot(): void {
  if (closeTarget.value) {
    // 关闭集合即忘掉本次会话的展开态：重新打开时根行回到默认收起
    setRootExpanded(closeTarget.value, false)
    emit('close-root', closeTarget.value)
  }
  closeShow.value = false
  closeTarget.value = ''
}

const searching = computed(() => keyword.value.trim() !== '')

// ---- 树搜索的「索引侧命中」：名称是本机同步匹配，URL/方法/路径 走 Go 索引（口径同顶部搜索栏）。
// 多根逐根查询（索引按集合分库）；keyword 防抖 150ms，递增 token 保证「最新一次输入」胜出。----
const hitByRoot = ref<Record<string, Set<string>>>({})
let hitToken = 0
let hitTimer: ReturnType<typeof setTimeout> | null = null
watch(keyword, (kw) => {
  const q = kw.trim()
  const token = ++hitToken
  if (hitTimer) clearTimeout(hitTimer)
  if (!q) {
    hitByRoot.value = {}
    return
  }
  hitTimer = setTimeout(() => {
    void Promise.all(
      props.roots.map(async (r) => {
        const nodes = await api.searchIndexFor(r.root, q, 500)
        return [r.root, new Set(nodes.map((n) => n.uid))] as const
      }),
    )
      .then((pairs) => {
        if (token === hitToken) hitByRoot.value = Object.fromEntries(pairs)
      })
      // 索引查询失败只退回名称匹配，不打扰用户（名称过滤仍然即时可用）
      .catch(() => {
        if (token === hitToken) hitByRoot.value = {}
      })
  }, 150)
})

// ---- 排序：关（磁盘顺序）/ 名称升序 / 名称降序，作用于每一层的同级条目 ----

const sortMode = ref<SortMode>('default')

/**
 * 「···」菜单：创建集合 + 排序三态（当前项打勾 —— 与工具条「工作目录」菜单同一惯例：
 * 勾选替换图标位）。下拉项统一「图标 + 文字」。
 */
const treeMenu = computed(() => [
  { key: 'create', label: t('sidebar.createCollection'), icon: () => h(NIcon, { component: AddOutline }) },
  { type: 'divider' as const, key: 'd-sort' },
  {
    key: 'sort:asc',
    label: t('sidebar.sortAsc'),
    icon: () => h(NIcon, { component: sortMode.value === 'asc' ? CheckmarkOutline : ArrowUpOutline }),
  },
  {
    key: 'sort:desc',
    label: t('sidebar.sortDesc'),
    icon: () => h(NIcon, { component: sortMode.value === 'desc' ? CheckmarkOutline : ArrowDownOutline }),
  },
  {
    key: 'sort:default',
    label: t('sidebar.sortDefault'),
    icon: () => h(NIcon, { component: sortMode.value === 'default' ? CheckmarkOutline : SwapVerticalOutline }),
  },
])

function onTreeMenu(key: string | number): void {
  if (key === 'create') {
    void startCreate()
    return
  }
  if (typeof key === 'string' && key.startsWith('sort:')) {
    const mode = key.slice('sort:'.length)
    if (mode === 'asc' || mode === 'desc' || mode === 'default') sortMode.value = mode
  }
}

// ---- 顶部工具条：打开集合 / 创建集合 ----

const createShow = ref(false)
const createName = ref('')
const createParent = ref('')

/** 创建集合：先选父目录（系统目录对话框自带「新建文件夹」），再填名称；目录不存在由 OpenCollection 初始化。 */
async function startCreate(): Promise<void> {
  let parent = ''
  try {
    parent = await api.pickDirectory()
  } catch {
    return // 系统对话框都打不起来：静默（无可用环境时按钮本就不该有反应）
  }
  if (!parent) return // 用户取消
  createParent.value = parent
  createName.value = ''
  createShow.value = true
}

/** 拼目标目录：去掉父目录尾部分隔符再拼接（/ 或 \ 由 Go 侧 filepath.Abs 统一处理）。 */
function joinDir(parent: string, name: string): string {
  return parent.replace(/[\\/]+$/, '') + '/' + name
}

/** 名称非法时返回 false：naive-ui dialog 收到 false 不关闭，用户可直接改。 */
function confirmCreate(): boolean {
  const name = createName.value.trim()
  if (!name || /[\\/]/.test(name)) {
    message.warning(t('sidebar.createNameInvalid'))
    return false
  }
  emit('create-collection', joinDir(createParent.value, name))
  createShow.value = false
  return true
}

/** 每个根的树（搜索时按关键字过滤：命中的请求保留其祖先分组；排序开启时深排序副本）。 */
function treeOf(root: CollectionSummary): TreeNode[] {
  const base = searching.value
    ? filterTree(root.info.tree, keyword.value.trim().toLowerCase(), hitByRoot.value[root.root])
    : root.info.tree
  return sortTree(base, sortMode.value)
}

/** 分组折叠态按「根 + 路径」记：两个根里可能有同路径的分组（拷贝出来的集合）。 */
function folderKey(root: string, path: string): string {
  return `${root}\n${path}`
}

/** 新目录默认折叠：首次出现时加入 collapsed；用户之后手动展开的目录已记录在 seenFolders，不会被再次收起。 */
function markNewFolders(root: string, nodes: TreeNode[], next: Set<string>): void {
  for (const n of nodes) {
    if (n.type !== 'folder') continue
    const key = folderKey(root, n.path)
    if (!seenFolders.has(key)) {
      seenFolders.add(key)
      next.add(key)
    }
    if (n.children?.length) markNewFolders(root, n.children, next)
  }
}
function collapseNewFolders(): void {
  const next = new Set(collapsed.value)
  for (const r of props.roots) markNewFolders(r.root, r.info.tree, next)
  if (next.size !== collapsed.value.size) collapsed.value = next
}
watch(() => props.roots, collapseNewFolders, { deep: true, immediate: true })

/** 根的真实折叠态（搜索时不改变持久化状态）。 */
function isRootFolded(root: string): boolean {
  return !expandedRoots.value.has(root)
}

/** 视觉折叠态：搜索时强制展开，让命中结果可见；清空搜索后恢复原状态。 */
function isRootCollapsed(root: string): boolean {
  return !searching.value && isRootFolded(root)
}

function setRootExpanded(root: string, open: boolean): void {
  const next = new Set(expandedRoots.value)
  if (open) next.add(root)
  else next.delete(root)
  expandedRoots.value = next
}

function expandRoot(root: string): void {
  if (isRootFolded(root)) setRootExpanded(root, true)
}

/**
 * 根行下方有没有可见内容（子行，或正在输入的「新建分组」行）。
 * 决定根行要不要从展开图标向下接一条连接线：收起 / 空集合时不画，
 * 否则会留下一截悬空的竖线。
 */
function rootHasVisibleKids(root: string): boolean {
  if (isRootCollapsed(root)) return false
  if (adding.value?.root === root && adding.value.parent === '') return true
  const item = props.roots.find((r) => r.root === root)
  return !!item && treeOf(item).length > 0
}

const rows = computed<Row[]>(() => {
  const out: Row[] = []
  // H5：收藏的请求置顶展示（不改变磁盘顺序）；置顶区只显示活动根的
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
    if (isRootCollapsed(r.root)) continue
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

/** 该根是否已关联服务端项目（关联后根行显示独立的同步刷新按钮）。 */
function rootLinked(root: string): boolean {
  return props.roots.find((r) => r.root === root)?.linked ?? false
}

/** 该根是否正在同步（刷新按钮转圈）。 */
function rootSyncing(root: string): boolean {
  return props.busySyncRoots?.includes(root) ?? false
}

/**
 * 根行刷新按钮：直接同步被点击的根，**不切换活动根/页签组**
 * （同步设置走 “···” 菜单；有冲突由上层弹出冲突页）。
 */
function onSyncRoot(root: string): void {
  if (rootSyncing(root)) return
  emit('sync-root', root)
}

/** 某个根下所有分组的折叠键（按该根当前的树遍历；根不存在时为空）。 */
function folderKeysOf(root: string): string[] {
  const out: string[] = []
  const walk = (nodes: TreeNode[]): void => {
    for (const x of nodes) {
      if (x.type !== 'folder') continue
      out.push(folderKey(root, x.path))
      if (x.children?.length) walk(x.children)
    }
  }
  const item = props.roots.find((r) => r.root === root)
  if (item) walk(treeOf(item))
  return out
}

/** 该根是否已「全部收起」：根行收起时子项均不可见，即视为全部收起（分组态留待展开时恢复）。 */
function rootAllCollapsed(root: string): boolean {
  return isRootFolded(root)
}

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
  setRootExpanded(root, isRootFolded(root))
}

/**
 * 收起 / 展开**某一个根**下的全部（根行「···」→「全部收起 / 全部展开」）。
 *
 * 只碰这个根的键：`collapsed` 的键是「根 + 路径」，`expandedRoots` 的键是根本身，
 * 因此对 A 根全量折叠不会影响 B 根（早先误用了"遍历 props.roots 全收"的实现，
 * 表现为点一个集合的「全部收起」所有集合一起收）。
 */
function toggleRootAll(root: string): void {
  const fold = !rootAllCollapsed(root)
  const keys = new Set(collapsed.value)
  for (const k of folderKeysOf(root)) {
    if (fold) keys.add(k)
    else keys.delete(k)
  }
  const roots = new Set(expandedRoots.value)
  if (fold) roots.delete(root)
  else roots.add(root)
  collapsed.value = keys
  expandedRoots.value = roots
}

/**
 * 点根行（集合名这一整片区域）：
 * - 非活动根：切过去并确保展开（点一下就能看到集合下的第一层）——切根时不顺手折叠，
 *   否则「刚切过去却面对一棵空树」；
 * - 活动根：折叠 / 展开该集合下的分组，点集合名即可折展（与目录行点名字一致），
 *   不必非去点那个小箭头。
 *
 * 行内控件（caret / ··· 点状菜单 / 徽章 / 下拉）自己处理点击，这里一律放行：
 * 右侧「···」菜单（Side Menu）照常打开，不会因为想折展集合就误触发切根或折叠。
 */
function onRootRowClick(root: string, ev: MouseEvent): void {
  if (clickSuppressed.value) return // 拖动调整集合顺序刚结束：别顺手把根切换/折叠了
  const el = ev.target as HTMLElement | null
  if (el?.closest('.act, .caret, .n-dropdown, .n-input, input, textarea, select, a')) return
  if (root !== props.activeRoot) {
    // 切到别的根：只保证展开 + 激活，绝不折叠（折叠当前根留给再点一次它的名字）
    expandRoot(root)
    emit('activate', root)
    return
  }
  toggleRoot(root)
}

/** 根行按下：拖动根行 = 调整集合之间的顺序（其余落点逻辑与树行共用 useTreeDnd）。 */
function onRootPointerDown(root: string, ev: PointerEvent): void {
  onRowPointerDown(root, ROOT_NODE, ev)
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
    else if (node.type === 'doc') await coll.renameDoc(cur.uid, name)
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
  expandRoot(root)
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
async function removeFolder(root: string, node: TreeNode, recursive = false): Promise<void> {
  const leafUids: string[] = []
  if (recursive) collectLeafUids(node, leafUids)
  try {
    await ensureActive(root)
    if (recursive) await coll.deleteFolderTree(node.uid)
    else await coll.deleteFolder(node.uid)
    // 整棵删除后这些页签已没有源文件，关掉避免点开报“文件不存在”
    for (const uid of leafUids) tabs.forgetRequest(uid)
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

/** B19：删除文档条目（进 .trash，可恢复）。 */
async function removeDoc(root: string, node: TreeNode): Promise<void> {
  try {
    await ensureActive(root)
    await coll.deleteDoc(node.uid)
    tabs.forgetRequest(node.uid)
  } catch (e) {
    fail(e)
  }
}

/**
 * 行内「···」菜单：把分组/请求的常用操作（新建子项、重命名、删除、收藏）收进一个下拉，
 * 不再在行内堆一排图标按钮；「更新」(根行立即同步) 仍作为直接按钮，不进菜单。
 */
function rowMenuOptions(row: Row): DropdownOption[] {
  const n = row.node!
  if (n.type === 'folder') {
    return [
      { key: 'request', label: t('tree.newRequest'), icon: () => h(NIcon, { component: AddOutline }), testid: 'tree.row.plus' },
      { key: 'grpc', label: t('grpc.newRequest'), icon: () => h(NIcon, { component: GitNetworkOutline }) },
      { key: 'curl', label: t('curl.title'), icon: () => h(NIcon, { component: TerminalOutline }) },
      { key: 'subfolder', label: t('tree.newSubFolder'), icon: () => h(NIcon, { component: FolderOpenOutline }) },
      { key: 'doc', label: t('tree.newDoc'), icon: () => h(NIcon, { component: DocumentTextOutline }), testid: 'tree.row.newDoc' },
      { type: 'divider', key: 'd1' },
      { key: 'rename', label: t('tree.rename'), icon: () => h(NIcon, { component: CreateOutline }), testid: 'tree.row.rename' },
      { key: 'delete', label: t('tree.delDir'), icon: () => h(NIcon, { component: TrashOutline }), testid: 'tree.row.delete' },
    ]
  }
  if (n.type === 'doc') {
    return [
      { key: 'rename', label: t('tree.rename'), icon: () => h(NIcon, { component: CreateOutline }), testid: 'tree.row.rename' },
      { key: 'delete', label: t('tree.delDoc'), icon: () => h(NIcon, { component: TrashOutline }), testid: 'tree.row.delete' },
    ]
  }
  const fav = coll.isFav(n.uid)
  return [
    { key: 'fav', label: fav ? t('tree.unfav') : t('tree.fav'), icon: () => h(NIcon, { component: fav ? Star : StarOutline }), testid: 'tree.row.fav' },
    { key: 'rename', label: t('tree.rename'), icon: () => h(NIcon, { component: CreateOutline }), testid: 'tree.row.rename' },
    { key: 'delete', label: t('tree.delApi'), icon: () => h(NIcon, { component: TrashOutline }), testid: 'tree.row.delete' },
  ]
}

/**
 * 菜单 label 渲染：naive-ui 2.45 的 NDropdown 不再透传 `#option` 插槽（只吃 render-label/render-option
 * 属性），故用 render-label 把 testid 带到 DOM；图标仍由下拉自身的 prefix 位渲染。
 * 参数必须声明为官方的 DropdownOption（2.45 起 MenuOption 变为 title/label 联合型，
 * 自定义窄参数会因 contravariance 检查不过而报 TS2322）；testid 经索引签名透传。
 */
function renderMenuLabel(option: DropdownOption) {
  const o = option as unknown as { label?: string; testid?: string }
  return h('div', { class: 'row-menu-item', 'data-testid': o.testid }, [h('span', { class: 'lbl' }, o.label ?? '')])
}

async function onRowMenu(row: Row, key: string | number): Promise<void> {
  const n = row.node!
  await ensureActive(row.root)
  if (n.type === 'folder') {
    if (key === 'request') emit('new-request', n.path)
    else if (key === 'grpc') emit('new-grpc-request', n.path)
    else if (key === 'curl') emit('import-curl', n.path)
    else if (key === 'subfolder') void startAdd(row.root, n.path)
    else if (key === 'doc') {
      // 新建后把目标分组展开：否则文档落在折叠的分组里，用户会以为「没建上」
      const next = new Set(collapsed.value)
      next.delete(folderKey(row.root, n.path))
      collapsed.value = next
      emit('new-doc', n.path)
    }
    else if (key === 'rename') void startRename(row.root, n)
    else if (key === 'delete') confirmDelete(row.root, n, 'folder')
  } else if (n.type === 'request') {
    if (key === 'fav') coll.toggleFav(n.uid)
    else if (key === 'rename') void startRename(row.root, n)
    else if (key === 'delete') confirmDelete(row.root, n, 'request')
  } else if (n.type === 'doc') {
    if (key === 'rename') void startRename(row.root, n)
    else if (key === 'delete') confirmDelete(row.root, n, 'doc')
  }
}

/** 删除二次确认（沿用全局 dialog，与弹窗确认风格一致）。 */
function confirmDelete(root: string, node: TreeNode, kind: 'folder' | 'request' | 'doc'): void {
  const n = node as TreeNode
  const title = kind === 'folder' ? t('tree.delDir') : kind === 'doc' ? t('tree.delDoc') : t('tree.delApi')
  const content =
    kind === 'folder'
      ? t('tree.confirmDelFolder', { name: n.name })
      : kind === 'doc'
        ? t('tree.confirmDelDoc', { name: n.name })
        : t('tree.confirmDelRequest', { name: n.name })
  // 非空分组：弹带「删除所有子项」复选框的危险确认（未勾选则确认按钮禁用）
  if (kind === 'folder' && (n.children?.length ?? 0) > 0) {
    deleteTarget.value = { root, node: n }
    deleteAgreed.value = false
    deleteShow.value = true
    return
  }
  dialog.warning({
    title,
    content,
    positiveText: t('common.delete'),
    negativeText: t('common.cancel'),
    onPositiveClick: () => {
      if (kind === 'folder') return removeFolder(root, n)
      if (kind === 'doc') return removeDoc(root, n)
      return removeRequest(root, n)
    },
  })
}

// ---- 拖动调整上级目录 / 自定义排序（自己实现，不用 HTML5 拖拽）----
// 落点分三种（判定见 composables/useTreeDnd）：
//   inside = 放进容器（分组 / 工作目录根）：改上级，追加到同类型末尾；
//   before/after = 插到命中行的前/后：同层自定义排序（分组一套、请求+文档一套）。
// 拖到工作目录根行的上下缘 = 放进该根的头/尾；拖动根行本身 = 调整集合之间的顺序。
//
// 为什么不用 draggable / HTML5 DnD：桌面端跑在 WebView2 里，原生拖拽在嵌入式 WebView 下
// 触发不可靠（用户反馈：鼠标根本拖不动），而且拖拽期间渲染进程的输入派发会被原生拖拽循环
// 阻塞 —— 现象就是「按下去动了、但什么都不会发生」。改成 pointerdown/move/up 自己算命中，
// 行为在 WebView2 / Chromium / Firefox 下一致，也不再依赖 dataTransfer。
// canDrop（能否落点）已抽到 lib/collTree；这里只补一个「重名分组」落点提示（需 props.roots + i18n）。

/** 落点重名提示：目标分组（含跨根）下已有同名分组时给原因（后端会拒同名目录复用）。 */
function dropTipFor(node: TreeDragNode, destRoot: string, destPath: string): string | undefined {
  const item = props.roots.find((r) => r.root === destRoot)
  if (!item) return undefined
  if (node.type === 'folder' && folderNameTaken(item.info.tree, destPath, node.name)) {
    return t('tree.dupFolderHint', { name: node.name })
  }
  return undefined
}

const { dragNode, dropHint, dragging, clickSuppressed, onRowPointerDown } = useTreeDnd({
  // 名称排序（asc/desc）只是显示过滤，与磁盘 seq 顺序不一致：此时禁用拖动，避免「拖了却没按落点排」。
  dragEnabled: () => !searching.value && editing.value === null && sortMode.value === 'default',
  dropTip: dropTipFor,
  onDrop: performDrop,
})

/**
 * 点击树里的行：请求行打开对应页签（拖动刚结束的那次点击忽略）；分组行折叠/展开。
 *
 * 分组行点**名字**也能折展（不必非去点那个小箭头）—— 文件管理器里就是这样，只认箭头的命中区太小。
 * 行内控件（箭头 / 下拉 / 操作按钮 / 重命名输入框）自己处理点击，这里要放行，否则点「+」建子项时会顺带折展一次。
 * 点的是非活动根里的请求时，先把那一根切成活动根 —— 页签是按根分组的。
 */
function onRowClick(root: string, node: TreeNode, ev: MouseEvent): void {
  if (editing.value || clickSuppressed.value) return
  const el = ev.target as HTMLElement | null
  if (el?.closest('.act, .caret, .n-dropdown, .n-input, input, textarea, select, a')) return
  if (node.type === 'doc') {
    void tabs.openDocTab(node.uid)
    return
  }
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

/** 移动成功后展开目标目录（若搬的是分组，连新建的那层也展开），否则默认折叠会让用户以为没搬成功。 */
function revealMoved(targetRoot: string, destPath: string, node: TreeNode): void {
  const paths = [destPath]
  if (node.type === 'folder') paths.push(destPath ? `${destPath}/${node.name}` : node.name)
  const next = new Set(collapsed.value)
  for (const p of paths) {
    if (!p) continue
    const key = folderKey(targetRoot, p)
    // 先标 seen：即使 loadRoots 的 deep watch 晚一拍，也不会把刚展开的搬入目录又收起
    seenFolders.add(key)
    next.delete(key)
  }
  collapsed.value = next
}

/** 某个根里指定分组的子节点（树序 = 磁盘 seq 顺序；path 空 = 集合根）。 */
function childrenOf(root: string, path: string): TreeNode[] {
  const item = props.roots.find((r) => r.root === root)
  if (!item) return []
  if (!path) return item.info.tree
  return findFolderByPath(item.info.tree, path)?.children ?? []
}

/** 同层「排序组」：分组一套、叶子（请求/文档）一套 —— 与集合树「分组在前」的排序规则一致。 */
function siblingsOf(root: string, path: string, folderGroup: boolean): TreeNode[] {
  return childrenOf(root, path).filter((n) => (n.type === 'folder') === folderGroup)
}

/** 命中行所属的父分组路径（分组取自身路径的上级；请求/文档取文件所在目录 —— 两者同一算法）。 */
function parentPathOf(node: TreeNode): string {
  return node.path.split('/').slice(0, -1).join('/')
}

/**
 * 把落点换算成「目标分组 + 锚点（插到哪个兄弟之前）」，beforeUID 空 = 同类型末尾。
 * 四种情况：放进容器 / 命中根行 / 叶子落到分组行（放进该分组头尾）/ 同组排序。
 */
function resolveDropTarget(
  node: TreeDragNode,
  hint: DropHint,
): { root: string; path: string; beforeUID: string } {
  if (node.type === 'root') return { root: hint.root, path: '', beforeUID: '' }
  if (hint.pos === 'inside') return { root: hint.root, path: hint.path, beforeUID: '' }
  const tree = props.roots.find((r) => r.root === hint.root)?.info.tree ?? []
  const hit = hint.uid === '__root__' ? null : searchNode(tree, hint.uid)
  const folderGroup = node.type === 'folder'
  if (!hit) {
    // 命中根行：放进该根的头（插到第一个同组兄弟之前）或末尾
    const first = siblingsOf(hint.root, '', folderGroup)[0]
    return { root: hint.root, path: '', beforeUID: hint.pos === 'before' ? (first?.uid ?? '') : '' }
  }
  if (hit.type === 'folder' && !folderGroup) {
    // 叶子落在分组行上/下缘：放进该分组的叶子头/尾
    const first = siblingsOf(hint.root, hit.path, false)[0]
    return { root: hint.root, path: hit.path, beforeUID: hint.pos === 'before' ? (first?.uid ?? '') : '' }
  }
  const parent = parentPathOf(hit)
  const list = siblingsOf(hint.root, parent, folderGroup)
  const idx = list.findIndex((n) => n.uid === hit.uid)
  const beforeUID = hint.pos === 'before' ? hit.uid : (list[idx + 1]?.uid ?? '')
  return { root: hint.root, path: parent, beforeUID }
}

/** 拖动落点分发：根行排序 / 放进容器（改上级）/ 同层排序。 */
async function performDrop(srcRoot: string, node: TreeDragNode, hint: DropHint): Promise<void> {
  if (node.type === 'root') {
    if (hint.pos !== 'inside') await reorderRoot(srcRoot, hint.root, hint.pos)
    return
  }
  if (hint.pos === 'inside') {
    await performMove(srcRoot, node, hint.root, hint.path)
    return
  }
  await performArrange(srcRoot, node, hint)
}

/** 集合之间排序：把 srcRoot 插到 targetRoot 的前/后（store 会同时写回「下次启动恢复」的顺序）。 */
async function reorderRoot(srcRoot: string, targetRoot: string, pos: 'before' | 'after'): Promise<void> {
  if (srcRoot === targetRoot) return
  const order = props.roots.map((r) => r.root).filter((r) => r !== srcRoot)
  const at = order.indexOf(targetRoot)
  if (at < 0) return
  order.splice(pos === 'before' ? at : at + 1, 0, srcRoot)
  try {
    await coll.reorderRoots(order)
  } catch (e) {
    fail(e)
  }
}

/** 同层排序：换算成目标分组 + 锚点后调用 ArrangeNode（同层排序 / 跨分组拖到指定位置）。 */
async function performArrange(srcRoot: string, node: TreeNode, hint: DropHint): Promise<void> {
  const target = resolveDropTarget(node, hint)
  // 拖到自己身上（上缘 = 插到自己之前）：空操作 —— 后端会把 uid==beforeUID 归一成「末尾」，
  // 不加这道判断会让「拖回原处」变成「甩到最后」。
  if (target.beforeUID === node.uid) return
  if (node.type === 'doc' && srcRoot !== target.root) {
    message.warning(t('tree.docCrossRoot'))
    return
  }
  if (node.type === 'folder' && !canDrop(node, srcRoot, target.root, target.path)) return
  const sameRoot = srcRoot === target.root
  try {
    let uid = node.uid
    if (!sameRoot) {
      // 跨工作目录：先搬（目标侧重建，请求会拿到新 uid），再把新节点插到落点
      if (node.type === 'folder') {
        // 分组跨根是整棵重建、新 uid 不回传：只搬（追加到目标分组末尾）
        const moved = await coll.moveFolderTo(srcRoot, node.uid, target.root, target.path)
        for (const u of moved) tabs.forgetRequest(u)
      } else {
        uid = await coll.moveRequestTo(srcRoot, node.uid, target.root, target.path)
        tabs.forgetRequest(node.uid)
      }
      message.success(t('tree.movedToDir', { name: rootName(target.root) }))
    }
    // 分组跨根时没有可用的新 uid，排序按「追加」处理
    if (sameRoot || node.type !== 'folder') {
      await ensureActive(target.root)
      await coll.arrangeNode(uid, target.path, target.beforeUID)
      if (node.type !== 'folder') tabs.syncPath(uid, target.path)
    }
    await nextTick()
    expandRoot(target.root)
    revealMoved(target.root, target.path, node)
  } catch (e) {
    fail(e)
  }
}

/** 执行移动：同目录改上级；跨目录则把请求 / 分组整棵搬到另一个工作目录。 */
async function performMove(
  root: string,
  node: TreeNode,
  destRoot: string,
  destPath: string,
): Promise<void> {
  if (!canDrop(node, root, destRoot, destPath)) return
  // 本期不支持文档跨工作目录移动（服务端分组归属按根，跨根需另设计）：同根内才放行
  if (node.type === 'doc' && root !== destRoot) {
    message.warning(t('tree.docCrossRoot'))
    return
  }
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
      await nextTick()
      expandRoot(destRoot)
      revealMoved(destRoot, destPath, node)
      return
    }
    await ensureActive(root)
    if (node.type === 'folder') await coll.moveFolder(node.uid, destPath)
    else if (node.type === 'doc') await coll.moveDoc(node.uid, destPath)
    else await coll.moveRequest(node.uid, destPath)
    // 目标分组（含新搬过去的分组本身）展开，否则用户看不到「移动成功了」
    await nextTick()
    revealMoved(root, destPath, node)
    // 已打开的页签要同步路径（面包屑与后续保存都靠它）
    if (node.type === 'request' || node.type === 'doc') tabs.syncPath(node.uid, destPath)
    // 成功不再弹提示：落点横线 + 树即时刷新已经把结果说清楚了，多一条 toast 只是噪音
  } catch (e) {
    fail(e)
  }
}

/** 在指定根的树里按 uid 找节点。 */
function findNode(root: string, uid: string): TreeNode | null {
  const item = props.roots.find((r) => r.root === root)
  return item ? searchNode(item.info.tree, uid) : null
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

// 点页签（或切换页签）→ 集合树同步选中：展开（该根 / 祖先分组）并滚动到可见位置。
// 同时监听 revealTick：重复点当前页签时 activeUid 不变，没有它就不会触发。
// 首次挂载 / 启动恢复会话时不自动展开根：默认保持收起，是否展开由上次记忆决定；
// 只有用户后续切页签才自动展开。
let firstReveal = true
watch(
  [() => props.activeUid, () => props.revealTick],
  async ([uid]) => {
    const initial = firstReveal
    firstReveal = false
    if (!uid) return
    const root = coll.activeRoot
    const item = props.roots.find((r) => r.root === root)
    if (!item) return
    const trail = ancestorPaths(item.info.tree, uid)
    const unfoldRoot = isRootFolded(root)
    const folded = (trail ?? []).filter((p) => collapsed.value.has(folderKey(root, p)))
    if (!initial && !coll.restoring && (unfoldRoot || folded.length)) {
      expandRoot(root)
      const next = new Set(collapsed.value)
      for (const p of folded) next.delete(folderKey(root, p))
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
    <!-- 树工具条：打开集合（图标）+ 「···」菜单（创建集合 / 排序三态，当前排序打勾） -->
    <div class="ops">
      <button class="op" type="button" data-testid="tree.openCollection" :title="t('sidebar.openCollection')"
        @click="emit('open-collection')">
        <n-icon :component="FolderOpenOutline" :size="15" />
      </button>
      <span class="op-sp" />
      <n-dropdown trigger="click" placement="bottom-end" :options="treeMenu" @select="onTreeMenu">
        <!-- 不能加 @click.stop：会拦在 NDropdown 的包装层之前，导致下拉打不开（同根行「···」） -->
        <button class="op" type="button" data-testid="tree.ops.menu" :title="t('tree.more')"
          :aria-label="t('tree.more')">
          <n-icon :component="EllipsisHorizontalOutline" :size="15" />
        </button>
      </n-dropdown>
    </div>

    <div class="search">
      <n-input v-model:value="keyword" size="small" clearable :placeholder="t('sidebar.search')"
        data-testid="sidebar.search">
        <template #prefix>
          <n-icon :component="SearchOutline" :size="14" data-testid="sidebar.searchIcon" />
        </template>
      </n-input>
    </div>

    <div ref="treeEl" class="tree" :class="{ 'dnd-active': dragging }">
      <div v-if="!rows.length && !adding" class="empty muted">{{ t('sidebar.empty') }}</div>

      <template v-for="row in rows" :key="row.isRoot ? `root:${row.root}` : `${row.root}|${row.node?.path}`">
        <!-- 工作目录根行：既是该根的标题，也是「移回该根顶层」的放置目标（拖动时高亮） -->
        <template v-if="row.isRoot">
          <div class="row root" :class="{
            on: row.root === props.activeRoot,
            'root-active': row.root === props.activeRoot,
            dragging: dragging && dragNode?.root === row.root && dragNode?.node.type === 'root',
            'drop-ok': dragging && dropHint?.root === row.root && dropHint?.uid === '__root__' && dropHint.pos === 'inside' && dropHint.ok,
            'drop-bad': dragging && dropHint?.root === row.root && dropHint?.uid === '__root__' && dropHint.pos === 'inside' && !dropHint.ok,
            'drop-before': dragging && dropHint?.root === row.root && dropHint?.uid === '__root__' && dropHint.pos === 'before' && dropHint.ok,
            'drop-after': dragging && dropHint?.root === row.root && dropHint?.uid === '__root__' && dropHint.pos === 'after' && dropHint.ok,
          }" :style="{ paddingLeft: '8px' }" :data-root="row.root" :data-path="''"
            :data-expanded="isRootCollapsed(row.root) ? 'false' : 'true'" data-testid="tree.root"
            @click="onRootRowClick(row.root, $event)"
            @pointerdown="onRootPointerDown(row.root, $event)">
            <!-- 根行 → 子行的连接线：从展开图标圆心向下接出（收起 / 没有子行时不画） -->
            <span v-if="rootHasVisibleKids(row.root)" class="guides" aria-hidden="true" data-testid="tree.root.guides">
              <span class="gl root-down" :style="{ left: GUIDE_OFFSET + 'px' }" />
            </span>
            <button class="caret" data-testid="tree.root.caret" :title="t('tree.expandAll')"
              @click.stop="toggleRoot(row.root)">
              <n-icon :component="isRootCollapsed(row.root) ? ChevronForwardOutline : ChevronDownOutline" :size="13" />
            </button>
            <span class="rname coll-name" :title="rootDir(row.root)">{{ rootName(row.root) }}</span>
            <!-- 本地集合不再显示徽章；只有已关联服务端的集合才标记「云端」 -->
            <span v-if="rootLinked(row.root)" class="badge">{{ t('tree.cloudBadge') }}</span>
            <span v-if="rootReadOnly(row.root)" class="badge badge-ro">{{ t('tree.readOnly') }}</span>
            <span v-if="dragging" class="drop-tip">{{ dropHint?.root === row.root ? dropHint?.tip : t('tree.dropRoot')
            }}</span>
            <span class="actions">
              <!-- 已关联服务端的集合：根行独立「刷新=立即同步」按钮，只同步该根、不切换集合 -->
              <button v-if="rootLinked(row.root)" class="act" type="button" data-testid="tree.root.sync"
                :title="t('tree.syncNow')" :aria-label="t('tree.syncNow')" :disabled="rootSyncing(row.root)"
                @click.stop="onSyncRoot(row.root)">
                <n-icon :component="SyncOutline" :size="13" :class="{ spin: rootSyncing(row.root) }" />
              </button>
              <n-dropdown trigger="click" placement="bottom-start" :options="rootMenuFor(row.root)"
                :render-label="renderMenuLabel" @select="onRootMenu(row.root, $event)">
                <!-- 不能加 @click.stop：会拦在 NDropdown 的包装层之前，导致下拉打不开（同分组行「＋」） -->
                <button class="act" type="button" data-testid="tree.root.menu" :title="t('tree.more')"
                  :aria-label="t('tree.more')">
                  <n-icon :component="EllipsisHorizontalOutline" :size="13" />
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
            clickable: row.node?.type === 'request' || row.node?.type === 'doc',
            dim: row.root !== props.activeRoot,
            dragging: dragNode?.root === row.root && dragNode?.node.uid === row.node?.uid,
            'drop-ok': dropHint?.root === row.root && dropHint?.uid === row.node?.uid && dropHint.pos === 'inside' && dropHint.ok,
            'drop-bad': dropHint?.root === row.root && dropHint?.uid === row.node?.uid && dropHint.pos === 'inside' && !dropHint.ok,
            'drop-before': dropHint?.root === row.root && dropHint?.uid === row.node?.uid && dropHint.pos === 'before' && dropHint.ok,
            'drop-after': dropHint?.root === row.root && dropHint?.uid === row.node?.uid && dropHint.pos === 'after' && dropHint.ok,
          }" :data-root="row.root" :data-uid="row.node?.uid" :data-kind="row.node?.type" :data-path="row.node?.path"
            :data-collapsed="row.node?.type === 'folder' && row.node.children?.length ? (isCollapsed(row.root, row.node.path) ? 'true' : 'false') : undefined"
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
              <!-- 目录行恒显示展开图标：本设计里目录行没有文件夹图标，caret 是「这是目录」的唯一
                   视觉标记。空目录显示**暗态禁用**箭头 —— 保留「这是目录」的辨识度，
                   又不暗示点开有内容（曾完全隐藏过，实测空目录像残缺的叶子行，还原）。 -->
              <button class="caret" data-testid="tree.row.caret" :class="{ empty: !row.node!.children?.length }"
                :disabled="!row.node!.children?.length" :title="t('tree.expandAll')"
                @click.stop="toggle(row.root, row.node!.path)">
                <n-icon :component="isCollapsed(row.root, row.node!.path) ? ChevronForwardOutline : ChevronDownOutline"
                  :size="13" />
              </button>
              <n-input v-if="editing?.root === row.root && editing?.uid === row.node!.uid" v-model:value="editing.value"
                size="tiny" class="rename" @keyup.enter="submitRename" @blur="submitRename"
                @keyup.esc="editing = null" />
              <span v-else class="fname" data-testid="tree.row.name">{{ row.node!.name }}</span>
              <span class="actions">
                <n-dropdown trigger="click" placement="bottom-start" :options="rowMenuOptions(row)"
                  :render-label="renderMenuLabel" @select="onRowMenu(row, $event)">
                  <!-- 不能加 @click.stop：会拦在 NDropdown 的包装层之前，导致下拉打不开 -->
                  <button class="act" type="button" data-testid="tree.row.menu" :title="t('tree.more')"
                    :aria-label="t('tree.more')">
                    <n-icon :component="EllipsisHorizontalOutline" :size="13" />
                  </button>
                </n-dropdown>
              </span>
            </template>

            <!-- B19：文档行（与请求行同构：MD 标记 + 行内重命名 + 行内菜单） -->
            <template v-else-if="row.node!.type === 'doc'">
              <span class="md-tree" data-testid="tree.docRow">MD</span>
              <n-input v-if="editing?.root === row.root && editing?.uid === row.node!.uid" v-model:value="editing.value"
                size="tiny" class="rename" @keyup.enter="submitRename" @blur="submitRename"
                @keyup.esc="editing = null" />
              <button v-else class="rname" data-testid="tree.row.name" :title="row.node!.name">
                {{ row.node!.name }}
              </button>
              <span class="actions">
                <n-dropdown trigger="click" placement="bottom-start" :options="rowMenuOptions(row)"
                  :render-label="renderMenuLabel" @select="onRowMenu(row, $event)">
                  <button class="act" type="button" data-testid="tree.row.menu" :title="t('tree.more')"
                    :aria-label="t('tree.more')">
                    <n-icon :component="EllipsisHorizontalOutline" :size="13" />
                  </button>
                </n-dropdown>
              </span>
            </template>

            <template v-else-if="row.node!.type === 'request'">
              <method-tag :method="row.node!.method ?? 'GET'" />
              <n-input v-if="editing?.root === row.root && editing?.uid === row.node!.uid" v-model:value="editing.value"
                size="tiny" class="rename" @keyup.enter="submitRename" @blur="submitRename"
                @keyup.esc="editing = null" />
              <button v-else class="rname" data-testid="tree.row.name" :title="row.node!.name">
                {{ row.node!.name }}
              </button>
              <span class="actions">
                <n-dropdown trigger="click" placement="bottom-start" :options="rowMenuOptions(row)"
                  :render-label="renderMenuLabel" @select="onRowMenu(row, $event)">
                  <!-- 不能加 @click.stop：会拦在 NDropdown 的包装层之前，导致下拉打不开 -->
                  <button class="act" type="button" data-testid="tree.row.menu" :title="t('tree.more')"
                    :aria-label="t('tree.more')">
                    <n-icon :component="EllipsisHorizontalOutline" :size="13" />
                  </button>
                </n-dropdown>
              </span>
            </template>

            <!-- 合成 Resp 子节点已移除（集合树不再展示 Resp 入口，响应字段只在请求编辑器「响应字段」分段维护） -->
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

    <sidebar-dialogs v-model:close-show="closeShow"
      :close-target-name="closeTarget ? rootName(closeTarget) : ''" v-model:delete-show="deleteShow"
      :delete-target-name="deleteTarget?.node.name ?? ''" v-model:delete-agreed="deleteAgreed"
      :delete-child-count="deleteChildCount" v-model:create-show="createShow" v-model:create-name="createName"
      @confirm-close="confirmCloseRoot" @confirm-delete="confirmTreeDelete" @confirm-create="confirmCreate" />
  </div>
</template>

<style scoped src="../styles/sidebar.css"></style>
