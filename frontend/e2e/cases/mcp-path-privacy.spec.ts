import { expect, test } from '../helpers/app'
import { openMcpSettings, openCollection, saveSettings } from '../helpers/ui'

/**
 * MCP 不暴露真实路径（端到端）：
 *
 * 链路：Node 侧最小 JSON-RPC 客户端 → 真实内嵌 MCP（devserver 反射桥启用）
 *       → list_workspaces / use_workspace / get_project_modules。
 *
 * 断言的是**外部调用方实际拿到的东西**：路径一旦出现在工具输出里，
 * AI 就能绕开集合直接读写文件，这是客户端唯一能拦的一层。
 */

interface McpResult {
  isError?: boolean
  content?: { type: string; text?: string }[]
}

test('[I7] MCP 工具输出不含真实路径：工作目录只给 ws_1 别名', async ({ page, app }) => {
  const dir = await app.newCollection('basic')
  await openCollection(page, app)

  // ① 经反射桥启用内嵌 MCP（随机端口，白名单授权当前集合）
  const port = 12000 + Math.floor(Math.random() * 30000)
  const settings = await app.ipc<{ mcp: Record<string, unknown> }>('GetSettings')
  settings.mcp = {
    ...(settings.mcp as object),
    enabled: true,
    addr: '127.0.0.1',
    port,
    readOnly: true, // 只跑读工具：写在 Go 单测里覆盖，这里不需要弹窗
    allowOrigins: [],
    allow: [{ path: dir, writable: true }],
  }
  await app.ipc('SaveSettings', settings)
  const status = await app.ipc<{ running: boolean; token: string }>('ApplyMCPSettings')
  expect(status.running).toBe(true)
  expect(status.token).toBeTruthy()

  let sessionId = ''
  async function rpc(method: string, params?: unknown, waitForResponse = true): Promise<unknown> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      Accept: 'application/json, text/event-stream',
      Authorization: `Bearer ${status.token}`,
    }
    if (sessionId) headers['Mcp-Session-Id'] = sessionId
    const res = await fetch(`http://127.0.0.1:${port}/mcp`, {
      method: 'POST',
      headers,
      body: JSON.stringify({ jsonrpc: '2.0', id: 1, method, params: params ?? {} }),
    })
    const sid = res.headers.get('mcp-session-id')
    if (sid) sessionId = sid
    if (!waitForResponse) return null
    const text = await res.text()
    if (res.headers.get('content-type')?.includes('text/event-stream')) {
      const line = text.split('\n').find((l) => l.startsWith('data:'))
      if (!line) throw new Error(`SSE 响应无 data 帧: ${text}`)
      return JSON.parse(line.slice(5).trim())
    }
    return JSON.parse(text)
  }

  const call = async (name: string, args: Record<string, unknown> = {}): Promise<string> => {
    const res = (await rpc('tools/call', { name, arguments: args })) as { result?: McpResult }
    expect(res.result?.content?.[0]?.text ?? '').toBeTruthy()
    return res.result?.content?.[0]?.text ?? ''
  }

  const init = (await rpc('initialize')) as { result?: { serverInfo?: { name?: string } } }
  expect(init.result?.serverInfo?.name).toBe('api-doc-go')
  await rpc('notifications/initialized', {}, false)

  // ② list_workspaces：只有别名，没有绝对路径
  const ws = await call('list_workspaces')
  expect(ws).toContain('ws_1')
  expect(ws).not.toContain(dir)
  expect(ws).not.toContain(app.runDir)

  // ③ 用别名选定工作目录（外部拿不到路径也照样能用）
  const used = await call('use_workspace', { workspace: 'ws_1' })
  expect(used).toContain('ws_1')
  expect(used).not.toContain(dir)

  // ④ 入参给绝对路径（兼容旧调用方）：回显必须已脱敏
  const mods = await call('get_project_modules', { project: dir })
  expect(mods).not.toContain(dir)
  expect(mods).not.toContain(app.runDir)

  // ⑤ 设置页（MCP 分区）也不摊路径：只有名称 + 序号
  await openMcpSettings(page)
  const item = page.getByTestId('settings.mcpAllowItem')
  await expect(item).toHaveCount(1)
  await expect(item).toContainText('basic')
  await expect(item).not.toContainText(dir)
  await expect(item).not.toContainText(app.runDir)
  await saveSettings(page)
})
