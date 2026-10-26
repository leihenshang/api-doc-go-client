// 编辑区布局与查找焦点：
//   ① 请求体 JSON / 响应数据编辑区在「下方没有其它元素」时占满分段剩余高度；
//   ② 打开页内查找后焦点落在搜索框，输入关键字时焦点不被 textarea 抢走；
//      按 Enter / Shift+Enter 时焦点跟到命中处（浏览器查找的常见操作习惯）。
import { expect, test } from '../helpers/app'
import { t } from '../helpers/i18n'
import { openCollection, openReqTab, openRequest } from '../helpers/ui'

/** 选请求体类型。 */
async function pickBodyType(page: import('@playwright/test').Page, label: string): Promise<void> {
    await page.locator('.btype').click()
    await page.locator('.n-base-select-option').filter({ hasText: label }).click()
}

/** 断言元素底边与容器底边对齐（允许 2px 的边框/取整误差）。 */
async function expectBottomAligned(
    page: import('@playwright/test').Page,
    childSel: string,
    parentSel: string,
): Promise<void> {
    const boxes = await page.evaluate(([child, parent]) => {
        const c = document.querySelector(child)?.getBoundingClientRect()
        const p = document.querySelector(parent)?.getBoundingClientRect()
        return c && p ? { cb: c.bottom, pb: p.bottom } : null
    }, [childSel, parentSel])
    expect(boxes).not.toBeNull()
    expect(Math.abs((boxes?.cb ?? 0) - (boxes?.pb ?? 0))).toBeLessThanOrEqual(2)
}

async function activeTestId(page: import('@playwright/test').Page): Promise<string> {
    return page.evaluate(() => document.activeElement?.getAttribute('data-testid') ?? '')
}

test.describe('编辑区布局与查找焦点', () => {
    test('请求体 / 响应数据编辑区占满分段剩余高度', async ({ page, app }) => {
        await app.newCollection('basic')
        await openCollection(page, app)
        await openRequest(page, 'ping')
        await openReqTab(page, 'body')
        await pickBodyType(page, t('editor.bodyJson'))
        await page.getByTestId('req.bodyRaw').fill('{"a":1}')

        // 请求体：下方没有其它元素 → .jb 底边与 .body-pane 底边对齐
        await expect(page.locator('.body-pane .jb')).toBeVisible()
        await expectBottomAligned(page, '.body-pane .jb', '.body-pane')

        // 纯文本（含 XML 文本）请求体同样占满
        await pickBodyType(page, t('editor.bodyText'))
        await page.locator('.body-text-editor textarea').fill('<root/>')
        await expectBottomAligned(page, '.body-text-editor', '.body-pane')

        // 响应数据：切 Resp → 响应数据（默认子页签），编辑区同样撑到底
        await openReqTab(page, 'resp')
        await page.locator('[data-testid="resp.segTab"][data-view="body"]').click()
        await expect(page.locator('.resp-data .jb')).toBeVisible()
        await expectBottomAligned(page, '.resp-data .jb', '.resp-data')
    })

    test('查找打开后焦点在搜索框；输入时不被抢走，回车上/下一个才跟到命中处', async ({ page, app }) => {
        await app.newCollection('basic')
        await openCollection(page, app)
        await openRequest(page, 'ping')
        await openReqTab(page, 'body')
        await pickBodyType(page, t('editor.bodyJson'))
        const json = '{"hello":"world","again":"world"}'
        await page.getByTestId('req.bodyRaw').fill(json)

        await page.getByTestId('req.find').click()
        await expect(page.getByTestId('find.bar')).toBeVisible()
        await expect.poll(() => activeTestId(page)).toBe('find.input')

        // 输入关键字后焦点仍在搜索框（此前会跳到 textarea，下一字符输进正文）
        await page.getByTestId('find.input').fill('world')
        await expect(page.getByTestId('find.count')).toHaveText('1/2')
        await expect.poll(() => activeTestId(page)).toBe('find.input')

        // 回车 = 下一个：焦点跟到 textarea 的命中处
        await page.getByTestId('find.input').press('Enter')
        await expect(page.getByTestId('find.count')).toHaveText('2/2')
        await expect.poll(() => page.locator('textarea:focus').count()).toBe(1)

        // 响应数据编辑区同样：打开查找后焦点在搜索框
        await openReqTab(page, 'resp')
        await page.locator('[data-testid="resp.segTab"][data-view="body"]').click()
        // 上一步的查找条仍开着（Enter 不会关闭），先关掉再重新打开
        await page.getByTestId('find.close').click()
        await page.getByTestId('req.find').click()
        await expect(page.getByTestId('find.bar')).toBeVisible()
        await expect.poll(() => activeTestId(page)).toBe('find.input')
    })
})
