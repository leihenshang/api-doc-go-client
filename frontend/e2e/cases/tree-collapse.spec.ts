import { expect, test } from '../helpers/app'
import { openCollection, reopenApp, treeRow } from '../helpers/ui'

/**
 * item 28：集合树每次启动/重开都全部收起，不记忆展开状态。
 * 会话内可展开；reload 后回到全部闭合。
 */
test('[T28] 集合树每次启动全部收起（不记忆展开）', async ({ page, app }) => {
    await app.newCollection('basic')
    await app.addCollection('bruno-sample')
    await openCollection(page, app, { expandRoots: false })

    const roots = page.getByTestId('tree.root')
    await expect(roots).toHaveCount(2)
    const basic = roots.filter({ hasText: 'e2e-basic' })
    const bruno = roots.filter({ hasText: 'e2e-bruno' })

    // 首次打开：所有根都收起，树里的请求行不可见
    await expect(basic).toHaveAttribute('data-expanded', 'false')
    await expect(bruno).toHaveAttribute('data-expanded', 'false')
    await expect(treeRow(page, 'ping')).toHaveCount(0)
    await expect(treeRow(page, '用户-列表')).toHaveCount(0)

    // 会话内只展开 basic；bruno 仍收起
    await basic.getByTestId('tree.root.caret').click()
    await expect(basic).toHaveAttribute('data-expanded', 'true')
    await expect(bruno).toHaveAttribute('data-expanded', 'false')
    await expect(treeRow(page, 'ping')).toHaveCount(1)

    // 重开：不记忆展开态，全部回到收起
    await page.reload()
    await reopenApp(page, { expandRoots: false })
    const basic2 = page.getByTestId('tree.root').filter({ hasText: 'e2e-basic' })
    const bruno2 = page.getByTestId('tree.root').filter({ hasText: 'e2e-bruno' })
    await expect(basic2).toHaveAttribute('data-expanded', 'false')
    await expect(bruno2).toHaveAttribute('data-expanded', 'false')
    await expect(treeRow(page, 'ping')).toHaveCount(0)
    await expect(treeRow(page, '用户-列表')).toHaveCount(0)

    // 再展开另一个根后重开：仍然是全部收起
    await bruno2.getByTestId('tree.root.caret').click()
    await expect(bruno2).toHaveAttribute('data-expanded', 'true')
    await page.reload()
    await reopenApp(page, { expandRoots: false })
    await expect(page.getByTestId('tree.root').filter({ hasText: 'e2e-basic' })).toHaveAttribute('data-expanded', 'false')
    await expect(page.getByTestId('tree.root').filter({ hasText: 'e2e-bruno' })).toHaveAttribute('data-expanded', 'false')
    await expect(treeRow(page, 'ping')).toHaveCount(0)
    await expect(treeRow(page, '用户-列表')).toHaveCount(0)
})
