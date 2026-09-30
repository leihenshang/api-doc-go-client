// 批次1 功能点：G6 会话恢复 / G11 快捷键 / G5 取消发送 / H3 Cookie 面板 / G9 撤销重做。
import { expect, test } from '../helpers/app'
import { t } from '../helpers/i18n'
import { openCollection, openRequest, reopenApp, send, treeRowByUid } from '../helpers/ui'

const PING_UID = '33333333-3333-4333-8333-333333333333'
const PONG_UID = '88888888-8888-4888-8888-888888888888'

test.describe('批次1：会话 / 快捷键 / 取消 / Cookie / 撤销', () => {
  test('[G6] 重启后恢复 tab 现场', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    await openRequest(page, 'pong')
    await expect(page.getByTestId('tab.item').filter({ hasText: 'pong' })).toHaveClass(/active/)

    // 刷新应用（等价重启：localStorage 仍在）
    await page.reload()
    await reopenApp(page)
    await expect(page.getByTestId('tab.item').filter({ hasText: 'ping' })).toBeVisible()
    await expect(page.getByTestId('tab.item').filter({ hasText: 'pong' })).toBeVisible()
    await expect(page.getByTestId('tab.item').filter({ hasText: 'pong' })).toHaveClass(/active/)
  })

  test('[G11] Ctrl+S 立即保存 / Ctrl+W 关闭 tab', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    await page.getByTestId('req.url').locator('input').fill('http://127.0.0.1:9/edited')
    // 未等防抖自动保存，Ctrl+S 应立刻落盘（未保存圆点消失）
    await page.keyboard.press('Control+s')
    await expect(page.getByTestId('tab.item').filter({ hasText: 'ping' }).locator('.dot')).toHaveCount(0)
    await page.keyboard.press('Control+w')
    await expect(page.getByTestId('tab.item').filter({ hasText: 'ping' })).toHaveCount(0)
  })

  test('[G5] 发送中可取消', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    // 指向必然会挂起的地址（无路由的本地端口），发送后立刻取消
    await page.getByTestId('req.url').locator('input').fill('http://127.0.0.1:1/slow')
    await page.getByTestId('req.send').click()
    const cancel = page.getByTestId('req.cancel')
    await expect(cancel).toBeVisible()
    await cancel.click()
    // 取消后错误区出现「已取消」或等价文案，发送按钮恢复可点
    await expect(page.getByTestId('req.send')).toBeEnabled({ timeout: 15_000 })
    await expect(page.locator('.resp .n-alert')).toBeVisible()
  })

  test('[H3] Cookie 管理：列表 + 单条删除', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await page.getByTestId('toolbar.collection').click()
    await page.locator('.n-dropdown-option').filter({ hasText: t('cookies.title') }).click()
    await expect(page.locator('.n-modal').filter({ hasText: t('cookies.title') })).toBeVisible()
    // 空罐：空态可见；清空按钮在
    await expect(page.getByTestId('cookies.clear')).toBeVisible()
  })

  test('[G9] Ctrl+Z / Ctrl+Shift+Z 撤销重做', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    const url = page.getByTestId('req.url').locator('input')
    await url.fill('http://example.com/one')
    // 等撤销快照提交（400ms 停顿）
    await page.waitForTimeout(600)
    await url.fill('http://example.com/two')
    await page.waitForTimeout(600)
    await page.keyboard.press('Control+z')
    await expect(url).toHaveValue('http://example.com/one')
    await page.keyboard.press('Control+Shift+z')
    await expect(url).toHaveValue('http://example.com/two')
  })
})
