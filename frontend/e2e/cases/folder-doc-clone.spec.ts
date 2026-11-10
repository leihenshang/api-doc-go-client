// 复制分组（递归整棵子树）与复制文档（同层级），名称统一用「原名 副本N」。
import { expect, test } from '../helpers/app'
import { expandAllFolders, openCollection, reopenApp, treeRow } from '../helpers/ui'

test.describe('复制分组 / 文档', () => {
  test('复制分组：整棵子树一起复制，内部名称保持不变', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await expandAllFolders(page)

    await treeRow(page, 'api').first().click({ button: 'right' })
    await page.getByTestId('tree.row.duplicate').click()

    await expect(treeRow(page, 'api 副本1')).toBeVisible()
    await expandAllFolders(page)
    // 副本里 ping / pong 各一份；内部条目不改名（改名的只有顶层分组）
    await expect(treeRow(page, 'ping')).toHaveCount(2)
    await expect(treeRow(page, 'pong')).toHaveCount(2)
  })

  test('复制文档：同层级生成「原名 副本1」', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    // 先建一篇文档（basic 种子集合里没有 .md），再刷新让树读到它
    await app.ipc('CreateDoc', 'guide', 'api')
    await page.reload()
    await reopenApp(page)
    await expandAllFolders(page)

    await treeRow(page, 'guide').first().click({ button: 'right' })
    await page.getByTestId('tree.row.duplicate').click()

    await expect(treeRow(page, 'guide 副本1')).toBeVisible()
  })
})
