// [E25] 响应面板「保存 ▾」：保存全部字段为变量 / 保存选中值为变量 / 字段变量书签（作用域 chips）。
// 用例只从语言包取文案；变量落点按作用域映射（集合→请求 vars.pre-request）。
import type { Page } from '@playwright/test'
import { expect, test } from '../helpers/app'
import { t } from '../helpers/i18n'
import { openCollection, openRequest, send } from '../helpers/ui'

const PING_UID = '33333333-3333-4333-8333-333333333333'

async function sendUrl(page: Page, app: import('../helpers/app').AppFixture, url: string) {
  const { patchRequest } = await import('../helpers/api')
  await patchRequest(app, PING_UID, { url })
  await page.getByTestId('req.url').fill(url)
  await page.getByTestId('req.url').blur()
  await send(page)
}

test.describe('响应区：保存为变量', () => {
  test('[E25] 保存全部字段/选中值/字段书签为变量', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    await sendUrl(page, app, '{{host}}/json/flat')

    // 作用域切到「集合」（不依赖环境即可写入）
    await page.getByTestId('resp.save').click()
    await page.locator('[data-testid="resp.scope"][data-scope="collection"]').click()

    // 字段表书签：点亮 = 保存为变量，再点 = 删除变量
    await page.getByTestId('resp.updateFields').click()
    const bm = page.getByTestId('resp.fields.bookmark').first()
    await bm.click()
    await expect(page.locator('.n-message').last()).toContainText(t('resp.scopeCollection'))
    await expect(bm).toHaveClass(/on/)
    await bm.click()
    await expect(bm).not.toHaveClass(/on/)

    // 保存全部字段为变量：叶子字段全部点亮
    await page.getByTestId('resp.save').click()
    await page.getByTestId('resp.saveAllVars').click()
    await expect(page.locator('.n-message').last()).toContainText(t('resp.scopeCollection'))
    await expect(bm).toHaveClass(/on/)

    // 保存选中值为变量：选中响应文本 → 弹窗命名 → 落点为当前作用域
    await page.getByTestId('resp.raw').click()
    await page.getByTestId('resp.rawBody').selectText()
    await page.getByTestId('resp.save').click()
    await page.getByTestId('resp.saveSelectedVar').click()
    await page.getByTestId('resp.varNameInput').locator('input').fill('selVal')
    await page
      .locator('.n-modal')
      .getByRole('button', { name: t('common.save'), exact: true })
      .click()
    await expect(page.locator('.n-message').last()).toContainText(
      t('resp.varSaved', { name: 'selVal', scope: t('resp.scopeCollection') }),
    )
  })
})
