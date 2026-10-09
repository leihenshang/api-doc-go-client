// 从请求数据自动生成一份 Markdown 接口文档，填入请求的 Docs 区。
// 纯函数：章节标题 / 表头文案由调用方传入当前语言的实际文案（labels），本文件不依赖 i18n。
// 结构对齐约定（参考采集文档）：
//   简要描述 → 请求URI → 请求方式 → 请求头 → 请求参数 → 请求示例 → 响应字段 → 响应示例 → 备注。
// 无顶层 `#` 标题；章节以 `## 章名` 开头；请求参数为「参数名|必选|类型|说明」（必选/类型留空待手填），
// 响应字段为「参数名|类型|说明」（无必选列）；header 用 `- **名称**: 值` 列表。URL 主机地址统一替换为 {{host}}。
import type { FieldRow } from '@/lib/responseFields'
import type { RequestDoc } from '@/types'

/** 文档中需要按当前语言展示的文案（由组件用 t() 填充）。 */
export interface ApiDocLabels {
  desc: string // 简要描述
  uri: string // 请求URI
  method: string // 请求方式
  headers: string // 请求头
  params: string // 请求参数
  reqExample: string // 请求示例
  resFields: string // 响应字段
  resExample: string // 响应示例
  note: string // 备注
  colName: string // 参数名
  colType: string // 类型
  colDesc: string // 说明
  colRequired: string // 必选
}

export interface ApiDocInput {
  request: RequestDoc
  /** 请求示例：curl 命令文本（HTTP 才有意义） */
  curl: string
  /** 响应字段（按请求 uid 存档的字段表） */
  fields: FieldRow[]
  /** 已选中的响应示例 body（JSON 文本；为空则省略「响应示例」小节） */
  exampleBody: string
  labels: ApiDocLabels
}

/** Markdown 表格单元格转义：管道符与换行会破坏表格行。 */
function esc(text: string): string {
  return (text ?? '').replace(/\|/g, '\\|').replace(/\r?\n/g, ' ').trim()
}

/** 提取 URL 的 scheme://authority（协议+主机[:端口]），找不到返回空串。 */
function authorityOf(url: string): string {
  const m = url.match(/^[a-zA-Z][a-zA-Z0-9+.-]*:\/\/[^/?#]+/)
  return m ? m[0] : ''
}

/** 把 URL 主机地址替换成 {{host}} 占位（保留路径 / 查询 / 锚点）；非绝对 URL 原样返回。 */
function maskHost(url: string): string {
  const auth = authorityOf(url)
  return auth ? `{{host}}${url.slice(auth.length)}` : url
}

export function buildApiDoc(input: ApiDocInput): string {
  const { request: r, curl, fields, exampleBody, labels: L } = input
  const out: string[] = []

  // 简要描述
  const title = r.name || r.url
  if (title) out.push(`## ${L.desc}`, `- ${title}`, '')

  // 请求URI
  const rawUrl = r.grpc ? (r.grpc.target ?? '') : (r.url ?? '')
  const urlLine = maskHost(rawUrl)
  if (urlLine) out.push(`## ${L.uri}`, `- ${urlLine}`, '')

  // 请求方式
  if (r.method) out.push(`## ${L.method}`, `- ${r.method}`, '')

  // 请求头：`- **名称**: 值`，每条启用且命名的 header 一行
  const headers = r.headers.filter((h) => h.enabled && h.name.trim())
  if (headers.length) {
    out.push(`## ${L.headers}`)
    for (const h of headers) out.push(`- **${esc(h.name)}**: ${esc(h.value)}`)
    out.push('')
  }

  // 请求参数及说明（query 参数；只列启用且有名字的行）。无类型数据，「类型」列留空待手填。
  const params = r.params.filter((p) => p.enabled && p.name.trim())
  if (params.length) {
    const hdr = [L.colName, L.colRequired, L.colType, L.colDesc].join(' | ')
    out.push(`## ${L.params}`, `| ${hdr} |`, '| --- | --- | --- | --- |')
    for (const p of params) out.push(`| ${esc(p.name)} |  |  | ${esc(p.description ?? '')} |`)
    out.push('')
  }

  // 请求示例 curl（把生成命令里的主机也替换成 {{host}}）
  const curlAuth = authorityOf(rawUrl)
  const curlOut = curlAuth && curl.includes(curlAuth) ? curl.split(curlAuth).join('{{host}}') : curl
  if (curlOut.trim()) {
    out.push(`## ${L.reqExample}`, '', '```shell', curlOut.trimEnd(), '```', '')
  }

  // 响应字段（参数名=字段路径、类型=f.type、说明=f.meaning；必选列留空待手填）
  if (fields.length) {
    const hdr = [L.colName, L.colRequired, L.colType, L.colDesc].join(' | ')
    out.push(`## ${L.resFields}`, `| ${hdr} |`, '| --- | --- | --- | --- |')
    for (const f of fields) out.push(`| ${esc(f.path)} |  | ${esc(f.type)} | ${esc(f.meaning)} |`)
    out.push('')
  }

  // 响应示例
  if (exampleBody.trim()) {
    out.push(`## ${L.resExample}`, '', '```json', exampleBody.trimEnd(), '```', '')
  }

  // 备注：空模板占位，留给用户手写
  out.push(`## ${L.note}`, '')

  return out.join('\n')
}