// 对应 doc §5 回归要点 6、8、9、10、11、12、13、14、15：
// 外壳常驻 / 概览页 / 状态栏计数 / tab↔树联动 / 设计令牌 / 内置字体 /
// 弹窗不挤布局（含缩放）/ 分栏拖动与布局切换 / 命令面板。
import type { Page } from '@playwright/test'
import { expect, test } from '../helpers/app'
import { listCollectionFiles } from '../helpers/fs'
import { t } from '../helpers/i18n'
import {
  openCollection,
  openRequest,
  openSettings,
  openSettingsSection,
  reopenApp,
  saveSettings,
  treeRow,
  treeRowByUid,
} from '../helpers/ui'

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

  test('[A4] 弹窗打开不挤压布局：缩放 100% 与 150% 下各状态尺寸一致', async ({ page, app }) => {
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

      // 新建请求已改为「未落盘草稿 tab」（不再弹对话框），且侧栏「＋」变成了协议下拉：
      // 原来这条「点新建 → 期望弹窗」的断言随交互变更失效，已移除（新建流程另开用例覆盖）。
      // 这里保留其余弹窗（设置 / 历史）的布局基线比较。

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
    expect(splitterWidth).toBeGreaterThanOrEqual(8) // 8px + 两侧 1px 描边（曾按 design-spec 的 w28）
    expect(splitterWidth).toBeLessThanOrEqual(10)
    const box = (await splitter.boundingBox())!
    // 布局切换已从分隔区挪到标题栏（TitleBar 的 .layouts）：中缝没有任何按钮热区，整条都可拖动
    await expect(splitter.locator('button')).toHaveCount(0)
    await expect(page.getByTestId('titlebar.layoutRight')).toBeVisible()
    await page.mouse.move(box.x + box.width / 2, box.y + 20)
    await page.mouse.down()
    await page.mouse.move(box.x + box.width / 2 - 140, box.y + 20, { steps: 10 })
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

    // 标题栏按钮切到「上下布局」：请求区在上、响应区在下，且不产生横向溢出
    await page.getByTitle(t('editor.layoutVertical')).click()
    await expect(page.locator('.work')).toHaveClass(/bottom/)
    await expect(page.getByTestId('titlebar.layoutBottom')).toHaveClass(/on/) // 激活态跟着走
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

  // 未保存的新建请求（草稿）没有磁盘副本：Ctrl+S 应该直接开「保存请求」框落盘，且保存后页签留在原地
  test('[G11] Ctrl+S 保存未落盘的草稿请求', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)

    await page.keyboard.press('Control+n') // 新建（不打扰：不弹框）
    await expect(page.getByTestId('draft.name')).toHaveCount(0)

    // Ctrl+S：这次是「只保存」模式，所以没有「不保存」按钮
    await page.keyboard.press('Control+s')
    await expect(page.getByTestId('draft.name')).toBeVisible()
    await expect(page.getByTestId('draft.discard')).toHaveCount(0)

    await page.getByTestId('draft.name').locator('input').fill('ctrl-s-request')
    await page.getByTestId('draft.save').click()

    // 落盘为集合内请求文件；对话框关闭、页签转正并留在原地（侧栏出现该请求）
    await expect.poll(() => listCollectionFiles(dir).some((p) => p.endsWith('ctrl-s-request.yml'))).toBe(true)
    await expect(page.getByTestId('draft.name')).toHaveCount(0)
    await expect(treeRow(page, 'ctrl-s-request')).toHaveCount(1)
    await expect(page.getByTestId('tab.item').filter({ hasText: 'ctrl-s-request' })).toBeVisible()
  })

  // tab 栏右键菜单：关闭当前 / 左侧 / 右侧 / 全部、复制新建、保存所有。
  // 复制新建走「未落盘草稿」路径（名字加「副本」），批量关闭时草稿要被跳过而不是静默丢掉。
  test('[A7] tab 右键菜单：关闭左侧/右侧/全部、复制新建、保存所有', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    await openRequest(page, 'pong')
    await expect(page.getByTestId('tab.item')).toHaveCount(2)

    const tab = (name: string) => page.getByTestId('tab.item').filter({ hasText: name })
    const option = (label: string) => page.locator('.n-dropdown-option').filter({ hasText: label })
    const tabs = page.getByTestId('tab.item')

    // 菜单 6 项；最左的页签没有「关闭左侧所有」（禁用）
    await tab('ping').click({ button: 'right' })
    await expect(option(t('tab.closeCurrent'))).toBeVisible()
    await expect(option(t('tab.closeAll'))).toBeVisible()
    await expect(page.locator('.n-dropdown-option-body--disabled').filter({ hasText: t('tab.closeLeft') })).toHaveCount(1)

    // 关闭右侧所有：ping 在最左，右边只剩 pong
    await option(t('tab.closeRight')).click()
    await expect(tabs).toHaveCount(1)
    await expect(tab('pong')).toHaveCount(0)

    // 复制新建：开一张未落盘草稿（名字带「副本」），关闭时走保存框
    await tab('ping').click({ button: 'right' })
    await option(t('tab.duplicate')).click()
    await expect(tabs).toHaveCount(2)
    await expect(tabs.filter({ hasText: t('tab.duplicateName', { name: 'ping' }) })).toBeVisible()
    await page.getByTestId('tab.close').last().click()
    await expect(page.getByTestId('draft.name')).toBeVisible() // 草稿：先问保存
    await page.getByTestId('draft.discard').click()
    await expect(tabs).toHaveCount(1)

    // 保存所有：改一下地址（产生脏改动）→ 右键「保存所有」→ 提示 + 脏点消失
    await tab('ping').click()
    await page.getByTestId('req.url').fill('{{host}}/json/flat')
    await page.getByTestId('req.url').blur()
    await expect(tab('ping').locator('.dot')).toHaveCount(1)
    await tab('ping').click({ button: 'right' })
    await option(t('tab.saveAll')).click()
    await expect(page.locator('.n-message').last()).toContainText(t('tab.savedAll'))
    await expect(tab('ping').locator('.dot')).toHaveCount(0)

    // 关闭全部：已落盘的关掉，**草稿跳过**并提示
    await page.keyboard.press('Control+n')
    await expect(tabs).toHaveCount(2)
    await tab('ping').click({ button: 'right' })
    await option(t('tab.closeAll')).click()
    await expect(page.locator('.n-message').last()).toContainText(t('tab.closedSkipDraft', { n: 1 }))
    await expect(tabs).toHaveCount(1) // 只剩那张草稿
    await expect(tabs.locator('.dot')).toHaveCount(1)
    await expect(page.getByTestId('tab.overview')).toBeVisible()
  })
  // 设置弹窗改为「左侧竖向分区菜单 + 右侧只显示当前分区」，这里钉住两件事：
  // ① 分区可切换且只有当前分区可见（字段散落在不同分区时别把人绕进去）
  // ② MCP 服务分区能启用内嵌服务并回显运行状态（设置 → 后端起服务这条链路）
  test('[G12] 设置分区菜单切换，MCP 分区可启用内嵌服务', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openSettings(page)

    // ① 四个分区都在左侧菜单里，右侧默认只显示第一个分区
    for (const key of ['appearance', 'network', 'local', 'mcp'] as const) {
      await expect(page.getByTestId(`settings.nav.${key}`)).toBeVisible()
    }
    await expect(page.getByTestId('settings.pane.appearance')).toBeVisible()
    await expect(page.getByTestId('settings.pane.network')).toBeHidden()
    await expect(page.getByTestId('settings.lang')).toBeVisible()
    await expect(page.getByTestId('settings.timeout')).toBeHidden() // 网络分区此刻不该出现

    // 切到网络分区：网络字段出现、界面字段消失、菜单项高亮
    await openSettingsSection(page, 'network')
    await expect(page.getByTestId('settings.timeout')).toBeVisible()
    await expect(page.getByTestId('settings.lang')).toBeHidden()
    await expect(page.getByTestId('settings.nav.network')).toHaveClass(/on/)

    // ② MCP 分区：启用 + 只读 + 保存 → 后端起服务，状态回显「运行中」与连接地址
    await openSettingsSection(page, 'mcp')
    await page.getByTestId('settings.mcpEnabled').check()
    await page.getByTestId('settings.mcpReadOnly').check() // 默认就是只读，显式确认
    await saveSettings(page)

    await openSettings(page)
    await openSettingsSection(page, 'mcp')
    await expect(page.getByTestId('settings.mcpRunning')).toBeVisible()
    await expect(page.getByTestId('settings.mcpRunning')).toContainText('/mcp')
    const token = await page.getByTestId('settings.mcpToken').locator('input').inputValue()
    expect(token).toHaveLength(32) // 后端自动生成并回填
  })
})
