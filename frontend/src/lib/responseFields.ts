// 响应字段表：字段表的真相源是请求模型的 RespDoc（随文件落盘、随同步往返 Web 端）；
// localStorage 仅用于未落盘草稿与旧版本存档的一次性迁移。
// 与请求体表格同构：表格**只在用户点「生成数据表格」/「从本次响应生成」时派生**，
// 派生后完全由用户编辑（5 列：字段名 / 类型 / 必填 / 示例值 / 说明 + 增行 / 删行）；
// 编辑「响应数据」（respDoc.sample）**不会**再联动重算表格。
import type { KV, ResponseDoc, ResponseParam } from '@/types'

/** 字段表的一行（5 列与请求体表格同构）。 */
export interface FieldRow {
  path: string
  type: string
  /** 必填：派生行与手工新增行默认 true（「是」），可由用户取消勾选 */
  required: boolean
  /** 字段取值（叶子为原始值文本，容器为空串）；用于「示例值」列 */
  value: string
  /** 字段含义（写回 RespDoc.params[].description，随同步与 Web 端往返） */
  meaning: string
}

const MAX_FIELDS = 200
const MAX_DEPTH = 4

const storeKey = (uid: string): string => `client.fieldmap.${uid}`

function typeName(v: unknown): string {
  if (v === null) return 'null'
  if (Array.isArray(v)) return 'array'
  return typeof v
}

/**
 * 把 JSON 文本摊平成字段说明（按结构，不按数组元素数量）：
 * - 对象展开子字段
 * - 数组用 `[]` 占位，只展开**首个元素**的字段作为结构描述
 * - 叶子占一行
 * 非法 JSON 返回空数组。派生行的必填一律为「是」（required=true）。
 */
export function extractFields(text: string): FieldRow[] {
  let data: unknown
  try {
    data = JSON.parse(text)
  } catch {
    return []
  }
  const out: FieldRow[] = []
  const seen = new Set<string>()

  const push = (path: string, type: string, value = ''): void => {
    if (seen.has(path)) return
    seen.add(path)
    out.push({ path, type, required: true, value, meaning: '' })
  }

  const walk = (v: unknown, path: string, depth: number): void => {
    if (out.length >= MAX_FIELDS || depth > MAX_DEPTH) return

    // 数组：按结构描述，不逐元素展开
    if (Array.isArray(v)) {
      // 空数组只在有路径时占一行（根数组 [] 不产生空路径行）
      if (v.length === 0) {
        if (path) push(path, 'array')
        return
      }
      push(path, 'array')
      const elemPath = `${path}[]`
      // 首元素作为结构样例（或数组里第一个非空对象的键集）
      const sample = v.find((x) => x !== null && x !== undefined)
      walk(sample, elemPath, depth + 1)
      return
    }

    // 对象：展开子字段
    if (v !== null && typeof v === 'object') {
      const entries = Object.entries(v as Record<string, unknown>)
      if (entries.length === 0) {
        if (path) push(path, 'object')
        return
      }
      for (const [k, child] of entries) {
        walk(child, path ? `${path}.${k}` : k, depth + 1)
      }
      return
    }

    // 叶子
    if (path) push(path, typeName(v), String(v))
  }

  walk(data, '', 0)
  return out
}

/**
 * 由 JSON 原文派生字段表行（仅在用户点「生成数据表格」/「从本次响应生成」时调用）。
 * 空串 / 非法 JSON → 空数组（调用方据此提示「无法生成」）。
 * 派生行的必填一律为「是」（required=true），含义留空（由用户在表格里填写）。
 */
export function deriveFields(text: string): FieldRow[] {
  if (text.trim() === '') return []
  try {
    JSON.parse(text)
  } catch {
    return []
  }
  return extractFields(text).map((f) => ({ ...f, required: true, meaning: '' }))
}

/** 表格行匹配键：字段路径 trim 后逐字完全相等。 */
function keyOf(path: string): string {
  return path.trim()
}

/**
 * 新增（append）：保留原表全部行与顺序，只把「本次派生出来、表里还没有的路径」追加到末尾。
 * 已有行一律不动（含义、必填、类型、示例值都不改）；新增行 required=true。
 */
export function mergeAppend(cur: FieldRow[], derived: FieldRow[]): FieldRow[] {
  const seen = new Set(cur.map((r) => keyOf(r.path)))
  const next = cur.slice()
  for (const d of derived) {
    const k = keyOf(d.path)
    if (seen.has(k)) continue
    seen.add(k)
    next.push({ ...d, required: true })
  }
  return next
}

/**
 * 覆盖（overwrite）：行数与顺序 = 本次派生结果；路径相同的旧行**只继承含义**，
 * 其余列（类型 / 示例值 / 必填）全部取自本次派生，必填回到「是」（required=true）。
 */
export function mergeOverwrite(cur: FieldRow[], derived: FieldRow[]): FieldRow[] {
  const prev = new Map(cur.map((r) => [keyOf(r.path), r]))
  return derived.map((d) => {
    const old = prev.get(keyOf(d.path))
    return {
      path: d.path,
      type: d.type,
      required: true,
      value: d.value,
      meaning: old?.meaning ?? '',
    }
  })
}

/** 手工「新增行」：5 列空、必填默认「是」。 */
export function emptyFieldRow(): FieldRow {
  return { path: '', type: '', required: true, value: '', meaning: '' }
}

/** 读取本地存档；兼容早期「路径 → 含义」的对象格式（旧数据没有 required，按「是」处理）。 */
export function loadFields(uid: string): FieldRow[] {
  if (!uid) return []
  let raw: unknown
  try {
    raw = JSON.parse(localStorage.getItem(storeKey(uid)) ?? '[]')
  } catch {
    return [] // 存档损坏按空处理，下一次生成重建
  }
  if (Array.isArray(raw)) {
    return raw
      .filter((x): x is Record<string, unknown> => !!x && typeof x === 'object')
      .map((x) => ({
        path: String(x.path ?? ''),
        type: String(x.type ?? ''),
        required: x.required !== false,
        value: String(x.value ?? ''),
        meaning: String(x.meaning ?? ''),
      }))
      .filter((x) => x.path !== '')
  }
  if (raw && typeof raw === 'object') {
    return Object.entries(raw as Record<string, unknown>).map(([path, meaning]) => ({
      path,
      type: '',
      required: true,
      value: '',
      meaning: String(meaning ?? ''),
    }))
  }
  return []
}

/** RespDoc 参数行 → 字段表行（fieldName→path、example→value、description→meaning，required 如实读取）。 */
export function rowsFromDoc(doc?: ResponseDoc | null): FieldRow[] {
  if (!doc?.params) return []
  return doc.params
    .filter((p) => !!p && !!p.fieldName)
    .map((p) => ({
      path: p.fieldName,
      type: p.type ?? '',
      required: p.required === true,
      value: p.example ?? '',
      meaning: p.description ?? '',
    }))
}

/** 字段表行 → RespDoc 参数行（required 如实写回；含义即 description）。
 * 手动录入但未填路径的空行不落盘（fieldName 为空无意义），避免给 respDoc 写入空字段名。 */
export function paramsFromRows(rows: FieldRow[]): ResponseParam[] {
  return rows
    .filter((r) => r.path.trim() !== '')
    .map((r) => ({
      fieldName: r.path,
      type: r.type,
      required: r.required === true,
      description: r.meaning,
      example: r.value,
    }))
}

/** 根据响应体文本推断根 JSON 类型（{→object、[→array，其余空串）。 */
export function rootJsonType(text: string): string {
  const s = text.trim()
  if (s.startsWith('{')) return 'object'
  if (s.startsWith('[')) return 'array'
  return ''
}

/** buildRespDoc 用到的「本次快照」：各字段可为 undefined，表示本次没有该信息、应回退既有 respDoc。 */
export interface RespDocLive {
  type?: string
  /** 响应数据原文：传空串是**真实值**（用户清空了数据），只有 undefined 才回退旧值 */
  sample?: string
  /** 响应内容类型（json/text/xml/...）；不传则沿用既有 doc 的值（避免编辑时被重置） */
  contentType?: string
  status?: number
  headers?: KV[]
}

/**
 * 组装 ResponseDoc：字段表(params)恒由 rows 重新导出；其余字段按「本次快照优先、缺省回退既有 doc」的
 * 统一优先级取值 —— 请求编辑器（只改字段/数据）与响应面板（带本次响应快照）共用同一套规则。
 * sample 用 nullish 语义：空串是用户真实清空的数据，不能用 `||` 回落成旧值（否则清空后旧 body 又复活）。
 * status/headers 同理（0 / undefined 判定）。
 */
export function buildRespDoc(rows: FieldRow[], base: ResponseDoc | null | undefined, live: RespDocLive): ResponseDoc {
  // 用户显式清空响应数据（sample=""）时，type 也一并清空：
  // Go 侧 UpdateResponseDoc 在 type/params/sample 全空时会把整段 respdoc 移除，不留空壳。
  const cleared = live.sample !== undefined && live.sample.trim() === ''
  return {
    type: cleared ? '' : live.type || base?.type || '',
    params: paramsFromRows(rows),
    sample: live.sample !== undefined ? live.sample : base?.sample || '',
    // 内容类型不随字段/数据编辑丢失：未显式传时继承既有 doc
    contentType: live.contentType !== undefined ? live.contentType : base?.contentType || '',
    // 字段表编辑不动快照：状态码/响应头本次没有就原样带回（item 8）
    status: live.status ?? base?.status ?? 0,
    headers: live.headers ?? base?.headers ?? [],
  }
}

/**
 * 易变 / 敏感响应头黑名单（小写，按名字大小写不敏感剔除）。
 * live 生成时只记录「能表达接口业务语义」的头；下面这些要么每次请求都不同
 * （Date / Content-Length / Keep-Alive / 连接与分块相关），要么是凭据
 * （Set-Cookie / Proxy-Authorization），写进集合 YAML 只会让文件每次抖动或泄密。
 */
const VOLATILE_RESP_HEADERS = new Set([
  'date',
  'connection',
  'keep-alive',
  'transfer-encoding',
  'te',
  'trailer',
  'upgrade',
  'proxy-authenticate',
  'proxy-authorization',
  'server',
  'content-length',
  'set-cookie',
])

/**
 * 过滤易变响应头（大小写不敏感），保留自定义头（如 X-Total-Count 常含业务语义）。
 * 返回新数组，不修改入参。
 */
export function stableRespHeaders(headers: KV[]): KV[] {
  return headers.filter((h) => !VOLATILE_RESP_HEADERS.has((h.name ?? '').trim().toLowerCase()))
}
