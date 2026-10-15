// 响应字段映射：字段表的真相源是请求模型的 RespDoc（随文件落盘、随同步往返 Web 端）；
// localStorage 仅用于未落盘草稿与旧版本存档的一次性迁移。
// 字段只在用户点「更新响应字段」时解析一次；重复更新**只追加新出现的字段**
// 并保留既有顺序与「含义」标注 —— 响应里时有时无的字段（可选字段、分页尾部字段等）不会被抹掉。
import { api } from '@/lib/ipc'
import type { KV, ResponseDoc, ResponseParam } from '@/types'

export interface FieldRow {
  path: string
  type: string
  /** 字段取值（叶子为原始值文本，容器为空串）；用于「值」列与保存为变量 */
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
 * 非法 JSON 返回空数组。
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
    out.push({ path, type, value, meaning: '' })
  }

  const walk = (v: unknown, path: string, depth: number): void => {
    if (out.length >= MAX_FIELDS || depth > MAX_DEPTH) return

    // 数组：按结构描述，不逐元素展开
    if (Array.isArray(v)) {
      if (v.length === 0) {
        push(path, 'array')
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
 * 覆盖式重建字段表（「更新响应字段」= 用本次响应体重新解析，响应里已消失的字段不再保留）。
 * inherit=true 时，仅当旧行「路径相同且取值一致」才继承已填含义 ——
 * 取值变了（同名字段新值不同）说明语义可能已变，不沿用旧说明；类型/取值一律以本次解析为准。
 */
export function rebuildFields(old: FieldRow[], text: string, inherit: boolean): { rows: FieldRow[]; changed: boolean } {
  const prev = new Map(old.map((r) => [r.path, r]))
  const rows = extractFields(text).map((f) => {
    if (!inherit) return f
    const p = prev.get(f.path)
    return p && p.value === f.value && p.meaning ? { ...f, meaning: p.meaning } : f
  })
  return { rows, changed: rows.length > 0 || old.length > 0 }
}

/** 读取本地存档；兼容早期「路径 → 含义」的对象格式。 */
export function loadFields(uid: string): FieldRow[] {
  if (!uid) return []
  let raw: unknown
  try {
    raw = JSON.parse(localStorage.getItem(storeKey(uid)) ?? '[]')
  } catch {
    return [] // 存档损坏按空处理，下一次更新重建
  }
  if (Array.isArray(raw)) {
    return raw
      .filter((x): x is Record<string, unknown> => !!x && typeof x === 'object')
      .map((x) => ({
        path: String(x.path ?? ''),
        type: String(x.type ?? ''),
        value: String(x.value ?? ''),
        meaning: String(x.meaning ?? ''),
      }))
      .filter((x) => x.path !== '')
  }
  if (raw && typeof raw === 'object') {
    return Object.entries(raw as Record<string, unknown>).map(([path, meaning]) => ({
      path,
      type: '',
      value: '',
      meaning: String(meaning ?? ''),
    }))
  }
  return []
}

export function saveFields(uid: string, rows: FieldRow[]): void {
  if (!uid) return
  localStorage.setItem(storeKey(uid), JSON.stringify(rows))
}

/** 删除本地存档（迁移进 RespDoc 后清理）。 */
export function removeFields(uid: string): void {
  if (!uid) return
  localStorage.removeItem(storeKey(uid))
}

/** RespDoc 参数行 → 字段表行（fieldName→path、example→value、description→meaning）。 */
export function rowsFromDoc(doc?: ResponseDoc | null): FieldRow[] {
  if (!doc?.params) return []
  return doc.params
    .filter((p) => !!p && !!p.fieldName)
    .map((p) => ({
      path: p.fieldName,
      type: p.type ?? '',
      value: p.example ?? '',
      meaning: p.description ?? '',
    }))
}

/** 字段表行 → RespDoc 参数行（客户端无 required 概念，恒 false；含义即 description）。 */
export function paramsFromRows(rows: FieldRow[]): ResponseParam[] {
  return rows.map((r) => ({
    fieldName: r.path,
    type: r.type,
    required: false,
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
  sample?: string
  status?: number
  headers?: KV[]
}

/**
 * 组装 ResponseDoc：字段表(params)恒由 rows 重新导出；其余四字段按「本次快照优先、缺省回退既有 doc」的
 * 统一优先级取值 —— 请求编辑器（只改字段、不动快照）与响应面板（带本次响应快照）共用同一套规则，
 * 避免两处字段集合/快照策略漂移。字符串用 `||`（空串视为缺省），status/headers 用 `??`（0 / undefined 判定）。
 */
export function buildRespDoc(rows: FieldRow[], base: ResponseDoc | null | undefined, live: RespDocLive): ResponseDoc {
  return {
    type: live.type || base?.type || '',
    params: paramsFromRows(rows),
    sample: live.sample || base?.sample || '',
    // 字段表编辑不动快照：状态码/响应头本次没有就原样带回（item 8）
    status: live.status ?? base?.status ?? 0,
    headers: live.headers ?? base?.headers ?? [],
  }
}

/** 防抖计时器句柄（调用方持有并可清理），用对象包一层以便 persistRespDoc 读写同一个引用。 */
export interface PersistTimer {
  current: ReturnType<typeof setTimeout> | null
}

/** persistRespDoc 入参：写回一条请求的 RespDoc 所需的全部上下文。 */
export interface RespDocPersistCtx {
  uid: string
  draft: boolean
  grpc: boolean
  rows: FieldRow[]
  doc: ResponseDoc
  /** true=立即写（删除/清空/「更新」按钮）；false=按 debounceMs 防抖（含义连续输入） */
  immediate: boolean
  timer: PersistTimer
  /** 写盘成功后把 doc 落回请求模型（调用方决定写到哪个 tab） */
  attach: (doc: ResponseDoc) => void
  /** 写盘失败的提示回调（保持调用方原有的 message.error 语义） */
  onError: (e: unknown) => void
  debounceMs?: number
}

/**
 * 把字段表写回请求模型的统一实现：草稿 / gRPC / 无 uid → 只存本地存档；
 * 否则 updateResponseDoc → attach → 清本地存档，按 immediate 决定立即执行还是防抖。
 * 返回是否「已发起/已成功落盘」（草稿路径返回 true）。两处调用点（请求编辑器、响应面板）共用，
 * 消除各自重复的防抖 + 落盘 + 错误处理逻辑。
 */
export async function persistRespDoc(ctx: RespDocPersistCtx): Promise<boolean> {
  if (ctx.draft || ctx.grpc || !ctx.uid) {
    saveFields(ctx.uid, ctx.rows)
    return true
  }
  const apply = async (): Promise<boolean> => {
    try {
      await api.updateResponseDoc(ctx.uid, ctx.doc)
      ctx.attach(ctx.doc)
      removeFields(ctx.uid)
      return true
    } catch (e) {
      ctx.onError(e)
      return false
    }
  }
  if (ctx.timer.current) clearTimeout(ctx.timer.current)
  if (ctx.immediate) return apply()
  ctx.timer.current = setTimeout(() => void apply(), ctx.debounceMs ?? 600)
  return true
}
