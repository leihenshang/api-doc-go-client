import { expect, test } from '../helpers/app'
import { openCollection } from '../helpers/ui'

/**
 * Linux 桌面壳的窗口缩放热区（components/WindowResizeHandles.vue）。
 *
 * 背景：外壳为画阴影让出 32px 留白，可见边框离窗口外沿 32px，而 Wails 运行时的边缘检测
 * 只认外沿 6px —— 缩小窗口后拖可见边框无法缩放。这里在可见边框内侧放 6px 热区，
 * 按下直接发送 `resize:<edge>`（与运行时同一协议，交给 GTK）。
 *
 * devserver 也走「桌面壳」样式（data-shell=desktop，模拟 Wails 外壳），所以先显式切到
 * data-shell=web 验证默认隐藏，再用 data-* + WailsInvoke 桩模拟桌面 Linux，
 * 验证「可见 + 发消息 + 最大化隐藏」。
 */
type Probe = { WailsInvoke?: (msg: string) => void; __resizeMsgs?: string[] }

test('[T23] Linux 窗口缩放热区：可见边框可拖、发送 resize 消息、最大化隐藏', async ({ page, app }) => {
  await app.newCollection('basic')
  await openCollection(page, app)

  // ① 浏览器态：热区不显示，不抢内容边缘的点击
  const se = page.getByTestId('window.resize.se')
  await page.evaluate(() => {
    document.documentElement.dataset.shell = 'web'
  })
  await expect(se).toBeHidden()

  // ② 模拟桌面 Linux + WailsInvoke 桩
  await page.evaluate(() => {
    const root = document.documentElement
    root.dataset.shell = 'desktop'
    root.dataset.platform = 'linux'
    const w = window as unknown as Probe
    w.__resizeMsgs = []
    w.WailsInvoke = (msg: string) => w.__resizeMsgs?.push(msg)
  })
  await expect(se).toBeVisible()
  await expect(page.getByTestId('window.resize.nw')).toBeVisible()

  // ③ 按下四角/四边 → 发送对应的 resize:<edge>
  await se.click({ position: { x: 3, y: 3 } })
  await page.getByTestId('window.resize.w').click({ position: { x: 2, y: 40 } })
  await page.getByTestId('window.resize.n').click({ position: { x: 60, y: 2 } })
  const msgs = await page.evaluate(() => (window as unknown as Probe).__resizeMsgs ?? [])
  expect(msgs).toEqual(['resize:se-resize', 'resize:w-resize', 'resize:n-resize'])

  // ④ 最大化时隐藏（Wails 自身外沿检测接管，避免误触）
  await page.evaluate(() => {
    document.documentElement.dataset.window = 'max'
  })
  await expect(se).toBeHidden()
})
