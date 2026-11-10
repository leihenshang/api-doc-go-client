// 右键「复制 API」：在同一分组生成副本，名称自动加「复制N」，并真实落盘。
import { readdirSync } from 'node:fs'
import path from 'node:path'
import { expect, test } from '../helpers/app'
import { openCollection, treeRow } from '../helpers/ui'

test.describe('复制 API', () => {
  test('右键复制：同层级生成「原名复制1」，再次复制递增到 2', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)

    // 树按 seq 渲染，原请求在前；复制出副本后 hasText 是子串匹配，所以要取第一个
    const original = treeRow(page, 'ping').first()
    await original.click({ button: 'right' })
    await page.getByTestId('tree.row.duplicate').click()

    // ① 树里出现副本
    await expect(treeRow(page, 'ping复制1')).toBeVisible()
    // ② 落在同一层级（basic 的请求都在 api/ 下），文件名带副本名
    const files = readdirSync(path.join(dir, 'api'))
    expect(files.some((f) => f.includes('ping复制1'))).toBe(true)

    // ③ 再复制一次：序号递增，不覆盖已有副本
    await original.click({ button: 'right' })
    await page.getByTestId('tree.row.duplicate').click()
    await expect(treeRow(page, 'ping复制2')).toBeVisible()
  })
})
