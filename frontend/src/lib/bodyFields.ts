// 请求体（JSON）字段表格：把 body.raw 摊平成「路径 / 类型 / 值（只读）/ 备注（可编辑）」行，
// 备注真值源是 body.descs（path → note），随 share Body.Descs 落盘、随同步往返服务端与 Web。
// 展平口径复用响应字段的 extractFields（同一 MAX_FIELDS/MAX_DEPTH，行为一致、避免两处漂移）。
import { extractFields } from '@/lib/responseFields'

export interface BodyFieldRow {
  path: string
  type: string
  /** 叶子为原始值文本、容器为空串；本列只读（值由 JSON 原文决定，表格只编辑备注） */
  value: string
  /** 字段备注（取自 body.descs[path]），可编辑并写回 body.descs */
  desc: string
}

/** JSON 原文 → 表格行；按 body.descs 附上各行备注；非法/空 JSON 返回空数组（表格显示空态）。 */
export function toBodyRows(raw: string, descs?: Record<string, string>): BodyFieldRow[] {
  const d = descs ?? {}
  return extractFields(raw).map((f) => ({
    path: f.path,
    type: f.type,
    value: f.value,
    desc: d[f.path] ?? '',
  }))
}

/**
 * 写回某路径的备注，返回**新** map（不就地改，便于 Vue 响应式 + 撤销语义）。
 * 空串（trim 后）删除该项，保持 body.descs 精简，避免同步里塞满空备注。
 */
export function withBodyDesc(
  descs: Record<string, string> | undefined,
  path: string,
  note: string,
): Record<string, string> {
  const next: Record<string, string> = { ...(descs ?? {}) }
  if (note.trim() === '') delete next[path]
  else next[path] = note
  return next
}
