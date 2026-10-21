// B19 文档条目树化 + 页签：树叶子新建 / 开页签 / 编辑保存 / 拖拽改目录。
//
// 覆盖：Go 门面 CreateDoc(folder)/ReadDoc/SaveDocExpectHash/MoveDoc + 前端
// Sidebar doc 行、tabs.openDocTab/saveDoc、useTreeDnd 的 doc 分支。
import { readdirSync, readFileSync } from 'node:fs'
import path from 'node:path'
import { expect, test } from '../helpers/app'
import { t } from '../helpers/i18n'
import { openCollection, treeRow, treeRowByUid } from '../helpers/ui'

/** 读集合里某个 .md 的正文（断言保存真的落盘了）。 */
function readMd(dir: string, rel: string): string {
  return readFileSync(path.join(dir, rel), 'utf8')
}

test('文档树叶子：分组内新建 → 开页签 → 编辑保存落盘', async ({ page, app }) => {
  const dir = await app.newCollection('basic')
  await openCollection(page, app)

  // ① 分组行「＋」菜单 → 新建文档（folder 由所选分组决定）
  const group = treeRow(page, 'api')
  await group.hover()
  await group.getByTestId('tree.row.menu').click()
  await page.getByTestId('tree.row.newDoc').click()
  const input = page.locator('.n-modal input').first()
  await input.fill('接口说明')
  await page.getByRole('button', { name: t('common.confirm') }).click()

  // 树同层出现文档叶子
  await expect(treeRow(page, '接口说明').getByTestId('tree.docRow')).toBeVisible()

  // ② 点叶子 → 开文档页签（保存按钮可见即编辑器已挂载）
  await treeRow(page, '接口说明').click()
  await expect(page.getByTestId('doc.save')).toBeVisible()

  // ③ 编辑正文 → 脏点出现 → Ctrl+S 落盘（poll 文件内容）
  await page.locator('.md-editor textarea, .md-editor .cm-content').first().click()
  await page.keyboard.type('第一版内容')
  await expect(page.getByTestId('tab.item').filter({ hasText: '接口说明' }).locator('.dot')).toHaveCount(1)
  await page.keyboard.press('Control+s')
  await expect
    .poll(() => {
      try {
        return readMd(dir, 'api/接口说明.md').includes('第一版内容')
      } catch {
        return false
      }
    })
    .toBe(true)
})

test('文档树叶子：拖动改目录后 uid 不变', async ({ page, app }) => {
  const dir = await app.newCollection('basic')
  await openCollection(page, app)

  // 根级建一篇文档（docs/ 默认位）
  const root = page.getByTestId('tree.root').first()
  await root.getByTestId('tree.root.menu').click()
  await page.getByTestId('tree.row.newDoc').click()
  await page.locator('.n-modal input').first().fill('根级文档')
  await page.getByRole('button', { name: t('common.confirm') }).click()
  await expect(treeRow(page, '根级文档')).toBeVisible()
  // 新建文档会自动开页签并滚动到树行：等 tab 就绪、行滚到可见后再拖，
  // 否则 boundingBox 可能在滚动/布局未稳定时取值，导致从错误坐标起拖（用例侧假失败）。
  await expect(page.getByTestId('tab.item').filter({ hasText: '根级文档' })).toBeVisible()
  const docRow = treeRow(page, '根级文档')
  await docRow.scrollIntoViewIfNeeded()
  await page.waitForTimeout(200) // 等自动滚动/布局落定，避免取到旧坐标
  const uid = await docRow.getAttribute('data-uid')
  const { dragRow } = await import('../helpers/ui')
  await dragRow(page, docRow, treeRow(page, 'api'))
  await expect.poll(() => {
    const files = readdirSync(path.join(dir, 'api')) as string[]
    return files.some((f) => f.endsWith('.md'))
  }).toBe(true)
  await expect(treeRowByUid(page, uid ?? '')).toBeVisible()
})
