// 集合状态（多工作目录 = 单树多根）。
//
// 身份用 root（工作目录标识 = 后端的规范化绝对路径），不是集合 uid：
// 两个工作目录完全可能是同一份集合的拷贝（uid 相同、路径不同）。
//
// 为什么 getter 仍叫 info/uid/tree/name/dir/envNames/isReadOnly：集合级动作（读请求、保存、
// 环境、同步…）在后端都作用于**活动根**，这些 getter 一律取活动根的数据 —— 于是既有组件
// （RequestBar / EnvPicker / Overview / StatusBar / CommandPalette …）不用改一行。
// 按根区分的状态（标签会话、环境选择、收藏）都以 root 为键。
import { api } from '@/lib/ipc'
import type { CollectionInfo, CollectionSummary, DocEntry, Env, RequestDoc, TreeNode } from '@/types'
import { defineStore } from 'pinia'

/** 取活动根的概要：getter 里不引用别的 getter（避免 Pinia 的 this 推断问题）。 */
function activeOf(state: {
  roots: CollectionSummary[]
  activeRoot: string
}): CollectionSummary | null {
  return state.roots.find((r) => r.root === state.activeRoot) ?? null
}

/** 每根一份的本地状态键。 */
const envKey = (root: string): string => `client.env.${root}`
const favKey = (root: string): string => `client.fav.${root}`
const DIRS_KEY = 'client.dirs'
const ACTIVE_DIR_KEY = 'client.activeDir'
const RECENT_KEY = 'client.recentDirs'
/**
 * 「最近打开」列表长度上限。
 *
 * 为什么要单独记一份：`client.dirs` 存的是**当前打开的**根（用于下次启动恢复），
 * 而启动恢复有数量上限（设置里的「启动恢复目录数」）—— 如果最近记录就是这份列表，
 * 被上限挡下的目录会在恢复后立刻被 `saveDirs()` 覆盖掉，用户再也找不到它们。
 * 关掉某个根同理：它不该从「最近打开」里消失，否则只能重新去系统对话框里翻。
 */
const RECENT_MAX = 20

/** 最近打开过的工作目录（MRU，最新在前）。只读 localStorage，不碰后端。 */
export function savedRecentDirs(): string[] {
  try {
    const raw = localStorage.getItem(RECENT_KEY)
    const dirs = raw ? (JSON.parse(raw) as string[]) : []
    if (Array.isArray(dirs)) return dirs.filter((d) => typeof d === 'string' && d)
  } catch {
    // 解析失败按「没有记录」处理
  }
  return []
}

/** 把一个目录记进「最近打开」（去重、忽略大小写、最新在前、截到上限）。 */
export function pushRecentDir(dir: string): void {
  const d = dir.trim()
  if (!d) return
  const key = d.toLowerCase()
  const next = [d, ...savedRecentDirs().filter((x) => x.trim().toLowerCase() !== key)]
  localStorage.setItem(RECENT_KEY, JSON.stringify(next.slice(0, RECENT_MAX)))
}

/** 清空「最近打开」历史（不影响当前打开的根）。 */
export function clearRecentDirs(): void {
  localStorage.removeItem(RECENT_KEY)
}

/**
 * 启动恢复的一条记录：目录 + 集合 uid。
 *
 * 为什么要存 uid：目录被改名后路径就失效了，但 opencollection.yml 里的 uid 不变 ——
 * 靠它才能在后端把「同一个集合」找回来（见 App.vue 的 restoreRoots）。
 */
export interface SavedRootEntry {
  dir: string
  uid: string
}

/** 解析 client.dirs：兼容旧格式（纯路径字符串数组），坏项直接丢。 */
function parseRootEntries(raw: string | null): SavedRootEntry[] {
  if (!raw) return []
  const arr = JSON.parse(raw) as unknown
  if (!Array.isArray(arr)) return []
  const out: SavedRootEntry[] = []
  for (const x of arr) {
    if (typeof x === 'string') {
      if (x) out.push({ dir: x, uid: '' })
      continue
    }
    if (x && typeof x === 'object') {
      const rec = x as { dir?: unknown; uid?: unknown }
      if (typeof rec.dir === 'string' && rec.dir) {
        out.push({ dir: rec.dir, uid: typeof rec.uid === 'string' ? rec.uid : '' })
      }
    }
  }
  return out
}

/**
 * 启动时要恢复的工作目录：优先新的 client.dirs，其次旧版本的 client.lastDir（自动迁移）。
 * 只读 localStorage，不碰后端，便于 App.vue 在挂载时决定要开哪些根。
 */
export function savedRootEntries(): SavedRootEntry[] {
  try {
    const entries = parseRootEntries(localStorage.getItem(DIRS_KEY))
    if (entries.length) return entries
  } catch {
    // 解析失败按「没有记录」处理
  }
  const legacy = localStorage.getItem('client.lastDir')
  return legacy ? [{ dir: legacy, uid: '' }] : []
}

/** 只取路径（欢迎页 lastDir 等只需要路径的地方用）。 */
export function savedRootDirs(): string[] {
  return savedRootEntries().map((e) => e.dir)
}

/**
 * 一次性写回启动恢复列表与活动目录。
 *
 * 为什么不复用 saveDirs：启动恢复期间 `roots` 只是「已恢复的那部分」，
 * 中途落盘会把还没轮到的根抹掉；恢复流程自己攒完整列表后调这里。
 */
export function writeSavedRootEntries(entries: SavedRootEntry[], activeDir = ''): void {
  try {
    const list = entries.filter((e) => e.dir).map((e) => ({ dir: e.dir, uid: e.uid }))
    if (list.length) localStorage.setItem(DIRS_KEY, JSON.stringify(list))
    else localStorage.removeItem(DIRS_KEY)
    if (activeDir) {
      localStorage.setItem(ACTIVE_DIR_KEY, activeDir)
      // 旧键保留一份，方便回退旧版本时仍能记住上次目录
      localStorage.setItem('client.lastDir', activeDir)
    } else {
      localStorage.removeItem(ACTIVE_DIR_KEY)
      localStorage.removeItem('client.lastDir')
    }
  } catch {
    // 忽略：localStorage 不可用不影响主流程
  }
}

/**
 * 从「启动恢复」列表里移除一个目录（只影响 client.dirs，不动「最近打开」）。
 *
 * 为什么要移除：目录被改名 / 删除后，后端会返回「集合目录不存在」而不是把它重建出来；
 * 若不摘掉这条记录，每次启动都要为一个已不存在的路径失败一次。
 * 「最近打开」保留旧路径与用户之后手动打开的新路径，需要时仍可一键重开。
 */
export function removeSavedRootDir(dir: string): void {
  const key = dir.trim().toLowerCase()
  if (!key) return
  const rest = savedRootEntries().filter((e) => e.dir.trim().toLowerCase() !== key)
  try {
    if (rest.length) {
      localStorage.setItem(DIRS_KEY, JSON.stringify(rest))
      return
    }
    localStorage.removeItem(DIRS_KEY)
    // 旧版本键同样要清：savedRootEntries 在 DIRS_KEY 为空时会回退到它
    if ((localStorage.getItem('client.lastDir') ?? '').trim().toLowerCase() === key) {
      localStorage.removeItem('client.lastDir')
    }
  } catch {
    // 忽略：localStorage 不可用不影响主流程
  }
}

/** 上次的活动根目录（重启后优先把它设为活动根）。 */
export function savedActiveDir(): string {
  return localStorage.getItem(ACTIVE_DIR_KEY) ?? ''
}

export const useCollectionStore = defineStore('collection', {
  state: () => ({
    /** 已打开的工作目录（打开顺序 = 侧栏根行顺序） */
    roots: [] as CollectionSummary[],
    /** 活动根的标识（集合级动作都作用于它） */
    activeRoot: '',
    /** 是否已经打开过至少一个根（欢迎页/工作台切换用） */
    ready: false,
    loading: false,
    /** 启动恢复多根/会话中：集合树暂不自动展开（默认收起由 Sidebar 的记忆决定） */
    restoring: false,
    currentEnv: '',
    lastError: '',
    /** 收藏的请求 uid（localStorage: client.fav.<root>） */
    favorites: [] as string[],
    /** 活动根的同步模式：''（未关联）| auto | manual | mirror */
    syncMode: '',
  }),
  getters: {
    /** 活动根的概要（含只读/关联/Mock 等会话态）。 */
    active: (state): CollectionSummary | null => activeOf(state),
    info: (state): CollectionInfo | null => activeOf(state)?.info ?? null,
    uid: (state): string => activeOf(state)?.info.uid ?? '',
    name: (state): string => activeOf(state)?.info.name ?? '',
    dir: (state): string => activeOf(state)?.info.dir ?? '',
    tree: (state): TreeNode[] => activeOf(state)?.info.tree ?? [],
    envNames: (state): string[] => activeOf(state)?.info.envs.map((e) => e.name) ?? [],
    /** 当前活动集合·所处环境的全部变量名（VarInput 自动提示的候选来源之一）。 */
    currentEnvVarNames: (state): string[] => {
      const info = activeOf(state)?.info
      const env = info?.envs.find((e) => e.name === state.currentEnv)
      return env?.vars.map((v) => v.name) ?? []
    },
    favSet: (state): Set<string> => new Set(state.favorites),
    /** mirror 模式只读：仅当活动根已关联且 mode=mirror 时才禁编辑。 */
    isReadOnly: (state): boolean => activeOf(state)?.readOnly ?? state.syncMode === 'mirror',
  },
  actions: {
    /** 拉取全部已打开的工作目录 + 活动根（启动、切换后、外部改动后都用它）。 */
    async loadRoots(): Promise<void> {
      const [roots, active] = await Promise.all([api.listCollections(), api.activeCollection()])
      this.roots = roots
      this.activeRoot = active || (roots[0]?.root ?? '')
      this.ready = this.roots.length > 0
      // 启动时后端可能已经打开了根（-dir / 刷新页面）：这里必须把该根的环境选择恢复出来。
      // 漏掉的话 currentEnv 一直是空串，顶栏显示「No Environment」，所有 {{变量}} 都解析不了 ——
      // 表现是「打开集合后所有请求都发不出去：变量未定义」。
      if (this.ready) {
        this.restoreEnv()
        this.loadFavs()
      }
    },
    /**
     * 打开一个工作目录（追加为新的根并置为活动根；同一目录重复打开幂等）。
     * 打开后立即恢复该根的环境选择与收藏。
     */
    async open(dir: string): Promise<void> {
      this.loading = true
      this.lastError = ''
      try {
        await api.openCollection(dir)
        await this.loadRoots()
        this.restoreEnv()
        this.loadFavs()
        this.saveDirs()
        // 最近打开：独立于「当前打开」，关掉后仍可一键重开
        pushRecentDir(dir)
      } catch (e) {
        this.lastError = e instanceof Error ? e.message : String(e)
        throw e
      } finally {
        this.loading = false
      }
    },
    /** 切换活动根（用户点击某个根 / 打开其下的请求时调用）。 */
    async setActive(root: string): Promise<void> {
      if (!root || root === this.activeRoot) return
      await api.setActiveCollection(root)
      this.activeRoot = root
      this.syncMode = '' // 同步状态由状态栏轮询重新拉（属于新的活动根）
      this.restoreEnv()
      this.loadFavs()
      this.saveDirs()
    },
    /** 关闭一个工作目录（其它根不受影响）。 */
    async closeRoot(root: string): Promise<void> {
      // 先记下目录：关掉后 roots 里就查不到了，但「最近打开」应保留它（可一键重开）。
      const dir = this.roots.find((r) => r.root === root)?.info.dir ?? ''
      await api.closeCollection(root)
      if (dir) pushRecentDir(dir)
      await this.loadRoots()
      this.restoreEnv()
      this.loadFavs()
      this.saveDirs()
    },
    /** 重载某个根（外部改动 / git 操作后）；不传 = 活动根。 */
    async reloadRoot(root = ''): Promise<void> {
      const info = await api.reloadCollectionOf(root)
      const key = root || this.activeRoot
      if (key === this.activeRoot) {
        const prev = this.currentEnv
        this.replaceInfo(info)
        this.currentEnv = this.envNames.includes(prev) ? prev : (this.envNames[0] ?? '')
      } else {
        this.replaceInfo(info)
      }
    },
    /** 重新扫描**活动根**（保持既有调用点不变）。 */
    async reload(): Promise<void> {
      if (!this.info) return
      this.replaceInfo(await api.reload())
      // 环境列表可能因**外部改动**（MCP / 编辑器 / 同步拉取）而变化：校正当前选择，
      // 别让 currentEnv 停在空串或已不存在的环境名上。停在空串时发送会走 envVars("")，
      // 只拿到内置动态变量 → URL 里的 {{变量}} 原样发出去，Go 侧报看不懂的协议头错误。
      // （reloadRoot 早就做了这件事，外部改动的主路径 reload 却漏了。）
      const prev = this.currentEnv
      this.currentEnv = prev && this.envNames.includes(prev) ? prev : (this.envNames[0] ?? '')
    },
    /** 把新的概要写回 roots（按目录匹配，root 标识不变）。 */
    replaceInfo(info: CollectionInfo): void {
      const i = this.roots.findIndex((r) => r.info.dir === info.dir)
      if (i >= 0) {
        this.roots[i] = { ...this.roots[i], info }
      }
    },
    /** 把「已打开目录 + 当前根」落 localStorage（重启恢复用）。 */
    saveDirs(): void {
      // 启动恢复期间不写：此时 roots 只是「已恢复的那部分」，落盘会把还没轮到的根抹掉。
      // 恢复流程自己攒一份完整列表，结束时统一 writeSavedRootEntries。
      if (this.restoring) return
      writeSavedRootEntries(
        this.roots.map((r) => ({ dir: r.info.dir, uid: r.info.uid })),
        this.dir,
      )
    },
    /** 新建请求（folder 为空 = 根）；返回落盘后的文档供调用方开 tab。 */
    async createRequest(folder: string, name: string, method: string): Promise<RequestDoc> {
      const r = await api.createRequest(folder, name, method)
      await this.reload()
      return r
    },
    /** 在同一分组内复制请求（显示名自动加「复制N」）；返回副本，树已刷新。 */
    async cloneRequest(uid: string): Promise<RequestDoc> {
      const r = await api.cloneRequest(uid)
      await this.reload()
      return r
    },
    /** 新建分组：parent 为相对路径（空 = 根）。 */
    async createFolder(parent: string, name: string): Promise<void> {
      await api.createFolder(parent, name)
      await this.reload()
    },
    async renameFolder(uid: string, name: string): Promise<void> {
      await api.renameFolder(uid, name)
      await this.reload()
    },
    async deleteFolder(uid: string): Promise<void> {
      await api.deleteFolder(uid)
      await this.reload()
    },
    /** 递归删除分组（含子分组/请求/文档，整棵进 .trash）。 */
    async deleteFolderTree(uid: string): Promise<void> {
      await api.deleteFolderTree(uid)
      await this.reload()
    },
    async renameRequest(uid: string, name: string): Promise<void> {
      await api.renameRequest(uid, name)
      await this.reload()
    },
    async moveRequest(uid: string, destFolder: string): Promise<void> {
      await api.moveRequest(uid, destFolder)
      await this.reload()
    },
    /** B19：文档条目（与请求同级的树叶子）。 */
    async renameDoc(uid: string, name: string): Promise<void> {
      await api.renameDoc(uid, name)
      await this.reload()
    },
    async moveDoc(uid: string, destFolder: string): Promise<void> {
      await api.moveDoc(uid, destFolder)
      await this.reload()
    },
    async deleteDoc(uid: string): Promise<void> {
      await api.deleteDoc(uid)
      await this.reload()
    },
    /** B19：新建文档（folder 空 = docs/ 默认位）；返回条目供调用方开页签。 */
    async createDoc(name: string, folder: string): Promise<DocEntry> {
      const doc = await api.createDoc(name, folder)
      // 必须重扫：否则新建的文档不会出现在集合树里（用户看到的就是「建了但树里没有」）
      await this.reload()
      return doc
    },
    async moveFolder(uid: string, destParent: string): Promise<void> {
      await api.moveFolder(uid, destParent)
      await this.reload()
    },
    /** 拖动排序：把节点放到目标分组的指定位置（beforeUID 空 = 同类型末尾）。 */
    async arrangeNode(uid: string, destFolder: string, beforeUID: string): Promise<void> {
      await api.arrangeNode(uid, destFolder, beforeUID)
      await this.reload()
    },
    /** 拖动集合调整根行顺序：后端会话顺序 + 本地「下次启动恢复」顺序一起更新。 */
    async reorderRoots(roots: string[]): Promise<void> {
      await api.reorderCollections(roots)
      await this.loadRoots()
      this.saveDirs()
    },
    /**
     * 跨工作目录移动请求（拖到另一个根）：目标侧重建 + 源文件进源集合 .trash。
     * 返回目标集合里新请求的 uid。两个根的树都要刷新（源少一个、目标多一个）。
     */
    async moveRequestTo(srcRoot: string, uid: string, destRoot: string, destFolder: string): Promise<string> {
      const newUid = await api.moveRequestToCollection(srcRoot, uid, destRoot, destFolder)
      await this.loadRoots()
      return newUid
    },
    /**
     * 跨工作目录移动分组（连同子分组与请求）：目标侧整棵重建 + 源分组进源集合 .trash。
     * 返回被搬走的**源**请求 uid —— 源根里这些请求已不存在，调用方据此清掉对应标签。
     */
    async moveFolderTo(
      srcRoot: string,
      uid: string,
      destRoot: string,
      destParent: string,
    ): Promise<string[]> {
      const moved = await api.moveFolderToCollection(srcRoot, uid, destRoot, destParent)
      await this.loadRoots()
      return moved ?? []
    },
    /** 恢复活动根上次选择的环境（新键没有时回退到旧的 uid 键）。 */
    restoreEnv(): void {
      const saved =
        localStorage.getItem(envKey(this.activeRoot)) ??
        (this.uid ? localStorage.getItem(`client.env.${this.uid}`) : null)
      this.currentEnv = saved && this.envNames.includes(saved) ? saved : (this.envNames[0] ?? '')
    },
    setEnv(name: string): void {
      this.currentEnv = name
      localStorage.setItem(envKey(this.activeRoot), name)
    },
    // ---- H5 收藏 / 置顶（按根隔离）----
    loadFavs(): void {
      try {
        const raw =
          localStorage.getItem(favKey(this.activeRoot)) ??
          (this.uid ? localStorage.getItem(`client.fav.${this.uid}`) : null)
        this.favorites = raw ? (JSON.parse(raw) as string[]) : []
      } catch {
        this.favorites = []
      }
    },
    saveFavs(): void {
      try {
        localStorage.setItem(favKey(this.activeRoot), JSON.stringify(this.favorites))
      } catch {
        // 忽略
      }
    },
    toggleFav(uid: string): void {
      const i = this.favorites.indexOf(uid)
      if (i >= 0) this.favorites.splice(i, 1)
      else this.favorites.unshift(uid) // 新收藏排最前
      this.saveFavs()
    },
    isFav(uid: string): boolean {
      return this.favorites.includes(uid)
    },
    /** 保存环境并保持当前选择（供环境管理弹窗使用）。 */
    async saveEnv(env: Env): Promise<void> {
      await api.saveEnv(env)
      await this.reload()
      if (this.envNames.includes(env.name)) this.setEnv(env.name)
    },
    /** 给环境改名（两个文件一起搬）；当前选中的正是旧名时跟着切到新名。 */
    async renameEnv(oldName: string, newName: string): Promise<void> {
      if (oldName === newName) return
      // 先记下「要不要跟着改名」：reload 会按新列表校正 currentEnv（旧名已不在列表里），
      // 若在 reload 之后再比 oldName 就永远判不中了。
      const follow = this.currentEnv === oldName
      await api.renameEnv(oldName, newName)
      await this.reload()
      if (follow) this.setEnv(newName)
    },
    async deleteEnv(name: string): Promise<void> {
      // 同上：删掉的环境不在新列表里，reload 后 currentEnv 已被校正，须先记下是否删的是当前项
      const wasCurrent = this.currentEnv === name
      await api.deleteEnv(name)
      await this.reload()
      if (wasCurrent) this.setEnv(this.envNames[0] ?? '')
    },
  },
})
