// 对应 doc §5 回归要点 3（round-trip 未知字段）、4（自写回环）、5（树操作）、16（参数表/Bulk Edit/地址栏）。
import { existsSync, readdirSync } from 'node:fs'
import path from 'node:path'
import { expect, test } from '../helpers/app'
import { hoverAndClick } from '../helpers/dom'
import { readCollectionFile } from '../helpers/fs'
import { t } from '../helpers/i18n'
import { openCollection, openReqTab, openRequest, pickMethod, treeRow, treeRowByUid } from '../helpers/ui'

/** naive popconfirm 的确认按钮 */
const confirmButton = (page: import('@playwright/test').Page) => page.locator('.n-popconfirm .n-button--primary-type')

// 种子里的固定 uid（basic 集合）
const EMPTY_UID = '55555555-5555-4555-8555-555555555555'
const API_UID = '22222222-2222-4222-8222-222222222222'
const PING_UID = '33333333-3333-4333-8333-333333333333'

test.describe('集合文件层与会话', () => {
  test('[B2] Bruno 超集样例：改 URL 保存后未知字段不丢 @smoke', async ({ page, app }) => {
    const dir = await app.newCollection('bruno-sample')
    await openCollection(page, app)
    await openRequest(page, '用户-列表')

    const url = page.getByTestId('req.url')
    await expect(url).toHaveValue('{{host}}/json/flat')
    await url.fill('{{host}}/json/nested')
    await url.blur()

    const file = 'account/用户-列表.yml'
    await expect.poll(() => readCollectionFile(dir, file), { timeout: 10_000 }).toContain('/json/nested')
    const text = readCollectionFile(dir, file) ?? ''
    for (const key of ['auth: inherit', 'vars:', 'script:', 'assert:', 'traceId', 'bru.setVar', '状态码为 200', 'maxRedirects']) {
      expect(text, `未知字段 ${key} 不应丢失`).toContain(key)
    }
  })

  test('[B3] 自写回环：文件落盘后编辑器内容不被覆盖', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')

    const url = page.getByTestId('req.url')
    const typed = '{{host}}/json/nested?probe=1'
    await url.fill(typed)
    await url.blur()
    await expect.poll(() => readCollectionFile(dir, 'api/ping.yml'), { timeout: 10_000 }).toContain('probe=1')

    // 等一轮文件监听/自动重载，确认编辑器内容与光标输入没有被回写覆盖
    await page.waitForTimeout(1_500)
    await expect(url).toHaveValue(typed)
    expect(readCollectionFile(dir, 'api/ping.yml') ?? '').toContain('probe=1')
  })

  test('[B4] 树操作：空分组可见 / 重命名不动目录与 uid / 非空分组拒删 / 删除进 .trash', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)

    // 空分组必须可见
    await expect(treeRow(page, 'empty')).toBeVisible()

    // 重命名：只改显示名，目录名与 uid 不变（行内进入编辑态后名称不在文本里，故全程按 uid 定位）
    const emptyRow = treeRowByUid(page, EMPTY_UID)
    await hoverAndClick(emptyRow, 'tree.row.rename')
    const renameInput = emptyRow.locator('input')
    await renameInput.fill('empty-renamed')
    await renameInput.press('Enter')
    await expect(treeRow(page, 'empty-renamed')).toBeVisible()
    await expect(treeRowByUid(page, EMPTY_UID)).toHaveAttribute('data-uid', EMPTY_UID)
    expect(existsSync(path.join(dir, 'empty/folder.yml'))).toBe(true)
    expect(existsSync(path.join(dir, 'empty-renamed'))).toBe(false)
    expect(readCollectionFile(dir, 'empty/folder.yml') ?? '').toContain('empty-renamed')

    // 非空分组（api 内有 ping）拒绝删除
    await hoverAndClick(treeRowByUid(page, API_UID), 'tree.row.delete')
    await confirmButton(page).click()
    await expect(treeRow(page, 'api')).toBeVisible()
    expect(existsSync(path.join(dir, 'api/folder.yml'))).toBe(true)

    // 删除请求 → 移入 .trash，树中消失，且校验接口不再返回
    await hoverAndClick(treeRowByUid(page, PING_UID), 'tree.row.delete')
    await confirmButton(page).click()
    await expect(treeRow(page, 'ping')).toHaveCount(0)
    expect(existsSync(path.join(dir, 'api/ping.yml'))).toBe(false)
    expect(readdirSync(path.join(dir, '.trash')).length).toBeGreaterThan(0)
  })

  test('[E16] 参数表说明列 round-trip / Bulk Edit 切换 / 地址栏变量与配色', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    await openReqTab(page, 'params')

    const row = page.getByTestId('kv.row').first()
    await row.getByTestId('kv.name').locator('input').fill('probe')
    await row.getByTestId('kv.value').locator('input').fill('1')
    await row.getByTestId('kv.desc').locator('input').fill('参数说明')
    await expect.poll(() => readCollectionFile(dir, 'api/ping.yml'), { timeout: 10_000 }).toContain('参数说明')

    // 多行批量编辑往返：同名行的「说明」应保留
    await page.getByTestId('kv.bulk').click()
    await page.getByTestId('kv.bulkText').locator('textarea').fill('probe: 1')
    await page.getByTestId('kv.bulk').click()
    await expect(page.getByTestId('kv.row').first().getByTestId('kv.desc').locator('input')).toHaveValue('参数说明')

    // 地址栏：{{变量}} 显示主色底纹
    await expect(page.locator('.req-line .hl .var').first()).toHaveText('{{host}}')

    // 方法选择器底色随方法变化（GET → POST）
    const tintOf = (sel: string) =>
      page.locator(sel).evaluate((el) => getComputedStyle(el).getPropertyValue('--m-tint').trim())
    const getTint = await tintOf('.req-line')
    await pickMethod(page, 'POST')
    const postTint = await tintOf('.req-line')
    expect(postTint).not.toBe(getTint)
    expect(postTint).toBe(await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--app-method-post-tint').trim()))
  })
})
