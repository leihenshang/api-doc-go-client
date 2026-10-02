// 多标签会话：tab = 一个打开的请求（uid + 内存草稿 + 响应 + 同步状态）。
// 规则（设计文档 §5.3 会话模型）：
//   - Send 用当前草稿，绝不因"未保存"阻塞；
//   - 自动保存（防抖 800ms，关闭 tab / 失焦 / 发送前 flush）；
//   - URL 预览与变量告警独立防抖（300ms）；
//   - 新建请求先开「未落盘草稿」tab（draft=true）：不写盘、不自动保存，关闭时由 UI 提示保存 + 选分组。
import { defineStore } from 'pinia'
import { i18n } from '@/i18n'
import { api } from '@/lib/ipc'
import { grpcSendBlocker } from '@/lib/grpc'
import { message } from '@/lib/notice'
import { useCollectionStore } from '@/stores/collection'
import type { GrpcSchema, RequestDoc, ResolveResult, SendResult } from '@/types'

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
  /** 新建但尚未落盘（不自动保存；关闭时提示保存 + 选分组） */
  draft: boolean
  /** 草稿的默认分组（保存对话框预选） */
  draftFolder: string
  /** gRPC 定义解析结果（Schema 分段与请求栏「服务 / 方法」共用；HTTP 请求恒为 null） */
  grpcSchema: GrpcSchema | null
  /** gRPC 定义解析错误（常驻展示，直到重新导入；HTTP 请求恒为空） */
  grpcError: string
}

let seq = 0
const nextKey = (): string => `tab-${++seq}`

/** 草稿 tab 的临时 uid：不落盘、不与磁盘 uid 冲突，仅用于取消发送等按 uid 定位的场景。 */
let draftSeq = 0
const nextDraftUid = (): string => `draft-${++draftSeq}-${Date.now().toString(36)}`

const SAVE_DEBOUNCE = 800
const RESOLVE_DEBOUNCE = 300

const saveTimers = new Map<string, ReturnType<typeof setTimeout>>()
const resolveTimers = new Map<string, ReturnType<typeof setTimeout>>()
let draftsTimer: ReturnType<typeof setTimeout> | null = null

/** 会话现场按集合 uid 记忆（与 client.env.<uid> 同一套 localStorage）。 */
const sessionKey = (collUid: string): string => `client.tabs.${collUid}`
/** 未保存草稿单独存一份：应用重启/切换集合后仍能找回（新建流程不写盘）。 */
const draftsKey = (collUid: string): string => `client.drafts.${collUid}`

interface TabSession {
  uids: string[]
  activeUid: string
}

/** 新建草稿的空请求模板：与 Go 侧 CreateRequest 的默认值一致（URL 留空，不预填 {{host}}）。 */
function blankRequest(): RequestDoc {
  return {
    uid: '',
    name: '',
    seq: 0,
    path: '',
    method: 'GET',
    url: '',
    params: [{ name: '', value: '', enabled: true }],
    headers: [{ name: 'Content-Type', value: 'application/json', enabled: true }],
    body: { type: 'none', raw: '', form: [] },
    auth: { type: 'none' },
    docs: '',
    baseRev: 0,
  }
}

/**
 * 新建 gRPC 草稿的空请求模板（G1.2）：grpc 段存在即 gRPC 请求；
 * target/service/method/proto/message 全空，由用户在请求栏与 Schema 分段补齐。
 * Method 固定 `GRPC`、URL 留空 —— 落盘时由集合层派生（对齐 collection.MethodGRPC / GrpcURL）。
 */
export function blankGrpcRequest(): RequestDoc {
  return {
    uid: '',
    name: '',
    seq: 0,
    path: '',
    method: 'GRPC',
    url: '',
    params: [],
    headers: [],
    body: { type: 'none', raw: '', form: [] },
    auth: { type: 'none' },
    grpc: {
      target: '',
      service: '',
      method: '',
      proto: '',
      imports: [],
      metadata: [{ name: '', value: '', enabled: true }],
      message: '',
      stream: '',
    },
    docs: '',
    baseRev: 0,
  }
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
    /**
     * 新建请求的主路径：开一个**未落盘**的草稿 tab（不写盘、不弹窗）。
     * doc 为「导入 cURL」等预填内容；folder 是保存对话框的默认分组。
     */
    openDraft(doc: RequestDoc | null, folder = ''): string {
      const r = doc ?? blankRequest()
      const tab: Tab = {
        key: nextKey(), uid: nextDraftUid(),
        title: r.name || i18n.global.t(r.grpc ? 'tab.newGrpcDraft' : 'tab.newDraft'), request: r,
        dirty: true, sending: false, response: null, error: '', resolve: null,
        draft: true, draftFolder: folder, grpcSchema: null, grpcError: '',
      }
      this.tabs.push(tab)
      this.setActive(tab.key)
      const current = this.tabs.find((t) => t.key === tab.key)
      if (current) {
        void this.doResolve(current)
        lastSnap.set(tab.key, snap(current.request))
      }
      // 恢复的 gRPC 草稿已带定义路径：直接把 Schema 解析回来（空定义时是空操作）
      void this.loadGrpcSchema(tab.key)
      this.saveSession()
      this.saveDrafts()
      return tab.key
    },
    /** 把草稿落盘成真实请求：uid / 文件名 / 序号由集合层分配，成功后 tab 转正（不再是草稿）。 */
    async saveDraft(key: string, folder: string, name: string): Promise<RequestDoc> {
      const tab = this.tabs.find((t) => t.key === key)
      if (!tab) throw new Error('tab 不存在')
      const created = await api.createRequestFromDraft(folder, name, { ...tab.request, name })
      tab.uid = created.uid
      tab.request = created
      tab.title = created.name
      tab.draft = false
      tab.draftFolder = ''
      tab.dirty = false
      lastSnap.set(key, snap(created))
      undoStacks.delete(key)
      redoStacks.delete(key)
      await useCollectionStore().reload()
      this.saveSession()
      this.saveDrafts()
      return created
    },
    /** 丢弃草稿（关闭 tab，不落盘）。 */
    async discardDraft(key: string): Promise<void> {
      await this.close(key)
    },
    pushTab(r: RequestDoc): void {
      const tab: Tab = {
        key: nextKey(), uid: r.uid, title: r.name, request: r,
        dirty: false, sending: false, response: null, error: '', resolve: null,
        draft: false, draftFolder: '', grpcSchema: null, grpcError: '',
      }
      this.tabs.push(tab)
      this.setActive(tab.key)
      // 必须取数组里的响应式代理再解析：直接改 push 进去的原始对象不会触发视图更新
      const current = this.tabs.find((t) => t.key === tab.key)
      if (current) {
        void this.doResolve(current)
        lastSnap.set(tab.key, snap(current.request))
      }
      void this.loadGrpcSchema(tab.key)
      this.saveSession()
    },
    /**
     * 解析该 tab 里已保存的 gRPC 定义（打开请求 / 导入 / 更新 / 换定义后调用）。
     * 结果与错误都常驻在 tab 上：成功 → 请求栏与 Schema 分段可用；失败 → 界面展示错误卡片（D4 的界面态）。
     */
    async loadGrpcSchema(key: string): Promise<void> {
      const tab = this.tabs.find((t) => t.key === key)
      const g = tab?.request.grpc
      if (!tab || !g) return
      const proto = (g.proto ?? '').trim()
      if (!proto) {
        tab.grpcSchema = null
        tab.grpcError = ''
        return
      }
      try {
        tab.grpcSchema = await api.loadGrpcSchema(proto, g.imports ?? [])
        tab.grpcError = ''
      } catch (e) {
        tab.grpcSchema = null
        tab.grpcError = e instanceof Error ? e.message : String(e)
      }
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
      // 自动保存；草稿不写盘，改为把内容落到 localStorage（重启不丢）
      clearTimeout(saveTimers.get(key))
      if (tab.draft) this.scheduleDrafts()
      else saveTimers.set(key, setTimeout(() => void this.flush(key), SAVE_DEBOUNCE))
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
    /** 立即保存（幂等；无脏改动、草稿、无 uid 时跳过）。
     *  写盘被拒（D4/G3.5：定义解析失败等）时保留脏标记，并把原因挂到 grpcError 上供界面展示。 */
    async flush(key: string): Promise<void> {
      clearTimeout(saveTimers.get(key))
      const tab = this.tabs.find((t) => t.key === key)
      if (!tab || tab.draft || !tab.dirty || !tab.uid) return
      try {
        await api.saveRequest(tab.request)
        tab.dirty = false
      } catch (e) {
        const msg = e instanceof Error ? e.message : String(e)
        // 同一条错误只提示一次（自动保存会反复触发），Schema 分段常驻展示这次失败原因
        if (tab.grpcError !== msg) {
          tab.grpcError = msg
          message.error(msg)
        }
        return
      }
    },
    async flushAll(): Promise<void> {
      for (const t of this.tabs) await this.flush(t.key)
    },
    async doResolve(tab: Tab): Promise<void> {
      const coll = useCollectionStore()
      if (!coll.ready) return
      // 变量来自「地址」：HTTP 是 URL，gRPC 是服务地址 target（G10.1）；两者都空时无需解析
      const text = (tab.request.grpc ? tab.request.grpc.target : tab.request.url).trim()
      if (!text) {
        tab.resolve = null // 刚新建的空草稿
        return
      }
      try {
        tab.resolve = await api.resolveText(text, coll.currentEnv)
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
      // gRPC 发送门禁（G3.6）：地址 / 定义 / 服务方法 / 消息任一未就绪都不发送（按钮已置灰，这里兜底）
      const blocker = grpcSendBlocker(tab.request, !!tab.grpcSchema)
      if (blocker) {
        tab.response = null
        tab.error = i18n.global.t(blocker)
        return
      }
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
      // flush 失败不阻止关闭：本地已自动保存，关闭不应卡住
      try {
        await this.flush(key)
      } catch {
        // 忽略保存错误，仍继续关闭
      }
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
    /** 关闭集合（切换集合）时清空内存会话（文件已自动保存；现场与草稿已单独落 localStorage）。 */
    reset(): void {
      for (const t of this.tabs) {
        clearTimeout(saveTimers.get(t.key))
        clearTimeout(resolveTimers.get(t.key))
        clearTimeout(historyTimers.get(t.key))
      }
      if (draftsTimer) clearTimeout(draftsTimer)
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
    /** 草稿编辑防抖落 localStorage（草稿没有磁盘副本，重启后靠它找回）。 */
    scheduleDrafts(): void {
      if (draftsTimer) clearTimeout(draftsTimer)
      draftsTimer = setTimeout(() => this.saveDrafts(), SAVE_DEBOUNCE)
    },
    /** 把所有未保存草稿写进 localStorage（没有草稿时清掉旧键）。 */
    saveDrafts(): void {
      const coll = useCollectionStore()
      if (!coll.uid) return
      const drafts = this.tabs
        .filter((t) => t.draft)
        .map((t) => ({ request: t.request, folder: t.draftFolder }))
      try {
        if (drafts.length) localStorage.setItem(draftsKey(coll.uid), JSON.stringify(drafts))
        else localStorage.removeItem(draftsKey(coll.uid))
      } catch {
        // localStorage 满/隐私模式：忽略
      }
    },
    /** 打开集合后恢复上次未保存的草稿（放最后，保持「草稿是最近打开的」直觉）。 */
    restoreDrafts(): void {
      const coll = useCollectionStore()
      if (!coll.uid) return
      let raw = ''
      try {
        raw = localStorage.getItem(draftsKey(coll.uid)) ?? ''
      } catch {
        return
      }
      if (!raw) return
      let list: { request: RequestDoc; folder?: string }[]
      try {
        list = JSON.parse(raw) as { request: RequestDoc; folder?: string }[]
      } catch {
        return
      }
      for (const d of list ?? []) {
        if (d?.request) this.openDraft(d.request, d.folder ?? '')
      }
    },
    /** 落盘 tab 现场（uid 列表 + 激活项）；草稿走 saveDrafts，不在这里重复。 */
    saveSession(): void {
      const coll = useCollectionStore()
      if (!coll.uid) return
      const active = this.tabs.find((t) => t.key === this.activeKey)
      const data: TabSession = {
        uids: this.tabs.filter((t) => !t.draft).map((t) => t.uid),
        activeUid: active && !active.draft ? active.uid : '',
      }
      try {
        localStorage.setItem(sessionKey(coll.uid), JSON.stringify(data))
      } catch {
        // localStorage 满/隐私模式：忽略，不影响使用
      }
      this.saveDrafts() // 会话与草稿同进同出，避免关闭草稿后 localStorage 里残留
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
      this.restoreDrafts()
      if (data.activeUid) {
        const tab = this.tabs.find((t) => t.uid === data.activeUid)
        if (tab) this.setActive(tab.key)
      }
    },
  },
})
