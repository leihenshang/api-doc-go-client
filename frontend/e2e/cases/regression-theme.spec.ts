// 对应 doc §5 回归要点 18（主题）、19（窗口圆角）、7（Markdown 预览与转义）与 H11（语言切换）。
import { readFileSync } from 'node:fs'
import path from 'node:path'
import type { Page } from '@playwright/test'
import { expect, test, type AppFixture } from '../helpers/app'
import { t, tEn } from '../helpers/i18n'
import { openCollection, openRequest, openSettings, openReqTab, reopenApp, saveSettings } from '../helpers/ui'

const readConfig = (app: AppFixture): string => {
  try {
    return readFileSync(path.join(app.configDir, 'api-doc-client', 'config.json'), 'utf8')
  } catch {
    return ''
  }
}

const themeOf = (page: Page): Promise<string | null> => page.locator('html').getAttribute('data-theme')

test.describe('主题与多语言', () => {
  test('[H19] 主题三处入口 / 落盘 / 刷新保持', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)

    // ① 标题栏按钮
    await page.getByTestId('titlebar.theme').click()
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
    expect(await page.evaluate(() => localStorage.getItem('client.theme'))).toBe('dark')
    await expect.poll(() => readConfig(app)).toContain('"theme": "dark"')

    // 刷新保持（首帧脚本按缓存上色，不会闪回浅色）
    await page.reload()
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
    await reopenApp(page)

    // ② 设置弹窗
    await openSettings(page)
    await page.getByTestId('settings.theme').click()
    await page.locator('.n-base-select-option').filter({ hasText: t('settings.themeLight') }).click()
    await saveSettings(page)
    expect(await themeOf(page)).toBe('light')

    // 落盘且不覆盖其它字段
    const cfg = readConfig(app)
    expect(cfg).toContain('"theme": "light"')
    for (const key of ['"uiScale"', '"timeoutSec"', '"responseLayout"', '"historyLimit"']) expect(cfg).toContain(key)

    // ③ 命令面板
    await page.keyboard.press('Control+k')
    await page.getByTestId('palette.input').locator('input').fill('主题')
    await expect(page.getByTestId('palette.item')).toHaveCount(1)
    await page.keyboard.press('Enter')
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  })

  test('[H19] 暗色无浅色残留 + 外壳圆角与弹窗遮罩（§5-19）', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    await page.getByTestId('titlebar.theme').click()
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')

    // 外壳：10px 圆角 + 1px 描边 + 正好铺满视口
    const shell = page.locator('.app')
    expect(await shell.evaluate((el) => getComputedStyle(el).borderRadius)).toBe('10px')
    expect(await shell.evaluate((el) => getComputedStyle(el).borderTopWidth)).toBe('1px')
    const box = await shell.evaluate((el) => el.getBoundingClientRect().toJSON())
    const win = await page.evaluate(() => ({ w: window.innerWidth, h: window.innerHeight }))
    expect([Math.round(box.width), Math.round(box.height)]).toEqual([win.w, win.h])

    // 页面自身透明（圆角外才能透出桌面）
    expect(await page.evaluate(() => getComputedStyle(document.body).backgroundColor)).toBe('rgba(0, 0, 0, 0)')
    expect(await page.evaluate(() => getComputedStyle(document.documentElement).backgroundColor)).toBe('rgba(0, 0, 0, 0)')

    // 弹窗遮罩与窗口同半径，弹窗打开时四角仍是圆角
    await openSettings(page)
    expect(await page.locator('.n-modal-mask').evaluate((el) => getComputedStyle(el).borderRadius)).toBe('10px')

    // 暗色下不应再出现浅色令牌底（扫外壳与弹窗内所有元素的计算背景色）
    const lightLeft = await page.evaluate(() => {
      const light = new Set(['rgb(255, 255, 255)', 'rgb(245, 245, 245)', 'rgb(250, 250, 250)', 'rgb(240, 242, 244)', 'rgb(247, 249, 250)'])
      const hits: string[] = []
      for (const el of Array.from(document.querySelectorAll('.app *, .n-modal *'))) {
        const bg = getComputedStyle(el).backgroundColor
        if (light.has(bg)) hits.push(`${el.tagName.toLowerCase()}.${String(el.className).slice(0, 40)} → ${bg}`)
      }
      return Array.from(new Set(hits)).slice(0, 6)
    })
    expect(lightLeft, `暗色下仍有浅色底：${lightLeft.join(' | ')}`).toHaveLength(0)
  })

  test('[H11] 语言切换即时生效 + 外壳无中文渗漏', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)

    await openSettings(page)
    await page.getByTestId('settings.lang').click()
    await page.locator('.n-base-select-option').filter({ hasText: 'English' }).click()
    await saveSettings(page)

    // 即时生效：主题按钮提示、侧栏按钮提示都变英文
    await expect(page.getByTestId('titlebar.theme')).toHaveAttribute('aria-label', tEn('app.themeToDark'))
    await expect(page.locator('.sidebar').getByTitle(tEn('tree.newFolder'))).toBeVisible()

    // 渗漏扫描：标题/占位符/内联文本都不应出现中文（集合与请求名是 ASCII 种子数据）
    const leaked = await page.evaluate(() => {
      const re = /[\u4e00-\u9fa5]/
      const root = document.querySelector('.app') as HTMLElement
      const out: string[] = []
      for (const el of Array.from(root.querySelectorAll('*'))) {
        const title = el.getAttribute('title')
        if (title && re.test(title)) out.push(`title=${title}`)
        const ph = el.getAttribute('placeholder')
        if (ph && re.test(ph)) out.push(`placeholder=${ph}`)
      }
      for (const line of root.innerText.split('\n')) {
        const s = line.trim()
        if (s && re.test(s)) out.push(`text=${s}`)
      }
      return Array.from(new Set(out)).slice(0, 8)
    })
    expect(leaked, `英文界面仍有中文文案：${leaked.join(' | ')}`).toHaveLength(0)
  })

  test('[H13] Markdown 预览渲染 + 原始 HTML 转义（§5-7）', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    await openReqTab(page, 'docs')

    const md = ['## 标题', '', '- 列表项一', '- 列表项二', '', '<script>window.__xss = 1</script>'].join('\n')
    await page.locator('.md-editor .cm-content').click()
    await page.keyboard.type(md)

    // 落盘 round-trip
    await expect.poll(() => readFileSync(path.join(dir, 'api/ping.yml'), 'utf8')).toContain('列表项一')

    // 切「仅预览」：h2/列表正常渲染，且原始 HTML 必须被转义（脚本不执行）
    await page.locator('.md-editor-toolbar-item[title="仅预览"]').click()
    const preview = page.locator('.md-editor-preview')
    await expect(preview).toBeVisible()
    await expect(preview.locator('h2').first()).toHaveText('标题')
    // 预览 DOM 里同一段内容可能出现多份（md-editor 的剪贴板副本），故按文本断言而不是计数
    await expect(preview.getByText('列表项一').first()).toBeVisible()
    await expect(preview.getByText('列表项二').first()).toBeVisible()
    expect(await page.evaluate(() => (window as unknown as Record<string, unknown>).__xss)).toBeUndefined()
    await expect(preview).toContainText('<script>')
  })
})
