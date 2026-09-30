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

/** 会话现场按集合 uid 记忆（与 client.env.<uid> 同一套 localStorage）。 */
const sessionKey = (collUid: string): string => `client.tabs.${collUid}`

interface TabSession {
  uids: string[]
  activeUid: string
}

// ---- 请求级撤销/重做（G9）：编辑停顿后把「上一状态」压栈，Ctrl+Z / Ctrl+Shift+Z 取出 ----
const HISTORY_LIMIT = 50
const HISTORY_DEBOUNCE = 400
const undoStacks = new Map<string, RequestDoc[]>()
const redoStacks = new Map<string, RequestDoc[]>()
const lastSnap = new Map<string, string>()
const historyTimers = new Map<string, ReturnType<typeof setTimeout>>()

function snap(r: RequestDoc): string {
  return JSON.stringify(r)
}

// ---- 滚动位置记忆（G8）：按 tab 记录请求区/响应区 scrollTop ----
const scrollPos = new Map<string, { editor: number; resp: number }>()

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
      if (current) {
        void this.doResolve(current)
        lastSnap.set(tab.key, snap(current.request))
      }
      this.saveSession()
    },
    setActive(key: string): void {
      this.activeKey = key
      this.saveSession()
    },
    /** G7：把 from 位置的 tab 挪到 to 位置。 */
    reorder(from: number, to: number): void {
      if (from === to || from < 0 || to < 0 || from >= this.tabs.length || to >= this.tabs.length) return
      const [item] = this.tabs.splice(from, 1)
      this.tabs.splice(to, 0, item)
      this.saveSession()
    },
    /** G8：记录某 tab 的滚动位置。 */
    saveScroll(key: string, pane: 'editor' | 'resp', top: number): void {
      const cur = scrollPos.get(key) ?? { editor: 0, resp: 0 }
      cur[pane] = top
      scrollPos.set(key, cur)
    },
    /** G8：取某 tab 的滚动位置（无记录时 0）。 */
    getScroll(key: string): { editor: number; resp: number } {
      return scrollPos.get(key) ?? { editor: 0, resp: 0 }
    },
    /** 任意编辑入口：标脏 + 调度自动保存、解析预览与撤销快照。 */
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
      // 撤销快照（编辑停顿后提交一段）
      clearTimeout(historyTimers.get(key))
      historyTimers.set(key, setTimeout(() => this.commitHistory(key), HISTORY_DEBOUNCE))
    },
    /** 把「编辑前状态」压入撤销栈（当前状态与上次快照不同时）。 */
    commitHistory(key: string): void {
      const tab = this.tabs.find((t) => t.key === key)
      if (!tab) return
      const cur = snap(tab.request)
      const last = lastSnap.get(key)
      if (last === undefined) {
        lastSnap.set(key, cur)
        return
      }
      if (last === cur) return
      const stack = undoStacks.get(key) ?? []
      stack.push(JSON.parse(last) as RequestDoc)
      if (stack.length > HISTORY_LIMIT) stack.shift()
      undoStacks.set(key, stack)
      redoStacks.set(key, [])
      lastSnap.set(key, cur)
    },
    /** 撤销一步（无历史时为空操作）。 */
    undo(key: string): void {
      const tab = this.tabs.find((t) => t.key === key)
      const stack = undoStacks.get(key)
      if (!tab || !stack?.length) return
      const prev = stack.pop() as RequestDoc
      const cur = JSON.parse(snap(tab.request)) as RequestDoc
      redoStacks.set(key, [...(redoStacks.get(key) ?? []), cur])
      tab.request = prev
      tab.title = prev.name || tab.title
      tab.dirty = true
      lastSnap.set(key, snap(prev))
      clearTimeout(saveTimers.get(key))
      saveTimers.set(key, setTimeout(() => void this.flush(key), SAVE_DEBOUNCE))
    },
    /** 重做一步。 */
    redo(key: string): void {
      const tab = this.tabs.find((t) => t.key === key)
      const stack = redoStacks.get(key)
      if (!tab || !stack?.length) return
      const next = stack.pop() as RequestDoc
      const cur = JSON.parse(snap(tab.request)) as RequestDoc
      undoStacks.set(key, [...(undoStacks.get(key) ?? []), cur])
      tab.request = next
      tab.title = next.name || tab.title
      tab.dirty = true
      lastSnap.set(key, snap(next))
      clearTimeout(saveTimers.get(key))
      saveTimers.set(key, setTimeout(() => void this.flush(key), SAVE_DEBOUNCE))
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
    /** 发送：先 flush，再用当前草稿真实发送；可用 cancel() 中止。 */
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
    /** 取消当前 tab 的在途发送。 */
    cancelSend(key: string): void {
      const tab = this.tabs.find((t) => t.key === key)
      if (!tab || !tab.sending || !tab.uid) return
      void api.cancelSend(tab.uid)
    },
    async close(key: string): Promise<void> {
      clearTimeout(saveTimers.get(key))
      clearTimeout(resolveTimers.get(key))
      clearTimeout(historyTimers.get(key))
      const idx = this.tabs.findIndex((t) => t.key === key)
      if (idx < 0) return
      await this.flush(key)
      this.tabs.splice(idx, 1)
      if (this.activeKey === key) {
        const next = this.tabs[Math.min(idx, this.tabs.length - 1)]
        this.activeKey = next?.key ?? ''
      }
      undoStacks.delete(key)
      redoStacks.delete(key)
      lastSnap.delete(key)
      scrollPos.delete(key)
      this.saveSession()
    },
    /** 关闭集合（切换集合）时清空内存会话（文件已自动保存；现场已单独落 localStorage）。 */
    reset(): void {
      for (const t of this.tabs) {
        clearTimeout(saveTimers.get(t.key))
        clearTimeout(resolveTimers.get(t.key))
        clearTimeout(historyTimers.get(t.key))
      }
      this.tabs = []
      this.activeKey = ''
      undoStacks.clear()
      redoStacks.clear()
      lastSnap.clear()
      scrollPos.clear()
    },
    async deleteRequest(uid: string): Promise<void> {
      await api.deleteRequest(uid)
      const tab = this.tabs.find((t) => t.uid === uid)
      if (tab) await this.close(tab.key)
    },
    /** 落盘 tab 现场（uid 列表 + 激活项）；草稿本身走自动保存，不在此重复。 */
    saveSession(): void {
      const coll = useCollectionStore()
      if (!coll.uid) return
      const data: TabSession = {
        uids: this.tabs.map((t) => t.uid),
        activeUid: this.tabs.find((t) => t.key === this.activeKey)?.uid ?? '',
      }
      try {
        localStorage.setItem(sessionKey(coll.uid), JSON.stringify(data))
      } catch {
        // localStorage 满/隐私模式：忽略，不影响使用
      }
    },
    /** 打开集合后恢复上次的 tab 现场；已删除的 uid 自动跳过。 */
    async restoreSession(): Promise<void> {
      const coll = useCollectionStore()
      if (!coll.uid) return
      let raw = ''
      try {
        raw = localStorage.getItem(sessionKey(coll.uid)) ?? ''
      } catch {
        return
      }
      if (!raw) return
      let data: TabSession
      try {
        data = JSON.parse(raw) as TabSession
      } catch {
        return
      }
      for (const uid of data.uids ?? []) {
        if (!uid) continue
        try {
          await this.openRequest(uid)
        } catch {
          // 请求已被删除：跳过，不阻断恢复
        }
      }
      if (data.activeUid) {
        const tab = this.tabs.find((t) => t.uid === data.activeUid)
        if (tab) this.setActive(tab.key)
      }
    },
  },
})
