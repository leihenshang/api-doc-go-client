// 集合树不再展示 Resp 合成节点（实际问题记录 item 7：入口收敛到请求编辑器「响应字段」分段），
// 以及 Docs「生成接口文档」覆盖确认 + 响应字段进文档。
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

test.describe('Resp 节点已移出集合树 + Docs 生成', () => {
  test('更新字段后侧栏不出现 Resp 节点、编辑器自动切「响应字段」分段；生成文档的覆盖确认与字段进文档照旧', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')

    // 初始没有 Resp 节点
    await expect(page.getByTestId('tree.resp.name')).toHaveCount(0)

    await sendUrl(page, app, '{{host}}/json/flat')
    await page.getByTestId('resp.updateFields').click()
    // 集合树不再合成 Resp 子节点（响应字段只在编辑器分段维护）
    await expect(page.getByTestId('tree.resp.name')).toHaveCount(0)
    // 替代行为：更新成功后编辑器切到「响应字段」分段并渲染字段表
    await expect(page.getByTestId('req.tab').filter({ hasText: t('editor.respFields') })).toHaveClass(/on/)
    await expect(page.getByTestId('resp.fields.row').first()).toBeVisible()

    // item 8：子页签切换「响应数据 / 响应头」快照（更新字段时一并留存）
    await page.locator('[data-testid="resp.segTab"][data-view="body"]').click()
    await expect(page.getByTestId('resp.segBody')).toContainText('"flat"')
    await page.locator('[data-testid="resp.segTab"][data-view="headers"]').click()
    await expect(page.getByTestId('resp.segHeaderRow').filter({ hasText: 'Content-Type' }).first()).toBeVisible()

    // item 18 回归：关闭标签再打开（从磁盘重读），响应头快照仍在
    await page.getByTestId('tab.close').click()
    // 标签若因改过 URL 是脏态，会弹自绘的「未保存询问」框：选「放弃更改」
    // （respdoc 已由更新字段直接落盘，不受影响）
    const discard = page.getByTestId('closeask.discard')
    if (await discard.isVisible().catch(() => false)) await discard.click()
    await page.locator('[data-testid="tree.row"][data-kind="request"]').first().click()
    await page.getByTestId('req.tab').filter({ hasText: t('editor.respFields') }).click()
    await page.locator('[data-testid="resp.segTab"][data-view="headers"]').click()
    await expect(page.getByTestId('resp.segHeaderRow').filter({ hasText: 'Content-Type' }).first()).toBeVisible()

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

    // 刷新重开：respdoc 仍随请求文件落盘（数据没丢），但树渲染完成后依旧没有 Resp 节点
    await page.reload()
    await expect(page.getByTestId('tree.row').first()).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('tree.resp.name')).toHaveCount(0)
    expect(readCollectionFile(dir, 'api/ping.yml')).toContain('respdoc')
  })
})
