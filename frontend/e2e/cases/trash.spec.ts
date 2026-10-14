// 回收站（item 20）与请求历史详情（item 19）：
// 删除的文件在底栏回收站可列出并恢复到原位；历史展开详情能看到当时的请求参数与响应。
import { expect, test } from '../helpers/app'
import { hoverAndClick } from '../helpers/dom'
import { t } from '../helpers/i18n'
import { openCollection, openRequest, send, treeRow } from '../helpers/ui'

const PING_UID = '33333333-3333-4333-8333-333333333333'

async function sendUrl(page: import('@playwright/test').Page, app: import('../helpers/app').AppFixture, url: string) {
    const { patchRequest } = await import('../helpers/api')
    await patchRequest(app, PING_UID, { url })
    await page.getByTestId('req.url').fill(url)
    await page.getByTestId('req.url').blur()
    await send(page)
}

/** 删除确认（全局 dialog.warning，按钮文案 common.delete）。 */
const delConfirm = (page: import('@playwright/test').Page) =>
    page.locator('.n-dialog:visible').getByRole('button', { name: t('common.delete') }).last()

test.describe('回收站与历史详情', () => {
    test('[T1] 删除 → 预览 → 下拉选位置恢复 → 再删后彻底删除（需确认）', async ({ page, app }) => {
        await app.newCollection('basic')
        await openCollection(page, app)
        await openRequest(page, 'ping')

        // 删除（进 .trash）
        await hoverAndClick(treeRow(page, 'ping'), 'tree.row.delete')
        await delConfirm(page).click()
        await expect(treeRow(page, 'ping')).toHaveCount(0)

        // 底栏入口打开回收站：应看到刚删的文件
        await page.getByTestId('statusbar.trash').click()
        const modal = page.locator('.n-modal').filter({ hasText: t('trash.title') })
        await expect(modal).toBeVisible()
        const row = page.getByTestId('trash.row').first()
        await expect(row).toBeVisible()
        await expect(row).toContainText('ping')

        // 预览：直接看到被删文件的内容（yml 里有 method: GET）
        await row.getByTestId('trash.preview').click()
        await expect(page.getByTestId('trash.previewBody').first()).toContainText('GET')

        // 恢复：点击后下拉选「原工作区（原路径）」
        await row.getByTestId('trash.restore').click()
        await page.locator('.n-dropdown-option').filter({ hasText: t('trash.targetOriginal') }).click()
        await expect(page.getByTestId('trash.row')).toHaveCount(0)
        await expect(treeRow(page, 'ping')).toBeVisible()
        await page.keyboard.press('Escape')

        // 再删一次 → 彻底删除：必须弹确认框，确认后条目与文件都真正消失
        await hoverAndClick(treeRow(page, 'ping'), 'tree.row.delete')
        await delConfirm(page).click()
        await page.getByTestId('statusbar.trash').click()
        await expect(page.getByTestId('trash.row')).toHaveCount(1)
        await page.getByTestId('trash.remove').first().click()
        const confirmDlg = page
            .locator('.n-dialog:visible')
            .filter({ hasText: t('trash.removeConfirm', { name: 'ping.yml' }) })
        await expect(confirmDlg).toBeVisible()
        await confirmDlg.getByRole('button', { name: t('common.delete') }).click()
        await expect(page.getByTestId('trash.row')).toHaveCount(0)
        await expect(treeRow(page, 'ping')).toHaveCount(0)
        await page.keyboard.press('Escape')
    })

    test('[T2] 历史「详情」展开：当时的请求参数与响应（含响应头）可见', async ({ page, app }) => {
        await app.newCollection('basic')
        await openCollection(page, app)
        await openRequest(page, 'ping')
        // 带 query 的地址：URL→params 联动让快照里确有请求参数；fixture 响应带 Content-Type 头
        await sendUrl(page, app, '{{host}}/json/nested?probe=hl')

        await page.getByTestId('statusbar.history').click()
        await expect(page.locator('.n-modal').filter({ hasText: t('history.title') })).toBeVisible()
        // 重放按钮已下线
        await expect(page.getByTestId('history.replay')).toHaveCount(0)
        await page.getByTestId('history.row').first().getByTestId('history.detail').click()
        const detail = page.getByTestId('history.detailBody').first()
        await expect(detail).toBeVisible()
        await expect(detail).toContainText('probe') // 请求参数
        await expect(detail).toContainText('hl') // 参数值
        await expect(detail).toContainText('Content-Type') // 响应头快照
    })
})
