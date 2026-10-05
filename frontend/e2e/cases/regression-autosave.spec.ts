// 对应 doc §5 回归要点 22：保存模型（默认手动 + 可选自动保存）与写盘冲突检测。
// 依赖 devserver 的 /api/events（SSE）转发文件监听事件 —— 浏览器态与桌面态行为一致。
import { readFileSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { expect, test } from '../helpers/app'
import { readCollectionFile } from '../helpers/fs'
import { t } from '../helpers/i18n'
import { openCollection, openRequest, openSettings, saveSettings } from '../helpers/ui'


async function editUrl(page: import('@playwright/test').Page, url: string) {
  await page.getByTestId('req.url').fill(url)
  await page.getByTestId('req.url').blur()
}

test.describe('保存模型与冲突检测', () => {
  test('[E26] 默认手动保存：编辑不写盘，Ctrl+S 落盘；外部改动回填干净页签', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    const file = path.join(dir, 'api', 'ping.yml')

    // 手动模式（默认）：编辑后文件不动，页签有未保存圆点
    const before = readFileSync(file, 'utf8')
    await editUrl(page, '{{host}}/json/flat')
    await page.waitForTimeout(1500)
    expect(readFileSync(file, 'utf8')).toBe(before)
    await expect(page.getByTestId('tab.item').locator('.dot')).toHaveCount(1)

    // Ctrl+S：落盘 + 圆点消失 + 状态栏「已保存」
    await page.keyboard.press('Control+s')
    await expect.poll(() => readCollectionFile(dir, 'api/ping.yml')).toContain('/json/flat')
    await expect(page.getByTestId('tab.item').locator('.dot')).toHaveCount(0)
    await expect(page.getByTestId('statusbar.saved')).toBeVisible()

    // 外部改动（绕过客户端直接写文件）→ 干净页签自动回填
    writeFileSync(file, readFileSync(file, 'utf8').replace('/json/flat', '/json/nested'), 'utf8')
    await expect(page.getByTestId('req.url')).toHaveValue(/\/json\/nested/, { timeout: 10_000 })
  })

  test('[E26] 脏页签遇外部改动：写盘被拒，冲突条给出重新加载 / 另存为副本', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    const file = path.join(dir, 'api', 'ping.yml')

    await editUrl(page, '{{host}}/json/flat')
    writeFileSync(file, readFileSync(file, 'utf8').replace('{{host}}', 'http://external.example'), 'utf8')
    await page.waitForTimeout(1500) // 等文件监听 → 标冲突

    // 冲突状态下保存被拒：文件不被覆盖
    await page.keyboard.press('Control+s')
    await expect(page.getByTestId('req.conflict')).toBeVisible()
    expect(readCollectionFile(dir, 'api/ping.yml')).toContain('external.example')

    // 出口一：另存为副本（文件落盘 + 打开副本页签）
    await page.getByTestId('conflict.copy').click()
    await expect.poll(() => exists(path.join(dir, 'api', 'ping 冲突副本.yml'))).toBe(true)
    await expect(page.getByTestId('tab.item').filter({ hasText: t('conflict.copyName', { name: 'ping' }) })).toBeVisible()

    // 出口二：重新加载（放弃编辑，页签与磁盘一致，冲突条消失）
    await page.getByTestId('tab.item').filter({ hasText: 'ping' }).first().click()
    await page.getByTestId('conflict.reload').click()
    await expect(page.getByTestId('req.url')).toHaveValue(/external\.example/)
    await expect(page.getByTestId('req.conflict')).toHaveCount(0)
  })

  test('[E26] 设置里的「自动保存」开关：开后编辑即落盘，关后恢复手动', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    const file = path.join(dir, 'api', 'ping.yml')

    await openSettings(page)
    await page.getByTestId('settings.autoSave').click()
    await saveSettings(page)

    await editUrl(page, '{{host}}/json/flat')
    await expect.poll(() => readCollectionFile(dir, 'api/ping.yml')).toContain('/json/flat')

    await openSettings(page)
    await page.getByTestId('settings.autoSave').click()
    await saveSettings(page)
    await editUrl(page, '{{host}}/json/fewer')
    await page.waitForTimeout(1500)
    expect(readCollectionFile(dir, 'api/ping.yml')).toContain('/json/flat') // 又回到手动：不写盘
  })

  test('[E26] 关闭脏页签先问保存：保存并关闭 / 不保存 / 取消', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    const before = readCollectionFile(dir, 'api/ping.yml') ?? ''
    const tab = page.getByTestId('tab.item').filter({ hasText: 'ping' })

    // ① 关页签必须先问，且此刻还没写盘
    await editUrl(page, '{{host}}/json/nested')
    await expect(tab.locator('.dot')).toHaveCount(1)
    await page.keyboard.press('Control+w')
    await expect(page.locator('.n-modal').filter({ hasText: t('prompt.unsavedTitle') })).toBeVisible()
    expect(readCollectionFile(dir, 'api/ping.yml')).toBe(before)
    await expect(tab).toBeVisible()

    // ② 取消：页签与未保存圆点都还在，磁盘未动
    await page.getByTestId('closeask.cancel').click()
    await expect(page.locator('.n-modal').filter({ hasText: t('prompt.unsavedTitle') })).toHaveCount(0)
    await expect(tab.locator('.dot')).toHaveCount(1)
    expect(readCollectionFile(dir, 'api/ping.yml')).toBe(before)

    // ③ 不保存：页签关掉，磁盘保持原样（丢弃真的丢弃）。这里走页签上的 × 按钮
    await tab.getByTestId('tab.close').click()
    await page.getByTestId('closeask.discard').click()
    await expect(tab).toHaveCount(0)
    expect(readCollectionFile(dir, 'api/ping.yml')).toBe(before)

    // ④ 保存并关闭：改动落盘后再关页签
    await openRequest(page, 'ping')
    await editUrl(page, '{{host}}/json/nested')
    await page.keyboard.press('Control+w')
    await page.getByTestId('closeask.save').click()
    await expect(tab).toHaveCount(0)
    await expect.poll(() => readCollectionFile(dir, 'api/ping.yml')).toContain('/json/nested')
  })

  test('[E26] 批量关闭脏页签：按页签顺序依次询问，取消即中止整批', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    await openRequest(page, 'pong')
    const pingYml = readCollectionFile(dir, 'api/ping.yml') ?? ''
    const pongYml = readCollectionFile(dir, 'api/pong.yml') ?? ''
    const tabs = page.getByTestId('tab.item')
    const ask = page.locator('.n-modal').filter({ hasText: t('prompt.unsavedTitle') })
    const closeAll = async (): Promise<void> => {
      await page.getByTestId('tab.item').first().click({ button: 'right' })
      await page.locator('.n-dropdown-option').filter({ hasText: t('tab.closeAll') }).click()
    }

    // 只是打开/切换页签不算脏：URL 的 watch 会随 props.tab 变化触发，旧实现会在这里凭空标脏
    await expect(tabs.locator('.dot')).toHaveCount(0)

    // 两张页签都改脏
    await page.getByTestId('req.url').fill('{{host}}/json/nested')
    await page.getByTestId('req.url').blur()
    await page.getByTestId('tab.item').filter({ hasText: 'ping' }).click()
    await page.getByTestId('req.url').fill('{{host}}/json/fewer')
    await page.getByTestId('req.url').blur()
    await expect(tabs.locator('.dot')).toHaveCount(2)

    // ① 依次询问：第一个取消 → 整批中止，两张都还在，磁盘没动
    await closeAll()
    await expect(ask).toBeVisible()
    await expect(page.getByTestId('closeask.seq')).toContainText(t('prompt.unsavedBatch', { i: 1, n: 2 }))
    await page.getByTestId('closeask.cancel').click()
    await expect(ask).toHaveCount(0)
    await expect(tabs).toHaveCount(2)
    await expect(page.locator('.n-message').last()).toContainText(t('tab.closeAborted', { n: 0 }))
    expect(readCollectionFile(dir, 'api/ping.yml')).toBe(pingYml)
    expect(readCollectionFile(dir, 'api/pong.yml')).toBe(pongYml)

    // ② 依次询问：第一个「不保存」→ 立刻问第二个（进度 2/2）→ 也不保存 → 全关且磁盘原样
    await closeAll()
    await expect(page.getByTestId('closeask.seq')).toContainText(t('prompt.unsavedBatch', { i: 1, n: 2 }))
    await page.getByTestId('closeask.discard').click()
    await expect(page.getByTestId('closeask.seq')).toContainText(t('prompt.unsavedBatch', { i: 2, n: 2 }))
    await page.getByTestId('closeask.discard').click()
    await expect(tabs).toHaveCount(0)
    await page.waitForTimeout(300)
    expect(readCollectionFile(dir, 'api/ping.yml')).toBe(pingYml)
    expect(readCollectionFile(dir, 'api/pong.yml')).toBe(pongYml)

    // ③ 混着选：第一个「保存并关闭」→ 落盘后继续问第二个
    await openRequest(page, 'ping')
    await openRequest(page, 'pong')
    await page.getByTestId('req.url').fill('{{host}}/json/nested')
    await page.getByTestId('req.url').blur()
    await page.getByTestId('tab.item').filter({ hasText: 'ping' }).click()
    await page.getByTestId('req.url').fill('{{host}}/json/fewer')
    await page.getByTestId('req.url').blur()
    await closeAll()
    await page.getByTestId('closeask.save').click()
    await page.getByTestId('closeask.discard').click()
    await expect(tabs).toHaveCount(0)
    await expect.poll(() => readCollectionFile(dir, 'api/ping.yml')).toContain('/json/fewer')
    expect(readCollectionFile(dir, 'api/pong.yml')).toBe(pongYml) // 第二个选了不保存
  })
})

async function exists(p: string): Promise<boolean> {
  const { existsSync } = await import('node:fs')
  return existsSync(p)
}
