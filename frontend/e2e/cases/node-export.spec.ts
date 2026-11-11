// 右键单个条目直接导出：请求 / 文档各自一份文件，文件名用条目名（不再弹范围选择）。
import { existsSync, readFileSync } from 'node:fs'
import path from 'node:path'
import { expect, test } from '../helpers/app'
import { expandAllFolders, openCollection, reopenApp, treeRow } from '../helpers/ui'

test.describe('右键导出单个条目', () => {
  test('请求：导出 Markdown，文件以条目名命名', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)

    await treeRow(page, 'ping').first().click({ button: 'right' })
    // 子菜单：hover 展开后点 Markdown
    await page.getByTestId('tree.row.export').hover()
    await page.getByTestId('tree.row.exportMd').click()

    const file = path.join(dir, 'ping.md')
    await expect.poll(() => existsSync(file)).toBe(true)
    const md = readFileSync(file, 'utf8')
    expect(md).toContain('ping')
    // 单条目导出不该把同组另一个请求也带进来
    expect(md).not.toContain('pong')
  })

  test('文档：导出 HTML，文件以条目名命名', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    await app.ipc('CreateDoc', 'guide', 'api')
    await page.reload()
    await reopenApp(page)
    await expandAllFolders(page)

    await treeRow(page, 'guide').first().click({ button: 'right' })
    await page.getByTestId('tree.row.export').hover()
    await page.getByTestId('tree.row.exportHtml').click()

    const file = path.join(dir, 'guide.html')
    await expect.poll(() => existsSync(file)).toBe(true)
    const html = readFileSync(file, 'utf8')
    expect(html).toContain('guide')
  })
})
