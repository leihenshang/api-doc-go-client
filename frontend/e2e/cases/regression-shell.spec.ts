// 对应 doc §5 回归要点 6、8、9、10、11、12、13、14、15：
// 外壳常驻 / 概览页 / 状态栏计数 / tab↔树联动 / 设计令牌 / 内置字体 /
// 弹窗不挤布局（含缩放）/ 分栏拖动与布局切换 / 命令面板。
import type { Page } from '@playwright/test'
import { expect, test } from '../helpers/app'
import { t } from '../helpers/i18n'
import { openCollection, openRequest, openSettings, reopenApp, saveSettings, treeRow, treeRowByUid } from '../helpers/ui'

const PING_UID = '33333333-3333-4333-8333-333333333333'
const PONG_UID = '88888888-8888-4888-8888-888888888888'

/** 关键区域尺寸快照（弹窗挤压/溢出回归用） */
async function layout(page: Page) {
  return page.evaluate(() => {
    const box = (sel: string): [number, number] | null => {
      const el = document.querySelector(sel)
      const b = el?.getBoundingClientRect()
      return b ? [Math.round(b.width), Math.round(b.height)] : null
    }
    return {
      app: box('.app'),
      editor: box('.editor-col'),
      resp: box('.resp-col'),
      overflowX: document.documentElement.scrollWidth - document.documentElement.clientWidth,
    }
  })
}

const cssVar = (page: Page, name: string) =>
  page.evaluate((n) => getComputedStyle(document.documentElement).getPropertyValue(n).trim(), name)

test.describe('外壳、布局与命令面板', () => {
  test('[A1] 外壳常驻 / 概览页 / 状态栏计数 @smoke', async ({ page, app }) => {
    await app.newCollection('basic')

    // 未打开集合时标题栏与主题按钮已在（无边框窗口必须有拖动区与主题入口）
    await page.goto(app.url)
    await expect(page.getByTestId('titlebar.theme')).toBeVisible()
    await page.getByRole('button', { name: t('welcome.open') }).click()
    await expect(page.getByTestId('toolbar.collection')).toBeVisible()

    // 未选中请求时是集合概览页
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('e2e-basic')
    await expect(page.locator('.overview').getByRole('button', { name: t('overview.newRequest') })).toBeVisible()

    // 状态栏计数与集合一致（种子：2 个请求 / 1 个环境）
    const counts = page.getByTestId('statusbar.counts')
    await expect(counts).toContainText(t('status.requests', { n: 2 }))
    await expect(counts).toContainText(t('status.envs', { n: 1 }))
  })

  test('[A2] tab↔树联动：选中态跟随 / 祖先收起后切 tab 自动展开 / 窗口标题', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    expect(await page.title()).toBe('api-doc-go')

    await openRequest(page, 'ping')
    await expect(treeRowByUid(page, PING_UID)).toHaveClass(/on/)

    await openRequest(page, 'pong')
    await expect(treeRowByUid(page, PONG_UID)).toHaveClass(/on/)
    await expect(treeRowByUid(page, PING_UID)).not.toHaveClass(/on/)

    // 收起 api 分组后，切回 ping 的 tab 应自动展开祖先并让该行可见
    await treeRow(page, 'api').locator('button.caret').click()
    await expect(treeRowByUid(page, PING_UID)).toHaveCount(0)
    await page.getByTestId('tab.item').filter({ hasText: 'ping' }).click()
    await expect(treeRowByUid(page, PING_UID)).toBeVisible()
  })

  test('[A3] 设计令牌与内置字体（design-spec §1 / §5-9、10）', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')

    expect(await page.evaluate(() => getComputedStyle(document.body).fontSize)).toBe('13px')
    expect(await cssVar(page, '--app-bg')).toBe('#f5f5f5')
    expect(await cssVar(page, '--app-border')).toBe('#e3e3e3')
    expect(await cssVar(page, '--app-text')).toBe('#202124')
    // 控件圆角 6px（Send 按钮）、面板圆角 10px（外壳，见 §5-19）
    expect(await page.getByTestId('req.send').evaluate((el) => getComputedStyle(el).borderRadius)).toBe('6px')
    expect(await page.locator('.app').evaluate((el) => getComputedStyle(el).borderRadius)).toBe('10px')

    // 内置中文字体生效 + 不合成粗体
    const fontOk = await page.evaluate(() =>
      document.fonts.ready.then(() => document.fonts.check('13px "Noto Sans SC Variable"', '用户管理')),
    )
    expect(fontOk).toBe(true)
    expect(await page.evaluate(() => getComputedStyle(document.body).fontFamily)).toContain('Noto Sans SC Variable')
    expect(await page.evaluate(() => getComputedStyle(document.body).fontSynthesis)).toBe('none')
  })

  test('[A4] 弹窗打开不挤压布局：缩放 100% 与 150% 下五种状态尺寸一致', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')

    const states = async (tag: string) => {
      const base = await layout(page)
      expect(base.overflowX, `${tag}: 不应出现横向溢出`).toBeLessThanOrEqual(0)

      await openSettings(page)
      expect(await layout(page), `${tag}: 设置弹窗不应改变布局`).toEqual(base)
      await page.keyboard.press('Escape')
      await expect(page.getByTestId('settings.theme')).toBeHidden()

      await page.getByTestId('toolbar.collection').click()
      await page.locator('.n-dropdown-option').filter({ hasText: t('history.title') }).click()
      expect(await layout(page), `${tag}: 历史弹窗不应改变布局`).toEqual(base)
      await page.keyboard.press('Escape')
      await expect(page.locator('.n-modal').filter({ hasText: t('history.title') })).toBeHidden()

      await page.locator('.sidebar').getByTitle(t('tree.newRequest')).click()
      await expect(page.locator('.n-modal')).toBeVisible()
      expect(await layout(page), `${tag}: 新建请求弹窗不应改变布局`).toEqual(base)
      await page.getByRole('button', { name: t('common.cancel') }).click()

      expect(await layout(page), `${tag}: 关闭弹窗后布局应回到基线`).toEqual(base)
      return base
    }

    await states('缩放 100%')

    // 切到 150% 缩放后同样成立
    await openSettings(page)
    await page.getByTestId('settings.scale').click()
    await page.locator('.n-base-select-option').filter({ hasText: '150%' }).click()
    await saveSettings(page)
    await states('缩放 150%')
  })

  test('[A5] 分栏拖动 / 松手落盘 / 布局切换 / 无溢出', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')

    const width = (sel: string): Promise<number> =>
      page.locator(sel).evaluate((el) => Math.round(el.getBoundingClientRect().width))
    const before = await width('.resp-col')

    const splitter = page.locator('.splitter')
    const splitterWidth = await splitter.evaluate((el) => Math.round(el.getBoundingClientRect().width))
    expect(splitterWidth).toBeGreaterThanOrEqual(28) // design-spec 的 w28（含 1px 描边）
    expect(splitterWidth).toBeLessThanOrEqual(30)
    const box = (await splitter.boundingBox())!
    // 胶囊位于分隔区正中且 @pointerdown.stop，故要从分隔区空白处（靠近顶端）按下才算拖分隔条
    const startY = box.y + 20
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
    await page.mouse.down()
    await page.mouse.move(box.x + box.width / 2 - 140, box.y + box.height / 2, { steps: 10 })
    await page.mouse.up()
    expect(await width('.resp-col')).toBe(before) // 在胶囊上按下不应触发拖动

    await page.mouse.move(box.x + box.width / 2, startY)
    await page.mouse.down()
    await page.mouse.move(box.x + box.width / 2 - 140, startY, { steps: 10 })
    await page.mouse.up()

    // 分隔区宽度不随拖动变化
    expect(await splitter.evaluate((el) => Math.round(el.getBoundingClientRect().width))).toBe(splitterWidth)

    const after = await width('.resp-col')
    expect(Math.abs(after - before - 140)).toBeLessThan(20) // 拖动 140px ≈ 响应区宽 140px（±1% 量级）

    // 松手落盘：刷新后沿用
    await page.reload()
    await reopenApp(page)
    await openRequest(page, 'ping')
    expect(Math.abs((await width('.resp-col')) - after)).toBeLessThan(12)

    // 胶囊切到「上下布局」：请求区在上、响应区在下，且不产生横向溢出
    await page.getByTitle(t('editor.layoutVertical')).click()
    await expect(page.locator('.work')).toHaveClass(/bottom/)
    const boxes = await page.evaluate(() => {
      const e = document.querySelector('.editor-col')!.getBoundingClientRect()
      const r = document.querySelector('.resp-col')!.getBoundingClientRect()
      return { editorBottom: Math.round(e.bottom), respTop: Math.round(r.top) }
    })
    expect(boxes.respTop).toBeGreaterThanOrEqual(boxes.editorBottom - 2)
    expect(await layout(page).then((l) => l.overflowX)).toBeLessThanOrEqual(0)
  })

  test('[A6] 命令面板：Ctrl+K / 过滤 / 上下键 / 回车执行 / ESC / 输入不落到编辑器', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')

    // 焦点在 URL 输入框时唤起面板，面板内的输入不应落到编辑器
    const url = page.getByTestId('req.url')
    await url.click()
    await url.fill('{{host}}/json/flat')
    await page.keyboard.press('Control+k')
    const palette = page.getByTestId('palette')
    await expect(palette).toBeVisible()

    await page.keyboard.type('pong')
    await expect(url).toHaveValue('{{host}}/json/flat')
    await expect(page.getByTestId('palette.item')).toHaveCount(1)

    const item = page.getByTestId('palette.item').first()
    await expect(item).toHaveClass(/on/) // 单条结果默认选中
    await page.keyboard.press('ArrowDown')
    await expect(item).toHaveClass(/on/)
    await page.keyboard.press('Enter')
    await expect(palette).toHaveCount(0)
    await expect(page.getByTestId('tab.item').filter({ hasText: 'pong' })).toBeVisible()

    // ESC 关闭
    await page.keyboard.press('Control+k')
    await expect(palette).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(palette).toHaveCount(0)
  })
})
