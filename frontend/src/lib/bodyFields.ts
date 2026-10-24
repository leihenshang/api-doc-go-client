// 请求体（JSON）字段表格的派生与合并算法（设计 §4）。
//
// 表格（body.fields）是请求体字段说明的**唯一真源**：手工点「生成数据表格」才派生，
// 派生后完全由用户编辑（改 5 列 / 增行 / 删行）；不再「打开即派生」，也不做表格 → JSON 的反向写回。
//
// 派生口径复用响应字段的 extractFields（同一 MAX_FIELDS/MAX_DEPTH，行为一致、避免两处漂移）：
// 路径即字段名（a.b / a[].c），类型取 JSON 类型名，示例值取叶子原始值文本（容器为空串）。
import type { BodyField } from '@/types'
import { extractFields } from '@/lib/responseFields'

/** 合并模式：新增（只追加）/ 覆盖（清空重建，仅继承同名行描述）。 */
export type BodyFieldsMergeMode = 'append' | 'overwrite'

/** 表格行匹配键：字段名 trim 后逐字完全相等。 */
function keyOf(name: string): string {
    return name.trim()
}

/**
 * 由 JSON 原文派生表格行（仅在点「生成数据表格」时调用）。
 * 描述列取 body.descs[path] 作为旧数据种子；JSON 非法 / 空串 / 含 {{变量}} 时返回空数组。
 */
export function deriveBodyFields(raw: string, descs?: Record<string, string>): BodyField[] {
    const d = descs ?? {}
    return extractFields(raw).map((f) => ({
        name: f.path,
        type: f.type,
        required: false,
        example: f.value,
        description: d[f.path] ?? '',
    }))
}

/** 新增行（用户点「新增行」）：5 列全空、非必填。 */
export function emptyFieldRow(): BodyField {
    return { name: '', type: '', required: false, example: '', description: '' }
}

/**
 * 新增（append）：保留原表全部行与顺序，只把「本次派生出来、表里还没有」的路径追加到末尾。
 * 已有行一律不动（描述、必填、类型都不改）。
 */
export function mergeAppend(cur: BodyField[], derived: BodyField[]): BodyField[] {
    const byName = new Set(cur.map((f) => keyOf(f.name)))
    const next = cur.slice()
    for (const d of derived) {
        const k = keyOf(d.name)
        if (byName.has(k)) continue
        byName.add(k)
        next.push({ ...d })
    }
    return next
}

/**
 * 覆盖（overwrite）：清空重建，行数与顺序 = 本次派生结果；
 * 只有「字段名一致」的行继承原描述，其余列（类型/示例值/必填）全部取自本次派生（required 回到 false）。
 */
export function mergeOverwrite(cur: BodyField[], derived: BodyField[]): BodyField[] {
    const byName = new Map(cur.map((f) => [keyOf(f.name), f]))
    return derived.map((d) => {
        const prev = byName.get(keyOf(d.name))
        return {
            name: d.name,
            type: d.type,
            required: false,
            example: d.example,
            description: prev?.description ?? d.description,
        }
    })
}

/** 按模式合并：新增 → append；覆盖 → overwrite。 */
export function mergeBodyFields(
    mode: BodyFieldsMergeMode,
    cur: BodyField[],
    derived: BodyField[],
): BodyField[] {
    return mode === 'overwrite' ? mergeOverwrite(cur, derived) : mergeAppend(cur, derived)
}

/** 就地更新第 i 行的部分列（返回新数组，便于 Vue 响应式与撤销语义）。 */
export function patchFieldRow(rows: BodyField[], index: number, patch: Partial<BodyField>): BodyField[] {
    if (index < 0 || index >= rows.length) return rows
    const next = rows.slice()
    next[index] = { ...next[index], ...patch }
    return next
}

/** 删除第 i 行（返回新数组）。 */
export function removeFieldRow(rows: BodyField[], index: number): BodyField[] {
    if (index < 0 || index >= rows.length) return rows
    return rows.filter((_, i) => i !== index)
}
