import { existsSync, readFileSync } from 'node:fs'
import path from 'node:path'
import { expect, test } from '../helpers/app'
import { openCollection } from '../helpers/ui'

/**
 * H10 导出范围：Markdown / HTML 不再只能整包导出，弹窗里可以只挑部分目录 / 请求 / 文档。
 *
 * 断言落盘产物而不是界面提示：devserver 的 headless 目录就是「另存为」的替代，
 * 文件里到底有什么才是用户拿到手的东西。basic 种子是 api/{ping,pong} + empty/。
 */

/**
 * 读导出的产物；还没写出来时返回空串，配合 expect.poll 等落盘。
 * headless 模式的落点由 fixture 指定（newCollection 里 SetHeadlessDir 设成了集合目录），
 * 所以「另存为」在这里就是直接落在集合根下。
 */
function readOut(collectionDir: string, name: string): string {
  const p = path.join(collectionDir, name)
  return existsSync(p) ? readFileSync(p, 'utf8') : ''
}

/** 勾选树里某个叶子（按显示文本定位）：naive-ui 的复选框容器是 .n-tree-node-checkbox。 */
function leafCheckbox(page: import('@playwright/test').Page, text: string) {
  return page.locator('.n-tree-node').filter({ hasText: text }).locator('.n-tree-node-checkbox')
}

test('[H10] 导出弹窗：只导出所选请求，Markdown 与 HTML 都按范围裁剪', async ({ page, app }) => {
  const collDir = await app.newCollection('basic')
  await openCollection(page, app)

  // ① Markdown：清空后只勾 ping，产物里不该出现 pong
  await page.getByTestId('overview.exportMd').click()
  await expect(page.getByTestId('export.dialog')).toBeVisible()
  await page.getByTestId('export.modePick').click()
  await page.getByTestId('export.clear').click()
  await leafCheckbox(page, 'ping').click()
  await expect(page.getByTestId('export.summary')).toContainText('1 个请求')
  await page.getByTestId('export.confirm').click()
  await expect(page.getByTestId('export.dialog')).toHaveCount(0)

  await expect.poll(() => readOut(collDir, 'e2e-basic.md')).toContain('ping')
  const md = readOut(collDir, 'e2e-basic.md')
  expect(md).not.toContain('pong')

  // ② HTML：同一范围，正文与左侧目录都不该出现未选中的请求
  await page.getByTestId('overview.exportHtml').click()
  await page.getByTestId('export.modePick').click()
  await page.getByTestId('export.clear').click()
  await leafCheckbox(page, 'ping').click()
  await page.getByTestId('export.confirm').click()

  await expect.poll(() => readOut(collDir, 'e2e-basic.html')).toContain('ping')
  const html = readOut(collDir, 'e2e-basic.html')
  expect(html).not.toContain('pong')
  expect(html).toContain('class="toc"')

  // ③ 默认「全部内容」仍是整包：两个请求都要在
  await page.getByTestId('overview.exportMd').click()
  await expect(page.getByTestId('export.modeAll')).toHaveClass(/on/)
  await page.getByTestId('export.confirm').click()

  await expect.poll(() => readOut(collDir, 'e2e-basic.md')).toContain('pong')
  const full = readOut(collDir, 'e2e-basic.md')
  expect(full).toContain('ping')
})
