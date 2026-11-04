import { writeFileSync } from 'node:fs'
import path from 'node:path'
import { expect, test } from '../helpers/app'
import { t } from '../helpers/i18n'
import { openCollection } from '../helpers/ui'

/**
 * 环境切换下拉 + 环境管理弹窗。
 *
 * 覆盖两个曾经坏掉的点：
 * ① 下拉的选项来自 props，一旦在脚本里解构 props 就丢掉响应性 —— 新建 / 改名 / 删除环境后
 *    下拉仍是首次渲染那份列表，新环境根本选不到（看着就是「环境切换失效」）；
 * ② 保存环境（含变量）后弹窗应当自动关闭，结果由 toast 说明，不用再手动关一次。
 */
test('[F1/F3] 环境下拉：新建的环境立刻可选、选中即切换；保存环境变量后弹窗自动关闭', async ({ page, app }) => {
  await app.newCollection('basic')
  await openCollection(page, app)

  const sel = page.getByTestId('env.select')
  const modal = page.locator('.n-modal').filter({ hasText: t('env.title') })

  // 下拉最后一项是「环境设置…」，选中即打开管理弹窗
  await sel.click()
  await page.locator('.n-dropdown-option').filter({ hasText: t('env.manage') }).click()
  await expect(modal).toBeVisible()

  // 新建环境 env-2 → 给它加一条变量 → 保存
  await page.getByTestId('env.add').click()
  await expect(modal.locator('.env').filter({ hasText: 'env-2' })).toBeVisible()
  await modal.locator('button', { hasText: t('env.addVar') }).click()
  await modal.locator('input[placeholder="name"]').fill('userName')
  await modal.locator('input[placeholder="value"]').fill('alice')
  await page.getByTestId('env.save').click()

  // ② 保存后弹窗自动关闭，结果由 toast 说明
  await expect(modal).toHaveCount(0)
  await expect(page.locator('.n-message').last()).toContainText(t('env.saved', { name: 'env-2' }))

  // ① 下拉里要立刻出现刚建的环境（解构 props 的旧实现这里只有 dev）
  await sel.click()
  const opt = page.locator('.n-dropdown-option').filter({ hasText: 'env-2' })
  await expect(opt).toBeVisible()
  await opt.click()

  // 选中即切换，并按集合（根）持久化 —— 界面确实把当前环境交出去了
  await expect
    .poll(() =>
      page.evaluate(() => {
        const k = Object.keys(localStorage).find((x) => x.startsWith('client.env.'))
        return k ? localStorage.getItem(k) : ''
      }),
    )
    .toBe('env-2')
})

/**
 * U1：全局变量（服务端 scope=3）在变量面板里只读展示。
 *
 * 真实来源是同步下发到集合根 `globals.yml`；这里直接写该文件模拟一次下发，
 * 断言面板能看到条目、且敏感值打码（只读镜像不该把明文摊在面板里）。
 */
test('U1 全局变量：变量面板只读展示，敏感值打码', async ({ page, app }) => {
  const dir = await app.newCollection('basic')
  await openCollection(page, app)

  writeFileSync(
    path.join(dir, 'globals.yml'),
    [
      'globals:',
      '  - uid: g1',
      '    name: global_host',
      '    value: https://global.example.com',
      '    enabled: true',
      '    secret: false',
      '  - uid: g2',
      '    name: global_token',
      '    value: s3cret-value',
      '    enabled: true',
      '    secret: true',
      '',
    ].join('\n'),
    'utf8',
  )

  await page.getByTestId('env.select').click()
  await page.locator('.n-dropdown-option').filter({ hasText: t('env.manage') }).click()
  const modal = page.locator('.n-modal').filter({ hasText: t('env.title') })
  await expect(modal).toBeVisible()

  const rows = page.getByTestId('env.globalRow')
  await expect(rows).toHaveCount(2)
  await expect(rows.nth(0)).toContainText('global_host')
  await expect(rows.nth(0)).toContainText('https://global.example.com')
  // 敏感值必须打码
  await expect(rows.nth(1)).toContainText('global_token')
  await expect(rows.nth(1)).toContainText('••••••')
  await expect(rows.nth(1)).not.toContainText('s3cret-value')
})
