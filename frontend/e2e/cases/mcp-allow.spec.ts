import { readFileSync } from 'node:fs'
import path from 'node:path'
import { expect, test } from '../helpers/app'
import { openCollection, openSettings, openSettingsSection, saveSettings } from '../helpers/ui'

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
  await openSettings(page)
  await openSettingsSection(page, 'mcp')

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
  await openSettings(page)
  await openSettingsSection(page, 'mcp')
  await expect(page.getByTestId('settings.mcpRunning')).toBeVisible({ timeout: 15_000 })
  await expect(writable).toBeChecked()

  // ⑤ 取消勾选该目录（移除授权）→ 保存：回到「未授权任何目录」的安全默认
  await allowCheck.click()
  await saveSettings(page)
  const cfg2 = JSON.parse(readFileSync(path.join(app.configDir, 'config.json'), 'utf8')) as {
    mcp: { allow?: unknown[] }
  }
  expect(cfg2.mcp.allow ?? []).toHaveLength(0)

  await openSettings(page)
  await openSettingsSection(page, 'mcp')
  await expect(allowCheck).not.toBeChecked()
})