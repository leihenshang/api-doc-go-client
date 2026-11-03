// B19 文档条目树化 + 页签：树叶子新建 / 开页签 / 编辑保存 / 拖拽改目录。
//
// 覆盖：Go 门面 CreateDoc(folder)/ReadDoc/SaveDocExpectHash/MoveDoc + 前端
// Sidebar doc 行、tabs.openDocTab/saveDoc、useTreeDnd 的 doc 分支。
import { readdirSync, readFileSync } from 'node:fs'
import path from 'node:path'
import { patchSettings } from '../helpers/api'
import { expect, test } from '../helpers/app'
import { t } from '../helpers/i18n'
import { openCollection, openSettings, saveSettings, treeRow, treeRowByUid } from '../helpers/ui'

/** 读集合里某个 .md 的正文（断言保存真的落盘了）。 */
function readMd(dir: string, rel: string): string {
  return readFileSync(path.join(dir, rel), 'utf8')
}

test('文档树叶子：分组内新建 → 开页签 → 编辑保存落盘', async ({ page, app }) => {
  const dir = await app.newCollection('basic')
  await openCollection(page, app)
  await expect(page.locator('body')).toHaveCSS('font-family', /HarmonyOS Sans SC/)

  // 文档树叶子：先确认请求页签显示 HTTP 方法、不显示文档图标
  await treeRow(page, 'ping').click()
  const reqTab = page.getByTestId('tab.item').filter({ hasText: 'ping' })
  await expect(reqTab.locator('.mt')).toHaveText('GET')
  await expect(reqTab.locator('.md-tag')).toHaveCount(0)
  const reqMethod = await reqTab.locator('.mt').boundingBox()
  const reqTitle = await reqTab.locator('.tt').boundingBox()
  expect(reqMethod && reqTitle).toBeTruthy()
  // 方法与标题垂直居中对齐，间距收窄（4px）
  expect(Math.abs(reqMethod!.y + reqMethod!.height / 2 - reqTitle!.y - reqTitle!.height / 2)).toBeLessThanOrEqual(2)
  expect(reqTitle!.x - (reqMethod!.x + reqMethod!.width)).toBeLessThanOrEqual(6)
  // 更严格：直接比较「GET」与中文标题的字形范围中心，而不是元素盒中心
  const glyphCenters = await reqTab.evaluate((el) => {
    const center = (node: Element | null): number => {
      if (!node) return 0
      const range = document.createRange()
      range.selectNodeContents(node)
      const r = range.getBoundingClientRect()
      return r.top + r.height / 2
    }
    return { method: center(el.querySelector('.mt')), title: center(el.querySelector('.tt')) }
  })
  expect(Math.abs(glyphCenters.method - glyphCenters.title)).toBeLessThanOrEqual(1)

  // ① 分组行「＋」菜单 → 新建文档（folder 由所选分组决定）
  const group = treeRow(page, 'api')
  await group.hover()
  await group.getByTestId('tree.row.menu').click()
  await page.getByTestId('tree.row.newDoc').click()
  const input = page.locator('.n-modal input').first()
  await input.fill('接口说明')
  await page.getByRole('button', { name: t('common.confirm') }).click()

  // 树同层出现文档叶子
  const docLeaf = treeRow(page, '接口说明').getByTestId('tree.docRow')
  await expect(docLeaf).toHaveText('MD')
  await expect(docLeaf).toHaveCSS('color', 'rgb(0, 120, 212)')
  await expect(docLeaf).toBeVisible()
  // 树上 MD 与名称的视觉间距应与请求方法一致（按字形右边界到名称左边界计算）
  const treeGaps = await page.evaluate(() => {
    const gapFor = (row: Element | undefined): number => {
      if (!row) return -1
      const prefix = row.querySelector('.mt, .md-tree')
      const name = row.querySelector('.rname')
      if (!prefix || !name) return -1
      const range = document.createRange()
      range.selectNodeContents(prefix)
      return name.getBoundingClientRect().left - range.getBoundingClientRect().right
    }
    const rows = [...document.querySelectorAll('[data-testid="tree.row"]')]
    return {
      req: gapFor(rows.find((r) => r.textContent?.includes('ping'))),
      doc: gapFor(rows.find((r) => r.getAttribute('data-kind') === 'doc')),
    }
  })
  // 树上 MD 与名称的视觉间距：徽标文字在固定 34px 列内**左对齐**后，
  // 间距 = 列宽 − 字形宽 + 1px，因此不再要求「请求与文档的间距相等」
  // （词宽不同自然不同）；只要求都留出可见间距。字形左缘同列由 tree-guides 的 B16 守护。
  expect(treeGaps.req).toBeGreaterThanOrEqual(2)
  expect(treeGaps.doc).toBeGreaterThanOrEqual(8)

  // ② 点叶子 → 开文档页签（Markdown 编辑器可见即已挂载；保存改由工具栏按钮 / Ctrl+S）
  await treeRow(page, '接口说明').click()
  await expect(page.locator('.md-editor').first()).toBeVisible()
  const docTab = page.getByTestId('tab.item').filter({ hasText: '接口说明' })
  await expect(docTab.locator('.md-tag')).toHaveText('MD')
  await expect(docTab.locator('.md-tag')).toHaveCSS('color', 'rgb(0, 120, 212)')
  await expect(docTab.locator('.mt')).toHaveCount(0)
  const docIcon = await docTab.locator('.md-tag').boundingBox()
  const docTitle = await docTab.locator('.tt').boundingBox()
  expect(docIcon && docTitle).toBeTruthy()
  expect(Math.abs(docIcon!.y + docIcon!.height / 2 - docTitle!.y - docTitle!.height / 2)).toBeLessThanOrEqual(2)

  // ③ 编辑正文 → 脏点出现 → Ctrl+S 落盘（poll 文件内容）
  await page.locator('.md-editor textarea, .md-editor .cm-content').first().click()
  await page.keyboard.type('第一版内容')
  await expect(page.getByTestId('tab.item').filter({ hasText: '接口说明' }).locator('.dot')).toHaveCount(1)
  await page.keyboard.press('Control+s')
  await expect
    .poll(() => {
      try {
        return readMd(dir, 'api/接口说明.md').includes('第一版内容')
      } catch {
        return false
      }
    })
    .toBe(true)
})

test('文档自动保存：保存期间的后续编辑不会误标为已保存', async ({ page, app }) => {
  const dir = await app.newCollection('basic')
  await openCollection(page, app)
  await openSettings(page)
  await page.getByTestId('settings.autoSave').click()
  await saveSettings(page)

  const group = treeRow(page, 'api')
  await group.hover()
  await group.getByTestId('tree.row.menu').click()
  await page.getByTestId('tree.row.newDoc').click()
  await page.locator('.n-modal input').first().fill('慢保存')
  await page.getByRole('button', { name: t('common.confirm') }).click()
  await treeRow(page, '慢保存').click()
  await expect(page.locator('.md-editor').first()).toBeVisible()

  let releaseFirst!: () => void
  const firstGate = new Promise<void>((resolve) => { releaseFirst = resolve })
  let markStarted!: () => void
  const firstStarted = new Promise<void>((resolve) => { markStarted = resolve })
  let saves = 0
  await page.route('**/api/App/SaveDocExpectHash', async (route) => {
    saves++
    if (saves === 1) {
      markStarted()
      await firstGate
    }
    await route.continue()
  })

  try {
    const editor = page.locator('.md-editor textarea, .md-editor .cm-content').first()
    await editor.click()
    await page.keyboard.type('第一版')
    await firstStarted
    await page.keyboard.type('第二版')
    await page.waitForTimeout(1000)
    releaseFirst()

    await expect.poll(() => readMd(dir, 'api/慢保存.md')).toContain('第一版第二版')
    await expect(page.getByTestId('tab.item').filter({ hasText: '慢保存' }).locator('.dot')).toHaveCount(0)
    expect(saves).toBeGreaterThanOrEqual(2)
  } finally {
    releaseFirst()
    await page.unroute('**/api/App/SaveDocExpectHash')
  }
})

// 设置 → 界面 → 「Markdown 编辑器默认状态」：只读预览 / 分屏编辑。
// 断言按实际渲染宽度量：previewOnly 下源码列宽为 0、预览列占满；切回后源码列恢复。
test('Markdown 编辑器默认状态：只读预览生效且可临时切回分屏', async ({ page, app }) => {
  await app.newCollection('basic')
  await openCollection(page, app)

  // ① 设置改为只读预览并保存
  await openSettings(page)
  await page.getByTestId('settings.mdEditorMode').click()
  await page.locator('.n-base-select-option').filter({ hasText: t('settings.mdEditorReadonly') }).click()
  await saveSettings(page)

  // ② 新建文档 → 打开页签：应直接是预览态
  const group = treeRow(page, 'api')
  await group.hover()
  await group.getByTestId('tree.row.menu').click()
  await page.getByTestId('tree.row.newDoc').click()
  await page.locator('.n-modal input').first().fill('只读文档')
  await page.getByRole('button', { name: t('common.confirm') }).click()
  await treeRow(page, '只读文档').click()
  await expect(page.locator('.md-editor').first()).toBeVisible()

  /** 当前是哪种态：只读预览只渲染 MdPreview，分屏编辑才有源码列。 */
  const mode = () =>
    page.evaluate(() => ({
      src: document.querySelectorAll('.md-editor-input-wrapper').length,
      preview: document.querySelectorAll('.md-editor-preview, .md-preview').length,
    }))

  await expect.poll(async () => (await mode()).src).toBe(0)
  expect((await mode()).preview).toBeGreaterThan(0)

  // ③ 页签内临时切回分屏：源码列出现、预览仍在
  await page.getByTestId('doc.toggleMode').click()
  await expect.poll(async () => (await mode()).src).toBe(1)
  expect((await mode()).preview).toBeGreaterThan(0)

  // ④ 切回只读预览仍生效（按钮是双向的）
  await page.getByTestId('doc.toggleMode').click()
  await expect.poll(async () => (await mode()).src).toBe(0)

  // 设置是整个 run 共享的一份：改完必须复原，否则后续用例都在「只读预览」下跑
  // （文档页签没有 .md-editor，相关断言会整片超时）
  await patchSettings(app, { mdEditorMode: 'split' })
})

// 接口说明（request.docs）的初始化同样跟随该设置：只读预览直接给渲染结果，「编辑」按钮可临时切回。
test('接口说明默认状态：只读预览下打开 Docs 段不出现源码列', async ({ page, app }) => {
  await app.newCollection('basic')
  await openCollection(page, app)

  await openSettings(page)
  await page.getByTestId('settings.mdEditorMode').click()
  await page.locator('.n-base-select-option').filter({ hasText: t('settings.mdEditorReadonly') }).click()
  await saveSettings(page)

  // 新建的请求页签才会读到新设置（已打开的页签保持原态，符合「默认状态」语义）
  await treeRow(page, 'ping').click()
  await page.locator('[data-testid="req.tab"][data-seg="docs"]').click()

  const srcCount = () => page.locator('.md-editor-input-wrapper').count()
  await expect.poll(srcCount).toBe(0)
  await expect(page.getByTestId('docs.edit')).toHaveText(t('docs.edit'))

  // 「编辑」按钮仍可临时切回分屏
  await page.getByTestId('docs.edit').click()
  await expect.poll(srcCount).toBe(1)

  await patchSettings(app, { mdEditorMode: 'split' }) // 复原共享设置
})

test('文档树叶子：拖动改目录后 uid 不变', async ({ page, app }) => {
  const dir = await app.newCollection('basic')
  await openCollection(page, app)

  // 根级建一篇文档（docs/ 默认位）
  const root = page.getByTestId('tree.root').first()
  await root.getByTestId('tree.root.menu').click()
  await page.getByTestId('tree.row.newDoc').click()
  await page.locator('.n-modal input').first().fill('根级文档')
  await page.getByRole('button', { name: t('common.confirm') }).click()
  await expect(treeRow(page, '根级文档')).toBeVisible()
  // 新建文档会自动开页签并滚动到树行：等 tab 就绪、行滚到可见后再拖，
  // 否则 boundingBox 可能在滚动/布局未稳定时取值，导致从错误坐标起拖（用例侧假失败）。
  await expect(page.getByTestId('tab.item').filter({ hasText: '根级文档' })).toBeVisible()
  const docRow = treeRow(page, '根级文档')
  await docRow.scrollIntoViewIfNeeded()
  await page.waitForTimeout(200) // 等自动滚动/布局落定，避免取到旧坐标
  const uid = await docRow.getAttribute('data-uid')
  const { dragRow } = await import('../helpers/ui')
  await dragRow(page, docRow, treeRow(page, 'api'))
  await expect.poll(() => {
    const files = readdirSync(path.join(dir, 'api')) as string[]
    return files.some((f) => f.endsWith('.md'))
  }).toBe(true)
  await expect(treeRowByUid(page, uid ?? '')).toBeVisible()
})
