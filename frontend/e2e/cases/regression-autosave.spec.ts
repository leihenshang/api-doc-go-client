// 对应 doc §5 回归要点 22：保存模型（默认手动 + 可选自动保存）与写盘冲突检测。
// 依赖 devserver 的 /api/events（SSE）转发文件监听事件 —— 浏览器态与桌面态行为一致。
import { existsSync, readFileSync, unlinkSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { expect, test } from '../helpers/app'
import { readCollectionFile } from '../helpers/fs'
import { t } from '../helpers/i18n'
import { openCollection, openRequest, openSettings, saveSettings, settleDetail } from '../helpers/ui'


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

  test('[E26] 自动保存：慢写盘期间的后续编辑会触发尾次保存', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    await openSettings(page)
    await page.getByTestId('settings.autoSave').click()
    await saveSettings(page)

    let releaseFirst!: () => void
    const firstGate = new Promise<void>((resolve) => { releaseFirst = resolve })
    let markStarted!: () => void
    const firstStarted = new Promise<void>((resolve) => { markStarted = resolve })
    let saves = 0
    await page.route('**/api/App/SaveRequest', async (route) => {
      saves++
      if (saves === 1) {
        markStarted()
        await firstGate
      }
      await route.continue()
    })

    try {
      await editUrl(page, '{{host}}/json/flat')
      await firstStarted
      await editUrl(page, '{{host}}/json/nested')
      // 让后续编辑的防抖定时器在第一次保存仍在途时触发，覆盖 single-flight 提前返回的边界。
      await page.waitForTimeout(1000)
      releaseFirst()

      await expect.poll(() => readCollectionFile(dir, 'api/ping.yml')).toContain('/json/nested')
      await expect(page.getByTestId('tab.item').filter({ hasText: 'ping' }).locator('.dot')).toHaveCount(0)
      expect(saves).toBeGreaterThanOrEqual(2)
    } finally {
      releaseFirst()
      await page.unroute('**/api/App/SaveRequest')
    }
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

    // 两张页签都改脏（直接点页签切换后先等 rb-fade 过渡结束，否则 fill 可能落在退场中的旧输入上）
    await page.getByTestId('req.url').fill('{{host}}/json/abc')
    await page.getByTestId('req.url').blur()
    await page.getByTestId('tab.item').filter({ hasText: 'ping' }).click()
    await settleDetail(page)
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
    await page.getByTestId('req.url').fill('{{host}}/json/abc')
    await page.getByTestId('req.url').blur()
    await page.getByTestId('tab.item').filter({ hasText: 'ping' }).click()
    await settleDetail(page)
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

/**
 * [E26] 文档页签的外部改动：MCP / 其它编辑器改了 .md 之后，
 * 干净页签要自动回填，脏页签要弹冲突条（旧实现 openDocTab 没记 baseHash，两种情况都被静默跳过）。
 */
function rewriteDocBody(file: string, body: string): void {
  const s = readFileSync(file, 'utf8')
  const close = s.indexOf('---\n', 4) // 跳过开头的 ---\n，找到 frontmatter 的结束行
  writeFileSync(file, s.slice(0, close + 4) + body, 'utf8')
}

test('[E26] 文档页签：外部改动回填干净页签；脏页签标冲突并可用「重新加载」', async ({ page, app }) => {
  const dir = await app.newCollection('basic')
  await openCollection(page, app)
  const created = await app.ipc<{ uid: string; path: string }>('CreateDoc', '说明', 'api')
  await app.ipc('SaveDoc', { uid: created.uid, name: '说明', path: created.path, content: '旧正文' })
  await openCollection(page, app) // 重进页面让树看到新文档

  await page.getByTestId('tree.row').filter({ hasText: '说明' }).first().click()
  const editor = page.locator('.md-editor').first()
  await expect(editor).toContainText('旧正文')
  const file = path.join(dir, created.path)

  // 上面用 IPC SaveDoc 造的数据：文档写盘用的是「内容无关」的 1.5s 自写忽略窗口，
  // 立刻从外部改会被吞掉（真实 MCP 走的是另一个 App 实例、不存在这个窗口）。
  // 等窗口过去再模拟外部写入。
  await page.waitForTimeout(1800)

  // ① 干净页签：外部写入新正文 → 自动回填（不需要用户手动重载）
  rewriteDocBody(file, 'MCP 更新后的正文')
  await expect(editor).toContainText('MCP 更新后的正文', { timeout: 10_000 })
  await expect(editor).not.toContainText('旧正文')

  // ② 脏页签：本地有未保存编辑时被外部改动 → 冲突条出现（提示用户选「重新加载 / 另存为副本」）
  await page.locator('.md-editor textarea, .md-editor .cm-content').first().click()
  await page.keyboard.type('本地未保存的编辑')
  rewriteDocBody(file, 'MCP 第二次更新')
  const bar = page.getByTestId('req.conflict')
  await expect(bar).toBeVisible({ timeout: 10_000 })

  // ③ 「重新加载」对文档同样有效：正文换成磁盘版本、冲突条消失、脏点清掉
  await page.getByTestId('conflict.reload').click()
  await expect(editor).toContainText('MCP 第二次更新')
  await expect(bar).toHaveCount(0)
  await expect(page.getByTestId('tab.item').filter({ hasText: '说明' }).locator('.dot')).toHaveCount(0)
})

/**
 * [E26] 文档被外部删除（MCP delete_doc / 其它编辑器 / 回收站）：
 * 页签保留内容并给出提示条，「仍然保存」可用当前内容重建文件。
 */
test('[E26] 文档被外部删除：提示条提供「仍然保存」并重建文件', async ({ page, app }) => {
  const dir = await app.newCollection('basic')
  await openCollection(page, app)
  const created = await app.ipc<{ uid: string; path: string }>('CreateDoc', '说明', 'api')
  await app.ipc('SaveDoc', { uid: created.uid, name: '说明', path: created.path, content: '待重建内容' })
  await openCollection(page, app)

  await page.getByTestId('tree.row').filter({ hasText: '说明' }).first().click()
  await expect(page.locator('.md-editor').first()).toContainText('待重建内容')
  // 等过 IPC SaveDoc 的自写忽略窗口，再模拟外部删除
  await page.waitForTimeout(1800)
  unlinkSync(path.join(dir, created.path))

  const bar = page.getByTestId('req.conflict')
  await expect(bar).toBeVisible({ timeout: 10_000 })
  await expect(bar).toContainText(t('conflict.docMissingBar'))
  // 文件没了：不给「重新加载」，但保留「仍然保存」
  await expect(page.getByTestId('conflict.reload')).toHaveCount(0)
  await expect(page.getByTestId('conflict.overwrite')).toBeVisible()

  await page.getByTestId('conflict.overwrite').click()
  await page.locator('.n-dialog').last()
    .getByRole('button', { name: t('conflict.overwrite'), exact: true }).click()

  await expect.poll(() => existsSync(path.join(dir, created.path))).toBe(true)
  expect(readFileSync(path.join(dir, created.path), 'utf8')).toContain('待重建内容')
  await expect(bar).toHaveCount(0)
  // 重建后的文档回到集合树
  await expect(page.getByTestId('tree.row').filter({ hasText: '说明' })).toHaveCount(1)
})

/**
 * [E26] 冲突时「仍然保存」：确认后当前编辑覆盖磁盘上的外部版本（不再只能重新加载/另存副本）。
 */
test('[E26] 文档冲突时「仍然保存」：当前编辑覆盖外部版本', async ({ page, app }) => {
  const dir = await app.newCollection('basic')
  await openCollection(page, app)
  const created = await app.ipc<{ uid: string; path: string }>('CreateDoc', '说明', 'api')
  await app.ipc('SaveDoc', { uid: created.uid, name: '说明', path: created.path, content: '旧正文' })
  await openCollection(page, app)

  await page.getByTestId('tree.row').filter({ hasText: '说明' }).first().click()
  const editor = page.locator('.md-editor textarea, .md-editor .cm-content').first()
  await expect(page.locator('.md-editor').first()).toContainText('旧正文')
  await page.waitForTimeout(1800)
  await editor.click()
  await page.keyboard.type('本地未保存')

  rewriteDocBody(path.join(dir, created.path), '外部版本')
  const bar = page.getByTestId('req.conflict')
  await expect(bar).toBeVisible({ timeout: 10_000 })

  await page.getByTestId('conflict.overwrite').click()
  await page.locator('.n-dialog').last()
    .getByRole('button', { name: t('conflict.overwrite'), exact: true }).click()

  await expect.poll(() => readFileSync(path.join(dir, created.path), 'utf8')).toContain('本地未保存')
  await expect(bar).toHaveCount(0)
  await expect(page.getByTestId('tab.item').filter({ hasText: '说明' }).locator('.dot')).toHaveCount(0)
})
