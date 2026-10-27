import type { Locator, Page } from '@playwright/test'
import { expect, test } from '../helpers/app'
import { listCollectionFiles, readCollectionFile } from '../helpers/fs'
import { openCollection, treeRow, treeRowByUid } from '../helpers/ui'

/**
 * 集合树拖动调整上级目录。
 *
 * 拖拽由**指针事件**实现（pointerdown/move/up），不用 HTML5 拖拽 ——
 * 桌面端跑在 WebView2 里，原生拖拽在嵌入式 WebView 下不可靠（用户实测鼠标拖不动），
 * 而且原生拖拽期间渲染进程的输入派发会被拖拽循环阻塞。
 * 换成指针事件后，这里可以直接用真实鼠标驱动，覆盖的就是用户的实际操作路径。
 */
type Box = { x: number; y: number; width: number; height: number }

const sameBox = (a: Box, b: Box): boolean =>
  a.x === b.x && a.y === b.y && a.width === b.width && a.height === b.height

/**
 * 等元素的盒子**稳定**（连续两次读数一致）再返回。
 *
 * 为什么不能只读一次：每次移动分组都会触发一次集合重载，树在这个窗口里会重排/重建 ——
 * 上一拍量到的坐标可能已经指向别的行，拖拽就会落错行或干脆不生效（实测约六分之一概率）。
 * 稳定即「树已经画完」，比固定 sleep 更可靠，也更快。
 */
async function waitBox(page: Page, loc: Locator, label: string): Promise<Box> {
  let prev = await loc.boundingBox()
  for (let attempt = 0; attempt < 50; attempt++) {
    await page.waitForTimeout(120)
    const next = await loc.boundingBox()
    if (prev && next && prev.width > 0 && sameBox(prev, next)) return next
    prev = next
  }
  throw new Error(`拖拽元素位置不稳定或不可见：${label}（匹配数 ${await loc.count()}）`)
}

async function mouseDrag(page: Page, src: Locator, dst: Locator, opts: { release?: boolean } = {}): Promise<void> {
  const a = await waitBox(page, src, '源')
  const b = await waitBox(page, dst, '目标')
  await page.mouse.move(a.x + a.width / 2, a.y + a.height / 2)
  await page.mouse.down()
  // 先小步越过 4px 阈值进入拖动状态，再移到目标
  await page.mouse.move(a.x + a.width / 2 + 8, a.y + a.height / 2 + 8, { steps: 3 })
  await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2, { steps: 12 })
  if (opts.release !== false) await page.mouse.up()
}

/**
 * 拖到目标行的指定纵向比例处：0.15 = 上缘（插到它之前）、0.85 = 下缘（插到它之后）、
 * 0.5 = 行中间（放进容器）。同层排序用上/下缘，改上级用中间。
 */
async function mouseDragAt(
  page: Page,
  src: Locator,
  dst: Locator,
  ratio: number,
  opts: { release?: boolean } = {},
): Promise<void> {
  const a = await waitBox(page, src, '源')
  const b = await waitBox(page, dst, '目标')
  await page.mouse.move(a.x + a.width / 2, a.y + a.height / 2)
  await page.mouse.down()
  await page.mouse.move(a.x + a.width / 2 + 8, a.y + a.height / 2 + 8, { steps: 3 })
  await page.mouse.move(b.x + b.width / 2, b.y + b.height * ratio, { steps: 12 })
  if (opts.release !== false) await page.mouse.up()
}

const folderRow = (page: Page, name: string): Locator =>
  page.locator('[data-kind="folder"]').filter({ hasText: new RegExp(`^\\s*${name}\\s*$`) }).first()

// basic 种子里两个分组的固定 uid
const API_UID = '22222222-2222-4222-8222-222222222222'
const EMPTY_UID = '55555555-5555-4555-8555-555555555555'

/**
 * 折叠的分组：子行不在 DOM 里就拖不到，先点开（已展开则不动）。
 * 带重试的原因：移动分组后集合会重新加载（collection:changed），加载瞬间子行可能又不可见，
 * 单次「展开 → 等可见」会偶发失败（实测约五分之一概率）。
 */
async function ensureExpanded(page: Page, parentUid: string, childUid: string): Promise<void> {
  const child = treeRowByUid(page, childUid)
  for (let attempt = 0; attempt < 5; attempt++) {
    if (await child.isVisible().catch(() => false)) return
    await treeRowByUid(page, parentUid).getByTestId('tree.row.caret').click()
    try {
      await expect(child).toBeVisible({ timeout: 1_500 })
      return
    } catch {
      // 重新加载把它又收起来了：再展开一次
    }
  }
  throw new Error(`分组 ${parentUid} 展开后子行 ${childUid} 仍不可见`)
}

test.describe('集合树拖动', () => {
  test('[T1] 拖动请求到另一个分组，落盘位置随之改变', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    expect(listCollectionFiles(dir)).toContain('api/ping.yml')

    // 拖动中：树进入拖动态（dnd-active），目标行高亮并带落点横线（.drop-ok::after）
    await mouseDrag(page, treeRow(page, 'ping'), folderRow(page, 'empty'), { release: false })
    await expect(page.locator('.tree')).toHaveClass(/dnd-active/)
    await expect(folderRow(page, 'empty')).toHaveClass(/drop-ok/)
    await page.mouse.up()

    await expect.poll(() => listCollectionFiles(dir)).toContain('empty/ping.yml')
    await expect.poll(() => listCollectionFiles(dir)).not.toContain('api/ping.yml')
    await expect(page.locator('.tree')).not.toHaveClass(/dnd-active/)
    // 移动不再弹 toast：落点横线 + 树刷新已经说明结果，多一条提示只是噪音
    await expect(page.locator('.n-message')).toHaveCount(0)
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
    await mouseDrag(page, treeRowByUid(page, API_UID), treeRowByUid(page, EMPTY_UID))
    await expect.poll(() => listCollectionFiles(dir)).toContain('empty/api/ping.yml')
    // 磁盘先变、树后刷新：必须等新路径上行，否则下一次按下可能落在已经换位的旧坐标上。
    await expect(treeRowByUid(page, API_UID)).toHaveAttribute('data-path', 'empty/api')

    // 再拖回根（api 现在在 empty 里，empty 若是折叠态就得先展开才能拖到子行）
    await ensureExpanded(page, EMPTY_UID, API_UID)
    const root = page.getByTestId('tree.root')
    await mouseDrag(page, treeRowByUid(page, API_UID), root, { release: false })
    await expect(root).toHaveClass(/drop-ok/)
    await page.mouse.up()
    await expect.poll(() => listCollectionFiles(dir)).toContain('api/ping.yml')
    await expect.poll(() => listCollectionFiles(dir)).not.toContain('empty/api/ping.yml')
    await expect(treeRowByUid(page, API_UID)).toHaveAttribute('data-path', 'api')

    // 非法：api 拖到自己的子分组（先造一个子分组：把 empty 拖进 api）
    await mouseDrag(page, treeRowByUid(page, EMPTY_UID), treeRowByUid(page, API_UID))
    await expect.poll(() => listCollectionFiles(dir)).toContain('api/empty/folder.yml')
    await expect(treeRowByUid(page, EMPTY_UID)).toHaveAttribute('data-path', 'api/empty')
    await ensureExpanded(page, API_UID, EMPTY_UID)
    await mouseDrag(page, treeRowByUid(page, API_UID), treeRowByUid(page, EMPTY_UID), { release: false })
    await expect(treeRowByUid(page, EMPTY_UID)).toHaveClass(/drop-bad/) // 非法目标给「禁止」样式
    await page.mouse.up()
    await expect(page.locator('.tree')).not.toHaveClass(/dnd-active/)
    expect(listCollectionFiles(dir)).toContain('api/empty/folder.yml') // 没成环

    // 拖到自己身上是空操作（不写盘、不弹提示）
    await mouseDrag(page, treeRowByUid(page, API_UID), treeRowByUid(page, API_UID))
    expect(listCollectionFiles(dir)).toContain('api/empty/folder.yml')
    await expect(page.locator('.n-message')).toHaveCount(0)
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
    await expect(page.locator('.tree')).not.toHaveClass(/dnd-active/) // 搜索时不进入拖动状态
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

  // ---- 自定义拖动排序：落点改到行上/下缘（同层排序），行中间仍是「放进容器」 ----

  test('[T7] 请求拖到同行上缘：按落点排序并写回 seq', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    const reqRows = page.locator('[data-testid="tree.row"][data-kind="request"]')
    await expect(reqRows.nth(0)).toHaveAttribute('data-path', 'api/ping.yml')

    // 拖动中：目标行给「插入线」样式（drop-before），不再是「放进容器」的高亮
    await mouseDragAt(page, treeRow(page, 'pong'), treeRow(page, 'ping'), 0.15, { release: false })
    await expect(treeRow(page, 'ping')).toHaveClass(/drop-before/)
    await expect(treeRow(page, 'ping')).not.toHaveClass(/drop-ok/)
    await page.mouse.up()

    await expect.poll(async () => reqRows.nth(0).getAttribute('data-path')).toBe('api/pong.yml')
    await expect.poll(async () => reqRows.nth(1).getAttribute('data-path')).toBe('api/ping.yml')
    expect(readCollectionFile(dir, 'api/pong.yml') ?? '').toContain('seq: 1')
    expect(readCollectionFile(dir, 'api/ping.yml') ?? '').toContain('seq: 2')

    // 再把它拖回 ping 的下缘：顺序恢复（下缘 = 插到 ping 之后）
    await mouseDragAt(page, treeRow(page, 'pong'), treeRow(page, 'ping'), 0.85)
    await expect.poll(async () => reqRows.nth(0).getAttribute('data-path')).toBe('api/ping.yml')
    await expect.poll(async () => reqRows.nth(1).getAttribute('data-path')).toBe('api/pong.yml')

    // 拖到自己身上（上缘）是空操作：不能把请求甩到末尾
    await mouseDragAt(page, treeRow(page, 'ping'), treeRow(page, 'ping'), 0.15)
    await expect.poll(async () => reqRows.nth(0).getAttribute('data-path')).toBe('api/ping.yml')
    await expect.poll(async () => reqRows.nth(1).getAttribute('data-path')).toBe('api/pong.yml')
  })

  test('[T8] 分组拖到同行上缘：按落点排序并写回 folder.yml 的 seq', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    const folderRows = page.locator('[data-testid="tree.row"][data-kind="folder"]')
    // 初始：两个分组的 seq 相同，按名称 api 在前
    await expect(folderRows.nth(0)).toHaveAttribute('data-path', 'api')

    await mouseDragAt(page, treeRowByUid(page, EMPTY_UID), treeRowByUid(page, API_UID), 0.15)

    await expect.poll(async () => folderRows.nth(0).getAttribute('data-path')).toBe('empty')
    await expect.poll(async () => folderRows.nth(1).getAttribute('data-path')).toBe('api')
    expect(readCollectionFile(dir, 'empty/folder.yml') ?? '').toContain('seq: 1')
    expect(readCollectionFile(dir, 'api/folder.yml') ?? '').toContain('seq: 2')
  })

  test('[T9] 拖动根行：集合之间的顺序按落点调整', async ({ page, app }) => {
    await app.newCollection('basic')
    await app.addCollection('basic')
    await openCollection(page, app)
    const roots = page.getByTestId('tree.root')
    await expect(roots).toHaveCount(2)
    const first = await roots.nth(0).getAttribute('data-root')
    const second = await roots.nth(1).getAttribute('data-root')
    expect(first).not.toBe(second)

    // 把第二个根拖到第一个根的上缘 → 两者交换
    await mouseDragAt(page, roots.nth(1), roots.nth(0), 0.15)
    await expect.poll(async () => roots.nth(0).getAttribute('data-root')).toBe(second)
    await expect.poll(async () => roots.nth(1).getAttribute('data-root')).toBe(first)
  })

  test('[T10] 文档与请求同层排序：文档拖到请求之后并写回 frontmatter seq', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    // 直接在 api 分组里造一篇文档（IPC 写盘后重进页面刷新树）
    await app.ipc('CreateDoc', '说明', 'api')
    await openCollection(page, app)

    const apiLeaves = page.locator(
      '[data-testid="tree.row"][data-path^="api/"][data-kind="request"], ' +
        '[data-testid="tree.row"][data-path^="api/"][data-kind="doc"]',
    )
    // 新建文档的 seq 为 0：排在请求之前（叶子组内按 seq 升序）
    await expect.poll(async () => apiLeaves.nth(0).getAttribute('data-path')).toBe('api/说明.md')

    // 把请求拖到文档的上缘：请求 1、文档 2（pong 顺延为 3）
    await mouseDragAt(page, treeRow(page, 'ping'), treeRow(page, '说明'), 0.15)
    await expect.poll(async () => apiLeaves.nth(0).getAttribute('data-path')).toBe('api/ping.yml')
    await expect.poll(async () => apiLeaves.nth(1).getAttribute('data-path')).toBe('api/说明.md')
    expect(readCollectionFile(dir, 'api/说明.md') ?? '').toContain('seq: 2')

    // 再把文档拖到 pong 之后：文档变成最后一行
    await mouseDragAt(page, treeRow(page, '说明'), treeRow(page, 'pong'), 0.85)
    await expect.poll(async () => apiLeaves.nth(2).getAttribute('data-path')).toBe('api/说明.md')
    expect(readCollectionFile(dir, 'api/说明.md') ?? '').toContain('seq: 3')
  })
})
