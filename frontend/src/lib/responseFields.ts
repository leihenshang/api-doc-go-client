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

  const push = (path: string, type: string): void => {
    if (seen.has(path)) return
    seen.add(path)
    out.push({ path, type, meaning: '' })
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
    if (path) push(path, typeName(v))
  }

  walk(data, '', 0)
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
