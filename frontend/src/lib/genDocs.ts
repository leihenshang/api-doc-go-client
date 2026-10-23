// 从请求数据自动生成一份 Markdown 接口文档，填入请求的 Docs 区。
// 纯函数：章节标题 / 表头文案由调用方传入当前语言的实际文案（labels），本文件不依赖 i18n。
// 结构对齐约定（参考采集文档）：
//   简要描述 → 请求URI → 请求方式 → 请求头 → 请求参数 → 请求体 → 请求示例 → 响应字段 → 响应示例 → 备注。
// 无顶层 `#` 标题；章节以 `## 章名` 开头；请求参数/响应字段均为「参数名|必选|类型|说明」，
// 必选默认填「是」，类型留空时默认 string。header 用 `- **名称**: 值` 列表。URL 主机地址统一替换为 {{host}}，
// 且「请求URI」只保留路径（查询串交由「请求参数」表体现）。
import { toBodyRows } from '@/lib/bodyFields'
import type { FieldRow } from '@/lib/responseFields'
import type { RequestDoc } from '@/types'

/** 文档中需要按当前语言展示的文案（由组件用 t() 填充）。 */
export interface ApiDocLabels {
  desc: string // 简要描述
  uri: string // 请求URI
  method: string // 请求方式
  headers: string // 请求头
  params: string // 请求参数
  body: string // 请求体
  bodyParams: string // 请求体字段（表小标题）
  reqExample: string // 请求示例
  resFields: string // 响应字段
  resExample: string // 响应示例
  note: string // 备注
  colName: string // 参数名
  colValue: string // 值（请求体表单体列）
  colType: string // 类型
  colExample: string // 示例值（响应字段表体列）
  colDesc: string // 说明
  colRequired: string // 必选
  requiredYes: string // 勾选参数（是 / Yes）
  requiredNo: string // 未勾选参数（否 / No）
}

/** 类型列留空时的默认值（JSON 类型名，不随语言变化）。 */
const DEFAULT_TYPE = 'string'

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

/** 只保留 URL 的路径部分：去掉查询串与锚点（查询参数交由「请求参数」表体现）。 */
function urlPathOnly(url: string): string {
  return url.replace(/[?#].*$/, '')
}

/** 把 JSON 原文缩进 2 格美化；解析失败（含 {{变量}} 占位）原样返回。 */
function prettyJson(text: string): string {
  try {
    return JSON.stringify(JSON.parse(text), null, 2)
  } catch {
    return text.trim()
  }
}

export function buildApiDoc(input: ApiDocInput): string {
  const { request: r, curl, fields, exampleBody, labels: L } = input
  const out: string[] = []

  // 简要描述
  const title = r.name || r.url
  if (title) out.push(`## ${L.desc}`, `- ${title}`, '')

  // 请求URI（只留路径，查询参数由下方「请求参数」表体现）
  const rawUrl = r.grpc ? (r.grpc.target ?? '') : (r.url ?? '')
  const urlLine = maskHost(urlPathOnly(rawUrl))
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

  // 请求参数及说明（query 参数）：导出文档保留全部有名字的行；
  // 勾选状态只表示「必选」，取消勾选不能让参数从文档里消失。类型默认 string。
  const params = r.params.filter((p) => p.name.trim())
  if (params.length) {
    const hdr = [L.colName, L.colRequired, L.colType, L.colDesc].join(' | ')
    out.push(`## ${L.params}`, `| ${hdr} |`, '| --- | --- | --- | --- |')
    for (const p of params)
      out.push(
        `| ${esc(p.name)} | ${p.enabled ? L.requiredYes : L.requiredNo} | ${DEFAULT_TYPE} | ${esc(p.description ?? '')} |`,
      )
    out.push('')
  }

  // 请求体：与查询参数**并存互不影响**（同章节内参数表在前、请求体在后）。
  // json 等原始体：先附「请求体字段」表（口径同编辑器表格视图：路径/类型/备注 descs），
  // 再给原文代码块；form/multipart 字段另起一张表，与 B14 导出语义一致。
  const body = r.body
  const bodyRaw = (body?.raw ?? '').trim()
  const bodyForm = (body?.form ?? []).filter((f) => f.enabled && f.name.trim())
  if (body && body.type !== 'none' && (bodyRaw || bodyForm.length)) {
    out.push(`## ${L.body}`, `- ${body.type}`)
    const bodyFields = body.type === 'json' ? toBodyRows(body.raw, body.descs) : []
    if (bodyFields.length) {
      const hdr = [L.colName, L.colRequired, L.colType, L.colDesc].join(' | ')
      out.push('', `**${L.bodyParams}**`, '', `| ${hdr} |`, '| --- | --- | --- | --- |')
      for (const f of bodyFields)
        out.push(`| ${esc(f.path)} | ${L.requiredYes} | ${esc(f.type) || DEFAULT_TYPE} | ${esc(f.desc)} |`)
    }
    if (bodyRaw) {
      const lang = body.type === 'json' ? 'json' : ''
      out.push('', '```' + lang, bodyRaw, '```')
    }
    if (bodyForm.length) {
      const hdr = [L.colName, L.colValue, L.colDesc].join(' | ')
      out.push('', `| ${hdr} |`, '| --- | --- | --- |')
      for (const f of bodyForm) out.push(`| ${esc(f.name)} | ${esc(f.value)} | ${esc(f.description ?? '')} |`)
    }
    out.push('')
  }

  // 请求示例 curl（把生成命令里的主机也替换成 {{host}}）
  const curlAuth = authorityOf(rawUrl)
  const curlOut = curlAuth && curl.includes(curlAuth) ? curl.split(curlAuth).join('{{host}}') : curl
  // JSON 体的「请求数据示例」：缩进美化后的原文（含变量占位则原样），与 curl 同节展示
  const reqBodyJson = body && body.type === 'json' && bodyRaw ? prettyJson(bodyRaw) : ''
  if (curlOut.trim() || reqBodyJson) {
    out.push(`## ${L.reqExample}`, '')
    if (curlOut.trim()) out.push('```shell', curlOut.trimEnd(), '```', '')
    if (reqBodyJson) out.push('```json', reqBodyJson, '```', '')
  }

  // 响应字段（参数名=字段路径、类型=f.type（空则 string）、示例值=f.value、说明=f.meaning；必选默认「是」）
  if (fields.length) {
    const hdr = [L.colName, L.colRequired, L.colType, L.colExample, L.colDesc].join(' | ')
    out.push(`## ${L.resFields}`, `| ${hdr} |`, '| --- | --- | --- | --- | --- |')
    for (const f of fields)
      out.push(
        `| ${esc(f.path)} | ${L.requiredYes} | ${esc(f.type) || DEFAULT_TYPE} | ${esc(f.value)} | ${esc(f.meaning)} |`,
      )
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