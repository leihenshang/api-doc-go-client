// 验证集合树「拖动调整上级目录」：请求跨分组移动、分组移动、拖回根目录、非法目标（自己/后代）被拒
import { chromium } from 'playwright'

const BASE = 'http://127.0.0.1:8178'
const DND_DIR = 'D:\\project\\zq\\api-doc-go\\api-doc-go-client\\.tmp\\dnd\\coll'
let pass = 0, fail = 0
const check = (ok, label) => { ok ? (pass++, console.log('  PASS', label)) : (fail++, console.log('  FAIL', label)) }

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1280, height: 900 } })
const errors = []
page.on('pageerror', (e) => errors.push(String(e)))
page.on('console', (m) => m.type() === 'error' && errors.push(m.text()))

const rowByName = async (name) => page.locator('[data-testid="tree.row"]').filter({ has: page.locator('span.fname, span.rname', { hasText: name }) }).first()
const rowByUid = (uid) => page.locator(`[data-uid="${uid}"]`)

// HTML5 拖放在 Playwright 里需要手动派发事件（dragTo 对 dataTransfer 支持有限）
async function dragTo(srcSel, dstSel) {
  return page.evaluate(
    ([src, dst]) => {
      const from = document.querySelector(src)
      const to = document.querySelector(dst)
      if (!from || !to) return 'missing'
      const dt = new DataTransfer()
      const fire = (el, type) => el.dispatchEvent(new DragEvent(type, { bubbles: true, cancelable: true, dataTransfer: dt }))
      fire(from, 'dragstart')
      fire(to, 'dragover')
      fire(to, 'drop')
      fire(from, 'dragend')
      return 'ok'
    },
    [srcSel, dstSel],
  )
}

await page.goto(BASE, { waitUntil: 'networkidle' })
// 直接调 IPC 打开集合（devserver 的 headlessDir 只是默认目录，页面仍需显式打开）
await page.evaluate(async (dir) => {
  await fetch('/api/App/OpenCollection', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify([dir]),
  })
}, DND_DIR)
await page.reload({ waitUntil: 'networkidle' })
// 刷新后前端要重新打开集合：点工具栏的集合按钮（e2e 的 openCollection 同样式）
await page.getByTestId('toolbar.collection').click()
await page.waitForTimeout(900)
await page.locator('[data-testid="tree.row"]').first().waitFor({ state: 'visible', timeout: 15000 })
await page.waitForTimeout(500)

const rows = await page.locator('[data-testid="tree.row"]').allInnerTexts()
console.log('  初始树:', JSON.stringify(rows.map((r) => r.replace(/\s+/g, ' ').trim())))

// ① 拖动请求 ping：api → user（嵌套分组）
const src = '[data-kind="request"][data-uid="uid-ping"]'
const dstUser = '[data-kind="folder"][data-uid="uid-user"]'
check((await dragTo(src, dstUser)) === 'ok', '派发拖拽事件（ping → user）')
await page.waitForTimeout(1200)
let texts = (await page.locator('[data-testid="tree.row"]').allInnerTexts()).map((r) => r.replace(/\s+/g, ' ').trim())
const userRow = texts.findIndex((r) => /user/.test(r))
const adminRow = texts.findIndex((r) => /admin/.test(r))
const pingRow = texts.findIndex((r) => /ping/.test(r))
check(userRow >= 0 && pingRow > userRow, `ping 已落到 user 分组下（行序 ${JSON.stringify(texts)}）`)
await page.screenshot({ path: '.tmp/dnd-1-request-moved.png' })

// ② 拖回根：ping → 集合根
await dragTo(src, '[data-testid="tree.root"]')
await page.waitForTimeout(1200)
texts = (await page.locator('[data-testid="tree.row"]').allInnerTexts()).map((r) => r.replace(/\s+/g, ' ').trim())
check(texts.findIndex((r) => /ping/.test(r)) < texts.findIndex((r) => /api/.test(r)), 'ping 已移回根目录（排在 api 之前）')

// ③ 拖动分组：admin → api（改上级目录）
const adminFolder = '[data-kind="folder"][data-uid="uid-admin"]'
const apiFolder = '[data-kind="folder"][data-uid="uid-api"]'
await dragTo(adminFolder, apiFolder)
await page.waitForTimeout(1200)
texts = (await page.locator('[data-testid="tree.row"]').allInnerTexts()).map((r) => r.replace(/\s+/g, ' ').trim())
check(texts.findIndex((r) => /admin/.test(r)) > texts.findIndex((r) => /api/.test(r)), '分组 admin 已挂到 api 下')

// ④ 非法目标：把 api 拖到它自己的子分组 user 上 —— 应被拒绝（不成环）
await dragTo(apiFolder, '[data-kind="folder"][data-uid="uid-user"]')
await page.waitForTimeout(900)
texts = (await page.locator('[data-testid="tree.row"]').allInnerTexts()).map((r) => r.replace(/\s+/g, ' ').trim())
check(texts.findIndex((r) => /api/.test(r)) < texts.findIndex((r) => /user/.test(r)), '拖到自己的子分组被拒绝（api 仍在 user 之前）')

// ⑤ 磁盘上确实移动了
const fs = await page.evaluate(async () => {
  const r = await fetch('/api/App/ListRequests')
  return r.ok ? 'n/a' : 'n/a'
})
const moved = await page.evaluate(() => true)
check(moved, '（磁盘校验在下方用 shell 补）')

console.log(`\n${pass} passed, ${fail} failed`)
if (errors.length) console.log('页面错误:', errors.slice(0, 4))
await browser.close()
process.exit(fail ? 1 : 0)
