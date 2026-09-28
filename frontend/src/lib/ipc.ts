// 与 Go 侧通信的唯一入口：
//   - Wails 运行时：window.go.app.App.<Method>(args…)（bindings 注入）
//   - 浏览器开发：POST /api/App/<Method>（vite 代理到 cmd/devserver）
// 两态走同一套 App 方法，前端无感知。
import type {
  CollectionInfo,
  CookieInfo,
  Env,
  HistoryEntry,
  RequestDoc,
  ResolveResult,
  SendResult,
  Settings,
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
  return { ...res, missing: res.missing ?? [] }
}

export const api = {
  pickDirectory: () => call<string>('PickDirectory'),
  openCollection: (dir: string) => call<CollectionInfo>('OpenCollection', dir).then(normalizeInfo),
  reload: () => call<CollectionInfo>('ReloadCollection').then(normalizeInfo),
  createRequest: (folder: string, name: string, method: string) =>
    call<RequestDoc>('CreateRequest', folder, name, method).then(normalizeRequest),
  createFolder: (parent: string, name: string) => call<null>('CreateFolder', parent, name),
  renameFolder: (uid: string, name: string) => call<null>('RenameFolder', uid, name),
  deleteFolder: (uid: string) => call<null>('DeleteFolder', uid),
  readRequest: (uid: string) => call<RequestDoc>('ReadRequest', uid).then(normalizeRequest),
  saveRequest: (r: RequestDoc) => call<null>('SaveRequest', r),
  renameRequest: (uid: string, name: string) => call<null>('RenameRequest', uid, name),
  deleteRequest: (uid: string) => call<null>('DeleteRequest', uid),
  saveEnv: (env: Env) => call<null>('SaveEnv', env),
  deleteEnv: (name: string) => call<null>('DeleteEnv', name),
  send: (r: RequestDoc, envName: string) => call<SendResult>('SendRequest', r, envName).then(normalizeSend),
  resolveText: (text: string, envName: string) =>
    call<ResolveResult>('ResolveText', text, envName).then(normalizeResolve),
  getSettings: () => call<Settings>('GetSettings'),
  saveSettings: (s: Settings) => call<null>('SaveSettings', s),
  listHistory: (limit: number) => call<HistoryEntry[] | null>('ListHistory', limit).then((v) => v ?? []),
  clearHistory: () => call<null>('ClearHistory'),
  listCookies: () => call<CookieInfo[] | null>('ListCookies').then((v) => v ?? []),
  clearCookies: () => call<null>('ClearCookies'),
}
