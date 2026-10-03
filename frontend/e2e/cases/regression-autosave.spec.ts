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
})

async function exists(p: string): Promise<boolean> {
  const { existsSync } = await import('node:fs')
  return existsSync(p)
}
