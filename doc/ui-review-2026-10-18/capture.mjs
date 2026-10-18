// 本次 UI 评审的截图脚本（存档用，不参与 CI）。复现步骤见同目录 README.md §1.2。
//
//   SHOT_BASE=http://localhost:5275/ SHOT_OUT=./screenshots node capture.mjs
//
// 依赖：Playwright + 完整 chromium（本机 headless shell 在截图阶段必崩，
// 且 /dev/shm 只有 10MB 时渲染进程会 SIGTRAP，必须把 TMPDIR 指到大盘目录）。
import { chromium } from 'playwright'
import fs from 'node:fs'

const BASE = process.env.SHOT_BASE || 'http://127.0.0.1:8198/'
const OUT = process.env.SHOT_OUT || '../.tmp/ui-review/shots'
fs.mkdirSync(OUT, { recursive: true })

const shot = async (page, name) => {
  await page.waitForTimeout(400)
  await page.screenshot({ path: `${OUT}/${name}.png`, fullPage: false })
  console.log('shot', name)
}

// 本机 headless shell 在截图时必崩（raster 阶段），改用完整 chromium 跑 new-headless。
const browser = await chromium.launch({ channel: 'chromium', args: ['--no-sandbox', '--disable-dev-shm-usage', '--disable-gpu'] })
const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2, locale: 'zh-CN' })
page.on('console', m => { if (m.type() === 'error') console.log('PAGE ERROR:', m.text()) })
page.on('pageerror', e => console.log('PAGEERROR:', e.message))
await page.goto(BASE, { waitUntil: 'domcontentloaded' })

// 全新浏览器没有 localStorage 记录，会停在欢迎页：点「打开集合目录…」
// devserver 下 PickDirectory 直接返回 -dir 指定的目录。
const openBtn = page.locator('[data-testid="welcome.open"]')
if (await openBtn.count()) {
  await openBtn.click()
}
await page.waitForSelector('[data-testid="tree.row"]', { timeout: 20000 })
await page.waitForTimeout(600)
await shot(page, '01-collection-tree')

// 2) 请求页：点「获取用户详情」
const reqRow = page.locator('[data-testid="tree.row"]', { hasText: '获取用户详情' }).first()
await reqRow.click()
await page.waitForSelector('[data-testid="req.url"], input[placeholder*="URL"], .req-url', { timeout: 10000 }).catch(() => {})
await page.waitForTimeout(700)
await shot(page, '02-request')

// 2b) 真实响应：健康检查（打本地 /echo）
const healthRow = page.locator('[data-testid="tree.row"]', { hasText: '健康检查' }).first()
await healthRow.click()
await page.waitForTimeout(500)
const sendBtn = page.getByRole('button', { name: /Send|发送/ }).first()
if (await sendBtn.count()) {
  await sendBtn.click()
  await page.waitForTimeout(1500)
  await shot(page, '02b-response')
}

// 2c) 命令面板
await page.keyboard.press('Control+k')
await page.waitForTimeout(400)
await shot(page, '06-palette')
await page.keyboard.press('Escape')
await page.waitForTimeout(300)

// 3) 文档页：点「接入指南」，再尝试进入编辑态
const docRow = page.locator('[data-testid="tree.docRow"], [data-testid="tree.row"]', { hasText: '接入指南' }).first()
await docRow.click()
await page.waitForTimeout(900)
await shot(page, '03-doc-view')
// 编辑态：找「编辑」按钮
const editBtn = page.getByRole('button', { name: /^编辑$/ }).first()
if (await editBtn.count()) {
  await editBtn.click()
  await page.waitForTimeout(700)
  await shot(page, '03b-doc-edit')
}

// 4) 同步设置：根行「更多」菜单里的同步项
await page.locator('[data-testid="tree.root.menu"]').first().click()
await page.waitForTimeout(300)
await shot(page, '04a-root-menu')
await page.locator('[data-testid="tree.root.sync"]').first().click()
await page.waitForTimeout(700)
await shot(page, '04-sync')
await page.keyboard.press('Escape')
await page.waitForTimeout(400)

// 5) 设置
await page.locator('[data-testid="statusbar.settings"]').first().click()
await page.waitForTimeout(800)
await shot(page, '05-settings')

await browser.close()
console.log('done ->', OUT)
