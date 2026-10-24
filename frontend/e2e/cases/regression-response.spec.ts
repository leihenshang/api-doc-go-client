// 回归：响应字段表格（手工生成 + 新增/覆盖合并）、Resp 树节点与保存响应示例（Bruno 的 Save Response）。
import type { Page } from '@playwright/test'
import { existsSync, readdirSync } from 'node:fs'
import path from 'node:path'
import { expect, test } from '../helpers/app'
import { readCollectionFile } from '../helpers/fs'
import { t } from '../helpers/i18n'
import { openCollection, openReqTab, openRequest, openRespTab, reopenApp, send } from '../helpers/ui'

const PING_UID = '33333333-3333-4333-8333-333333333333'

const exampleModal = (page: Page) =>
  page.locator('.n-modal').filter({ has: page.getByPlaceholder(t('resp.exampleNamePlaceholder')) })

// 「生成数据表格」确认弹窗（已有表格时二选一）
const genModal = (page: Page) => page.getByTestId('resp.genDialog')

const fieldRow = (page: Page, fieldPath: string) =>
  page.locator(`[data-testid="resp.fields.row"][data-path="${fieldPath}"]`)
const meaningInput = (page: Page, fieldPath: string) =>
  fieldRow(page, fieldPath).getByTestId('resp.fields.meaning').locator('input')

// 响应示例保存入口收进「保存 ▾」菜单（response-panel 设计稿）：点触发器再点菜单项
async function openSaveExample(page: Page) {
  await page.getByTestId('resp.save').click()
  await page.getByTestId('resp.saveExample').click()
}

async function sendUrl(page: Page, app: import('../helpers/app').AppFixture, url: string) {
  const { patchRequest } = await import('../helpers/api')
  await patchRequest(app, PING_UID, { url })
  await page.getByTestId('req.url').fill(url)
  await page.getByTestId('req.url').blur()
  await send(page)
}

/** 打开请求编辑器的「响应字段」分段并用「从本次响应生成」派生表格（空表直接生成）。 */
async function genFromLive(page: Page) {
  await openReqTab(page, 'resp')
  await page.getByTestId('resp.generateLive').click()
}

test.describe('响应区：响应字段（新增/覆盖合并）与响应示例', () => {
  test('[E23] 从本次响应生成字段表：进 Resp 分段、同名行说明在覆盖时保留 @smoke', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')

    await sendUrl(page, app, '{{host}}/json/flat')
    await genFromLive(page)
    await expect(page.getByTestId('resp.json')).toBeVisible() // 生成后响应体不消失

    // 空表直接生成，并把请求编辑器切到「字段表格」子页签
    await expect(page.getByTestId('req.tab').filter({ hasText: t('editor.respFields') })).toHaveClass(/on/)
    const rows = page.getByTestId('resp.fields.row')
    await expect(rows.first()).toBeVisible()
    expect(await rows.count()).toBe(2) // flat=true / value=1

    // 给 value 字段填含义
    await meaningInput(page, 'value').fill('业务主体')
    await meaningInput(page, 'value').blur()
    await page.waitForTimeout(700) // 含义去抖写盘（600ms）

    // 再打同结构的 flat：已有表格 → 弹「新增 / 覆盖」；选覆盖，同名字段说明保留、必填回「是」
    await sendUrl(page, app, '{{host}}/json/flat')
    await genFromLive(page)
    await expect(genModal(page)).toBeVisible()
    await page.getByTestId('resp.genOverwrite').click()
    await expect(meaningInput(page, 'value')).toHaveValue('业务主体')
    await expect(fieldRow(page, 'value').getByTestId('resp.fields.required')).toHaveAttribute('aria-checked', 'true')

    // 换成 withArray（覆盖：flat/value 字段消失，items[].* 出现）
    await sendUrl(page, app, '{{host}}/json/withArray')
    await genFromLive(page)
    await expect(genModal(page)).toBeVisible()
    await page.getByTestId('resp.genOverwrite').click()
    await expect(rows.first()).toBeVisible()
    expect(await rows.count()).toBeGreaterThan(2) // total / items / items[].id / items[].name
    await expect(fieldRow(page, 'value')).toHaveCount(0) // 覆盖：旧字段消失

    // 取消分支：有旧字段时取消不合并
    await sendUrl(page, app, '{{host}}/json/fewer')
    const arrayCount = await rows.count()
    await genFromLive(page)
    await genModal(page).getByRole('button', { name: t('common.cancel') }).click()
    await expect(rows).toHaveCount(arrayCount) // 取消后字段表不变

    // 字段表仍停留在 withArray（覆盖未执行）：respdoc 段随 withArray 字段落盘
    await expect.poll(() => readCollectionFile(dir, 'api/ping.yml') ?? '', { timeout: 5000 }).toContain('respdoc')
    await expect.poll(() => readCollectionFile(dir, 'api/ping.yml') ?? '', { timeout: 5000 }).toContain('fieldName: total')
    await expect.poll(() => readCollectionFile(dir, 'api/ping.yml') ?? '', { timeout: 5000 }).not.toContain('业务主体')
    // 集合树不再合成 Resp 子节点（item 7）：请求行下没有任何子行入口
    await expect(page.getByTestId('tree.resp.name')).toHaveCount(0)
    // 请求编辑器仍在「响应字段」分段
    await expect(page.getByTestId('req.tab').filter({ hasText: t('editor.respFields') })).toHaveClass(/on/)
  })

  test('[E23] 「新增」只追加缺失字段：同名行的说明/必填/示例值都不动', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')

    await sendUrl(page, app, '{{host}}/json/flat')
    await genFromLive(page)
    await expect(page.getByTestId('resp.fields.row')).toHaveCount(2)
    await meaningInput(page, 'value').fill('旧说明')
    await meaningInput(page, 'value').blur()
    await page.waitForTimeout(700)

    // 换成 withArray：已有表格 → 选「新增」，flat/value 保留（含说明），只追加 items 相关字段
    await sendUrl(page, app, '{{host}}/json/withArray')
    await genFromLive(page)
    await expect(genModal(page)).toBeVisible()
    await page.getByTestId('resp.genAppend').click()
    await expect(fieldRow(page, 'flat')).toBeVisible()
    await expect(meaningInput(page, 'value')).toHaveValue('旧说明')
    await expect(fieldRow(page, 'items[].id')).toBeVisible()
    expect(await page.getByTestId('resp.fields.row').count()).toBeGreaterThan(2)
  })

  test('[E23] 保存响应示例：落盘 / 同名去重 / 回看只读 / 删除进 .trash', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')

    await sendUrl(page, app, '{{host}}/json/flat')
    await expect(page.getByTestId('resp.status')).toContainText('200')

    const name = '成功示例'
    await openSaveExample(page)
    await exampleModal(page).locator('input').first().fill(name)
    await exampleModal(page).getByRole('button', { name: t('common.save'), exact: true }).click()

    const exampleFile = `examples/api/ping/${name}.yml`
    await expect.poll(() => readCollectionFile(dir, exampleFile), { timeout: 10_000 }).toContain('/json/flat')
    const yaml = readCollectionFile(dir, exampleFile) ?? ''
    for (const key of ['type: response-example', 'status: 200', 'request_uid']) expect(yaml).toContain(key)

    // 同名再存一次 → 自动加序号
    await page.getByTestId('resp.backToLive').click()
    await openSaveExample(page)
    await exampleModal(page).locator('input').first().fill(name)
    await exampleModal(page).getByRole('button', { name: t('common.save'), exact: true }).click()
    await expect.poll(() => existsSync(path.join(dir, `examples/api/ping/${name} (1).yml`))).toBe(true)

    // 回看：面板切到示例快照（只读），并能回到实时响应
    await expect(page.getByTestId('resp.exnote')).toBeVisible()
    await expect(page.getByTestId('resp.exnote')).toContainText('正在查看已保存的示例')
    await expect(page.getByTestId('resp.status')).toContainText('200')
    // 连接/性能计数只属实时响应：回看已保存示例时不展示（不写入示例文件/不随同步）
    await expect(page.getByTestId('resp.conn')).toHaveCount(0)
    await page.getByTestId('resp.backToLive').click()
    await expect(page.getByTestId('resp.exnote')).toHaveCount(0)

    // 删除示例 → 文件进入 .trash
    const options = page.getByTestId('resp.examples')
    await options.click()
    await page.locator('.n-base-select-option').filter({ hasText: new RegExp(`^${name}$`) }).click()
    await page.getByTestId('resp.exampleDelete').click()
    await page.locator('.n-popconfirm .n-button--primary-type').click()

    await expect.poll(() => existsSync(path.join(dir, exampleFile))).toBe(false)
    expect(readdirSync(path.join(dir, '.trash')).some((f) => f.includes(name))).toBe(true)
  })

  // 回归：回看示例（快照可能缺 headers/body 字段）时从本次响应生成字段表，
  // 曾把整个面板渲染打断——三个页签内容全空白、响应下拉卡住不收
  test('[E23] 回看示例时生成字段表，响应体/响应头/响应字段都可见', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    await sendUrl(page, app, '{{host}}/json/flat')

    await openSaveExample(page)
    await exampleModal(page).locator('input').first().fill('回看快照')
    await exampleModal(page).getByRole('button', { name: t('common.save'), exact: true }).click()
    await expect(page.getByTestId('resp.exnote')).toBeVisible()

    await genFromLive(page)
    await expect(page.getByTestId('resp.json')).toBeVisible() // 响应体
    await openRespTab(page, 'headers')
    await expect(page.getByTestId('resp.headerRow').first()).toBeVisible() // 响应头
    // 响应字段在请求编辑器 Resp 分段
    await expect(page.getByTestId('req.tab').filter({ hasText: t('editor.respFields') })).toHaveClass(/on/)
    await expect(page.getByTestId('resp.fields.row').first()).toBeVisible()
  })

  // 首次打开请求（尚未发送）时，响应区状态栏与「已保存的响应示例」下拉必须与发送后同一位置/样式，
  // 只是隐藏状态码徽章与耗时/体积 —— 此前下拉挂在空态提示下方（居中、220px），位置与样式都对不上
  test('[E23] 未发送时响应区：状态栏占位 + 示例下拉就地切换', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')

    await sendUrl(page, app, '{{host}}/json/flat')
    await openSaveExample(page)
    await exampleModal(page).locator('input').first().fill('成功示例')
    await exampleModal(page).getByRole('button', { name: t('common.save'), exact: true }).click()
    await expect(page.getByTestId('resp.exnote')).toBeVisible()

    // 刷新重开：会话只记 uid、响应不落 localStorage → 回到「尚未发送」的状态
    await page.reload()
    await reopenApp(page)
    await openRequest(page, 'ping')
    await expect(page.getByText(t('resp.empty'), { exact: true })).toBeVisible()

    const bar = page.getByTestId('resp.statusbar')
    await expect(bar).toContainText(t('resp.title'))
    await expect(page.getByTestId('resp.status')).toHaveCount(0) // 无响应：不出现状态码徽章
    await expect(page.getByTestId('resp.meta')).toHaveCount(0) // 也不出现耗时/体积
    await expect(bar.getByTestId('resp.examples')).toHaveCount(1) // 下拉就在状态栏里（与发送后同位置）

    // 直接切到已保存示例：面板换成快照，徽章与耗时在同一个状态栏里出现
    await bar.getByTestId('resp.examples').click()
    await page.locator('.n-base-select-option').filter({ hasText: '成功示例' }).click()
    await expect(page.getByTestId('resp.exnote')).toContainText(t('resp.viewingExample', { name: '成功示例' }))
    await expect(page.getByTestId('resp.status')).toContainText('200')
    await expect(page.getByTestId('resp.statusbar')).toHaveCount(1) // 没有换成第二个状态栏
    await expect(page.getByTestId('resp.meta')).toHaveCount(1)

    // 回到本次响应 = 回到空态：状态栏与下拉留在原位
    await page.getByTestId('resp.backToLive').click()
    await expect(page.getByText(t('resp.empty'), { exact: true })).toBeVisible()
    await expect(page.getByTestId('resp.status')).toHaveCount(0)
    await expect(page.getByTestId('resp.statusbar').getByTestId('resp.examples')).toHaveCount(1)
  })

  // 回归守门：状态栏改成常驻后，模板里的 v-else-if 一度挂到了状态栏的 v-if 上，
  // 链条变成「无错误 → 只渲染状态栏」，于是发送后响应区空白（响应体/页签都不出现）、
  // 切示例也没反应，而编译与类型检查都发现不了。三种状态这里逐个钉住。
  test('[E23] 发送后响应体可见，示例切换与回到本次响应都正常', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')

    // ① 未发送：状态栏占位（无状态码/耗时）+ 空态提示与飞机图标
    await expect(page.getByTestId('resp.statusbar')).toBeVisible()
    await expect(page.getByTestId('resp.status')).toHaveCount(0)
    await expect(page.getByTestId('resp.emptyIcon')).toBeVisible()
    await expect(page.getByText(t('resp.empty'), { exact: true })).toBeVisible()

    // ② 发送：响应体必须真的渲染出来（这条就是那次回归的守门人）
    await sendUrl(page, app, '{{host}}/json/flat')
    await expect(page.getByTestId('resp.status')).toContainText('200')
    await expect(page.getByTestId('resp.meta')).toHaveCount(1)
    await expect(page.getByTestId('resp.json')).toBeVisible()
    await expect(page.getByTestId('resp.emptyIcon')).toHaveCount(0) // 空态让位给响应区
    await expect(page.getByText(t('resp.empty'), { exact: true })).toHaveCount(0)

    // ③ 存一个示例 → 切走再切回：提示条与响应体都要跟着换
    const name = '切换示例'
    await openSaveExample(page)
    await exampleModal(page).locator('input').first().fill(name)
    await exampleModal(page).getByRole('button', { name: t('common.save'), exact: true }).click()
    await expect(page.getByTestId('resp.exnote')).toContainText(t('resp.viewingExample', { name }))

    const pick = page.getByTestId('resp.examples')
    await pick.click()
    await page.locator('.n-base-select-option').filter({ hasText: t('resp.liveResponse') }).click()
    await expect(page.getByTestId('resp.exnote')).toHaveCount(0) // 回到本次响应
    await expect(page.getByTestId('resp.json')).toBeVisible()
    await expect(page.getByTestId('resp.statusbar')).toHaveCount(1) // 状态栏没有被换掉

    await pick.click()
    await page.locator('.n-base-select-option').filter({ hasText: name }).click()
    await expect(page.getByTestId('resp.exnote')).toBeVisible()
    await expect(page.getByTestId('resp.json')).toBeVisible()
  })

  test('[E23b] 折行模式：超长值逐行折，字段名/冒号不被挤成逐字换行（回归）', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    // echo 回显 query：超长字段名 + 超长值那一行，正是截图里的复现形态
    await sendUrl(page, app, '{{host}}/echo?someExtremelyLongQueryParameterNameABCDEFGHIJKLMNOPQRSTUVWXYZ=' + 'x'.repeat(300))
    await expect(page.getByTestId('resp.json')).toBeVisible()

    // 换行开关默认开；不在态就点开
    const wrapBtn = page.getByTestId('resp.wrap')
    if (!(await wrapBtn.evaluate((el) => el.classList.contains('on')))) await wrapBtn.click()

    const row = page.locator('.jr').filter({ hasText: 'someExtremelyLongQueryParameterName' }).first()
    await expect(row).toBeVisible()
    // 行高 24：字段名仍占一行（修复前 break-all 把它挤成 1 字符/行，高度远超）
    const kh = await row.locator('.k').evaluate((el) => el.getBoundingClientRect().height)
    expect(kh).toBeLessThanOrEqual(28)
    // 值真的折出了多行（换行功能本身没被破坏）
    const vh = await row.locator('.v').evaluate((el) => el.getBoundingClientRect().height)
    expect(vh).toBeGreaterThan(28)
  })

  test('[E23c] 原始视图固定折行：长 JSON 向下展开不横向溢出（回归 item 17）', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    // echo 回显长 query：响应体是一行超长单行 JSON
    await sendUrl(page, app, '{{host}}/echo?q=' + 'x'.repeat(300))
    await page.getByTestId('resp.raw').click()
    const raw = page.getByTestId('resp.rawBody')
    await expect(raw).toContainText('xxx')
    const geo = await raw.evaluate((el) => ({
      overflowX: el.scrollWidth - el.clientWidth,
      lines: Math.round(el.getBoundingClientRect().height / parseFloat(getComputedStyle(el).lineHeight)),
    }))
    expect(geo.overflowX).toBeLessThanOrEqual(2) // 不再一行溢出展示框
    expect(geo.lines).toBeGreaterThan(2) // 向下折行展开
  })

  test('[E25] 响应体原文：XML 支持一键美化（美化/原始可来回切）', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    await sendUrl(page, app, '{{host}}/xml/flat')

    const raw = page.getByTestId('resp.rawBody')
    await expect(raw).toContainText('<root>')
    // 默认美化：单行 XML 被缩进为多行
    expect(await raw.evaluate((el) => el.textContent)).toContain('\n  <item id="1">a</item>')

    // 原始：恢复一行原文
    await page.getByTestId('resp.raw').click()
    await expect(raw).toHaveText('<root><item id="1">a</item><item id="2">b</item></root>')

    // 再美化：保持多行（XML 与 JSON 的「美化」同一入口）
    await page.getByTestId('resp.pretty').click()
    expect(await raw.evaluate((el) => el.textContent)).toContain('\n  <item id="2">b</item>')
  })

  test('[E24] 文件类响应（.md / Content-Disposition）提示下载、不解析内容', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    await sendUrl(page, app, '{{host}}/files/readme.md')
    // 命中下载卡片 + 下载按钮；内容不再被当接口结果解析/展示（无原始 body 区、无工具条）
    await expect(page.getByTestId('resp.fileCard')).toBeVisible()
    await expect(page.getByTestId('resp.downloadFile')).toBeVisible()
    await expect(page.getByTestId('resp.rawBody')).toHaveCount(0)
    await expect(page.getByTestId('resp.pretty')).toHaveCount(0)
  })
})
