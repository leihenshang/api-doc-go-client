// 回归：响应字段覆盖式更新（值一致时继承说明）、Resp 树节点与保存响应示例（Bruno 的 Save Response）。
import { existsSync, readdirSync } from 'node:fs'
import path from 'node:path'
import type { Page } from '@playwright/test'
import { expect, test } from '../helpers/app'
import { readCollectionFile } from '../helpers/fs'
import { t } from '../helpers/i18n'
import { openCollection, openReqTab, openRequest, openRespTab, reopenApp, send } from '../helpers/ui'

const PING_UID = '33333333-3333-4333-8333-333333333333'

const exampleModal = (page: Page) =>
  page.locator('.n-modal').filter({ has: page.getByPlaceholder(t('resp.exampleNamePlaceholder')) })

// 「更新响应字段」覆盖确认弹窗（naive 标题是 prop 渲染，按文案定位）
const overwriteModal = (page: Page) =>
  page.locator('.n-modal').filter({ hasText: t('resp.fieldsOverwriteTitle') })

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

test.describe('响应区：响应字段（覆盖更新 / 说明继承）与响应示例', () => {
  test('[E23] 覆盖式更新响应字段：值一致继承说明、字段表进 Resp 分段、侧栏出 Resp 节点 @smoke', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')

    await sendUrl(page, app, '{{host}}/json/flat')
    await page.getByTestId('resp.updateFields').click()
    await expect(page.getByTestId('resp.json')).toBeVisible() // 更新后响应体不消失

    // 首次更新（无旧字段）：直接执行并把请求编辑器切到「响应字段」分段
    await expect(page.getByTestId('req.tab').filter({ hasText: t('editor.respFields') })).toHaveClass(/on/)
    const rows = page.getByTestId('resp.fields.row')
    await expect(rows.first()).toBeVisible()
    expect(await rows.count()).toBe(2) // flat=true / value=1
    await expect(page.locator('.n-message').last()).toContainText(t('resp.fieldsUpdated', { n: 2 }))

    // 给 value 字段（两次响应取值都为 1）填含义
    const valueMeaning = page
      .getByTestId('resp.fields.row')
      .filter({ has: page.locator('.fp', { hasText: 'value' }) })
      .getByTestId('resp.fields.meaning')
      .locator('input')
    await valueMeaning.fill('业务主体')
    await valueMeaning.blur()
    await page.waitForTimeout(700) // 含义去抖写盘（600ms）

    // 再打同结构的 flat：字段已存在 → 弹覆盖确认（默认勾选继承说明）
    await sendUrl(page, app, '{{host}}/json/flat')
    await page.getByTestId('resp.updateFields').click()
    await expect(overwriteModal(page)).toBeVisible() // 覆盖确认
    await page.getByTestId('resp.fieldsConfirm').click()
    await expect(valueMeaning).toHaveValue('业务主体') // 路径与取值都一致 → 说明继承

    // 换成 withArray（覆盖：flat/value 字段消失，items.* 出现）
    await sendUrl(page, app, '{{host}}/json/withArray')
    await page.getByTestId('resp.updateFields').click()
    await expect(overwriteModal(page)).toBeVisible()
    await page.getByTestId('resp.fieldsConfirm').click()
    await expect(rows.first()).toBeVisible()
    expect(await rows.count()).toBeGreaterThan(2) // total / items.0.id / items.0.name / items.1.* 
    await expect(
      page.getByTestId('resp.fields.row').filter({ has: page.locator('.fp', { hasText: 'value' }) }),
    ).toHaveCount(0) // 覆盖：旧字段消失

    // 取消分支：有旧字段时取消不覆盖
    await sendUrl(page, app, '{{host}}/json/fewer')
    const arrayCount = await rows.count()
    await page.getByTestId('resp.updateFields').click()
    await overwriteModal(page).getByRole('button', { name: t('common.cancel') }).click()
    await expect(rows).toHaveCount(arrayCount) // 取消后字段表不变

    // 取消后字段表停留在 fewer（覆盖未执行）：respdoc 段随 fewer 字段落盘，侧栏 Resp 节点仍在
    await expect.poll(() => readCollectionFile(dir, 'api/ping.yml') ?? '', { timeout: 5000 }).toContain('respdoc')
    await expect.poll(() => readCollectionFile(dir, 'api/ping.yml') ?? '', { timeout: 5000 }).toContain('fieldName: total')
    await expect.poll(() => readCollectionFile(dir, 'api/ping.yml') ?? '', { timeout: 5000 }).not.toContain('业务主体')
    // 若 api 分组处于折叠态则展开（默认是展开；仅当 ping 行不在 DOM 时点 caret，避免把展开的折叠掉）。
    // 注意 Resp 合成子行 data-uid 与请求相同，须用 data-kind="request" 精确定位请求行。
    const pingRow = page.locator('[data-testid="tree.row"][data-kind="request"][data-uid="33333333-3333-4333-8333-333333333333"]')
    if ((await pingRow.count()) === 0) {
      await page.locator('[data-testid="tree.row"][data-kind="folder"][data-path="api"] .caret').click()
    }
    await pingRow.scrollIntoViewIfNeeded()
    await expect(pingRow).toBeVisible()
    const respName = page.getByTestId('tree.resp.name')
    await respName.scrollIntoViewIfNeeded()
    await expect(respName).toBeVisible()

    // 点侧栏 Resp 节点：定位到请求的「响应字段」分段
    await respName.click()
    await expect(page.getByTestId('req.tab').filter({ hasText: t('editor.respFields') })).toHaveClass(/on/)
  })

  test('[E23] 勾选取消继承：同路径取值变化时说明不继承', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')

    await sendUrl(page, app, '{{host}}/json/flat')
    await page.getByTestId('resp.updateFields').click()
    const valueMeaning = page
      .getByTestId('resp.fields.row')
      .filter({ has: page.locator('.fp', { hasText: 'value' }) })
      .getByTestId('resp.fields.meaning')
      .locator('input')
    await valueMeaning.fill('旧说明')
    await valueMeaning.blur()
    await page.waitForTimeout(700)

    // 再打 flat：覆盖弹窗里取消勾选继承 → 同路径同值的说明也不沿用
    await sendUrl(page, app, '{{host}}/json/flat')
    await page.getByTestId('resp.updateFields').click()
    await page.getByTestId('resp.fieldsInherit').uncheck()
    await page.getByTestId('resp.fieldsConfirm').click()
    await expect(valueMeaning).toHaveValue('')
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

  // 回归：回看示例（快照可能缺 headers/body 字段）时点「更新响应字段」，
  // 曾把整个面板渲染打断——三个页签内容全空白、响应下拉卡住不收
  test('[E23] 回看示例时更新响应字段，响应体/响应头/响应字段都可见', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    await sendUrl(page, app, '{{host}}/json/flat')

    await openSaveExample(page)
    await exampleModal(page).locator('input').first().fill('回看快照')
    await exampleModal(page).getByRole('button', { name: t('common.save'), exact: true }).click()
    await expect(page.getByTestId('resp.exnote')).toBeVisible()

    await page.getByTestId('resp.updateFields').click()
    await expect(page.getByTestId('resp.json')).toBeVisible() // 响应体
    await openRespTab(page, 'headers')
    await expect(page.getByTestId('resp.headerRow').first()).toBeVisible() // 响应头
    // 响应字段在请求编辑器 Resp 分段（更新后自动切过去）
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
})
