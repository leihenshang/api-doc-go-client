// U2：接口生命周期（服务端标记的草稿 / 已废弃）在客户端只读展示。
//
// 真实来源是同步下发到请求文件 meta 段的 status/version；这里直接改文件模拟一次下发，
// 靠文件监听回填验证「不重开页签也能看到最新状态」。
import { readFileSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { expect, test } from '../helpers/app'
import { t } from '../helpers/i18n'
import { openCollection, openRequest } from '../helpers/ui'

test('U2 生命周期徽标：服务端标废弃的接口在请求栏显示「已废弃」', async ({ page, app }) => {
  const dir = await app.newCollection('basic')
  await openCollection(page, app)
  await openRequest(page, 'ping')

  // 已发布（status=2，默认）不显示徽标
  await expect(page.getByTestId('req.lifecycle')).toHaveCount(0)

  const file = path.join(dir, 'api/ping.yml')
  const before = readFileSync(file, 'utf8')
  writeFileSync(
    file,
    before.replace('meta:\n', 'meta:\n    status: 3\n    version: v2\n'),
    'utf8',
  )

  const badge = page.getByTestId('req.lifecycle')
  await expect(badge).toBeVisible({ timeout: 10_000 })
  await expect(badge).toContainText(t('editor.statusDeprecated'))
  await expect(badge).toContainText('v2')

  // 改回已发布 → 徽标消失
  writeFileSync(file, before.replace('meta:\n', 'meta:\n    status: 2\n'), 'utf8')
  await expect(badge).toHaveCount(0, { timeout: 10_000 })
})

test('U2 生命周期徽标：草稿状态使用「草稿」样式', async ({ page, app }) => {
  const dir = await app.newCollection('basic')
  await openCollection(page, app)

  const file = path.join(dir, 'api/ping.yml')
  const before = readFileSync(file, 'utf8')
  writeFileSync(file, before.replace('meta:\n', 'meta:\n    status: 1\n'), 'utf8')

  await openRequest(page, 'ping')
  const badge = page.getByTestId('req.lifecycle')
  await expect(badge).toBeVisible({ timeout: 10_000 })
  await expect(badge).toContainText(t('editor.statusDraft'))
  await expect(badge).toHaveClass(/draft/)
})
