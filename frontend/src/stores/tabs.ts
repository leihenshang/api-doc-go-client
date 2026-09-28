// 多标签会话：tab = 一个打开的请求（uid + 内存草稿 + 响应 + 同步状态）。
// 规则（设计文档 §5.3 会话模型）：
//   - Send 用当前草稿，绝不因"未保存"阻塞；
//   - 自动保存（防抖 800ms，关闭 tab / 失焦 / 发送前 flush）；
//   - URL 预览与变量告警独立防抖（300ms）。
import { defineStore } from 'pinia'
import { api } from '@/lib/ipc'
import { useCollectionStore } from '@/stores/collection'
import type { RequestDoc, ResolveResult, SendResult } from '@/types'

export interface Tab {
  key: string
  uid: string
  title: string
  request: RequestDoc
  dirty: boolean
  sending: boolean
  response: SendResult | null
  error: string
  resolve: ResolveResult | null
}

let seq = 0
const nextKey = (): string => `tab-${++seq}`

const SAVE_DEBOUNCE = 800
const RESOLVE_DEBOUNCE = 300

const saveTimers = new Map<string, ReturnType<typeof setTimeout>>()
const resolveTimers = new Map<string, ReturnType<typeof setTimeout>>()

export const useTabsStore = defineStore('tabs', {
  state: () => ({
    tabs: [] as Tab[],
    activeKey: '',
  }),
  getters: {
    active: (s) => s.tabs.find((t) => t.key === s.activeKey) ?? null,
    count: (s) => s.tabs.length,
  },
  actions: {
    /** 打开一条请求（已有 tab 则激活）。 */
    async openRequest(uid: string): Promise<void> {
      const exist = this.tabs.find((t) => t.uid === uid)
      if (exist) {
        this.setActive(exist.key)
        return
      }
      const r = await api.readRequest(uid)
      this.pushTab(r)
    },
    /** 新建请求（由集合层先落盘拿到 uid，再开 tab）。 */
    openDoc(r: RequestDoc): void {
      this.pushTab(r)
    },
    pushTab(r: RequestDoc): void {
      const tab: Tab = {
        key: nextKey(), uid: r.uid, title: r.name, request: r,
        dirty: false, sending: false, response: null, error: '', resolve: null,
      }
      this.tabs.push(tab)
      this.setActive(tab.key)
      // 必须取数组里的响应式代理再解析：直接改 push 进去的原始对象不会触发视图更新
      const current = this.tabs.find((t) => t.key === tab.key)
      if (current) void this.doResolve(current)
    },
    setActive(key: string): void {
      this.activeKey = key
    },
    /** 任意编辑入口：标脏 + 调度自动保存与解析预览。 */
    touch(key: string): void {
      const tab = this.tabs.find((t) => t.key === key)
      if (!tab) return
      tab.title = tab.request.name || tab.title
      tab.dirty = true
      // 自动保存
      clearTimeout(saveTimers.get(key))
      saveTimers.set(key, setTimeout(() => void this.flush(key), SAVE_DEBOUNCE))
      // 解析预览
      clearTimeout(resolveTimers.get(key))
      resolveTimers.set(key, setTimeout(() => void this.doResolve(tab), RESOLVE_DEBOUNCE))
    },
    /** 立即保存（幂等；无脏改动时跳过）。 */
    async flush(key: string): Promise<void> {
      clearTimeout(saveTimers.get(key))
      const tab = this.tabs.find((t) => t.key === key)
      if (!tab || !tab.dirty || !tab.uid) return
      await api.saveRequest(tab.request)
      tab.dirty = false
    },
    async flushAll(): Promise<void> {
      for (const t of this.tabs) await this.flush(t.key)
    },
    async doResolve(tab: Tab): Promise<void> {
      const coll = useCollectionStore()
      if (!coll.ready) return
      try {
        tab.resolve = await api.resolveText(tab.request.url, coll.currentEnv)
      } catch {
        tab.resolve = null
      }
    },
    /** 环境变化 / 集合变更后刷新解析预览。 */
    refreshResolve(): void {
      for (const t of this.tabs) void this.doResolve(t)
    },
    /** 发送：先 flush，再用当前草稿真实发送。 */
    async send(key: string): Promise<void> {
      const tab = this.tabs.find((t) => t.key === key)
      if (!tab || tab.sending) return
      const coll = useCollectionStore()
      await this.flush(key)
      tab.sending = true
      tab.error = ''
      try {
        tab.response = await api.send(tab.request, coll.currentEnv)
      } catch (e) {
        tab.response = null
        tab.error = e instanceof Error ? e.message : String(e)
      } finally {
        tab.sending = false
      }
    },
    async close(key: string): Promise<void> {
      clearTimeout(saveTimers.get(key))
      clearTimeout(resolveTimers.get(key))
      const idx = this.tabs.findIndex((t) => t.key === key)
      if (idx < 0) return
      await this.flush(key)
      this.tabs.splice(idx, 1)
      if (this.activeKey === key) {
        const next = this.tabs[Math.min(idx, this.tabs.length - 1)]
        this.activeKey = next?.key ?? ''
      }
    },
    /** 关闭集合（切换集合）时清空会话（文件已自动保存，无数据丢失）。 */
    reset(): void {
      for (const t of this.tabs) {
        clearTimeout(saveTimers.get(t.key))
        clearTimeout(resolveTimers.get(t.key))
      }
      this.tabs = []
      this.activeKey = ''
    },
    async deleteRequest(uid: string): Promise<void> {
      await api.deleteRequest(uid)
      const tab = this.tabs.find((t) => t.uid === uid)
      if (tab) await this.close(tab.key)
    },
  },
})
