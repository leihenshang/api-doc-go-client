// 集合状态：打开的集合（树/环境）与当前环境选择。
// 当前环境按集合 uid 记忆（localStorage），与服务端 Web 的 apidoc.env.<projectId> 习惯一致。
import { defineStore } from 'pinia'
import { api } from '@/lib/ipc'
import type { CollectionInfo, Env, RequestDoc } from '@/types'

export const useCollectionStore = defineStore('collection', {
  state: () => ({
    info: null as CollectionInfo | null,
    ready: false,
    loading: false,
    currentEnv: '',
    lastError: '',
    /** H5：收藏的请求 uid 集合（localStorage: client.fav.<集合uid>） */
    favorites: [] as string[],
    /** 同步模式：''（未关联）| auto | manual | mirror */
    syncMode: '',
  }),
  getters: {
    uid: (s) => s.info?.uid ?? '',
    name: (s) => s.info?.name ?? '',
    dir: (s) => s.info?.dir ?? '',
    tree: (s) => s.info?.tree ?? [],
    envNames: (s) => s.info?.envs.map((e) => e.name) ?? [],
    favSet: (s) => new Set(s.favorites),
    /** mirror 模式只读：仅当已关联且 mode=mirror 时才禁编辑。 */
    isReadOnly: (s) => s.syncMode === 'mirror',
  },
  actions: {
    async open(dir: string): Promise<void> {
      this.loading = true
      this.lastError = ''
      try {
        this.info = await api.openCollection(dir)
        this.ready = true
        localStorage.setItem('client.lastDir', dir)
        this.restoreEnv()
        this.loadFavs()
      } catch (e) {
        this.lastError = e instanceof Error ? e.message : String(e)
        throw e
      } finally {
        this.loading = false
      }
    },
    async reload(): Promise<void> {
      if (!this.info) return
      const prev = this.currentEnv
      this.info = await api.reload()
      this.currentEnv = this.envNames.includes(prev) ? prev : (this.envNames[0] ?? '')
    },
    /** 新建请求（folder 为空 = 根）；返回落盘后的文档供调用方开 tab。 */
    async createRequest(folder: string, name: string, method: string): Promise<RequestDoc> {
      const r = await api.createRequest(folder, name, method)
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
    async renameRequest(uid: string, name: string): Promise<void> {
      await api.renameRequest(uid, name)
      await this.reload()
    },
    async moveRequest(uid: string, destFolder: string): Promise<void> {
      await api.moveRequest(uid, destFolder)
      await this.reload()
    },
    async moveFolder(uid: string, destParent: string): Promise<void> {
      await api.moveFolder(uid, destParent)
      await this.reload()
    },
    restoreEnv(): void {
      const saved = localStorage.getItem(`client.env.${this.uid}`)
      this.currentEnv = saved && this.envNames.includes(saved) ? saved : (this.envNames[0] ?? '')
    },
    setEnv(name: string): void {
      this.currentEnv = name
      localStorage.setItem(`client.env.${this.uid}`, name)
    },
    // ---- H5 收藏 / 置顶 ----
    loadFavs(): void {
      try {
        const raw = localStorage.getItem(`client.fav.${this.uid}`)
        this.favorites = raw ? (JSON.parse(raw) as string[]) : []
      } catch {
        this.favorites = []
      }
    },
    saveFavs(): void {
      try {
        localStorage.setItem(`client.fav.${this.uid}`, JSON.stringify(this.favorites))
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
    async deleteEnv(name: string): Promise<void> {
      await api.deleteEnv(name)
      await this.reload()
      if (this.currentEnv === name) this.setEnv(this.envNames[0] ?? '')
    },
  },
})
