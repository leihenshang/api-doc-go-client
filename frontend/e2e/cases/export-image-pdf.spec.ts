// 导出：HTML 把集合图片内联成 data URL（单文件）；PDF 走「打印视图 + 系统打印」。
import { existsSync, readFileSync } from 'node:fs'
import path from 'node:path'
import { expect, test } from '../helpers/app'
import { openCollection } from '../helpers/ui'

/** 1×1 PNG（内联测试用的最小合法图片）。 */
const PNG_1X1 =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=='

test.describe('导出：图片内嵌与 PDF', () => {
  test('导出 HTML 把集合图片内联成 data URL', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    // 存一张图，并把引用（/doc-assets/<hash>）写进文档正文
    const url = await app.ipc<string>('SaveDocImage', PNG_1X1)
    const doc = await app.ipc<{ uid: string; name: string; path: string; content: string }>('CreateDoc', 'pic', 'api')
    doc.content = `# 图\n\n![示例](${url})`
    await app.ipc('SaveDoc', doc)

    await openCollection(page, app)
    await page.getByTestId('overview.exportHtml').click()
    await page.getByTestId('export.confirm').click()

    const file = path.join(dir, 'e2e-basic.html')
    await expect.poll(() => existsSync(file)).toBe(true)
    const html = readFileSync(file, 'utf8')
    // 图片已内联，产物是单个文件
    expect(html).toContain('data:image/png;base64,')
    // 原来的 /doc-assets 地址应被替换（外部打开才不会破图）
    expect(html).not.toContain(url)
  })

  test('导出 PDF：生成打印视图，内容完整且带打印样式', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    // headless 下别真去调打印对话框：桩掉 window.print（对新建 iframe 同样生效）
    await page.addInitScript(() => {
      window.print = () => undefined
    })

    await page.getByTestId('overview.exportPdf').click()
    await page.getByTestId('export.confirm').click()

    const frame = page.locator('#app-print-frame')
    await expect(frame).toHaveCount(1)
    const html = await frame.evaluate(
      (el) => (el as HTMLIFrameElement).contentDocument?.documentElement.outerHTML ?? '',
    )
    expect(html).toContain('e2e-basic')
    expect(html).toContain('@media print')
  })
})
