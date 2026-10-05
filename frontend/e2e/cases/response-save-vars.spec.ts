// [E25] 响应面板「保存 ▾」：菜单只保留「保存响应示例」与「保存响应体为文件（.json）」。
// 「保存全部字段为变量 / 保存选中值为变量」与「作用域」chips 已下线，变量入口只剩字段表的
// 「变量书签」，落点固定为当前环境 —— 本用例同时钉住这两件事。
// 用例只从语言包取文案。
import type { Page } from '@playwright/test'
import { expect, test } from '../helpers/app'
import { t } from '../helpers/i18n'
import { openCollection, openRequest, openRespTab, send } from '../helpers/ui'

const PING_UID = '33333333-3333-4333-8333-333333333333'

async function sendUrl(page: Page, app: import('../helpers/app').AppFixture, url: string) {
  const { patchRequest } = await import('../helpers/api')
  await patchRequest(app, PING_UID, { url })
  await page.getByTestId('req.url').fill(url)
  await page.getByTestId('req.url').blur()
  await send(page)
}

test.describe('响应区：保存▾与变量书签', () => {
  test('[E25] 保存▾只剩响应示例与响应体文件，变量书签写入当前环境', async ({ page, app }) => {
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

    // 字段表书签：点亮 = 该字段值存成当前环境的变量，再点 = 删除。
    // 不假设环境里是否已存在同名变量（fixture 的 env 可能已定义），只断言「点一下翻转、再点回来」。
    await page.getByTestId('resp.updateFields').click()
    await openRespTab(page, 'fields') // 字段表只在「响应字段」页签里渲染
    const bm = page.getByTestId('resp.fields.bookmark').first()
    const lit = async (): Promise<boolean> => ((await bm.getAttribute('class')) ?? '').includes('on')
    const before = await lit()

    await bm.click()
    await expect.poll(lit).toBe(!before)
    if (!before) await expect(page.locator('.n-message').last()).toContainText(t('resp.scopeEnv'))

    await bm.click()
    await expect.poll(lit).toBe(before)
  })
})
