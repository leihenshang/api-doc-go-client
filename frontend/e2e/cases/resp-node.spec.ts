// 侧栏 Resp 子节点（响应字段表入口）与 Docs「生成接口文档」覆盖确认 + 字段进文档。
import { expect, test } from '../helpers/app'
import { readCollectionFile } from '../helpers/fs'
import { t } from '../helpers/i18n'
import { openCollection, openReqTab, openRequest, send } from '../helpers/ui'

const PING_UID = '33333333-3333-4333-8333-333333333333'

async function sendUrl(page: import('@playwright/test').Page, app: import('../helpers/app').AppFixture, url: string) {
  const { patchRequest } = await import('../helpers/api')
  await patchRequest(app, PING_UID, { url })
  await page.getByTestId('req.url').fill(url)
  await page.getByTestId('req.url').blur()
  await send(page)
}

test.describe('侧栏 Resp 节点 + Docs 生成', () => {
  test('更新字段后侧栏出现 Resp；生成接口文档时二次确认且响应字段进入文档', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')

    // 初始没有 Resp 节点
    await expect(page.getByTestId('tree.resp.name')).toHaveCount(0)

    await sendUrl(page, app, '{{host}}/json/flat')
    await page.getByTestId('resp.updateFields').click()
    // 侧栏 Resp 节点出现（响应字段表已随请求文件落盘）
    await expect(page.getByTestId('tree.resp.name')).toBeVisible()

    // 切换请求编辑器到 Docs 分段（空文档直接生成）
    await openReqTab(page, 'docs')
    await page.getByTestId('docs.gen').click()
    // 预览态出现生成内容（无需确认）
    await expect(page.locator('.md-preview').first()).toContainText(t('docs.resFields'))
    // 响应字段进了「响应字段」章节（value 字段说明此前填写过，这里只断言字段路径）
    await expect(page.locator('.md-preview').first()).toContainText('value')

    // 已有内容：再生成必须弹覆盖确认；取消保留原文
    await page.getByTestId('docs.edit').click()
    await page.getByTestId('docs.gen').click()
    const modal = page.locator('.n-modal').filter({ hasText: t('docs.overwriteTitle') })
    await expect(modal).toBeVisible()
    await modal.getByRole('button', { name: t('common.cancel') }).click()
    await expect(modal).toHaveCount(0)

    // 确认后覆盖
    await page.getByTestId('docs.gen').click()
    await expect(modal).toBeVisible()
    await modal.getByRole('button', { name: t('docs.overwriteOk') }).click()
    await expect(modal).toHaveCount(0)

    // 刷新重开：Resp 节点随概要加载（字段在请求文件 respdoc 段）
    await page.reload()
    await expect(page.getByTestId('tree.resp.name')).toBeVisible({ timeout: 10_000 })
    expect(readCollectionFile(dir, 'api/ping.yml')).toContain('respdoc')
  })
})
