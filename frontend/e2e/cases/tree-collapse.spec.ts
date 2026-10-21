import { expect, test } from '../helpers/app'
import { openCollection, reopenApp, treeRow } from '../helpers/ui'

/**
 * item 28：集合树每次启动/重开时，根与根下所有目录都默认收起。
 * 会话内可逐级展开；reload 后回到全部闭合。
 */
test('[T28] 集合树每次启动全部收起（根与目录都不记忆）', async ({ page, app }) => {
    await app.newCollection('basic')
    await app.addCollection('bruno-sample')
    await openCollection(page, app, { expandRoots: false })

    const roots = page.getByTestId('tree.root')
    await expect(roots).toHaveCount(2)
    const basic = roots.filter({ hasText: 'e2e-basic' })
    const bruno = roots.filter({ hasText: 'e2e-bruno' })

    // 首次打开：所有根都收起
    await expect(basic).toHaveAttribute('data-expanded', 'false')
    await expect(bruno).toHaveAttribute('data-expanded', 'false')
    await expect(treeRow(page, 'ping')).toHaveCount(0)
    await expect(treeRow(page, '用户-列表')).toHaveCount(0)

    // 会话内展开 basic：根展开，但目录 api 仍收起，请求行不可见
    await basic.getByTestId('tree.root.caret').click()
    await expect(basic).toHaveAttribute('data-expanded', 'true')
    const api = treeRow(page, 'api')
    await expect(api).toHaveAttribute('data-collapsed', 'true')
    await expect(treeRow(page, 'ping')).toHaveCount(0)

    // 手动展开目录 api 后才看到请求行
    await api.getByTestId('tree.row.caret').click()
    await expect(api).toHaveAttribute('data-collapsed', 'false')
    await expect(treeRow(page, 'ping')).toHaveCount(1)

    // 重开：根与目录全部回到收起；重新展开根后目录仍需手动展开
    await page.reload()
    await reopenApp(page, { expandRoots: false })
    const basic2 = page.getByTestId('tree.root').filter({ hasText: 'e2e-basic' })
    const bruno2 = page.getByTestId('tree.root').filter({ hasText: 'e2e-bruno' })
    await expect(basic2).toHaveAttribute('data-expanded', 'false')
    await expect(bruno2).toHaveAttribute('data-expanded', 'false')
    await expect(treeRow(page, 'ping')).toHaveCount(0)

    await basic2.getByTestId('tree.root.caret').click()
    const api2 = treeRow(page, 'api')
    await expect(api2).toHaveAttribute('data-collapsed', 'true')
    await expect(treeRow(page, 'ping')).toHaveCount(0)
    await api2.getByTestId('tree.row.caret').click()
    await expect(api2).toHaveAttribute('data-collapsed', 'false')
    await expect(treeRow(page, 'ping')).toHaveCount(1)
    await expect(bruno).toHaveAttribute('data-expanded', 'false')
})
