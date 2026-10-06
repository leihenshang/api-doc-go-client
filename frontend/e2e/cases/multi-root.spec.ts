import { existsSync, mkdirSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import type { Locator } from '@playwright/test'
import { expect, test } from '../helpers/app'
import { listCollectionFiles } from '../helpers/fs'
import { t } from '../helpers/i18n'
import {
  openCollection,
  openReqTab,
  openSettings,
  openSettingsSection,
  reopenApp,
  saveSettings,
  treeRow,
} from '../helpers/ui'

/** naive popconfirm 的确认按钮：取最后可见的那个（用过的 popconfirm 会留在 DOM 里）。 */
const confirmButton = (page: import('@playwright/test').Page) =>
  page.locator('.n-popconfirm:visible').getByRole('button', { name: t('common.confirm') }).last()

/**
 * 多工作目录（单树多根）。
 *
 * 要点：
 * - 侧栏同时显示多个根行（每个工作目录一份），各有自己的树；
 * - 活动根明确标识：集合级动作（打开请求、新建、环境、同步…）都作用于它；
 * - 点非活动根里的请求会先把那一根切成活动根；
 * - 标签页按根分组：切根等价于换一组标签，切回来能看到原来的现场。
 */
test('[M1] 多工作目录：两个根并存 / 切换活动根 / 标签随根切换', async ({ page, app }) => {
  await app.newCollection('basic')
  await app.addCollection('bruno-sample')
  await openCollection(page, app)

  const roots = page.getByTestId('tree.root')
  await expect(roots).toHaveCount(2)
  // 两个根的树都在（同名请求不会串台）
  await expect(treeRow(page, 'ping')).toHaveCount(1)
  await expect(treeRow(page, '用户-列表')).toHaveCount(1)

  const basic = roots.filter({ hasText: 'e2e-basic' })
  const bruno = roots.filter({ hasText: 'e2e-bruno' })
  // 最后打开的那个是活动根（左侧强调条）
  await expect(bruno).toHaveClass(/root-active/)
  await expect(basic).not.toHaveClass(/root-active/)

  const tabs = page.getByTestId('tab.item')

  // ① 在活动根（bruno）里打开请求 → 一个标签
  await treeRow(page, '用户-列表').getByTestId('tree.row.name').click()
  await expect(tabs).toHaveCount(1)

  // ② 切到 basic：标签组随之换掉（bruno 的那个不在这一组里）
  await basic.click()
  await expect(basic).toHaveClass(/root-active/)
  await expect(bruno).not.toHaveClass(/root-active/)
  await expect(tabs).toHaveCount(0)

  // ③ 打开 basic 里的请求 → 它落在 basic 这一组
  await treeRow(page, 'ping').getByTestId('tree.row.name').click()
  await expect(tabs).toHaveCount(1)
  await expect(tabs.first()).toContainText('ping')

  // ④ 切回 bruno：现场还在（标签按根各自持久）
  await bruno.click()
  await expect(bruno).toHaveClass(/root-active/)
  await expect(tabs).toHaveCount(1)
  await expect(tabs.first()).toContainText('用户-列表')

  // ⑤ 点非活动根里的请求：自动把那一切成活动根，再打开
  await basic.click()
  await expect(basic).toHaveClass(/root-active/)
  await treeRow(page, '用户-列表').getByTestId('tree.row.name').click()
  await expect(bruno).toHaveClass(/root-active/)
  await expect(tabs.first()).toContainText('用户-列表')
})

test('[M1] 多工作目录：工具条菜单可切换 / 根可折叠 / 可关闭', async ({ page, app }) => {
  await app.newCollection('basic')
  await app.addCollection('bruno-sample')
  await openCollection(page, app)

  const roots = page.getByTestId('tree.root')
  const basic = roots.filter({ hasText: 'e2e-basic' })
  const bruno = roots.filter({ hasText: 'e2e-bruno' })
  await expect(roots).toHaveCount(2)

  // 工具条「集合」菜单里能直接切到另一个工作目录
  await page.getByTestId('toolbar.collection').click()
  await page.locator('.n-dropdown-option').filter({ hasText: 'e2e-basic' }).click()
  await expect(basic).toHaveClass(/root-active/)

  // 折叠某个根：它的行整片收起，另一个根不受影响
  await bruno.getByTestId('tree.root.caret').click()
  await expect(treeRow(page, '用户-列表')).toHaveCount(0)
  await expect(treeRow(page, 'ping')).toHaveCount(1)
  await bruno.getByTestId('tree.root.caret').click()
  await expect(treeRow(page, '用户-列表')).toHaveCount(1)

  // 关闭一个根：另一个继续可用；关掉活动根时活动标记回到剩余根
  await bruno.getByTestId('tree.root.close').click()
  await confirmButton(page).click()
  await expect(roots).toHaveCount(1)
  await expect(basic).toHaveClass(/root-active/)
  await expect(treeRow(page, 'ping')).toHaveCount(1)
  await expect(page.getByTestId('tab.item')).toHaveCount(0)
})

test('[M1] 命令面板跨目录搜索：本目录 / 所有目录 + 按目录路由打开', async ({ page, app }) => {
  await app.newCollection('basic')
  await app.addCollection('bruno-sample')
  await openCollection(page, app)

  // 把活动根明确设为 basic（bruno 里才有「用户-列表」）
  const roots = page.getByTestId('tree.root')
  const basic = roots.filter({ hasText: 'e2e-basic' })
  const bruno = roots.filter({ hasText: 'e2e-bruno' })
  await basic.click()
  await expect(basic).toHaveClass(/root-active/)

  await page.keyboard.press('Control+k')
  await expect(page.getByTestId('palette')).toBeVisible()
  await page.keyboard.type('用户')
  // 默认「本目录」：活动根（basic）里没有命中
  await expect(page.getByTestId('palette.item')).toHaveCount(0)

  // 切到「所有目录」：命中出现在另一个工作目录，并标注所属集合
  await page.getByTestId('palette.dirScope').filter({ hasText: t('palette.scopeAll') }).click()
  const hit = page.getByTestId('palette.item').filter({ hasText: '用户-列表' })
  await expect(hit).toHaveCount(1)
  await expect(hit.getByTestId('palette.itemDir')).toContainText('e2e-bruno')

  // 回车打开：自动切到该目录并把标签开在它自己的那一组
  await page.keyboard.press('Enter')
  await expect(page.getByTestId('tab.item').first()).toContainText('用户-列表')
  await expect(bruno).toHaveClass(/root-active/)
  // 编辑器里确实是那个请求（夹具里它带 {{userName}} 查询参数，ping 没有）
  await openReqTab(page, 'params')
  await expect(page.getByTestId('kv.row').first().getByTestId('kv.value').locator('input')).toHaveValue('{{userName}}')
})

test('[M1] 跨工作目录拖动：请求单搬；分组整棵搬（目标重名会被提前拦住）', async ({ page, app }) => {
  const basic = await app.newCollection('basic')
  const bruno = await app.addCollection('bruno-sample')
  // 目标根里先放一个同名分组：验证「跨根搬重名分组」会被提前拦住（后端拒之前就置灰并说明）
  mkdirSync(path.join(basic, 'account'), { recursive: true })
  writeFileSync(
    path.join(basic, 'account', 'folder.yml'),
    'info:\n    name: account\n    type: folder\n    seq: 9\nmeta:\n    uid: 99999999-9999-4999-8999-999999999999\n',
  )
  await openCollection(page, app)

  const roots = page.getByTestId('tree.root')
  const basicRoot = roots.filter({ hasText: 'e2e-basic' })
  const brunoRoot = roots.filter({ hasText: 'e2e-bruno' })
  // 两个根里都有 account，所以定位必须带上根（data-root = 该根的目录标识）
  const rowsIn = (dir: string, kind: 'folder' | 'request'): Locator =>
    page.locator(`[data-kind="${kind}"][data-root*="${path.basename(dir)}" i]`)
  const dragOnto = async (from: Locator, to: Locator): Promise<void> => {
    const a = (await from.boundingBox())!
    const b = (await to.boundingBox())!
    await page.mouse.move(a.x + a.width / 2, a.y + a.height / 2)
    await page.mouse.down()
    await page.mouse.move(a.x + a.width / 2 + 8, a.y + a.height / 2 + 8, { steps: 3 })
    await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2, { steps: 12 })
  }

  // ① 请求跨根：basic 的 ping 拖到 bruno 的根行 → 源进 .trash、目标多一份
  const from = (await treeRow(page, 'ping').boundingBox())!
  const to = (await brunoRoot.boundingBox())!
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2)
  await page.mouse.down()
  await page.mouse.move(from.x + from.width / 2 + 8, from.y + from.height / 2 + 8, { steps: 3 })
  await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 12 })
  await expect(brunoRoot).toHaveClass(/drop-ok/) // 跨根拖请求是合法落点
  await page.mouse.up()
  await expect.poll(() => existsSync(path.join(basic, 'api', 'ping.yml'))).toBe(false)
  await expect.poll(() => existsSync(path.join(basic, '.trash'))).toBe(true)
  await expect.poll(() => existsSync(path.join(bruno, 'ping.yml'))).toBe(true)
  await expect(treeRow(page, 'ping')).toHaveCount(1)

  // ② 分组跨根（重名）：bruno 的 account 拖到 basic 的根行 → 落点变非法并写明原因，松手后什么都没发生
  const accountInBruno = rowsIn(bruno, 'folder').filter({ hasText: 'account' }).first()
  const brunoFiles = listCollectionFiles(bruno)
  await dragOnto(accountInBruno, basicRoot)
  await expect(basicRoot).toHaveClass(/drop-bad/)
  await expect(page.getByText(t('tree.dupFolderHint', { name: 'account' }))).toBeVisible()
  await page.mouse.up()
  expect(listCollectionFiles(bruno)).toEqual(brunoFiles) // 源分组一点没动

  // ③ 分组跨根（成功）：bruno 的 account 拖到 basic 的 account 分组里 → 整棵（含其中请求）搬过去
  const accountInBasic = rowsIn(basic, 'folder').filter({ hasText: 'account' }).first()
  await dragOnto(accountInBruno, accountInBasic)
  await expect(accountInBasic).toHaveClass(/drop-ok/)
  await page.mouse.up()
  await expect.poll(() => existsSync(path.join(basic, 'account', 'account', '用户-列表.yml'))).toBe(true)
  await expect.poll(() => existsSync(path.join(basic, 'account', 'account', 'folder.yml'))).toBe(true)
  // 源分组整棵进 .trash（可找回），bruno 侧不再有它
  await expect.poll(() => existsSync(path.join(bruno, 'account'))).toBe(false)
  await expect.poll(() => existsSync(path.join(bruno, '.trash'))).toBe(true)
  // 树里该请求只剩一份，且现在属于 basic 这一侧
  await expect(treeRow(page, '用户-列表')).toHaveCount(1)
  await expect(rowsIn(basic, 'request').filter({ hasText: '用户-列表' })).toHaveCount(1)
})

test('[M4] 启动恢复上限：只恢复前 N 个，其余从工具条「最近打开」补开', async ({ page, app }) => {
  const basic = await app.newCollection('basic')
  const bruno = await app.addCollection('bruno-sample')
  await openCollection(page, app)
  await expect(page.getByTestId('tree.root')).toHaveCount(2)

  // 上限调成 1；顺带确认「当前打开的工作目录」如实反映占用（每个根一个文件监听）
  await openSettings(page)
  await openSettingsSection(page, 'local')
  await expect(page.getByTestId('settings.openRoots')).toHaveText('2')
  await page.getByTestId('settings.restoreLimit').locator('input').fill('1')
  await saveSettings(page)

  // 造出「下次启动」的现场：后端把根全关掉（关进程后就是这个状态），
  // localStorage 里只留下上次打开过的目录列表 —— 应用启动时按它恢复。
  const list = await app.ipc<{ root: string }[]>('ListCollections')
  for (const r of list) await app.ipc('CloseCollection', r.root)
  // 用 addInitScript 而不是 page.evaluate：openCollection 里那个「清空 client.dirs」的脚本在每次导航
  // 都会重跑，按注册顺序它在前面，所以这里设的值才能活到应用启动时被读到。
  // recentDirs 是独立的 MRU 列表（「最近打开」用它，不会被「恢复上限」截断影响）。
  await page.addInitScript(
    ([dirs, recent]) => {
      localStorage.setItem('client.dirs', JSON.stringify(dirs))
      localStorage.setItem('client.recentDirs', JSON.stringify(recent))
    },
    [[basic, bruno], [basic, bruno]],
  )

  // 重进应用：只恢复 1 个根，且**不静默丢**——明确提示还有几个没恢复
  await page.reload()
  await expect(page.getByTestId('toolbar.collection')).toBeVisible()
  await expect(page.getByTestId('tree.root')).toHaveCount(1)
  await expect(page.locator('.n-message').last()).toContainText(
    t('welcome.restoreTruncated', { n: 1, m: 1 }),
  )

  // 没恢复的那个在工具条「最近打开」里，一点就补开
  // （naive-ui 的分组标题是 .n-dropdown-option-body--group，选项标签是 .n-dropdown-option-body__label）
  await page.getByTestId('toolbar.collection').click()
  await expect(
    page.locator('.n-dropdown-option-body--group').filter({ hasText: t('toolbar.recentDirs') }),
  ).toBeVisible()
  await page
    .locator('.n-dropdown-option-body__label')
    .filter({ hasText: path.basename(bruno) })
    .click()
  await expect(page.getByTestId('tree.root')).toHaveCount(2)
  // 打开后进 MRU 队首（「最近打开」的排序与下次启动的恢复顺序都靠它）
  await expect
    .poll(() =>
      page.evaluate(() => {
        const raw = localStorage.getItem('client.recentDirs')
        return raw ? (JSON.parse(raw) as string[])[0] : ''
      }),
    )
    .toBe(bruno)
})
