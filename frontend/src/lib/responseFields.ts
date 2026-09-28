// 响应字段映射的本地存档（按请求 uid 存 localStorage）。
// 与旧实现的关键差异：字段只在用户点「更新响应字段」时解析一次；重复更新**只追加新出现的字段**
// 并保留既有顺序与「含义」标注 —— 响应里时有时无的字段（可选字段、分页尾部字段等）不会被抹掉。
export interface FieldRow {
  path: string
  type: string
  /** 本地标注的含义，不写回集合文件 */
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

function childEntries(v: unknown): [string, unknown][] {
  if (Array.isArray(v)) return v.map((x, i) => [String(i), x] as [string, unknown])
  if (v && typeof v === 'object') return Object.entries(v as Record<string, unknown>)
  return []
}

/** 把 JSON 文本摊平成叶子字段（容器不占行）；非法 JSON 返回空数组。 */
export function extractFields(text: string): FieldRow[] {
  let data: unknown
  try {
    data = JSON.parse(text)
  } catch {
    return []
  }
  const out: FieldRow[] = []
  const walk = (v: unknown, path: string, depth: number): void => {
    if (out.length >= MAX_FIELDS) return
    const kids = depth >= MAX_DEPTH ? [] : childEntries(v)
    if (kids.length === 0) {
      out.push({ path, type: typeName(v), meaning: '' })
      return
    }
    for (const [k, child] of kids) walk(child, path ? `${path}.${k}` : k, depth + 1)
  }
  const root = childEntries(data)
  if (root.length === 0) return [{ path: '$', type: typeName(data), meaning: '' }]
  for (const [k, child] of root) walk(child, k, 1)
  return out
}

/** 合并一次解析结果：已存在的字段只刷新类型（保留含义），新字段追加在末尾。 */
export function mergeFields(rows: FieldRow[], text: string): { rows: FieldRow[]; added: number } {
  const merged = rows.map((r) => ({ ...r }))
  const index = new Map(merged.map((r) => [r.path, r]))
  let added = 0
  for (const field of extractFields(text)) {
    const exist = index.get(field.path)
    if (exist) {
      exist.type = field.type
      continue
    }
    index.set(field.path, field)
    merged.push(field)
    added += 1
  }
  return { rows: merged, added }
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
      .map((x) => ({ path: String(x.path ?? ''), type: String(x.type ?? ''), meaning: String(x.meaning ?? '') }))
      .filter((x) => x.path !== '')
  }
  if (raw && typeof raw === 'object') {
    return Object.entries(raw as Record<string, unknown>).map(([path, meaning]) => ({
      path,
      type: '',
      meaning: String(meaning ?? ''),
    }))
  }
  return []
}

export function saveFields(uid: string, rows: FieldRow[]): void {
  if (!uid) return
  localStorage.setItem(storeKey(uid), JSON.stringify(rows))
}
