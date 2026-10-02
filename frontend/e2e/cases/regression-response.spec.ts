// 对应 doc §5 回归要点 17：响应字段「只追加不删除」与保存响应示例（Bruno 的 Save Response）。
import { existsSync, readdirSync } from 'node:fs'
import path from 'node:path'
import type { Page } from '@playwright/test'
import { expect, test } from '../helpers/app'
import { readCollectionFile } from '../helpers/fs'
import { t } from '../helpers/i18n'
import { openCollection, openRequest, openRespTab, reopenApp, send } from '../helpers/ui'

const PING_UID = '33333333-3333-4333-8333-333333333333'

const exampleModal = (page: Page) =>
  page.locator('.n-modal').filter({ has: page.getByPlaceholder(t('resp.exampleNamePlaceholder')) })

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

test.describe('响应区：字段映射与响应示例', () => {
  test('[E23] 响应字段只追加不删除，且已填含义保留 @smoke', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')

    await sendUrl(page, app, '{{host}}/json/withArray')
    await page.getByTestId('resp.updateFields').click()
    const rows = page.getByTestId('resp.fields.row')
    await expect(rows.first()).toBeVisible() // 字段就地出现在响应体下方
    await expect(page.getByTestId('resp.json')).toBeVisible() // 回归：更新字段不切页签，响应体不消失
    const count = await rows.count()
    expect(count).toBeGreaterThan(0) // 叶子字段：items.0.id / items.0.name / items.1.id / items.1.name / total
    await expect(page.locator('.n-message').last()).toContainText(t('resp.fieldsAdded', { n: count }))

    // 标注含义后，换成字段更少的响应再更新一次
    await page.getByTestId('resp.fields.meaning').first().locator('input').fill('业务主体')
    await sendUrl(page, app, '{{host}}/json/fewer')
    await page.getByTestId('resp.updateFields').click()

    await expect(rows).toHaveCount(count) // 只追加：字段变少也不会删掉已有的
    await expect(page.getByTestId('resp.fields.meaning').first().locator('input')).toHaveValue('业务主体')
    await expect(page.locator('.n-message').last()).toContainText(t('resp.fieldsNoChange'))

    // 字段含义存在本地存档（localStorage key 按请求 uid）
    const stored = await page.evaluate((uid) => localStorage.getItem(`client.fieldmap.${uid}`), PING_UID)
    expect(stored ?? '').toContain('业务主体')
    expect(dir).toContain('collection-')
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
    await openRespTab(page, 'fields')
    await expect(page.getByTestId('resp.fields.row').first()).toBeVisible() // 响应字段
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
    await expect(page.getByText(t('resp.empty'))).toBeVisible()
    await expect(page.getByTestId('resp.status')).toHaveCount(0)
    await expect(page.getByTestId('resp.statusbar').getByTestId('resp.examples')).toHaveCount(1)
  })
})
