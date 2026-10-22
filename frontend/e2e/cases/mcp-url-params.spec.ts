// MCP/文件导入的请求可能把 GET 参数写在 params 里、url 不带 query；
// 请求页首次打开时应按参数表补全 URL 展示，且不能因此标成未保存。
import { expect, test } from '../helpers/app'
import { patchRequest } from '../helpers/api'
import { openCollection, openRequest, reopenApp } from '../helpers/ui'

const PING_UID = '33333333-3333-4333-8333-333333333333'

test('[MCP] GET 请求 params 在首次打开时补全 URL query，且不标脏', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await patchRequest(app, PING_UID, {
        url: '{{host}}/json/flat',
        params: [
            { name: 'q', value: 'hello world', enabled: true },
            { name: 'skip', value: '1', enabled: false },
        ],
    })

    await page.reload()
    await reopenApp(page)
    await openRequest(page, 'ping')

    await expect(page.getByTestId('req.url')).toHaveValue('{{host}}/json/flat?q=hello%20world')
    await expect(page.getByTestId('tab.item').filter({ hasText: 'ping' }).locator('.dot')).toHaveCount(0)
})
