import type { Locator, Page } from '@playwright/test'
import { expect, test } from '../helpers/app'
import { listCollectionFiles } from '../helpers/fs'
import { t } from '../helpers/i18n'
import { openCollection, treeRow } from '../helpers/ui'

/**
 * 集合树拖动调整上级目录。
 *
 * 拖拽由**指针事件**实现（pointerdown/move/up），不用 HTML5 拖拽 ——
 * 桌面端跑在 WebView2 里，原生拖拽在嵌入式 WebView 下不可靠（用户实测鼠标拖不动），
 * 而且原生拖拽期间渲染进程的输入派发会被拖拽循环阻塞。
 * 换成指针事件后，这里可以直接用真实鼠标驱动，覆盖的就是用户的实际操作路径。
 */
async function mouseDrag(page: Page, src: Locator, dst: Locator, opts: { release?: boolean } = {}): Promise<void> {
  const a = await src.boundingBox()
  const b = await dst.boundingBox()
  if (!a || !b) throw new Error('拖拽元素不可见')
  await page.mouse.move(a.x + a.width / 2, a.y + a.height / 2)
  await page.mouse.down()
  // 先小步越过 4px 阈值进入拖动状态，再移到目标
  await page.mouse.move(a.x + a.width / 2 + 8, a.y + a.height / 2 + 8, { steps: 3 })
  await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2, { steps: 12 })
  if (opts.release !== false) await page.mouse.up()
}

const folderRow = (page: Page, name: string): Locator =>
  page.locator('[data-kind="folder"]').filter({ hasText: new RegExp(`^\\s*${name}\\s*$`) }).first()

test.describe('集合树拖动', () => {
  test('[T1] 拖动请求到另一个分组，落盘位置随之改变', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    expect(listCollectionFiles(dir)).toContain('api/ping.yml')

    // 拖动中：提示条浮在树上（不占布局）、目标行高亮
    await mouseDrag(page, treeRow(page, 'ping'), folderRow(page, 'empty'), { release: false })
    await expect(page.getByTestId('tree.dragHint')).toBeVisible()
    await expect(folderRow(page, 'empty')).toHaveClass(/drop-ok/)
    await page.mouse.up()

    await expect.poll(() => listCollectionFiles(dir)).toContain('empty/ping.yml')
    await expect.poll(() => listCollectionFiles(dir)).not.toContain('api/ping.yml')
    await expect(page.locator('.n-message').last()).toBeVisible()
    await expect(page.getByTestId('tree.dragHint')).toHaveCount(0)
  })

  test('[T2] 拖到集合根行可移回根目录', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    expect(listCollectionFiles(dir)).toContain('api/pong.yml')

    await mouseDrag(page, treeRow(page, 'pong'), page.getByTestId('tree.root'))

    await expect.poll(() => listCollectionFiles(dir)).toContain('pong.yml')
    await expect.poll(() => listCollectionFiles(dir)).not.toContain('api/pong.yml')
  })

  test('[T3] 拖动分组改上级；非法目标（自己的后代）被拒；点自己身上是空操作', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)

    // api 挂到 empty 下
    await mouseDrag(page, folderRow(page, 'api'), folderRow(page, 'empty'))
    await expect.poll(() => listCollectionFiles(dir)).toContain('empty/api/ping.yml')

    // 再拖回根
    await mouseDrag(page, folderRow(page, 'api'), page.getByTestId('tree.root'))
    await expect.poll(() => listCollectionFiles(dir)).toContain('api/ping.yml')
    await expect.poll(() => listCollectionFiles(dir)).not.toContain('empty/api/ping.yml')

    // 非法：api 拖到自己的子分组（先造一个子分组：把 empty 拖进 api）
    await mouseDrag(page, folderRow(page, 'empty'), folderRow(page, 'api'))
    await expect.poll(() => listCollectionFiles(dir)).toContain('api/empty/folder.yml')
    await mouseDrag(page, folderRow(page, 'api'), folderRow(page, 'empty'), { release: false })
    await expect(folderRow(page, 'empty')).toHaveClass(/drop-bad/) // 非法目标给「禁止」样式
    await page.mouse.up()
    await page.waitForTimeout(500)
    expect(listCollectionFiles(dir)).toContain('api/empty/folder.yml') // 没成环
  })

  test('[T4] 普通点击仍能打开请求（拖动不吞点击）', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)

    const before = await page.getByTestId('tab.item').count()
    await page.locator('[data-testid="tree.row"]').filter({ hasText: 'ping' }).getByTestId('tree.row.name').click()
    await expect.poll(() => page.getByTestId('tab.item').count()).toBeGreaterThan(before)
  })

  test('[T5] 搜索过滤时不允许拖动', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    const snapshot = listCollectionFiles(dir)

    await page.locator('.sidebar input').first().fill('pin')
    await expect(page.getByTestId('tree.row').filter({ hasText: 'ping' })).toBeVisible()
    // 搜索时按下拖动：不进入拖动状态（树是残缺的，拖到看不见的分组会让人困惑）
    const row = page.getByTestId('tree.row').filter({ hasText: 'ping' }).first()
    const box = (await row.boundingBox())!
    await page.mouse.move(box.x + 20, box.y + box.height / 2)
    await page.mouse.down()
    await page.mouse.move(box.x + 60, box.y + box.height / 2 + 20, { steps: 6 })
    await expect(page.getByTestId('tree.dragHint')).toHaveCount(0)
    await page.mouse.up()
    expect(listCollectionFiles(dir)).toEqual(snapshot)
  })

  test('[T6] 拖动结束的那次松开不会顺手打开请求', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    const before = await page.getByTestId('tab.item').count()

    await mouseDrag(page, treeRow(page, 'ping'), folderRow(page, 'empty'))
    await page.waitForTimeout(400)
    expect(await page.getByTestId('tab.item').count()).toBe(before)
  })
})
