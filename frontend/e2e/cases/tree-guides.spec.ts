import { mkdirSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import type { Locator, Page } from '@playwright/test'
import { expect, test } from '../helpers/app'
import { t } from '../helpers/i18n'
import { openCollection, treeRowByUid } from '../helpers/ui'

/**
 * 集合树连接线（│ ├ └）：层级可见性 + 与展开图标的对齐。
 *
 * 断言的是「结构 + 几何」：哪些列该有线、祖先列什么时候继续、末行是否收成 └、
 * 竖线是否正好落在展开图标的圆心、横线是否落在行的垂直中线上、缩进是否随深度递增 ——
 * 连接线是绝对定位的装饰层，但这些性质都能通过 DOM 类别、offsetLeft 与盒子尺寸稳定观察到。
 *
 * basic 夹具只有两层，所以在打开集合前直接铺一层嵌套目录：
 *   api/sub/deep/inner（请求）+ api/sub/sibling，正好覆盖深度 1–3。
 * 排序规则（分组在前、组内 seq 升序）保证 last 判定稳定：sub 后面还有 ping/pong、
 * deep 后面还有 sibling —— 这两处是「祖先竖线必须继续」的关键前提。
 */
const API_UID = '22222222-2222-4222-8222-222222222222'
const EMPTY_UID = '55555555-5555-4555-8555-555555555555'
const PING_UID = '33333333-3333-4333-8333-333333333333'
const PONG_UID = '88888888-8888-4888-8888-888888888888'
const SUB_UID = 'aaaaaaaa-1111-4111-8111-111111111111'
const DEEP_UID = 'bbbbbbbb-2222-4222-8222-222222222222'
const SIBLING_UID = 'dddddddd-4444-4444-8444-444444444444'
const INNER_UID = 'cccccccc-3333-4333-8333-333333333333'

/** 与 Sidebar.vue 保持一致：每层缩进 / 连接线列相对内容起点的偏移 / 短横线宽度 */
const INDENT = 16
const GUIDE_OFFSET = 8
const STUB_W = 8

const folderYml = (name: string, seq: number, uid: string): string =>
  `info:\n    name: ${name}\n    type: folder\n    seq: ${seq}\nmeta:\n    uid: ${uid}\n`

const requestYml = (name: string, seq: number, uid: string): string =>
  `info:\n    name: ${name}\n    type: http\n    seq: ${seq}\nmeta:\n    uid: ${uid}\n    base_rev: 0\n` +
  `http:\n    method: GET\n    url: '{{host}}/json/flat'\n    body:\n        type: none\n    auth: none\n`

/** 在集合目录里铺出 api/sub/{deep/inner, sibling} 结构（分组在前、组内按 seq 排）。 */
function seedNested(dir: string): void {
  const write = (rel: string, body: string): void => {
    const full = path.join(dir, rel)
    mkdirSync(path.dirname(full), { recursive: true })
    writeFileSync(full, body)
  }
  write('api/sub/folder.yml', folderYml('sub', 0, SUB_UID))
  write('api/sub/deep/folder.yml', folderYml('deep', 1, DEEP_UID))
  write('api/sub/sibling/folder.yml', folderYml('sibling', 2, SIBLING_UID))
  write('api/sub/deep/inner.yml', requestYml('inner', 1, INNER_UID))
}

/** 某个树行的连接线层与各列元素 */
const guides = (page: Page, uid: string): Locator =>
  treeRowByUid(page, uid).locator('[data-testid="tree.row.guides"]')
/** 祖先竖线（不含本行的 ├/└ 与横线） */
const ancestors = (page: Page, uid: string): Locator => guides(page, uid).locator('.gl:not(.own):not(.stub)')
const own = (page: Page, uid: string): Locator => guides(page, uid).locator('.gl.own')
const stub = (page: Page, uid: string): Locator => guides(page, uid).locator('.gl.stub')

const offsetLeft = (loc: Locator): Promise<number> => loc.evaluate((el) => (el as HTMLElement).offsetLeft)
const midX = async (loc: Locator): Promise<number> => {
  const b = (await loc.boundingBox())!
  return b.x + b.width / 2
}

test('[B16] 集合树连接线：深度缩进 / 祖先竖线延续 / └ 收线 / 横线落在行中线', async ({ page, app }) => {
  const dir = await app.newCollection('basic')
  seedNested(dir)
  await openCollection(page, app)

  // 深度 0（集合根直挂的分组）：也有连接线 —— 它那一列正好是根行展开图标的圆心，
  // 由根行自己向下接出（.gl.root-down），于是「集合 → 顶层条目」连成一条完整的分支。
  await expect(guides(page, API_UID)).toHaveCount(1)
  await expect(ancestors(page, API_UID)).toHaveCount(0)
  await expect(own(page, API_UID)).not.toHaveClass(/last/) // api 后面还有 empty
  await expect(own(page, EMPTY_UID)).toHaveClass(/last/)
  expect(await offsetLeft(own(page, API_UID))).toBe(GUIDE_OFFSET)
  expect(await offsetLeft(stub(page, API_UID))).toBe(GUIDE_OFFSET)

  // 根行的接出线：从行中线落到行底（与第一层子行的竖线对接），收起根后缩回不画
  const rootGuides = page.locator('[data-testid="tree.root.guides"]')
  await expect(rootGuides).toHaveCount(1)
  const rootRow = (await page.getByTestId('tree.root').boundingBox())!
  const rootDown = (await rootGuides.locator('.gl.root-down').boundingBox())!
  expect(rootDown.height).toBeCloseTo(rootRow.height / 2, 0)
  expect(Math.abs(rootDown.y + rootDown.height - (rootRow.y + rootRow.height))).toBeLessThanOrEqual(1)
  await page.getByTestId('tree.root.caret').click() // 收起该根
  await expect(treeRowByUid(page, API_UID)).toHaveCount(0)
  await expect(rootGuides).toHaveCount(0)
  await page.getByTestId('tree.root.caret').click() // 展开回来
  await expect(rootGuides).toHaveCount(1)

  // 深度 1：只有本行一列；ping 后面还有 pong → ├（竖线满行高），pong 是末行 → └
  await expect(ancestors(page, PING_UID)).toHaveCount(0)
  await expect(own(page, PING_UID)).toHaveCount(1)
  await expect(own(page, PING_UID)).not.toHaveClass(/last/)
  await expect(own(page, PONG_UID)).toHaveClass(/last/)
  expect(await offsetLeft(own(page, PING_UID))).toBe(INDENT + GUIDE_OFFSET)
  expect(await offsetLeft(stub(page, PING_UID))).toBe(INDENT + GUIDE_OFFSET)

  // ├ 满行高、└ 只到中线：两种画法在观感上的唯一区别
  const rowH = (await treeRowByUid(page, PING_UID).boundingBox())!.height
  const pingOwn = (await own(page, PING_UID).boundingBox())!
  const pongOwn = (await own(page, PONG_UID).boundingBox())!
  expect(pingOwn.height).toBeCloseTo(rowH, 0)
  expect(pongOwn.height).toBeCloseTo(rowH / 2, 0)

  // 深度 2（api/sub/sibling）：sub 后面还有 ping/pong → 祖先列继续（.on）
  await expect(ancestors(page, SIBLING_UID)).toHaveCount(1)
  await expect(ancestors(page, SIBLING_UID).first()).toHaveClass(/on/)
  expect(await offsetLeft(ancestors(page, SIBLING_UID).first())).toBe(INDENT + GUIDE_OFFSET)
  await expect(own(page, SIBLING_UID)).toHaveClass(/last/)
  expect(await offsetLeft(own(page, SIBLING_UID))).toBe(2 * INDENT + GUIDE_OFFSET)

  // 深度 3（…/deep/inner）：两列祖先竖线都在（sub、deep 后面都还有兄弟），本行列在 3 * INDENT + 偏移
  await expect(ancestors(page, INNER_UID)).toHaveCount(2)
  await expect(ancestors(page, INNER_UID).nth(0)).toHaveClass(/on/)
  await expect(ancestors(page, INNER_UID).nth(1)).toHaveClass(/on/)
  expect(await offsetLeft(ancestors(page, INNER_UID).nth(0))).toBe(INDENT + GUIDE_OFFSET)
  expect(await offsetLeft(ancestors(page, INNER_UID).nth(1))).toBe(2 * INDENT + GUIDE_OFFSET)
  expect(await offsetLeft(own(page, INNER_UID))).toBe(3 * INDENT + GUIDE_OFFSET)
  expect(await offsetLeft(stub(page, INNER_UID))).toBe(3 * INDENT + GUIDE_OFFSET)

  // 横线：宽度固定、落在行中线（±1px），不压到行的底边
  const innerRow = (await treeRowByUid(page, INNER_UID).boundingBox())!
  const innerStub = (await stub(page, INNER_UID).boundingBox())!
  expect(innerStub.width).toBe(STUB_W)
  expect(innerStub.y + innerStub.height / 2).toBeCloseTo(innerRow.y + innerRow.height / 2, 0)

  // 连接线是绝对定位的装饰层：不改变行的缩进（内容起点仍是 8 + (depth+1) * INDENT）
  const padOf = (uid: string): Promise<string> =>
    treeRowByUid(page, uid).evaluate((el) => getComputedStyle(el).paddingLeft)
  expect(await padOf(PING_UID)).toBe(`${8 + 2 * INDENT}px`)
  expect(await padOf(INNER_UID)).toBe(`${8 + 4 * INDENT}px`)

  // 全部收起后子行消失、展开回来连接线照旧（连接线随行渲染，不留残影）。
  // 注意：分组行的展开箭头也用「全部展开」这个 title，所以必须限定在工具条里点。
  const head = page.locator('.sidebar .head')
  await head.getByTitle(t('tree.collapseAll')).click()
  await expect(treeRowByUid(page, INNER_UID)).toHaveCount(0)
  await head.getByTitle(t('tree.expandAll')).click()
  await expect(treeRowByUid(page, INNER_UID)).toBeVisible()
  await expect(ancestors(page, INNER_UID)).toHaveCount(2)
  expect(await offsetLeft(stub(page, INNER_UID))).toBe(3 * INDENT + GUIDE_OFFSET)
})

test('[B16] 连接线竖线落在展开图标圆心；方法标签与请求名同字号同基线', async ({ page, app }) => {
  const dir = await app.newCollection('basic')
  seedNested(dir)
  await app.addCollection('bruno-sample') // 第二个根：用于对比「活动根 / 非活动根」的字号与强调
  await openCollection(page, app)

  // ① 竖线列 = 某一层分组的展开图标圆心（不是按钮左边缘）：这样线是从图标里垂下来的。
  //    第 m 列 = 深度 m 行的 ├/└ 列 = 深度 m-1 那个分组的展开图标圆心。
  const caretCenterX = (uid: string): Promise<number> => midX(treeRowByUid(page, uid).locator('.caret svg'))
  const lineLeftX = async (loc: Locator): Promise<number> => (await loc.boundingBox())!.x
  // 竖线宽 1px，用左边缘比图标圆心：理想差 0.5px，取 ≤1px 视为同列
  const sameColumn = (lineX: number, caretX: number, label: string): void => {
    expect(Math.abs(lineX - caretX), `${label}：竖线 ${lineX} / 图标圆心 ${caretX}`).toBeLessThanOrEqual(1)
  }
  // 深度 0 的那一列 = **根行**展开图标的圆心；根行的接出线也在同一列（连线才算接上）
  const basicRootRow = page.getByTestId('tree.root').filter({ hasText: 'e2e-basic' })
  const basicCaret = basicRootRow.locator('.caret svg')
  sameColumn(await lineLeftX(own(page, API_UID)), await midX(basicCaret), 'api 列=根行图标')
  sameColumn(await lineLeftX(basicRootRow.locator('.gl.root-down')), await midX(basicCaret), '根行接出线')
  // deep / sibling 是 sub 的子行：它们的列落在 sub 的展开图标圆心
  sameColumn(await lineLeftX(own(page, DEEP_UID)), await caretCenterX(SUB_UID), 'deep')
  sameColumn(await lineLeftX(own(page, SIBLING_UID)), await caretCenterX(SUB_UID), 'sibling')
  // inner（深度 3）：自己的列 = deep 的图标圆心；两列祖先竖线分别是 api 与 sub 的图标圆心
  sameColumn(await lineLeftX(own(page, INNER_UID)), await caretCenterX(DEEP_UID), 'inner')
  sameColumn(await lineLeftX(ancestors(page, INNER_UID).nth(0)), await caretCenterX(API_UID), 'inner 第 1 列')
  sameColumn(await lineLeftX(ancestors(page, INNER_UID).nth(1)), await caretCenterX(SUB_UID), 'inner 第 2 列')
  // 横线端点：不越过本行内容起点，且长度就是 INDENT - GUIDE_OFFSET
  const innerNameX = (await treeRowByUid(page, INNER_UID).locator('.mt').boundingBox())!.x
  const innerLineX = await lineLeftX(own(page, INNER_UID))
  expect(innerLineX).toBeLessThan(innerNameX)
  expect(Math.abs(innerNameX - innerLineX - (INDENT - GUIDE_OFFSET))).toBeLessThanOrEqual(1)

  // ② 方法标签与请求名：同字号、同基线（否则树上一眼看去是错位的两个字号）
  const tag = treeRowByUid(page, PING_UID).locator('.mt')
  const name = treeRowByUid(page, PING_UID).locator('.rname')
  const fontOf = (loc: Locator): Promise<string> => loc.evaluate((el) => getComputedStyle(el).fontSize)
  expect(await fontOf(tag)).toBe(await fontOf(name))
  // 字面底边（基线 + 下伸部）对齐：字号不同的旧实现这里会差出 1px 以上
  const textBottom = (loc: Locator): Promise<number> =>
    loc.evaluate((el) => {
      const r = document.createRange()
      r.selectNodeContents(el)
      return r.getBoundingClientRect().bottom
    })
  expect(Math.abs((await textBottom(tag)) - (await textBottom(name)))).toBeLessThanOrEqual(1)
  // 两个字面都还在行内居中（别为了对齐把文字顶到行顶部）
  const row = (await treeRowByUid(page, PING_UID).boundingBox())!
  const tagBox = (await tag.boundingBox())!
  expect(Math.abs(tagBox.y + tagBox.height / 2 - (row.y + row.height / 2))).toBeLessThanOrEqual(2)

  // ③ 集合目录字号：比树里的条目大一档；**被选中的那个集合再大一档**（点一下就换过去）。
  //    注意旧实现里 `.coll-name` 的 14px 被 `.row > .rname` 的 12.5px 盖掉了 —— 这里断言真实生效值。
  const px = async (loc: Locator): Promise<number> => parseFloat(await fontOf(loc))
  const basicName = basicRootRow.locator('.coll-name')
  expect(await px(basicName)).toBeGreaterThan(await px(name))
  const idleSize = await px(basicName)
  await basicRootRow.click() // 切成活动根
  await expect(basicRootRow).toHaveClass(/root-active/)
  expect(await px(basicName)).toBeGreaterThan(idleSize)
})
