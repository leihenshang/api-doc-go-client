// 与 Go 侧通信的唯一入口：
//   - Wails 运行时：window.go.app.App.<Method>(args…)（bindings 注入）
//   - 浏览器开发：POST /api/App/<Method>（vite 代理到 cmd/devserver）
// 两态走同一套 App 方法，前端无感知。
import type {
  CollectionInfo,
  ConflictItem,
  CookieInfo,
  DocEntry,
  Env,
  HistoryEntry,
  ImportSummary,
  IndexNode,
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
export function onAppEvent(name: string, handler: (payload: unknown) => void): void {
  const rt = (window as unknown as {
    runtime?: { EventsOn?: (n: string, cb: (d: unknown) => void) => void }
  }).runtime
  if (rt?.EventsOn) {
    rt.EventsOn(name, handler)
    return
  }
  // 浏览器调试：无 Wails 事件总线
}

/** 自绘标题栏的窗口控制（无边框模式下替代系统装饰）。 */
export const windowCtl = {
  minimise: (): void => void wailsApp()?.WindowMinimise(),
  toggleMaximise: (): void => void wailsApp()?.WindowToggleMaximise(),
  quit: (): void => void wailsApp()?.Quit(),
  isMaximised: async (): Promise<boolean> => (await wailsApp()?.WindowIsMaximised()) === true,
}

async function call<T>(method: string, ...args: unknown[]): Promise<T> {
  const fn = wailsApp()?.[method]
  if (fn) {
    return fn(...args) as Promise<T>
  }
  const res = await fetch(`/api/App/${method}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
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
  }
}

function normalizeInfo(info: CollectionInfo): CollectionInfo {
  return {
    ...info,
    tree: info.tree ?? [],
    envs: (info.envs ?? []).map((e) => ({ ...e, vars: e.vars ?? [] })),
  }
}

function normalizeSend(res: SendResult): SendResult {
  return { ...res, headers: res.headers ?? [] }
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
  openCollection: (dir: string) => call<CollectionInfo>('OpenCollection', dir).then(normalizeInfo),
  reload: () => call<CollectionInfo>('ReloadCollection').then(normalizeInfo),
  createRequest: (folder: string, name: string, method: string) =>
    call<RequestDoc>('CreateRequest', folder, name, method).then(normalizeRequest),
  /** 新建流程：把内存草稿落盘成新请求（uid/路径/序号由集合层分配）。 */
  createRequestFromDraft: (folder: string, name: string, doc: RequestDoc) =>
    call<RequestDoc>('CreateRequestFromDraft', folder, name, doc).then(normalizeRequest),
  /** 导入 cURL：把 curl 命令解析成请求草稿（不落盘）。 */
  parseCurl: (text: string) => call<RequestDoc>('ParseCurl', text).then(normalizeRequest),
  createFolder: (parent: string, name: string) => call<null>('CreateFolder', parent, name),
  renameFolder: (uid: string, name: string) => call<null>('RenameFolder', uid, name),
  deleteFolder: (uid: string) => call<null>('DeleteFolder', uid),
  readRequest: (uid: string) => call<RequestDoc>('ReadRequest', uid).then(normalizeRequest),
  saveRequest: (r: RequestDoc) => call<null>('SaveRequest', r),
  renameRequest: (uid: string, name: string) => call<null>('RenameRequest', uid, name),
  moveRequest: (uid: string, destFolder: string) => call<null>('MoveRequest', uid, destFolder),
  moveFolder: (uid: string, destParent: string) => call<null>('MoveFolder', uid, destParent),
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
