// 使用问题记录 23–25：
//  23 顶部搜索面板 / 左侧集合树搜索支持按 API URL 命中（共用 Go 索引口径：标题/URL/方法/路径）；
//  24 集合搜索框带放大镜图标（可识别性）；
//  25 标签页不再展示 HTTP 状态码（响应状态看响应面板 resp.status）。
import type { Page } from '@playwright/test'
import { expect, test } from '../helpers/app'
import { openCollection, openRequest, send } from '../helpers/ui'

/** 侧栏搜索框（naive NInput 的 testid 在外层包装，fill 要打到内层 input）。 */
const searchBox = (page: Page) => page.getByTestId('sidebar.search').locator('input')

async function openPalette(page: Page): Promise<void> {
    await page.keyboard.press('Control+k')
    await expect(page.getByTestId('palette')).toBeVisible()
}

test.describe('URL 搜索与标签展示', () => {
    test('[U1] 侧栏树搜索：URL 命中保留、无命中的被过滤', async ({ page, app }) => {
        await app.newCollection('basic')
        await openCollection(page, app)

        // ping/pong 的名称都不含 /json —— 两条都在 = URL 索引命中生效
        await searchBox(page).fill('/json')
        await expect(page.getByTestId('tree.row').filter({ hasText: 'ping' })).toBeVisible()
        await expect(page.getByTestId('tree.row').filter({ hasText: 'pong' })).toBeVisible()

        // flat 只是 /json/flat 的段：只命中 ping，pong 被过滤
        await searchBox(page).fill('flat')
        await expect(page.getByTestId('tree.row').filter({ hasText: 'ping' })).toBeVisible()
        await expect(page.getByTestId('tree.row').filter({ hasText: 'pong' })).toHaveCount(0)

        // 名称与 URL 都不命中 → 请求行消失
        await searchBox(page).fill('zzz-nothing')
        await expect(page.getByTestId('tree.row').filter({ hasText: 'ping' })).toHaveCount(0)
    })

    test('[U2] 顶部搜索面板：输入 URL 段命中对应请求', async ({ page, app }) => {
        await app.newCollection('basic')
        await openCollection(page, app)
        await openPalette(page)

        // nested 只出现在 pong 的 URL（{{host}}/json/nested）；NInput testid 在外层，打内层 input
        await page.getByTestId('palette.input').locator('input').fill('nested')
        const items = page.getByTestId('palette.item')
        await expect(items).toHaveCount(1)
        await expect(items.first()).toContainText('pong')
        // 结果行展示命中请求的 URL（可解释「为什么命中」）
        await expect(items.first().getByTestId('palette.itemUrl')).toContainText('/json/nested')
    })

    test('[U3] 集合搜索框有放大镜图标', async ({ page, app }) => {
        await app.newCollection('basic')
        await openCollection(page, app)
        await expect(page.getByTestId('sidebar.searchIcon')).toBeVisible()
    })

    test('[U4] 标签页不展示 HTTP 状态码（发送成功后）', async ({ page, app }) => {
        await app.newCollection('basic')
        await openCollection(page, app)
        await openRequest(page, 'ping')
        await send(page)
        // 响应确实回来了（状态面板可见 200），此时标签上也不应出现状态码
        await expect(page.getByTestId('resp.status')).toContainText('200')

        const tab = page.getByTestId('tab.item').filter({ hasText: 'ping' })
        await expect(tab).toBeVisible()
        await expect(tab.locator('.st')).toHaveCount(0)
        await expect(tab).not.toContainText('200')
    })
})
