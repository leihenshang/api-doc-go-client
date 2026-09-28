// 与 Go 侧 `internal/collection`、`internal/runner`、`internal/config` 的 JSON 契约对齐（手工维护）。

export interface KV {
  name: string
  value: string
  enabled: boolean
  /** 参数说明（对齐设计稿参数表 Description 列；Go 侧同名字段保证落盘 round-trip） */
  description?: string
}

export type BodyType = 'none' | 'json' | 'text' | 'form' | 'multipart'

export interface ReqBody {
  type: BodyType
  raw: string
  form: KV[]
}

export type AuthType = 'none' | 'basic' | 'bearer' | 'apikey'

export interface Auth {
  type: AuthType | string
  username?: string
  password?: string
  token?: string
  key?: string
  value?: string
  in?: 'header' | 'query'
}

// 请求级覆盖项：未设置时沿用全局设置
export interface RequestSettings {
  timeoutSec?: number
  followRedirects?: boolean
  maxRedirects?: number
  insecureSsl?: boolean
  encodeUrl?: boolean
}

export interface RequestDoc {
  uid: string
  name: string
  seq: number
  path: string
  method: string
  url: string
  params: KV[]
  headers: KV[]
  body: ReqBody
  auth?: Auth | null
  settings?: RequestSettings | null
  docs: string
  baseRev: number
}

export interface TreeNode {
  type: 'folder' | 'request'
  uid: string
  name: string
  path: string
  method?: string
  children?: TreeNode[]
}

export interface EnvVar {
  name: string
  value: string
  enabled: boolean
  secret: boolean
}

export interface Env {
  name: string
  vars: EnvVar[]
}

export interface CollectionInfo {
  dir: string
  name: string
  uid: string
  tree: TreeNode[]
  envs: Env[]
}

export interface SendResult {
  url: string
  status: number
  proto: string
  timeMs: number
  size: number
  contentType: string
  /** true 时 body 为 base64 编码 */
  binary: boolean
  headers: KV[]
  body: string
}

export interface ResolveResult {
  text: string
  missing: string[]
}

/** 全局设置（internal/config.Settings）。 */
export interface Settings {
  insecureSsl: boolean
  timeoutSec: number
  followRedirects: boolean
  maxRedirects: number
  persistCookies: boolean
  historyLimit: number
  /** 界面缩放倍率（1 = 100%） */
  uiScale: number
  /** 响应区位置 */
  responseLayout: 'right' | 'bottom'
  /** 响应区占比（%）：right 为宽度、bottom 为高度，可拖动调整（20–80） */
  responseSize: number
}

/** 一条发送历史（internal/history.Entry）。 */
export interface HistoryEntry {
  time: number
  uid: string
  name: string
  method: string
  url: string
  status: number
  timeMs: number
  size: number
  error?: string
}

/** Cookie 罐条目（internal/cookiejar.Info）。 */
export interface CookieInfo {
  name: string
  value: string
  domain: string
  path: string
  /** Unix 毫秒；0 表示会话 Cookie */
  expires: number
}
