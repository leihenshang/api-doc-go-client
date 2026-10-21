// 响应字段：与请求体同构——「响应数据」子页签可编辑，合法 JSON 时**实时**自动生成只读字段表格
// （字段表格子页签不加/删字段），只有「含义」可编辑并写回 respDoc（随同步/MCP 往返）。
import { expect, test } from '../helpers/app'
import { readCollectionFile } from '../helpers/fs'
import { openCollection, openReqTab, openRequest } from '../helpers/ui'

const FILE = 'api/ping.yml'
const dataInput = (page: import('@playwright/test').Page) =>
    page.getByTestId('resp.dataInput').locator('textarea')
const tab = (page: import('@playwright/test').Page, view: string) =>
    page.locator(`[data-testid="resp.segTab"][data-view="${view}"]`)
const row = (page: import('@playwright/test').Page, path: string) =>
    page.locator(`[data-testid="resp.fields.row"][data-path="${path}"]`)

test.describe('响应字段：响应数据可编辑、自动生成表格', () => {
    test('响应数据子页签可编辑并自动生成字段；含义可编辑并落盘', async ({ page, app }) => {
        const dir = await app.newCollection('basic')
        await openCollection(page, app)
        await openRequest(page, 'ping')
        await openReqTab(page, 'resp')

        // 响应数据在独立子页签里可编辑（字段表格子页签无输入框、无「添加字段」）
        await expect(page.getByTestId('resp.fields.add')).toHaveCount(0)
        await tab(page, 'body').click()
        await dataInput(page).fill('{"code": 0, "data": {"id": 7}}')

        // 切回「字段表格」：已由响应数据自动生成
        await tab(page, 'fields').click()
        await expect(row(page, 'data.id')).toBeVisible()
        await expect(row(page, 'code')).toBeVisible()

        // 只有含义可写：给 data.id 填说明 → 落盘进 respdoc
        await row(page, 'data.id').getByTestId('resp.fields.meaning').locator('input').fill('主键')
        await expect
            .poll(() => readCollectionFile(dir, FILE) ?? '', { timeout: 5000 })
            .toContain('主键')
        await expect(readCollectionFile(dir, FILE)).toContain('data.id')
    })

    test('值列只读（来自数据）；改成非法 JSON 时保留既有表格', async ({ page, app }) => {
        const dir = await app.newCollection('basic')
        await openCollection(page, app)
        await openRequest(page, 'ping')
        await openReqTab(page, 'resp')

        await tab(page, 'body').click()
        await dataInput(page).fill('{"total": 42}')
        await tab(page, 'fields').click()
        await expect(row(page, 'total')).toBeVisible()
        // 值列只读展示解析出的取值
        await expect(row(page, 'total').getByTestId('resp.fields.value')).toHaveText('42')

        // 把响应数据改成非法 JSON（非空但解析失败）：不清空既有表格
        await tab(page, 'body').click()
        await dataInput(page).fill('{oops')
        await tab(page, 'fields').click()
        await expect(row(page, 'total')).toBeVisible()
        await expect.poll(() => readCollectionFile(dir, FILE) ?? '', { timeout: 5000 }).toContain('fieldName: total')
    })

    test('清空响应数据：表格清空且不会被防抖写回旧值（H1）', async ({ page, app }) => {
        const dir = await app.newCollection('basic')
        await openCollection(page, app)
        await openRequest(page, 'ping')
        await openReqTab(page, 'resp')

        await tab(page, 'body').click()
        await dataInput(page).fill('{"keep": 1}')
        await tab(page, 'fields').click()
        await expect(row(page, 'keep')).toBeVisible()

        // 清空响应数据：等防抖落盘 + 任何自写回流后，不得把旧 body 又填回来
        await tab(page, 'body').click()
        await dataInput(page).fill('')
        await page.waitForTimeout(900)
        await expect(dataInput(page)).toHaveValue('')
        await tab(page, 'fields').click()
        await expect(page.getByTestId('resp.fields.row')).toHaveCount(0)
        // 落盘也不应再有该字段；且 type 一并清空 → 整段 respdoc 被移除（不留空壳）
        const file = () => readCollectionFile(dir, FILE) ?? ''
        await expect.poll(file, { timeout: 5000 }).not.toContain('keep')
        await expect.poll(file, { timeout: 5000 }).not.toContain('respdoc')
    })
})
