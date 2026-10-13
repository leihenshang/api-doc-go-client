import { expect, test } from '../helpers/app'
import { openCollection } from '../helpers/ui'
import { readFileSync, existsSync } from 'node:fs'
import path from 'node:path'

/**
 * T2 交互授权 + T3 审计：经真实 streamable HTTP MCP 调用，驱动浏览器弹窗决策。
 *
 * 链路：Node 侧最小 JSON-RPC 客户端 → 内嵌 MCP（devserver 反射桥启用）
 *       → guard 发 mcp:authz-request → devserver SSE → 页面弹窗 → AnswerMCPAuthz
 *       → 工具结果；审计落 audit.log.jsonl 并可经门面查询。
 */

interface McpResult {
  isError?: boolean
  content?: { type: string; text?: string }[]
}

test('[T2/T3][@smoke] MCP 风险工具弹窗授权（拒绝/15 分钟）+ 审计记录', async ({ page, app }) => {
  const dir = await app.newCollection('basic')
  await openCollection(page, app)

  // ① 通过反射桥启用内嵌 MCP：随机端口、关掉只读、白名单可写授权当前集合
  const port = 12000 + Math.floor(Math.random() * 30000)
  const settings = await app.ipc<{ mcp: Record<string, unknown> }>('GetSettings')
  settings.mcp = {
    ...(settings.mcp as object),
    enabled: true,
    addr: '127.0.0.1',
    port,
    readOnly: false,
    allowOrigins: [],
    allow: [{ path: dir, writable: true }],
  }
  await app.ipc('SaveSettings', settings)
  const statusP = await app.ipc<{ running: boolean; token: string }>('ApplyMCPSettings')
  expect(statusP.running).toBe(true)
  const token = statusP.token
  expect(token).toBeTruthy()

  const base = `http://127.0.0.1:${port}/mcp`
  let sessionId = ''

  async function rpc(method: string, params?: unknown, waitForResponse = true): Promise<unknown> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      Accept: 'application/json, text/event-stream',
    }
    if (token) headers.Authorization = `Bearer ${token}`
    if (sessionId) headers['Mcp-Session-Id'] = sessionId
    const res = await fetch(base, {
      method: 'POST',
      headers,
      body: JSON.stringify({ jsonrpc: '2.0', id: 1, method, params: params ?? {} }),
    })
    const sid = res.headers.get('mcp-session-id')
    if (sid) sessionId = sid
    if (!waitForResponse) return null
    const text = await res.text()
    // streamable HTTP 可能直接回 JSON，也可能回 text/event-stream（data: 帧）
    if (res.headers.get('content-type')?.includes('text/event-stream')) {
      const line = text.split('\n').find((l) => l.startsWith('data:'))
      if (!line) throw new Error(`SSE 响应无 data 帧: ${text}`)
      return JSON.parse(line.slice(5).trim())
    }
    return JSON.parse(text)
  }

  // initialize + initialized 通知
  const init = (await rpc('initialize')) as { result?: { serverInfo?: { name?: string } } }
  expect(init.result?.serverInfo?.name).toBe('api-doc-go')
  await rpc('notifications/initialized', {}, false)

  // ② 高危工具调用 → 弹窗出现
  const deniedCall = rpc('tools/call', {
    name: 'delete_request',
    arguments: { project: 'basic', uid: 'will-be-denied' },
  })
  const dialog = page.getByTestId('mcp.consent.dialog')
  await expect(dialog).toBeVisible({ timeout: 10_000 })
  await expect(page.getByTestId('mcp.consent.tool')).toHaveText('delete_request')
  await page.getByTestId('mcp.consent.deny').click()
  const denied = (await deniedCall) as { result?: McpResult; error?: { message?: string } }
  expect(denied.result?.isError).toBe(true)
  expect(denied.result?.content?.[0]?.text ?? '').toContain('拒绝')
  await expect(dialog).toBeHidden({ timeout: 5_000 })

  // ③ 第二次选「允许 15 分钟」：业务执行（uid 不存在 → 业务错误，但已放行，非拒绝）
  const allowedCall = rpc('tools/call', {
    name: 'delete_request',
    arguments: { project: 'basic', uid: 'no-such-uid-after-allow' },
  })
  await expect(dialog).toBeVisible({ timeout: 10_000 })
  await page.getByTestId('mcp.consent.allow15m').click()
  const allowed = (await allowedCall) as { result?: McpResult }
  expect(allowed.result?.isError).toBe(true)
  const allowedText = allowed.result?.content?.[0]?.text ?? ''
  expect(allowedText).toContain('失败')
  expect(allowedText).not.toContain('拒绝')
  await expect(dialog).toBeHidden({ timeout: 5_000 })

  // ④ 只读工具不弹窗
  const low = (await rpc('tools/call', { name: 'list_workspaces', arguments: {} })) as {
    result?: McpResult
  }
  expect(low.result?.isError).toBeFalsy()
  await expect(dialog).toBeHidden()

  // ⑤ 审计：门面查询覆盖 denied / biz_error(放行) / ok 三出口
  const audit = await app.ipc<{ total: number; events: { tool: string; resultKind: string }[] }>(
    'ListMCPAudit',
    { limit: 50 },
  )
  const kinds = new Map(audit.events.map((e) => [`${e.tool}:${e.resultKind}`, true]))
  expect(kinds.get('delete_request:denied')).toBe(true)
  expect(kinds.get('delete_request:biz_error')).toBe(true)
  expect(kinds.get('list_workspaces:ok')).toBe(true)

  // 审计文件确实落盘（0600 由 Go 侧保证；这里只验证存在且为 jsonl）
  const auditFile = path.join(app.configDir, 'audit.log.jsonl')
  expect(existsSync(auditFile)).toBe(true)
  const lines = readFileSync(auditFile, 'utf8')
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean)
  expect(lines.length).toBeGreaterThanOrEqual(3)
  for (const line of lines) {
    const ev = JSON.parse(line) as { tool?: string }
    expect(typeof ev.tool).toBe('string')
  }

  // ⑥ 清空审计：归档后门面查询为空
  await app.ipc('ClearMCPAudit')
  const after = await app.ipc<{ total: number }>('ListMCPAudit', { limit: 50 })
  expect(after.total).toBe(0)

  // 关闭 MCP，释放端口
  settings.mcp = { ...(settings.mcp as object), enabled: false }
  await app.ipc('SaveSettings', settings)
  await app.ipc('ApplyMCPSettings')
})
