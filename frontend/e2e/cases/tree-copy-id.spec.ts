import { expect, test } from '../helpers/app'
import { openCollection, treeRow } from '../helpers/ui'
import { t } from '../helpers/i18n'

/**
 * 集合树：右键「复制 ID」/「复制 MCP 指令」。
 *
 * 目的：把 MCP 查询需要的 uid（以及修改时的 ifMatch 短 hash）一键带出来，
 * 用户可直接贴给 AI：「请查看 mcp:api-doc-go 中 id 为 … 的数据，并 …」。
 */
const PING_UID = '33333333-3333-4333-8333-333333333333'

async function readClipboard(page: import('@playwright/test').Page): Promise<string> {
  return page.evaluate(() => navigator.clipboard.readText())
}

/** 打开某行的右键菜单（与悬停「···」共用选项） */
async function openRowCtx(page: import('@playwright/test').Page, name: string): Promise<void> {
  await treeRow(page, name).first().click({ button: 'right' })
  await expect(page.locator('.n-dropdown-option').filter({ hasText: t('tree.copyId') }).last()).toBeVisible()
}

async function pickCtxItem(page: import('@playwright/test').Page, label: string): Promise<void> {
  await page.locator('.n-dropdown-option').filter({ hasText: label }).last().click()
}

test.describe('集合树复制 ID / MCP 指令', () => {
  test('[T21] 请求行：复制 ID 与带短 hash 的 MCP 指令', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])

    // ① 复制 ID：纯 uid，便于贴进任意模板
    await openRowCtx(page, 'ping')
    await pickCtxItem(page, t('tree.copyId'))
    await expect.poll(() => readClipboard(page)).toBe(PING_UID)

    // ② 复制 MCP 指令：uid + get_request_detail + 12 位短 hash（可直接当 ifMatch）
    await openRowCtx(page, 'ping')
    await pickCtxItem(page, t('tree.copyMcp'))
    await expect.poll(() => readClipboard(page)).toContain(PING_UID)
    const prompt = await readClipboard(page)
    expect(prompt).toContain('get_request_detail')
    expect(prompt).toMatch(/ifMatch=[0-9a-f]{12}/)
  })

  test('[T21] 文档行：复制 ID 与带 get_doc/ifMatch 的 MCP 指令', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    const created = await app.ipc<{ uid: string; path: string }>('CreateDoc', '说明', 'api')
    await app.ipc('SaveDoc', { uid: created.uid, name: '说明', path: created.path, content: '正文' })
    await openCollection(page, app) // 重进页面让树看到新文档
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])

    await openRowCtx(page, '说明')
    await pickCtxItem(page, t('tree.copyId'))
    await expect.poll(() => readClipboard(page)).toBe(created.uid)

    await openRowCtx(page, '说明')
    await pickCtxItem(page, t('tree.copyMcp'))
    await expect.poll(() => readClipboard(page)).toContain(created.uid)
    const prompt = await readClipboard(page)
    expect(prompt).toContain('get_doc')
    expect(prompt).toMatch(/ifMatch=[0-9a-f]{12}/)
  })

  test('[T21] 目录行不提供复制项（目录没有 MCP 实体 id）', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)

    // 目录行的右键菜单只有新建/重命名/删除
    await treeRow(page, 'api').first().click({ button: 'right' })
    await expect(page.locator('.n-dropdown-option').filter({ hasText: t('tree.rename') }).last()).toBeVisible()
    await expect(page.getByText(t('tree.copyId'), { exact: true })).toHaveCount(0)
  })
})
