import { Buffer } from 'node:buffer'
import { readFileSync } from 'node:fs'
import path from 'node:path'
import { expect, test } from '../helpers/app'
import { openCollection, treeRow } from '../helpers/ui'

/**
 * Markdown 本地图片（方案 A）：
 * 编辑器选图/粘贴 → App.SaveDocImage 落 <集合>/assets/<sha256> → 插入 /doc-assets/<hash>；
 * devserver 与桌面壳用同一个只读路由提供图片，预览可直接显示（离线可用）。
 */
const PNG_1PX =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVR4nGNgAAIAAAUAAXpeqz8AAAAASUVORK5CYII='

test.describe('Markdown 本地图片', () => {
  test('[B24] 保存到 assets、可预览、同图去重、拒绝非图片', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)

    // ① 保存：返回 /doc-assets/<sha256>，文件落在集合 assets/
    const url = await app.ipc<string>('SaveDocImage', PNG_1PX)
    expect(url).toMatch(/^\/doc-assets\/[0-9a-f]{64}$/)
    const hash = url.split('/').pop() as string
    const saved = readFileSync(path.join(dir, 'assets', hash))
    expect(saved.length).toBe(Buffer.from(PNG_1PX, 'base64').length)

    // ② 只读路由：devserver 与桌面壳同一处理器，Content-Type 来自文件头嗅探
    const res = await page.request.get(app.url + url)
    expect(res.status()).toBe(200)
    expect(res.headers()['content-type']).toContain('image/png')
    expect(res.headers()['x-content-type-options']).toBe('nosniff')
    expect((await res.body()).length).toBe(saved.length)

    // ③ 写进文档 → 打开文档页签，预览里的图片能真正加载
    const created = await app.ipc<{ uid: string; path: string }>('CreateDoc', '带图文档', 'api')
    await app.ipc('SaveDoc', {
      uid: created.uid,
      name: '带图文档',
      path: created.path,
      content: `# 图\n\n![示意](${url})\n`,
    })
    await openCollection(page, app)
    await treeRow(page, '带图文档').first().click()
    const img = page.locator(`.md-editor-preview img[src="${url}"]`).first()
    await expect(img).toBeVisible()
    await expect
      .poll(async () => img.evaluate((el) => (el as HTMLImageElement).naturalWidth))
      .toBeGreaterThan(0)

    // ④ data URL 前缀等价、同图去重
    expect(await app.ipc<string>('SaveDocImage', `data:image/png;base64,${PNG_1PX}`)).toBe(url)

    // ⑤ SVG 明确拒绝（同源下可执行脚本，不进白名单）
    const svg = Buffer.from('<svg xmlns="http://www.w3.org/2000/svg"/>').toString('base64')
    await expect(app.ipc('SaveDocImage', svg)).rejects.toThrow(/不支持/)
    // 非法 base64
    await expect(app.ipc('SaveDocImage', '!!!not-base64!!!')).rejects.toThrow(/解码失败/)
  })

  test('[B24] 编辑器上传入口：选文件后插入 /doc-assets 引用', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    const created = await app.ipc<{ uid: string; path: string }>('CreateDoc', '选图测试', 'api')
    await app.ipc('SaveDoc', { uid: created.uid, name: '选图测试', path: created.path, content: '' })
    await openCollection(page, app)
    await treeRow(page, '选图测试').first().click()

    // md-editor 工具栏图片下拉里的隐藏 file input：设文件即触发 onUploadImg
    const input = page.locator('.md-editor input[type=file]').last()
    await input.setInputFiles({
      name: 'dot.png',
      mimeType: 'image/png',
      buffer: Buffer.from(PNG_1PX, 'base64'),
    })

    // 保存后文档正文里应带上该 URL（CodeMirror 会把图片语法渲染成占位 widget，
    // 所以不从 DOM 文本断言，直接看落盘结果）
    await page.locator('.md-editor .cm-content').click()
    await page.keyboard.press('Control+S')
    await expect
      .poll(async () => (await app.ipc<{ content: string }>('ReadDoc', created.uid)).content)
      .toMatch(/\/doc-assets\/[0-9a-f]{64}/)
    // 图片文件确实落在集合 assets/
    const doc = await app.ipc<{ content: string }>('ReadDoc', created.uid)
    const m = /\/doc-assets\/([0-9a-f]{64})/.exec(doc.content)
    expect(m).not.toBeNull()
    expect(readFileSync(path.join(dir, 'assets', m?.[1] as string)).length).toBeGreaterThan(0)
  })
})
