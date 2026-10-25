// 集合树不再展示 Resp 合成节点（实际问题记录 item 7：入口收敛到请求编辑器「响应字段」分段），
// 以及 Docs「生成接口文档」结果弹窗（预览/编辑 + 覆盖/追加/下载）+ 响应字段进文档。
import { patchRequest } from '../helpers/api'
import { expect, test } from '../helpers/app'
import { readCollectionFile } from '../helpers/fs'
import { t } from '../helpers/i18n'
import { openCollection, openReqTab, openRequest, reopenApp, saveNow, send } from '../helpers/ui'

const PING_UID = '33333333-3333-4333-8333-333333333333'

/**
 * 只改 UI 里的 URL（不再用 IPC 直接改盘：手动保存模型下，未保存的编辑会让「外部写盘」变成冲突，
 * Ctrl+S 会被拒写）。send 读的是内存模型，UI 填好即可。
 */
async function sendUrl(page: import('@playwright/test').Page, app: import('../helpers/app').AppFixture, url: string) {
  void app
  await page.getByTestId('req.url').fill(url)
  await page.getByTestId('req.url').blur()
  await send(page)
}

test.describe('Resp 节点已移出集合树 + Docs 生成', () => {
  test('生成字段表后侧栏不出现 Resp 节点、编辑器切「响应字段」分段；生成文档的覆盖确认与字段进文档照旧', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')

    // 初始没有 Resp 节点
    await expect(page.getByTestId('tree.resp.name')).toHaveCount(0)

    await sendUrl(page, app, '{{host}}/json/flat')
    await openReqTab(page, 'resp')
    await page.getByTestId('resp.generateLive').click()
    // 集合树不再合成 Resp 子节点（响应字段只在编辑器分段维护）
    await expect(page.getByTestId('tree.resp.name')).toHaveCount(0)
    // 生成后编辑器停在「响应字段」分段并渲染字段表
    await expect(page.getByTestId('req.tab').filter({ hasText: t('editor.respFields') })).toHaveClass(/on/)
    await expect(page.getByTestId('resp.fields.row').first()).toBeVisible()

    // item 8：子页签切换「响应数据 / 响应头」。
    // 「从本次响应生成」会把本次响应的 status/headers 记进 respdoc（响应头快照 + 状态徽标的数据源），
    // 但「响应数据」（sample）保持原样，不会被响应体填充。
    await page.locator('[data-testid="resp.segTab"][data-view="body"]').click()
    await expect(page.getByTestId('resp.dataInput').locator('textarea')).toHaveValue('')
    await expect(page.locator('[data-testid="resp.segTab"][data-view="body"]')).toContainText('200')
    await page.locator('[data-testid="resp.segTab"][data-view="headers"]').click()
    await expect(page.getByTestId('resp.segHeaderRow').filter({ hasText: 'Content-Type' }).first()).toBeVisible()

    // 手动保存：生成/编辑只改内存，先落盘再走「关闭标签 → 重开读盘」的验证
    await saveNow(page)

    // item 18 回归：关闭标签再打开（从磁盘重读），字段表（respdoc）与响应头快照仍在
    await page.getByTestId('tab.close').click()
    // 标签若因改过 URL 是脏态，会弹自绘的「未保存询问」框：选「放弃更改」
    // （respdoc 已在上一步保存落盘，不受影响）
    const discard = page.getByTestId('closeask.discard')
    if (await discard.isVisible().catch(() => false)) await discard.click()
    await page.locator('[data-testid="tree.row"][data-kind="request"]').first().click()
    await page.getByTestId('req.tab').filter({ hasText: t('editor.respFields') }).click()
    await page.locator('[data-testid="resp.segTab"][data-view="fields"]').click()
    await expect(page.getByTestId('resp.fields.row').first()).toBeVisible()
    await page.locator('[data-testid="resp.segTab"][data-view="headers"]').click()
    await expect(page.getByTestId('resp.segHeaderRow').filter({ hasText: 'Content-Type' }).first()).toBeVisible()

    // 必填列进文档：取消 flat 的必填 → 文档里该行必选写「否」
    // （字段表只在「字段表格」子页签渲染，先切回来）
    await page.locator('[data-testid="resp.segTab"][data-view="fields"]').click()
    await page
      .locator('[data-testid="resp.fields.row"][data-path="flat"]')
      .getByTestId('resp.fields.required')
      .click()
    await page.waitForTimeout(700)

    // 切换请求编辑器到 Docs 分段：点「生成接口文档」→ 弹出结果对话框（预览态含生成内容）
    await openReqTab(page, 'docs')
    await page.getByTestId('docs.gen').click()
    const dlg = page.locator('.n-modal').filter({ hasText: t('docs.gen') })
    await expect(dlg).toBeVisible()
    await expect(page.getByTestId('docs.genPreview')).toContainText(t('docs.resFields'))
    // 响应字段进了「响应字段」章节（断言字段路径 + 必选按 required 输出）
    await expect(page.getByTestId('docs.genPreview')).toContainText('value')
    await expect(page.getByTestId('docs.genPreview')).toContainText(t('docs.requiredNo'))
    // 切到编辑态：可编辑（md-editor 容器 testid 存在）后回到预览
    await page.locator('[data-testid="docs.mode"][data-mode="edit"]').click()
    await expect(page.getByTestId('docs.genEditor')).toBeVisible()
    await page.locator('[data-testid="docs.mode"][data-mode="preview"]').click()
    await expect(page.getByTestId('docs.genPreview')).toBeVisible()

    // 选「完全覆盖」→ 关闭对话框，主区预览显示生成内容
    await page.getByTestId('docs.dlgOverwrite').click()
    await expect(page.getByTestId('docs.genDialog')).toHaveCount(0)
    await expect(page.locator('.md-preview').first()).toContainText(t('docs.resFields'))

    // 已有内容：再生成 → 选「放到已有文档下方」（追加）；主区仍含生成章节，且原内容未被清掉
    await page.getByTestId('docs.gen').click()
    await page.getByTestId('docs.dlgAppend').click()
    await expect(page.getByTestId('docs.genDialog')).toHaveCount(0)
    await expect(page.locator('.md-preview').first()).toContainText(t('docs.resFields'))

    // 刷新重开：respdoc 仍随请求文件落盘（数据没丢），但树渲染完成后依旧没有 Resp 节点
    await page.reload()
    await reopenApp(page) // 每次重开树默认收起：用例需要请求行，先展开
    await expect(page.getByTestId('tree.row').first()).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('tree.resp.name')).toHaveCount(0)
    expect(readCollectionFile(dir, 'api/ping.yml')).toContain('respdoc')
  })

  // H15 回归：请求同时有查询参数与请求体时，生成的文档两章都要在（旧版缺「请求体」章）。
  test('生成接口文档：参数与请求体并存时两章齐备', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    // 请求体字段表的唯一真源是 body.fields（body.descs 只作旧数据种子，不再参与文档生成）
    await patchRequest(app, PING_UID, {
      params: [{ name: 'page', value: '1', enabled: true }],
      body: {
        type: 'json',
        raw: '{"uid": "u-42", "vip": true}',
        form: [],
        fields: [
          { name: 'uid', type: 'string', required: true, example: 'u-42', description: '用户ID' },
          { name: 'vip', type: 'boolean', required: false, example: 'true', description: '是否 VIP' },
        ],
      },
    })
    await page.reload()
    await reopenApp(page) // 每次重开树默认收起：用例需要请求行，先展开
    await expect(page.getByTestId('tree.row').first()).toBeVisible({ timeout: 10_000 })
    await page.locator('[data-testid="tree.row"][data-kind="request"]').first().click()

    await openReqTab(page, 'docs')
    await page.getByTestId('docs.gen').click()
    const md = page.getByTestId('docs.genPreview')
    await expect(md).toContainText(t('docs.params')) // 参数表仍在
    await expect(md).toContainText('page')
    await expect(md).toContainText(t('docs.body')) // 修复点：请求体章节不再丢
    await expect(md).toContainText('u-42') // 原始体内容进代码块
    await expect(md).toContainText(t('docs.bodyParams')) // JSON 体附字段表（小标题）
    await expect(md).toContainText('用户ID') // body.fields 的「说明」列进表
    await expect(md).toContainText('vip') // 字段名进表
  })
})
