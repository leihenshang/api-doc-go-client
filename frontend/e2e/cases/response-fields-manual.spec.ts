// 响应字段：与请求体表格同构 —— 表格只在点「生成数据表格」/「从当前响应生成」时派生
// （5 列全可编辑：字段名 / 类型 / 必填 / 示例值 / 说明 + 增行 / 删行），编辑「响应数据」不再自动改表格。
// 生成默认只动表格；「从当前响应生成」时响应数据为空会一并填入，已有数据选「覆盖」才替换。
import type { Locator, Page } from '@playwright/test'
import { expect, test } from '../helpers/app'
import { readCollectionFile } from '../helpers/fs'
import { t } from '../helpers/i18n'
import { openCollection, openReqTab, openRequest, reopenApp, saveNow, send } from '../helpers/ui'

const FILE = 'api/ping.yml'

const dataInput = (page: Page) => page.getByTestId('resp.dataInput').locator('textarea')
const tab = (page: Page, view: string) => page.locator(`[data-testid="resp.segTab"][data-view="${view}"]`)
const row = (page: Page, path: string) => page.locator(`[data-testid="resp.fields.row"][data-path="${path}"]`)
const inputOf = (page: Page, path: string, testid: string) =>
    row(page, path).getByTestId(testid).locator('input')
const requiredOf = (page: Page, path: string) => row(page, path).getByTestId('resp.fields.required')

/** 断言某个行区容器可纵向滚动（scrollHeight > clientHeight，且 scrollTop 能改变）。 */
async function expectScrollable(locator: Locator): Promise<void> {
    await expect.poll(() => locator.evaluate((el) => el.scrollHeight > el.clientHeight)).toBe(true)
    const before = await locator.evaluate((el) => el.scrollTop)
    await locator.evaluate((el) => {
        el.scrollTop = 120
    })
    await expect.poll(() => locator.evaluate((el) => el.scrollTop)).toBeGreaterThan(before)
}

test.describe('响应字段：手工生成 + 5 列可编辑 + 必填贯通', () => {
    test('响应数据不再自动出表；点「生成数据表格」出 5 列、必填默认「是」并可落盘', async ({ page, app }) => {
        const dir = await app.newCollection('basic')
        await openCollection(page, app)
        await openRequest(page, 'ping')
        await openReqTab(page, 'resp')

        // 空表：只有点生成才有内容（不再实时派生）
        await tab(page, 'fields').click()
        await expect(page.getByTestId('resp.fields.empty')).toBeVisible()
        await tab(page, 'body').click()
        await dataInput(page).fill('{"code": 0, "data": {"id": 7}}')
        await page.waitForTimeout(900) // 旧实现在此窗口内有防抖写盘：数据变化不得带出表格
        await tab(page, 'fields').click()
        await expect(page.getByTestId('resp.fields.row')).toHaveCount(0)

        // 手工生成：按结构两行，必填默认「是」，示例值取自数据
        await page.getByTestId('resp.generate').click()
        await expect(page.getByTestId('resp.fields.row')).toHaveCount(2)
        await expect(row(page, 'data.id')).toBeVisible()
        await expect(requiredOf(page, 'data.id')).toHaveAttribute('aria-checked', 'true')
        await expect(requiredOf(page, 'code')).toHaveAttribute('aria-checked', 'true')
        await expect(inputOf(page, 'data.id', 'resp.fields.example')).toHaveValue('7')
        await expect(inputOf(page, 'data.id', 'resp.fields.name')).toHaveValue('data.id')

        // 单元格可编辑：含义 → respDoc.params[].description
        await inputOf(page, 'data.id', 'resp.fields.meaning').fill('主键')
        // 取消必填 → required 如实落盘（旧实现恒写 false，必填是静默丢数据的点）
        await requiredOf(page, 'data.id').click()
        await expect(requiredOf(page, 'data.id')).toHaveAttribute('aria-checked', 'false')

        const file = () => readCollectionFile(dir, FILE) ?? ''
        // 手动保存：生成/编辑都只改内存，等过旧的防抖窗口后磁盘仍无 respdoc
        await page.waitForTimeout(900)
        expect(file()).not.toContain('fieldName: data.id')
        // Ctrl+S 才落盘（整条请求含 respDoc）
        await saveNow(page)
        await expect.poll(file, { timeout: 5000 }).toContain('fieldName: data.id')
        await expect.poll(file, { timeout: 5000 }).toContain('主键')
        await expect.poll(file, { timeout: 5000 }).toContain('required: false')
        // 另一行没被连坐：仍是「是」
        await expect(requiredOf(page, 'code')).toHaveAttribute('aria-checked', 'true')
    })

    test('已有表格时生成会弹窗：新增只多不删、同名跳过；覆盖后同名行只保留说明', async ({ page, app }) => {
        await app.newCollection('basic')
        await openCollection(page, app)
        await openRequest(page, 'ping')
        await openReqTab(page, 'resp')

        await tab(page, 'body').click()
        await dataInput(page).fill('{"total": 42, "page": 1}')
        await page.getByTestId('resp.generate').click()
        await expect(page.getByTestId('resp.fields.row')).toHaveCount(2)

        // 给 total 填说明并取消必填，作为「新增/覆盖」的观察点
        await inputOf(page, 'total', 'resp.fields.meaning').fill('总数')
        await requiredOf(page, 'total').click()
        await expect(requiredOf(page, 'total')).toHaveAttribute('aria-checked', 'false')
        await page.waitForTimeout(800)

        // 换一份数据：total 仍在、page 消失、list 新增
        await tab(page, 'body').click()
        await dataInput(page).fill('{"total": 43, "list": []}')
        await page.getByTestId('resp.generate').click()
        await expect(page.getByTestId('resp.genDialog')).toBeVisible()

        // 新增：原表全部行与顺序不动（page 没被删、total 的说明/必填/示例值都没动），只追加 list
        await page.getByTestId('resp.genAppend').click()
        await expect(page.getByTestId('resp.genDialog')).toHaveCount(0)
        await expect(page.getByTestId('resp.fields.row')).toHaveCount(3)
        await expect(row(page, 'page')).toBeVisible()
        await expect(row(page, 'list')).toBeVisible()
        await expect(inputOf(page, 'total', 'resp.fields.meaning')).toHaveValue('总数')
        await expect(requiredOf(page, 'total')).toHaveAttribute('aria-checked', 'false')
        await expect(inputOf(page, 'total', 'resp.fields.example')).toHaveValue('42')

        // 覆盖：行数/顺序 = 本次派生；同名字段只继承说明，必填回「是」、示例值取新值
        await page.getByTestId('resp.generate').click()
        await page.getByTestId('resp.genOverwrite').click()
        await expect(page.getByTestId('resp.fields.row')).toHaveCount(2)
        await expect(row(page, 'page')).toHaveCount(0) // 覆盖：本次派生里没有的字段消失
        await expect(inputOf(page, 'total', 'resp.fields.meaning')).toHaveValue('总数')
        await expect(requiredOf(page, 'total')).toHaveAttribute('aria-checked', 'true')
        await expect(inputOf(page, 'total', 'resp.fields.example')).toHaveValue('43')

        // 取消：表格不变（字段表只在「字段表格」子页签渲染，先切回去再数行）
        await tab(page, 'body').click()
        await dataInput(page).fill('{"only": true}')
        await page.getByTestId('resp.generate').click()
        await page.getByTestId('resp.genCancel').click()
        await tab(page, 'fields').click()
        await expect(page.getByTestId('resp.fields.row')).toHaveCount(2)
        await expect(row(page, 'total')).toBeVisible()
    })

    test('「从当前响应生成」：无响应时禁用；有响应可生成（含快照）且自动填入空的「响应数据」', async ({ page, app }) => {
        const dir = await app.newCollection('basic')
        await openCollection(page, app)
        await openRequest(page, 'ping')
        await openReqTab(page, 'resp')

        // 还没发送：按钮禁用，提示说明「先发送请求」
        await expect(page.getByTestId('resp.generateLive')).toBeDisabled()
        await expect(page.getByTestId('resp.generateLive')).toHaveAttribute('title', t('resp.genNoLive'))

        // 只改 UI 里的 URL（不再用 IPC 直接改盘：手动保存模型下，外部写盘会让页签基准 hash
        // 失效 → Ctrl+S 被当成冲突拒写）。send 读的是内存模型，UI 填好即可。
        const url = '{{host}}/json/flat'
        await page.getByTestId('req.url').fill(url)
        await page.getByTestId('req.url').blur()
        await send(page)

        await openReqTab(page, 'resp')
        await expect(page.getByTestId('resp.generateLive')).toBeEnabled()
        await page.getByTestId('resp.generateLive').click()
        await expect(page.getByTestId('resp.fields.row')).toHaveCount(2) // flat / value
        await expect(row(page, 'value')).toBeVisible()
        await expect(inputOf(page, 'value', 'resp.fields.example')).toHaveValue('1')
        await expect(requiredOf(page, 'value')).toHaveAttribute('aria-checked', 'true')

        // 本次响应来源会顺带记下 status/headers（响应头子页签与状态徽标的数据源）；
        // 但仍是内存编辑：Ctrl+S 之前文件不变
        const file = () => readCollectionFile(dir, FILE) ?? ''
        await page.waitForTimeout(900)
        expect(file()).not.toContain('status: 200')
        await saveNow(page)
        await expect.poll(file, { timeout: 5000 }).toContain('status: 200')

        // 新行为：响应数据为空时也一并填入当前响应体（不再生成了表格、样例还空着）
        await tab(page, 'body').click()
        await expect(dataInput(page)).toHaveValue(/"flat":\s*true/)

        // 手动来源（响应数据）生成只改表格：当前响应已记下的 status/headers 不被清
        await dataInput(page).fill('{"other": 1}')
        await page.getByTestId('resp.generate').click()
        await expect(page.getByTestId('resp.genDialog')).toBeVisible()
        await page.getByTestId('resp.genOverwrite').click()
        await expect(page.getByTestId('resp.fields.row')).toHaveCount(1)
        await expect(row(page, 'other')).toBeVisible()
        await saveNow(page)
        await expect.poll(file, { timeout: 5000 }).toContain('status: 200')
        await expect.poll(file, { timeout: 5000 }).toContain('Content-Type')
    })

    test('「从当前响应生成」选「覆盖」：响应体一并写入「响应数据」', async ({ page, app }) => {
        const dir = await app.newCollection('basic')
        await openCollection(page, app)
        await openRequest(page, 'ping')
        await openReqTab(page, 'resp')

        // 先用手动来源造一张非空表（生成时才弹「新增 / 覆盖」）
        await tab(page, 'body').click()
        await dataInput(page).fill('{"old": 1}')
        await page.getByTestId('resp.generate').click()
        await expect(page.getByTestId('resp.fields.row')).toHaveCount(1)

        // 发送真实请求，再从本次响应生成 → 覆盖
        const url = '{{host}}/json/flat'
        await page.getByTestId('req.url').fill(url)
        await page.getByTestId('req.url').blur()
        await send(page)
        await page.getByTestId('resp.generateLive').click()
        await expect(page.getByTestId('resp.genDialog')).toBeVisible()
        await expect(page.getByTestId('resp.genHintLive')).toBeVisible()
        await page.getByTestId('resp.genOverwrite').click()

        // 字段表按本次响应重建：旧字段消失，本次响应的字段出现
        await expect(row(page, 'old')).toHaveCount(0)
        await expect(row(page, 'flat')).toBeVisible()
        await expect(row(page, 'value')).toBeVisible()

        // 关键修复：覆盖时「响应数据」也替换为本次响应体（不再是旧样例）
        await tab(page, 'body').click()
        await expect(dataInput(page)).toHaveValue(/"flat":\s*true/)

        // 仍是内存编辑：Ctrl+S 才把 sample 落进 respdoc
        await saveNow(page)
        const file = () => readCollectionFile(dir, FILE) ?? ''
        await expect.poll(file, { timeout: 5000 }).toContain('sample:')
    })

    test('「从当前响应生成」按右侧当前展示的示例派生，而不是最后一次实时响应', async ({ page, app }) => {
        await app.newCollection('basic')
        await openCollection(page, app)
        await openRequest(page, 'ping')
        await openReqTab(page, 'resp')

        // 1) /json/flat 发送并存成示例（flat / value），保存后面板自动切到该示例快照
        await page.getByTestId('req.url').fill('{{host}}/json/flat')
        await page.getByTestId('req.url').blur()
        await send(page)
        await page.getByTestId('resp.save').click()
        await page.getByTestId('resp.saveExample').click()
        const modal = page.locator('.n-modal').filter({ has: page.getByPlaceholder(t('resp.exampleNamePlaceholder')) })
        await modal.locator('input').first().fill('快照示例')
        await modal.getByRole('button', { name: t('common.save'), exact: true }).click()
        await expect(page.getByTestId('resp.exnote')).toBeVisible()

        // 2) 再发一次 /json/withArray：实时响应变成 total/items，面板回到实时响应
        await page.getByTestId('req.url').fill('{{host}}/json/withArray')
        await page.getByTestId('req.url').blur()
        await send(page)
        await expect(page.getByTestId('resp.exnote')).toHaveCount(0)

        // 3) 在下拉里切回示例：右侧展示的是 flat/value 快照
        await page.getByTestId('resp.examples').click()
        await page.locator('.n-base-select-option').filter({ hasText: '快照示例' }).click()
        await expect(page.getByTestId('resp.exnote')).toBeVisible()

        // 4) 生成读的是示例（flat/value），不是实时响应的 total/items
        await openReqTab(page, 'resp')
        await page.getByTestId('resp.generateLive').click()
        await expect(page.getByTestId('resp.fields.row')).toHaveCount(2)
        await expect(row(page, 'flat')).toBeVisible()
        await expect(row(page, 'total')).toHaveCount(0)

        // 5) 响应数据为空 → 生成时一并填入当前展示的响应体
        await tab(page, 'body').click()
        await expect(dataInput(page)).toHaveValue(/"flat":\s*true/)
    })

    test('响应类型：未设置时不冒充 JSON；选择后落盘并在刷新后保留', async ({ page, app }) => {
        const dir = await app.newCollection('basic')
        await openCollection(page, app)
        await openRequest(page, 'ping')
        await openReqTab(page, 'resp')

        // 未设置：不出现类型徽标（旧实现默认 json，且会把原始 MIME 直接显示成 application/json）
        await expect(page.getByTestId('resp.contentTypeBadge')).toHaveCount(0)

        // 选 XML → 徽标即时显示 XML
        await page.getByTestId('resp.contentType').click()
        await page.locator('.n-base-select-option').filter({ hasText: t('editor.ctXml') }).click()
        await expect(page.getByTestId('resp.contentTypeBadge')).toHaveText(t('editor.ctXml'))

        // 手动保存：落盘的是五个取值之一（不是原始 MIME）
        await saveNow(page)
        await expect.poll(() => readCollectionFile(dir, FILE) ?? '', { timeout: 5000 }).toContain('contentType: xml')

        // 刷新重开（真读盘）：响应类型仍是 XML，不回退成 json
        await page.reload()
        await reopenApp(page)
        await openRequest(page, 'ping')
        await openReqTab(page, 'resp')
        await expect(page.getByTestId('resp.contentTypeBadge')).toHaveText(t('editor.ctXml'))
    })

    test('字段表格超出可视高度时行区可纵向滚动（请求体 + 响应字段）', async ({ page, app }) => {
        await app.newCollection('basic')
        await openCollection(page, app)
        await openRequest(page, 'ping')

        // 40 个字段足以超过请求编辑器的可视高度
        const many = JSON.stringify(Object.fromEntries(Array.from({ length: 40 }, (_, i) => [`f${i}`, i])))

        // 请求体：切 JSON → 填原文 → 生成表格
        await openReqTab(page, 'body')
        await page.getByTestId('body.type').click()
        await page.locator('.n-base-select-option').filter({ hasText: t('editor.bodyJson') }).click()
        await page.getByTestId('req.bodyRaw').fill(many)
        await page.getByTestId('body.generate').click()
        await expect(page.getByTestId('body.fieldsRow')).toHaveCount(40)
        await expectScrollable(page.locator('.bfields .rows'))

        // 响应字段：同一份 JSON 填「响应数据」→ 生成表格
        await openReqTab(page, 'resp')
        await tab(page, 'body').click()
        await dataInput(page).fill(many)
        await page.getByTestId('resp.generate').click()
        await expect(page.getByTestId('resp.fields.row')).toHaveCount(40)
        await expectScrollable(page.locator('.fields .rows'))
    })

    test('清空响应数据不影响已有表格（生成只动表格）', async ({ page, app }) => {
        const dir = await app.newCollection('basic')
        await openCollection(page, app)
        await openRequest(page, 'ping')
        await openReqTab(page, 'resp')

        await tab(page, 'body').click()
        await dataInput(page).fill('{"keep": 1}')
        await page.getByTestId('resp.generate').click()
        await expect(page.getByTestId('resp.fields.row')).toHaveCount(1)

        await tab(page, 'body').click()
        await dataInput(page).fill('')
        await page.waitForTimeout(900)
        await expect(dataInput(page)).toHaveValue('')
        await tab(page, 'fields').click()
        await expect(page.getByTestId('resp.fields.row')).toHaveCount(1)
        // 表格仍在内存里；保存后 respdoc.params 照旧落盘（清空 sample 不删字段）
        await saveNow(page)
        await expect.poll(() => readCollectionFile(dir, FILE) ?? '', { timeout: 5000 }).toContain('fieldName: keep')
    })

    test('表格可增行 / 删行，新增行必填默认「是」', async ({ page, app }) => {
        const dir = await app.newCollection('basic')
        await openCollection(page, app)
        await openRequest(page, 'ping')
        await openReqTab(page, 'resp')

        await tab(page, 'body').click()
        await dataInput(page).fill('{"a": 1}')
        await page.getByTestId('resp.generate').click()
        await expect(page.getByTestId('resp.fields.row')).toHaveCount(1)

        // 新增行：必填默认「是」，填完字段名只改内存（Ctrl+S 才落盘）
        await page.getByTestId('resp.fields.add').click()
        await expect(page.getByTestId('resp.fields.row')).toHaveCount(2)
        await expect(page.getByTestId('resp.fields.row').last().getByTestId('resp.fields.required'))
            .toHaveAttribute('aria-checked', 'true')
        await page.getByTestId('resp.fields.row').last().getByTestId('resp.fields.name').locator('input')
            .fill('manual.field')
        await page.waitForTimeout(900)
        expect(readCollectionFile(dir, FILE) ?? '').not.toContain('fieldName: manual.field')
        await saveNow(page)
        await expect.poll(() => readCollectionFile(dir, FILE) ?? '', { timeout: 5000 })
            .toContain('fieldName: manual.field')

        // 删除行
        await row(page, 'a').getByTestId('resp.fields.remove').click()
        await expect(page.getByTestId('resp.fields.row')).toHaveCount(1)
        await expect(row(page, 'a')).toHaveCount(0)
    })

    test('「从当前响应生成」落盘时过滤易变/敏感响应头，保留自定义头', async ({ page, app }) => {
        const dir = await app.newCollection('basic')
        await openCollection(page, app)
        await openRequest(page, 'ping')
        await openReqTab(page, 'resp')

        // 夹具同时返回：Date（net/http 自动）/ Keep-Alive / Server / Set-Cookie + 自定义 X-Live-Proof
        const url = '{{host}}/json/live-headers'
        await page.getByTestId('req.url').fill(url)
        await page.getByTestId('req.url').blur()
        await send(page)

        await page.getByTestId('resp.generateLive').click()
        await expect(row(page, 'ok')).toBeVisible()
        await saveNow(page)

        const file = () => readCollectionFile(dir, FILE) ?? ''
        await expect.poll(file, { timeout: 5000 }).toContain('name: X-Live-Proof')
        await expect.poll(file, { timeout: 5000 }).toContain('name: Content-Type')
        const text = file()
        for (const volatile of ['name: Date', 'name: Keep-Alive', 'name: Server', 'name: Set-Cookie', 'name: Content-Length']) {
            expect(text, `易变/敏感响应头 ${volatile} 不应落盘`).not.toContain(volatile)
        }
    })

    test('响应数据支持 JSON / XML 一键格式化', async ({ page, app }) => {
        await app.newCollection('basic')
        await openCollection(page, app)
        await openRequest(page, 'ping')
        await openReqTab(page, 'resp')
        await tab(page, 'body').click()

        await dataInput(page).fill('{"a":1,"b":[2]}')
        await page.getByTestId('resp.formatData').click()
        await expect(dataInput(page)).toHaveValue('{\n  "a": 1,\n  "b": [\n    2\n  ]\n}')

        // 格式化按钮常显：空内容 / 非法内容点击后给内联提示；重新输入即清除
        await dataInput(page).fill('')
        await expect(page.getByTestId('resp.formatData')).toBeVisible()
        await page.getByTestId('resp.formatData').click()
        await expect(page.getByTestId('resp.formatBroken')).toBeVisible()
        await dataInput(page).fill('not-json')
        await page.getByTestId('resp.formatData').click()
        await expect(page.getByTestId('resp.formatBroken')).toBeVisible()
        await dataInput(page).fill('{"ok":true}')
        await expect(page.getByTestId('resp.formatBroken')).toHaveCount(0)

        await dataInput(page).fill('<root><a>1</a></root>')
        await page.getByTestId('resp.formatData').click()
        await expect(dataInput(page)).toHaveValue('<root>\n  <a>1</a>\n</root>')
    })

    test('响应数据编辑器：JSON/XML 着色、Ctrl+Z、查找、复制', async ({ page, app }) => {
        await app.newCollection('basic')
        await openCollection(page, app)
        await openRequest(page, 'ping')
        await openReqTab(page, 'resp')
        await tab(page, 'body').click()

        const ta = dataInput(page)
        await ta.fill('{"name":"alice","age":3}')
        // 与请求体原文同一套高亮层：键名拿到 .k
        await expect(page.locator('.resp-data-editor .jb .hl span.k').first()).toContainText('"name"')

        // 页内查找
        await page.getByTestId('req.find').click()
        await page.getByTestId('find.input').fill('alice')
        await expect(page.getByTestId('find.count')).toHaveText('1/1')
        await page.getByTestId('find.close').click()

        // 原生 textarea 撤销：输入 X 后 Ctrl+Z 回到输入前
        await ta.click()
        await page.keyboard.press('End')
        await ta.pressSequentially('X')
        await expect(ta).toHaveValue('{"name":"alice","age":3}X')
        await page.keyboard.press('Control+z')
        await expect(ta).toHaveValue('{"name":"alice","age":3}')

        // 一键复制
        await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])
        await page.getByTestId('resp.copyData').click()
        const copied = await page.evaluate(() => navigator.clipboard.readText())
        expect(copied).toBe('{"name":"alice","age":3}')

        // XML 也走着色层
        await ta.fill('<root><item id="1">a</item></root>')
        await expect(page.locator('.resp-data-editor .jb .hl span.k').first()).toContainText('root')
    })
})
