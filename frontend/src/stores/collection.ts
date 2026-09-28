// 集合状态：打开的集合（树/环境）与当前环境选择。
// 当前环境按集合 uid 记忆（localStorage），与服务端 Web 的 apidoc.env.<projectId> 习惯一致。
import { defineStore } from 'pinia'
import { api } from '@/lib/ipc'
import type { CollectionInfo, Env } from '@/types'

export const useCollectionStore = defineStore('collection', {
  state: () => ({
    info: null as CollectionInfo | null,
    ready: false,
    loading: false,
    currentEnv: '',
    lastError: '',
  }),
  getters: {
    uid: (s) => s.info?.uid ?? '',
    name: (s) => s.info?.name ?? '',
    dir: (s) => s.info?.dir ?? '',
    tree: (s) => s.info?.tree ?? [],
    envNames: (s) => s.info?.envs.map((e) => e.name) ?? [],
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
    restoreEnv(): void {
      const saved = localStorage.getItem(`client.env.${this.uid}`)
      this.currentEnv = saved && this.envNames.includes(saved) ? saved : (this.envNames[0] ?? '')
    },
    setEnv(name: string): void {
      this.currentEnv = name
      localStorage.setItem(`client.env.${this.uid}`, name)
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
