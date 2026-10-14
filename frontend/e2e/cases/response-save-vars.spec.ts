// [E25] 响应面板「保存 ▾」：菜单只保留「保存响应示例」与「保存响应体为文件（.json）」。
// item 24：字段表的「值列 / 变量书签」已移除——当时的响应数据与响应头在编辑器
// 「响应字段」分段的子页签里看；变量入口不再挂在字段表上。
import type { Page } from '@playwright/test'
import { expect, test } from '../helpers/app'
import { openCollection, openRequest, send } from '../helpers/ui'

const PING_UID = '33333333-3333-4333-8333-333333333333'

async function sendUrl(page: Page, app: import('../helpers/app').AppFixture, url: string) {
  const { patchRequest } = await import('../helpers/api')
  await patchRequest(app, PING_UID, { url })
  await page.getByTestId('req.url').fill(url)
  await page.getByTestId('req.url').blur()
  await send(page)
}

test.describe('响应区：保存▾与字段表（瘦身版）', () => {
  test('[E25] 保存▾只剩响应示例与响应体文件；字段表无值列/书签，快照看子页签', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    await sendUrl(page, app, '{{host}}/json/flat')

    // 菜单项：保存响应示例 + 保存响应体为文件；变量类菜单项与作用域 chips 已下线
    await page.getByTestId('resp.save').click()
    await expect(page.getByTestId('resp.saveExample')).toBeVisible()
    await expect(page.getByTestId('resp.saveFile')).toBeVisible()
    await expect(page.getByTestId('resp.saveAllVars')).toHaveCount(0)
    await expect(page.getByTestId('resp.saveSelectedVar')).toHaveCount(0)
    await expect(page.getByTestId('resp.scope')).toHaveCount(0)
    await page.keyboard.press('Escape')

    // 更新响应字段后：字段表在编辑器分段渲染，但不再有值列与变量书签（item 24）
    await page.getByTestId('resp.updateFields').click()
    await expect(page.getByTestId('resp.fields.row').first()).toBeVisible()
    await expect(page.getByTestId('resp.fields.value')).toHaveCount(0)
    await expect(page.getByTestId('resp.fields.bookmark')).toHaveCount(0)

    // 当时的响应数据/响应头改由子页签承载，且响应数据是格式化（缩进）展示的
    await page.locator('[data-testid="resp.segTab"][data-view="body"]').click()
    await expect(page.getByTestId('resp.segBody')).toContainText('"flat": true')
    await page.locator('[data-testid="resp.segTab"][data-view="headers"]').click()
    await expect(page.getByTestId('resp.segHeaderRow').filter({ hasText: 'Content-Type' }).first()).toBeVisible()
  })
})
