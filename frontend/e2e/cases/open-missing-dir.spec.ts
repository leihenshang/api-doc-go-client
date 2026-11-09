// 回归：集合目录被改名 / 移动后重启，不能把旧名字的目录重新建出来。
//
// 旧实现 collection.Open() 对不存在的路径无条件 MkdirAll：
// 启动时前端拿 localStorage 里的旧绝对路径去 Open，于是每次重启都留下一个没有清单的
// 空壳目录（名字还是旧名字）。用户一旦对它点「初始化」，就凭空多出一个空集合。
import { existsSync, renameSync, rmSync } from 'node:fs'
import { expect, test } from '../helpers/app'

test.describe('集合目录改名后重启', () => {
  test('不重建旧目录，并从启动恢复列表里摘掉', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    // 关掉后端内存里的根，模拟「应用已退出」：
    // 否则 EnsureDefaultCollection 会直接复用这个还开着的根，走不到恢复失败的分支。
    const list = await app.ipc<{ root: string }[]>('ListCollections')
    for (const s of list) await app.ipc('CloseCollection', s.root)

    // 改名（等价于用户在文件管理器里 mv）
    const renamed = `${dir}-renamed`
    renameSync(dir, renamed)

    // 下一次启动以为「上次打开的根」还是旧路径
    await page.addInitScript((old) => {
      localStorage.setItem('client.dirs', JSON.stringify([old]))
      localStorage.removeItem('client.activeDir')
    }, dir)

    await page.goto(app.url)
    await expect(page.getByTestId('toolbar.collection')).toBeVisible({ timeout: 20_000 })

    // ① 旧目录没被重建（这是本次修复的核心）
    expect(existsSync(dir)).toBe(false)
    // ② 恢复记录被摘掉，不会每次启动都为一个死路径失败一次
    await expect.poll(() => page.evaluate(() => localStorage.getItem('client.dirs'))).toBeNull()

    rmSync(renamed, { recursive: true, force: true })
  })
})
