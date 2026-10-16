// B9 文档条目 UI 入口：侧栏「⋯」→ 文档 → 新建 / 删除（Go 门面 ListDocs/CreateDoc/DeleteDoc）。
import { expect, test } from '../helpers/app'
import { t } from '../helpers/i18n'
import { openCollection } from '../helpers/ui'

test('文档入口：新建与删除文档', async ({ page, app }) => {
  await app.newCollection('basic')
  await openCollection(page, app)

  await page.getByTestId('tree.ops.menu').click()
  await page.locator('.n-dropdown-option').filter({ hasText: t('docs.title') }).first().click()
  await expect(page.getByTestId('docs.dialog')).toBeVisible()

  // 新建：内联输入名 → 确认后列表出现该文档
  await page.getByTestId('docs.new').click()
  await page.getByTestId('docs.nameInput').locator('input').fill('发布说明')
  await page.getByTestId('docs.newConfirm').click()
  await expect(page.getByTestId('docs.item')).toHaveCount(1)

  // 选中后 DocEditor 挂载（保存按钮存在即编辑器已接上）
  await page.getByTestId('docs.item').first().click()
  await expect(page.getByTestId('doc.save')).toBeVisible()

  // 删除：popconfirm 确认后列表清空（文件进 .trash，行为由 B5 既有测试覆盖）
  await page.getByTestId('docs.delete').first().click()
  await page.locator('.n-popconfirm').last()
    .locator('button').filter({ hasText: t('common.confirm') }).click()
  await expect(page.getByTestId('docs.item')).toHaveCount(0)
})
