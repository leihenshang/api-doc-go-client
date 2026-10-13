// 页内查找：响应结果区（DOM 高亮）与请求编辑区 textarea（选区查找，含 JSON / 文本 / 脚本 / Docs）。
import { expect, test } from '../helpers/app'
import { t } from '../helpers/i18n'
import { openCollection, openReqTab, openRequest, send } from '../helpers/ui'

async function sendUrl(page: import('@playwright/test').Page, app: import('../helpers/app').AppFixture, url: string) {
  const { patchRequest } = await import('../helpers/api')
  await patchRequest(app, '33333333-3333-4333-8333-333333333333', { url })
  await page.getByTestId('req.url').fill(url)
  await page.getByTestId('req.url').blur()
  await send(page)
}

/** 选请求体类型（JSON / 文本）。 */
async function pickBodyType(page: import('@playwright/test').Page, label: string) {
  await page.locator('.btype').click()
  await page.locator('.n-base-select-option').filter({ hasText: label }).click()
}

test.describe('页内查找', () => {
  test('响应区：查找按钮 / Ctrl+F / 高亮计数 / 原始与美化一致 / Esc 关闭', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    await sendUrl(page, app, '{{host}}/json/flat')

    await page.getByTestId('resp.find').click()
    await expect(page.getByTestId('find.bar')).toBeVisible()
    await page.getByTestId('find.input').fill('flat')
    await expect(page.getByTestId('find.count')).toHaveText('1/1')
    // 当前命中加 .on（琥珀实色）
    await expect(page.locator('mark.find-hit.on')).toHaveCount(1)

    // 切原始视图：同一份文本仍命中（不重新打开查找条）
    await page.getByTestId('resp.raw').click()
    await expect(page.getByTestId('find.count')).toHaveText('1/1')

    // 无结果态
    await page.getByTestId('find.input').fill('zz-not-here')
    await expect(page.getByTestId('find.count')).toHaveText(t('find.none'))

    // Esc 关闭
    await page.getByTestId('find.input').press('Escape')
    await expect(page.getByTestId('find.bar')).toHaveCount(0)

    // Ctrl+F 在响应区内重新唤起
    await page.locator('.resp').press('Control+f')
    await expect(page.getByTestId('find.input')).toBeVisible()
  })

  test('请求编辑区：JSON / 文本请求体、脚本区 textarea 选区查找；Params 无编辑区不唤起', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    await openReqTab(page, 'body')
    await pickBodyType(page, t('editor.bodyJson'))

    const json = '{"hello":"world","again":"world"}'
    await page.getByTestId('req.bodyRaw').fill(json)

    await page.getByTestId('req.find').click()
    await expect(page.getByTestId('find.bar')).toBeVisible()
    await page.getByTestId('find.input').fill('world')
    await expect(page.getByTestId('find.count')).toHaveText('2/2')
    // Enter = 下一个：选区落到第二处
    await page.getByTestId('find.input').press('Enter')
    await expect
      .poll(async () =>
        page.locator('textarea:focus').evaluate((el) => {
          const ta = el as HTMLTextAreaElement
          return { s: ta.selectionStart, e: ta.selectionEnd }
        }),
      )
      .toMatchObject({ s: json.indexOf('world', json.indexOf('world') + 1), e: json.indexOf('world', json.indexOf('world') + 1) + 5 })

    // Shift+Enter = 上一个
    await page.getByTestId('find.input').press('Shift+Enter')
    await expect
      .poll(async () =>
        page.locator('textarea:focus').evaluate((el) => {
          const ta = el as HTMLTextAreaElement
          return { s: ta.selectionStart, e: ta.selectionEnd }
        }),
      )
      .toMatchObject({ s: json.indexOf('world'), e: json.indexOf('world') + 5 })

    // 切文本请求体：单一 textarea 仍可查找
    await pickBodyType(page, t('editor.bodyText'))
    await page.locator('.raw textarea').fill('abc abc')
    await page.getByTestId('find.input').fill('abc')
    await expect(page.getByTestId('find.count')).toHaveText('2/2')

    // Params 分段没有 textarea：不显示查找入口，Ctrl+F 不拦截
    await openReqTab(page, 'params')
    await expect(page.getByTestId('req.find')).toHaveCount(0)
    await page.locator('.editor').press('Control+f')
    await expect(page.getByTestId('find.bar')).toHaveCount(0)

    // 脚本分段：pre / post 两个 textarea 跨域计数
    await openReqTab(page, 'script')
    const pre = page.getByTestId('script.pre')
    await pre.fill('aaa')
    const post = page.getByTestId('script.post')
    await post.fill('aaaa')
    await page.locator('.editor').press('Control+f')
    await page.getByTestId('find.input').fill('aaa')
    await expect(page.getByTestId('find.count')).toHaveText('7/7') // pre 3 + post 4
  })
})
