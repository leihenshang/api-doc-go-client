import type { Page } from '@playwright/test'
import { expect, test } from '../helpers/app'
import { readCollectionFile } from '../helpers/fs'
import { t } from '../helpers/i18n'
import { openCollection, openRequest } from '../helpers/ui'

/** 请求区：切到 Body 页签并把 body 类型切成 JSON */
async function useJsonBody(page: Page): Promise<void> {
  await page.locator('[data-testid="req.tab"][data-seg="body"]').click()
  await page.locator('.btype').click()
  await page.locator('.n-base-select-option').filter({ hasText: t('editor.bodyJson') }).first().click()
  await expect(page.getByTestId('req.bodyRaw')).toBeVisible()
}

test.describe('请求体 JSON 着色', () => {
  test('[B1] JSON 体按语法着色，且高亮层与输入层严格对齐', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    await useJsonBody(page)

    // 多行 + 超长行（触发软换行）+ 各类字面量 + {{变量}}
    const body = JSON.stringify(
      {
        name: '中文名字测长度',
        url: '{{host}}/very/long/path/' + 'x'.repeat(60),
        count: 42,
        ok: true,
        nothing: null,
      },
      null,
      2,
    )
    await page.getByTestId('req.bodyRaw').fill(body)

    // 各类 token 都拿到独立类名（着色生效）
    const hl = page.locator('.jb .hl')
    await expect(hl.locator('span.k').first()).toContainText('"name"')
    await expect(hl.locator('span.s').first()).toContainText('中文名字测长度')
    await expect(hl.locator('span.n').first()).toContainText('42')
    await expect(hl.locator('span.b').first()).toContainText('true')
    await expect(hl.locator('span.z').first()).toContainText('null')
    await expect(hl.locator('span.v').first()).toContainText('{{host}}') // 变量沿用主色淡底

    // 两层必须严格重叠：高度一致 = 换行一致；文本逐字符一致 = 没混入模板空白
    const align = await page.evaluate(() => {
      const layer = document.querySelector('.jb .hl') as HTMLElement
      const input = document.querySelector('.jb .ta') as HTMLTextAreaElement
      const a = getComputedStyle(layer)
      const b = getComputedStyle(input)
      return {
        sameHeight: layer.scrollHeight === input.scrollHeight,
        sameText: layer.textContent === input.value,
        sameFont: a.fontFamily === b.fontFamily && a.fontSize === b.fontSize && a.lineHeight === b.lineHeight,
        sameWrap: a.whiteSpace === b.whiteSpace && a.wordBreak === b.wordBreak,
        samePad: a.paddingTop === b.paddingTop && a.paddingLeft === b.paddingLeft,
      }
    })
    expect(align).toEqual({
      sameHeight: true,
      sameText: true,
      sameFont: true,
      sameWrap: true,
      samePad: true,
    })
  })

  test('[B2] 半成品 JSON 提示语法有误，含 {{变量}} 时不误报', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    await useJsonBody(page)

    await page.getByTestId('req.bodyRaw').fill('{"a": 1,}')
    await expect(page.getByTestId('body.jsonBroken')).toBeVisible()

    // 模板化请求体（含变量占位）本来就无法直接 parse，不该报错
    await page.getByTestId('req.bodyRaw').fill('{"id": {{id}}}')
    await expect(page.getByTestId('body.jsonBroken')).toHaveCount(0)
  })

  test('[B3] 着色编辑器不影响落盘：Ctrl+S 后文件里有请求体', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    await useJsonBody(page)

    await page.getByTestId('req.bodyRaw').fill('{"probe":"着色"}')
    await page.keyboard.press('Control+s')

    await expect.poll(() => readCollectionFile(dir, 'api/ping.yml') ?? '').toContain('probe')
  })

  test('[B4] 纯文本类型仍用原输入框（不引入不必要的着色）', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    await page.locator('[data-testid="req.tab"][data-seg="body"]').click()

    await page.locator('.btype').click()
    await page.locator('.n-base-select-option').filter({ hasText: t('editor.bodyText') }).first().click()
    await expect(page.getByTestId('req.bodyRaw')).toHaveCount(0)
    await expect(page.locator('.body-pane textarea').first()).toBeVisible()
  })

  test('[B5] JSON 体「表格」视图按字段编辑备注并落盘 descs', async ({ page, app }) => {
    const dir = await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    await useJsonBody(page)
    await page.getByTestId('req.bodyRaw').fill('{"name":"alice","age":3}')

    // 切到表格视图：按字段路径展平，name / age 各一行
    await page.locator('[data-testid="body.viewTab"][data-view="table"]').click()
    await expect(page.getByTestId('body.fieldsRow')).toHaveCount(2)

    // 编辑首个字段备注并保存 → body.descs 落盘（备注列是 naive NInput，测试 id 在外层包装，聚焦内层 input）
    await page.getByTestId('body.fieldDesc').first().locator('input').fill('用户名')
    await page.keyboard.press('Control+s')
    await expect.poll(() => readCollectionFile(dir, 'api/ping.yml') ?? '').toContain('用户名')
  })

  test('[B6] 请求体 JSON / XML 一键格式化（文本体 XML 同样生效）', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')
    await useJsonBody(page)

    // JSON：压缩 → 缩进
    const raw = page.getByTestId('req.bodyRaw')
    await raw.fill('{"a":1,"b":[2,3]}')
    await page.getByTestId('body.format').click()
    await expect(raw).toHaveValue('{\n  "a": 1,\n  "b": [\n    2,\n    3\n  ]\n}')
    // 一键复制（与响应数据同一套按钮）；粘贴板校验
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])
    await page.getByTestId('body.copy').click()
    // Windows 剪贴板会把 LF 规范成 CRLF，比较前先归一分行符
    const copied = (await page.evaluate(() => navigator.clipboard.readText())).replace(/\r\n/g, '\n')
    expect(copied).toBe('{\n  "a": 1,\n  "b": [\n    2,\n    3\n  ]\n}')

    // 文本类型里的 XML：同样给格式化按钮；非法 XML 保持原样
    await page.locator('.btype').click()
    await page.locator('.n-base-select-option').filter({ hasText: t('editor.bodyText') }).first().click()
    const ta = page.locator('.body-pane textarea').first()
    await ta.fill('<root><item id="1">a</item></root>')
    await page.getByTestId('body.format').click()
    await expect(ta).toHaveValue('<root>\n  <item id="1">a</item>\n</root>')

    await ta.fill('<root><item></root>')
    await page.getByTestId('body.format').click()
    await expect(ta).toHaveValue('<root><item></root>')
  })
})
