// 响应字段映射：字段表的真相源是请求模型的 RespDoc（随文件落盘、随同步往返 Web 端）；
// localStorage 仅用于未落盘草稿与旧版本存档的一次性迁移。
// 字段只在用户点「更新响应字段」时解析一次；重复更新**只追加新出现的字段**
// 并保留既有顺序与「含义」标注 —— 响应里时有时无的字段（可选字段、分页尾部字段等）不会被抹掉。
import type { ResponseDoc, ResponseParam } from '@/types'

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
