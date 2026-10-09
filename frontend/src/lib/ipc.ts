// 与 Go 侧通信的唯一入口：
//   - Wails 运行时：window.go.app.App.<Method>(args…)（bindings 注入）
//   - 浏览器开发：POST /api/App/<Method>（vite 代理到 cmd/devserver）
// 两态走同一套 App 方法，前端无感知。
import type {
  CollectionInfo,
  CollectionSummary,
  ConflictItem,
  CookieInfo,
  DocEntry,
  Env,
  GrpcDefault,
  GrpcFieldInfo,
  GrpcProtoFile,
  GrpcSchema,
  HistoryEntry,
  ImportSummary,
  IndexNode,
  MCPStatus,
  MockStatus,
  RequestDoc,
  ResolveResult,
  ResponseExample,
  SendResult,
  Settings,
  SyncBindInfo,
  SyncReport,
  SyncStatus,
} from '@/types'

type WailsMethods = Record<string, (...args: unknown[]) => Promise<unknown>>

// Wails 绑定按「包名」注册：本仓库为 window.go.app.App（见 frontend/wailsjs/go/app/App.js）。
// 兼容 go.main.App 写法，避免硬编码查不到而静默回退到 HTTP。
function wailsApp(): WailsMethods | undefined {
  const go = (window as unknown as { go?: Record<string, { App?: WailsMethods }> }).go
  return go?.app?.App ?? go?.main?.App
}

/** 是否运行在 Wails 桌面壳内：窗口控制等能力仅桌面端可用。 */
export function hasWailsRuntime(): boolean {
  return wailsApp() !== undefined
}

/**
 * 订阅 Go 侧 EventsEmit（桌面端）；devserver 无此通道。
 * 统一转发为 window CustomEvent，App 用 addEventListener 接。
 */
/** 浏览器态的 SSE 事件桥（devserver /api/events）：按事件名分发，多条 handler 共用一条连接。 */
let eventSource: EventSource | null = null
const eventHandlers = new Map<string, Set<(payload: unknown) => void>>()

function ensureEventSource(): void {
  if (eventSource) return
  eventSource = new EventSource('/api/events')
  eventSource.onmessage = (ev) => {
    try {
      const { name, payload } = JSON.parse(ev.data) as { name: string; payload: unknown }
      eventHandlers.get(name)?.forEach((h) => h(payload))
    } catch {
      // 忽略坏帧
    }
  }
}

export function onAppEvent(name: string, handler: (payload: unknown) => void): void {
  const rt = (window as unknown as {
    runtime?: { EventsOn?: (n: string, cb: (d: unknown) => void) => void }
  }).runtime
  if (rt?.EventsOn) {
    rt.EventsOn(name, handler)
    return
  }
  // 浏览器调试：无 Wails 事件总线，走 devserver 的 SSE
  ensureEventSource()
  if (!eventHandlers.has(name)) eventHandlers.set(name, new Set())
  eventHandlers.get(name)?.add(handler)
}

/** 自绘标题栏的窗口控制（无边框模式下替代系统装饰）。 */
export const windowCtl = {
  minimise: (): void => void wailsApp()?.WindowMinimise(),
  toggleMaximise: (): void => void wailsApp()?.WindowToggleMaximise(),
  // 先放行关闭守卫（见 Go 侧 WindowShouldClose）再 Quit，避免程序化退出被 Alt+F4 拦截逻辑挡住
  quit: async (): Promise<void> => {
    await wailsApp()?.SetQuitAllowed(true)
    await wailsApp()?.Quit()
  },
  isMaximised: async (): Promise<boolean> => (await wailsApp()?.WindowIsMaximised()) === true,
}

async function call<T>(method: string, ...args: unknown[]): Promise<T> {
  const fn = wailsApp()?.[method]
  if (fn) {
    return fn(...args) as Promise<T>
  }
  // devserver 反射桥要求：POST + application/json（跨站简单请求打不进来）。
  // 若 devserver 用 -token 启动（绑非回环地址时必填），令牌从 localStorage 读并随请求带上。
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  const devToken = localStorage.getItem('dev.token')
  if (devToken) headers['X-Dev-Token'] = devToken
  const res = await fetch(`/api/App/${method}`, {
    method: 'POST',
    headers,
    body: JSON.stringify(args),
  })
  const text = await res.text()
  // 桌面端若误走 HTTP（Wails 资源服务对 POST 返回 405），这里给出可定位的报错而非 JSON 解析异常
  if (!res.ok || !text.trim().startsWith('{')) {
    throw new Error(`IPC 调用失败: ${method} (HTTP ${res.status})`)
  }
  const payload = JSON.parse(text) as { ok: boolean; data?: T; error?: string }
  if (!payload.ok) {
    throw new Error(payload.error ?? 'IPC 调用失败')
  }
  return payload.data as T
}

// Go 侧 nil 切片会序列化为 null（params/headers/body.form/envs/tree），
// 在 IPC 边界统一归一化，组件层可直接按 types.ts 的必填数组契约使用。
function normalizeRequest(r: RequestDoc): RequestDoc {
  return {
    ...r,
    params: r.params ?? [],
    headers: r.headers ?? [],
    auth: r.auth ?? { type: 'none' },
    body: {
      type: r.body?.type ?? 'none',
      raw: r.body?.raw ?? '',
      form: r.body?.form ?? [],
    },
    // grpc 段的 imports/metadata 同样是可省略字段（Go 侧 omitempty）：归一成数组，界面免判空
    grpc: r.grpc
      ? { ...r.grpc, imports: r.grpc.imports ?? [], metadata: r.grpc.metadata ?? [] }
      : null,
  }
}

function normalizeInfo(info: CollectionInfo): CollectionInfo {
  return {
    ...info,
    tree: info.tree ?? [],
    envs: (info.envs ?? []).map((e) => ({ ...e, vars: e.vars ?? [] })),
  }
}

/** 多根概要：树的形状与 CollectionInfo 一致，这里逐项归一化（null 树会让渲染层炸）。 */
function normalizeSummaries(list: CollectionSummary[] | null): CollectionSummary[] {
  return (list ?? []).map((s) => ({ ...s, info: normalizeInfo(s.info) }))
}

function normalizeSend(res: SendResult): SendResult {
  return { ...res, headers: res.headers ?? [], warnings: res.warnings ?? [] }
}

function normalizeResolve(res: ResolveResult): ResolveResult {
  return { ...res, missing: res.missing ?? [], values: res.values ?? {} }
}

function normalizeExample(ex: ResponseExample): ResponseExample {
  return {
    ...ex,
    request: {
      ...ex.request,
      headers: ex.request?.headers ?? [],
      body: { type: ex.request?.body?.type ?? 'none', raw: ex.request?.body?.raw ?? '', form: ex.request?.body?.form ?? [] },
    },
    response: { ...ex.response, headers: ex.response?.headers ?? [] },
  }
}

export const api = {
  pickDirectory: () => call<string>('PickDirectory'),
  pickFile: () => call<string>('PickFile'),
  /** 打开工作目录（多根：追加并置为活动根；同一目录重复打开是幂等的）。 */
  openCollection: (dir: string) => call<CollectionInfo>('OpenCollection', dir).then(normalizeInfo),
  /** 全部已打开的工作目录（按打开顺序），用于侧栏单树多根。 */
  listCollections: () => call<CollectionSummary[] | null>('ListCollections').then(normalizeSummaries),
  /** 当前活动根的标识（空串 = 未打开任何目录）。 */
  activeCollection: () => call<string>('ActiveCollection'),
  /** 切换活动根（集合级动作都作用于活动根）。 */
  setActiveCollection: (root: string) => call<null>('SetActiveCollection', root),
  /** 关闭一个工作目录（其它根不受影响）。 */
  closeCollection: (root: string) => call<null>('CloseCollection', root),
  /** 重载指定工作目录（外部改动 / git 操作后）；不传 = 活动根。 */
  reloadCollectionOf: (root = '') => call<CollectionInfo>('ReloadCollectionOf', root).then(normalizeInfo),
  reload: () => call<CollectionInfo>('ReloadCollection').then(normalizeInfo),
  createRequest: (folder: string, name: string, method: string) =>
    call<RequestDoc>('CreateRequest', folder, name, method).then(normalizeRequest),
  /** 新建流程：把内存草稿落盘成新请求（uid/路径/序号由集合层分配）。 */
  createRequestFromDraft: (folder: string, name: string, doc: RequestDoc) =>
    call<RequestDoc>('CreateRequestFromDraft', folder, name, doc).then(normalizeRequest),
  /** 导入 cURL：把 curl 命令解析成请求草稿（不落盘）。 */
  parseCurl: (text: string) => call<RequestDoc>('ParseCurl', text).then(normalizeRequest),
  /** gRPC：选择 .proto 定义（可多选，带 .proto 过滤；桌面端原生对话框）。 */
  pickGrpcProtos: () => call<string[] | null>('PickGrpcProtoFiles').then((v) => v ?? []),
  /** gRPC：导入 .proto（先编译校验，成功才复制进集合 protos/ 并重新解析；失败不落盘）。 */
  importGrpcProtos: (files: string[], imports: string[]) =>
    call<GrpcSchema>('ImportGrpcProtos', files, imports),
  /** gRPC：集合内已导入的定义清单（免点文件选择器）。 */
  listGrpcProtos: () => call<GrpcProtoFile[] | null>('ListGrpcProtos').then((v) => v ?? []),
  /** gRPC：删除集合内的定义（引用检查由界面负责）。 */
  removeGrpcProto: (rel: string) => call<null>('RemoveGrpcProto', rel),
  /** gRPC：解析请求里已保存的定义（打开请求 / 更新定义后调用，命中缓存）。 */
  loadGrpcSchema: (protoRel: string, imports: string[]) =>
    call<GrpcSchema>('LoadGrpcSchema', protoRel, imports),
  /** gRPC：入参消息的字段提示（Message 分段；G5.4）。 */
  grpcMessageFields: (protoRel: string, imports: string[], service: string, method: string) =>
    call<GrpcFieldInfo[] | null>('GrpcMessageFields', protoRel, imports, service, method).then((v) => v ?? []),
  /** gRPC：按定义校验请求消息（Message 分段的内联提示；G5.3）。空串 = 通过。 */
  grpcValidateMessage: (protoRel: string, imports: string[], service: string, method: string, message: string) =>
    call<string>('GrpcValidateMessage', protoRel, imports, service, method, message),
  /** gRPC：按定义生成请求消息样例（protojson 文本；G5.2）。 */
  grpcSampleMessage: (protoRel: string, imports: string[], service: string, method: string) =>
    call<string>('GrpcSampleMessage', protoRel, imports, service, method),
  /** gRPC：集合级默认定义（P8；未配置返回 null）。 */
  getGrpcDefault: () => call<GrpcDefault | null>('GetGrpcDefault'),
  /** gRPC：写集合级默认定义（proto 为空 = 清除）。 */
  setGrpcDefault: (proto: string, imports: string[]) => call<null>('SetGrpcDefault', proto, imports),
  createFolder: (parent: string, name: string) => call<null>('CreateFolder', parent, name),
  renameFolder: (uid: string, name: string) => call<null>('RenameFolder', uid, name),
  deleteFolder: (uid: string) => call<null>('DeleteFolder', uid),
  readRequest: (uid: string) => call<RequestDoc>('ReadRequest', uid).then(normalizeRequest),
  saveRequest: (r: RequestDoc) => call<null>('SaveRequest', r),
  renameRequest: (uid: string, name: string) => call<null>('RenameRequest', uid, name),
  moveRequest: (uid: string, destFolder: string) => call<null>('MoveRequest', uid, destFolder),
  moveFolder: (uid: string, destParent: string) => call<null>('MoveFolder', uid, destParent),
  /** 跨工作目录移动请求：目标侧重建（新 uid/路径），源文件进源集合 .trash；返回新 uid。 */
  moveRequestToCollection: (srcRoot: string, uid: string, destRoot: string, destFolder: string) =>
    call<string>('MoveRequestToCollection', srcRoot, uid, destRoot, destFolder),
  /**
   * 跨工作目录移动分组（连同其下子分组与请求）：目标侧整棵重建，源分组进源集合 .trash。
   * 返回**被搬走的源请求 uid**（界面据此清掉源根里已失效的标签）。
   */
  moveFolderToCollection: (srcRoot: string, uid: string, destRoot: string, destParent: string) =>
    call<string[]>('MoveFolderToCollection', srcRoot, uid, destRoot, destParent),
  deleteRequest: (uid: string) => call<null>('DeleteRequest', uid),
  saveEnv: (env: Env) => call<null>('SaveEnv', env),
  deleteEnv: (name: string) => call<null>('DeleteEnv', name),
  send: (r: RequestDoc, envName: string) => call<SendResult>('SendRequest', r, envName).then(normalizeSend),
  cancelSend: (uid: string) => call<null>('CancelSend', uid),
  saveResponseBody: (defaultName: string, binary: boolean, body: string) =>
    call<string>('SaveResponseBody', defaultName, binary, body),
  listResponseExamples: (reqUid: string) =>
    call<ResponseExample[] | null>('ListResponseExamples', reqUid).then((v) => (v ?? []).map(normalizeExample)),
  saveResponseExample: (r: RequestDoc, name: string, res: SendResult) =>
    call<ResponseExample>('SaveResponseExample', r, name, res).then(normalizeExample),
  deleteResponseExample: (reqUid: string, exampleUid: string) =>
    call<null>('DeleteResponseExample', reqUid, exampleUid),
  resolveText: (text: string, envName: string) =>
    call<ResolveResult>('ResolveText', text, envName).then(normalizeResolve),
  getSettings: () => call<Settings>('GetSettings'),
  saveSettings: (s: Settings) => call<null>('SaveSettings', s),
  mcpStatus: () => call<MCPStatus>('MCPStatusText'),
  applyMCPSettings: () => call<MCPStatus>('ApplyMCPSettings'),
  regenerateMCPToken: () => call<MCPStatus>('RegenerateMCPToken'),
  listHistory: (limit: number) => call<HistoryEntry[] | null>('ListHistory', limit).then((v) => v ?? []),
  clearHistory: () => call<null>('ClearHistory'),
  replayHistory: (index: number, envName: string) =>
    call<SendResult>('ReplayHistory', index, envName).then(normalizeSend),
  listCookies: () => call<CookieInfo[] | null>('ListCookies').then((v) => v ?? []),
  clearCookies: () => call<null>('ClearCookies'),
  deleteCookie: (domain: string, name: string, path: string) =>
    call<null>('DeleteCookie', domain, name, path),
  searchIndex: (q: string, limit = 200) => call<IndexNode[] | null>('SearchIndex', q, limit).then((v) => v ?? []),
  rebuildIndex: () => call<null>('RebuildIndex'),
  generateCode: (lang: string, envName: string, r: RequestDoc) =>
    call<string>('GenerateCode', lang, envName, r),
  importCollection: (parent: string, format: string, text: string) =>
    call<ImportSummary>('ImportCollection', parent, format, text),
  importBrunoDir: (srcDir: string) => call<ImportSummary>('ImportBrunoDir', srcDir),
  exportDoc: (format: string, defaultName: string) => call<string>('ExportDoc', format, defaultName),
  startMock: (port: number) => call<MockStatus>('StartMock', port),
  stopMock: () => call<null>('StopMock'),
  mockStatus: () => call<MockStatus>('MockStatus'),
  getSyncBind: () => call<SyncBindInfo>('GetSyncBind'),
  setSyncBind: (serverUrl: string, projectId: number, mode: string, token: string) =>
    call<SyncBindInfo>('SetSyncBind', serverUrl, projectId, mode, token),
  unbindSync: () => call<null>('UnbindSync'),
  runSync: (token: string) => call<SyncReport>('RunSync', token),
  listConflicts: () => call<ConflictItem[] | null>('ListConflicts').then((v) => v ?? []),
  resolveConflict: (file: string, choice: string) => call<null>('ResolveConflict', file, choice),
  listDocs: () => call<DocEntry[] | null>('ListDocs').then((v) => v ?? []),
  readDoc: (uid: string) => call<DocEntry>('ReadDoc', uid),
  createDoc: (name: string) => call<DocEntry>('CreateDoc', name),
  saveDoc: (d: DocEntry) => call<null>('SaveDoc', d),
  deleteDoc: (uid: string) => call<null>('DeleteDoc', uid),
  getSyncStatus: () => call<SyncStatus>('GetSyncStatus'),
  startAutoSync: (intervalSec: number) => call<null>('StartAutoSync', intervalSec),
  stopAutoSync: () => call<null>('StopAutoSync'),
}
