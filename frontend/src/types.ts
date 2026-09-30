// 与 Go 侧 `internal/collection`、`internal/runner`、`internal/config` 的 JSON 契约对齐（手工维护）。

export interface KV {
  name: string
  value: string
  enabled: boolean
  /** 参数说明（对齐设计稿参数表 Description 列；Go 侧同名字段保证落盘 round-trip） */
  description?: string
  /** multipart 表单行类型：text（默认）| file（value 为文件路径） */
  type?: string
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
  /** vars.pre-request（发送前赋值，value 支持 {{变量}}） */
  varsPreRequest?: ScriptVar[] | null
  /** script.pre-request / post-response（goja） */
  script?: ScriptBlock | null
  /** assert 列表（post-response 后求值） */
  asserts?: ScriptAssert[] | null
}

/** vars.pre-request 的一行。 */
export interface ScriptVar {
  name: string
  value: string
  enabled: boolean
}

/** script 段。 */
export interface ScriptBlock {
  preRequest?: string
  postResponse?: string
}

/** assert 的一条。 */
export interface ScriptAssert {
  name?: string
  expr: string
}

/** 脚本/断言阶段产物（runner.Result.script）。 */
export interface ScriptResult {
  vars?: Record<string, string>
  asserts?: AssertResult[]
  scriptError?: string
}

/** 一条断言结果。 */
export interface AssertResult {
  name: string
  expr: string
  passed: boolean
  error?: string
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
  /** 脚本/断言产物（无脚本时缺省） */
  script?: ScriptResult | null
}

/** 保存的响应示例里的请求快照（internal/collection.ExampleRequest）。 */
export interface ExampleRequest {
  method: string
  url: string
  headers: KV[]
  body: ReqBody
}

/** 保存的响应示例里的响应快照（internal/collection.ExampleResponse）。 */
export interface ExampleResponse {
  status: number
  proto: string
  timeMs: number
  size: number
  contentType: string
  /** true 时 body 为 base64 */
  binary: boolean
  headers: KV[]
  body: string
}

/** 一个保存的响应示例（internal/collection.ResponseExample，落集合 examples/）。 */
export interface ResponseExample {
  uid: string
  name: string
  requestUid: string
  /** 相对集合根的文件路径 */
  path: string
  /** 同一请求内的保存序号（新 → 旧排序依据） */
  seq: number
  /** Unix 毫秒 */
  createdAt: number
  request: ExampleRequest
  response: ExampleResponse
}

export interface ResolveResult {
  text: string
  missing: string[]
  /** 文本里引用到、且已定义的变量取值（名字 → 值）；未定义的不出现 */
  values: Record<string, string>
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
  /** 主题：light | dark（原生窗口底色也跟随该值） */
  theme: 'light' | 'dark'
  /** HTTP(S) 代理；空 = 直连 */
  proxyUrl: string
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
  /** 发送时请求快照（原样重放用；旧记录可缺） */
  request?: RequestDoc | null
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

/** 本地索引节点（internal/index.Node）。 */
export interface IndexNode {
  uid: string
  type: 'folder' | 'request'
  path: string
  title: string
  method?: string
  url?: string
  mtime: number
  hash?: string
}

/** 一次导入的结果（collection.ImportSummary）。 */
export interface ImportSummary {
  imported: number
  skipped: number
  failures?: string[]
}

/** 本地 Mock 运行状态（mocksrv.Status）。 */
export interface MockStatus {
  running: boolean
  port: number
  url: string
  hits: number
}

/** 同步绑定（.sync.json，不含 PAT）。 */
export interface SyncBindInfo {
  linked: boolean
  serverUrl: string
  projectId: number
  mode: string
  cursor: number
}

/** 一轮同步摘要。 */
export interface SyncReport {
  pulled: number
  pushed: number
  conflicts: number
  rejected: number
  errors?: string[]
  cursor: number
  gap: boolean
}

/** 一份冲突副本。 */
export interface ConflictItem {
  file: string
  ofUid: string
  name: string
  serverRev: number
  createdAt: number
}

/** 文档条目（B13）。 */
export interface DocEntry {
  uid: string
  name: string
  path: string
  content: string
  icon?: string
}

/** 状态栏同步状态。 */
export interface SyncStatus {
  linked: boolean
  mode: string
  running: boolean
  lastSyncAt: number
  lastError: string
  dirtyCount: number
  conflicts: number
  cursor: number
  lastReport?: SyncReport | null
}
