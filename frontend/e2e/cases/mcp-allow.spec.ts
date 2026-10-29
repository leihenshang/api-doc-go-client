import { readFileSync } from 'node:fs'
import path from 'node:path'
import { expect, test } from '../helpers/app'
import { openCollection, openMcpSettings, reopenApp, saveSettings } from '../helpers/ui'

/**
 * MCP 工作目录白名单（设置页）。
 *
 * 安全默认：白名单为空 = 不授权任何目录（AI 读写不到集合）；
 * 需要时勾选工作目录加入白名单（默认可写），「只读模式」开启时所有可写开关一并被禁用；
 * 保存后内嵌服务立即重启生效。
 */
test('[I7] MCP 白名单：空白名单安全默认 / 勾选目录（默认可写）/ 可写受只读控制 / 取消勾选移除', async ({ page, app }) => {
  const dir = await app.newCollection('basic')
  await openCollection(page, app)
  await openMcpSettings(page)

  const allowCheck = page.getByTestId('settings.mcpAllowCheck')
  const writable = page.getByTestId('settings.mcpAllowWritable')

  // ① 默认还没授权任何目录：列表里有该工作区但均未勾选
  await expect(page.getByTestId('settings.mcpAllowList')).toBeVisible()
  await expect(allowCheck).toHaveCount(1)
  await expect(allowCheck).not.toBeChecked()

  // ② 启用服务；默认「只读模式」开启 → 「可写」受只读控制被禁用。
  //    n-checkbox 的禁用只有 class（--disabled），没有 disabled 属性，故用 toHaveClass 断言。
  await page.getByTestId('settings.mcpEnabled').click()
  // 随机端口：避免本机已跑的实例占用默认 8189 导致服务起不来
  await page.getByTestId('settings.mcpPort').locator('input').fill(String(12000 + Math.floor(Math.random() * 30000)))
  await allowCheck.click()
  const item = page.getByTestId('settings.mcpAllowItem')
  await expect(item).toHaveCount(1)
  await expect(item).toContainText('basic')
  await expect(writable).toHaveClass(/--disabled/) // 只读模式下可写不可勾

  // ③ 关闭「只读模式」→ 可写恢复，且授权默认可写 → 保存（保存会关掉弹窗）：配置落盘
  await page.getByTestId('settings.mcpReadOnly').click()
  await expect(writable).not.toHaveClass(/--disabled/)
  await expect(writable).toBeChecked()
  await saveSettings(page)

  const cfg = JSON.parse(readFileSync(path.join(app.configDir, 'config.json'), 'utf8')) as {
    mcp: { allow?: { path: string; writable: boolean }[] }
  }
  expect(cfg.mcp.allow?.length).toBe(1)
  expect(cfg.mcp.allow?.[0]?.path).toBe(dir)
  expect(cfg.mcp.allow?.[0]?.writable).toBe(true)

  // ④ 重新打开：服务运行中，授权与可写仍保持
  await openMcpSettings(page)
  await expect(page.getByTestId('settings.mcpRunning')).toBeVisible({ timeout: 15_000 })
  await expect(writable).toBeChecked()

  // ⑤ 取消勾选该目录（移除授权）→ 保存：回到「未授权任何目录」的安全默认
  await allowCheck.click()
  await saveSettings(page)
  const cfg2 = JSON.parse(readFileSync(path.join(app.configDir, 'config.json'), 'utf8')) as {
    mcp: { allow?: unknown[] }
  }
  expect(cfg2.mcp.allow ?? []).toHaveLength(0)

  await openMcpSettings(page)
  await expect(allowCheck).not.toBeChecked()
})

/**
 * 回归：打开 MCP 设置不应凭空提示「配置已修改」。
 * 根因是 Go 侧把空列表归一成 null，而对话框把 allowOrigins 归一成 []：
 * dirty 用 JSON 快照比较，null ≠ [] 就会一打开就显示未保存提示。
 */
test('[I7] 打开 MCP 设置不显示未保存提示（空列表归一）', async ({ page, app }) => {
  await app.newCollection('basic')
  await openCollection(page, app)
  const hint = page.getByTestId('settings.dirtyHint')

  // ① 首次打开：什么都没改 → 不应有未保存提示
  await openMcpSettings(page)
  await expect(hint).toHaveCount(0)

  // ② 原样保存一次（把归一后的空列表写回磁盘，模拟用户曾保存过配置）→ 再打开仍不应提示
  //    注意不要动启用/端口：同一 worker 的配置会在用例间保留，MCP 可能已是启用/运行态。
  await saveSettings(page)
  await openMcpSettings(page)
  await expect(hint).toHaveCount(0)

  // ③ 关闭弹窗 → 刷新页面（模拟重启后重开）→ 直接进 MCP 分区：仍不应提示
  await page.getByTestId('settings.cancel').click()
  await page.reload()
  await reopenApp(page)
  await openMcpSettings(page)
  await expect(hint).toHaveCount(0)
})

/**
 * 「一键复制示例」的 mcpServers 服务名必须是 api-doc-go（与使用手册/设计文档一致），
 * 不能是内部简称 apidoc —— 用户会把这段 JSON 直接粘进 AI 工具。
 */
test('[I8] MCP 配置示例：服务名为 api-doc-go', async ({ page, app }) => {
  await app.newCollection('basic')
  await openCollection(page, app)
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])
  await openMcpSettings(page)

  // 需要服务处于运行态才会显示「一键复制示例」：没启用就先启用（随机端口避免占用默认 8189）
  if (!(await page.getByTestId('settings.mcpCopyExample').isVisible().catch(() => false))) {
    const enabled = page.getByTestId('settings.mcpEnabled')
    if (!(await enabled.getAttribute('class') ?? '').includes('--checked')) {
      await enabled.click()
    }
    await page.getByTestId('settings.mcpPort').locator('input').fill(String(12000 + Math.floor(Math.random() * 30000)))
    // 空白名单时服务不会真的监听（安全默认）：授权当前工作区，运行态才会出现「一键复制示例」
    const allow = page.getByTestId('settings.mcpAllowCheck').first()
    if (!(await allow.getAttribute('class') ?? '').includes('--checked')) {
      await allow.click()
    }
    await saveSettings(page)
    await openMcpSettings(page)
  }
  await expect(page.getByTestId('settings.mcpCopyExample')).toBeVisible({ timeout: 15_000 })
  await page.getByTestId('settings.mcpCopyExample').click()

  const text = await page.evaluate(() => navigator.clipboard.readText())
  const cfg = JSON.parse(text) as {
    mcpServers: Record<string, { type: string; url: string; headers: { Authorization: string } }>
  }
  expect(Object.keys(cfg.mcpServers)).toEqual(['api-doc-go'])
  const entry = cfg.mcpServers['api-doc-go']
  expect(entry.type).toBe('http')
  expect(entry.url).toContain('/mcp')
  expect(entry.headers.Authorization).toMatch(/^Bearer [0-9a-f]{32}$/)
})
